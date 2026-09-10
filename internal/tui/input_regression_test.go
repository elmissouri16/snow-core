package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestBracketedPasteReachesNameFields(t *testing.T) {
	for _, field := range []string{"session", "rename", "fork", "delete", "loading"} {
		t.Run(field, func(t *testing.T) {
			m := prepareScrollableModel(t)
			m.editor.SetValue("composer draft")
			limit := 64
			if field == "session" {
				m.sessions = []session.SessionInfo{{ID: "session-fixture"}}
				m.pickSession, m.sessionRenaming = true, true
				limit = 72
			} else {
				m.branches = []protocol.SessionBranch{{ID: "branch-fixture", Active: true}}
				m.pickTree, m.branchAction = true, field
				if field == "loading" {
					m.treeLoading, m.branchAction = true, "rename"
				}
			}
			for _, msg := range decodeTerminalBytes(t, "\x1b[200~pasted name\x1b[201~") {
				m.Update(msg)
			}
			value := func() string {
				if field == "session" {
					return m.sessionRenameInput
				}
				return m.branchInput
			}
			if field == "delete" || field == "loading" {
				if value() != "" {
					t.Fatal("paste changed a non-editable field")
				}
				return
			}
			if value() != "pasted name" {
				t.Fatalf("field received %q", value())
			}
			m.Update(tea.PasteMsg{Content: "\n/help\x1b[31m" + strings.Repeat("界", 100)})
			if len([]rune(value())) != limit || strings.ContainsAny(value(), "\n\r\x1b") {
				t.Fatalf("unsafe or unbounded field: %q", value())
			}
			if m.pickHelp || m.busy || m.editor.Value() != "composer draft" {
				t.Fatal("paste escaped field ownership")
			}
		})
	}
}

func TestModifiedArrowsDoNotBrowseHistory(t *testing.T) {
	for _, browsing := range []bool{false, true} {
		for _, code := range []rune{tea.KeyUp, tea.KeyDown} {
			for _, mod := range []tea.KeyMod{tea.ModShift, tea.ModAlt, tea.ModCtrl | tea.ModShift} {
				m := prepareScrollableModel(t)
				m.rememberInputHistory("old prompt")
				m.editor.SetValue("current draft")
				m.editor.CursorEnd()
				if browsing {
					m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
				}
				before, index := m.editor.Value(), m.inputHistoryIndex
				m.Update(tea.KeyPressMsg{Code: code, Mod: mod})
				if m.editor.Value() != before || m.inputHistoryIndex != index {
					t.Fatalf("modified arrow recalled history: browsing=%v code=%v mod=%v", browsing, code, mod)
				}
			}
		}
	}
	m := prepareScrollableModel(t)
	m.rememberInputHistory("old")
	m.editor.SetValue("first\nsecond")
	m.editor.MoveToEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift})
	if !m.editor.HasSelection() || m.editor.Value() != "first\nsecond" {
		t.Fatal("multiline selection was stolen")
	}
}

func TestSelectionCopyPreservesRunAndDialog(t *testing.T) {
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	for _, surface := range []string{"idle", "busy", "dialog", "whole draft"} {
		t.Run(surface, func(t *testing.T) {
			m := prepareScrollableModel(t)
			m.editor.SetValue("draft")
			m.editor.CursorEnd()
			canceled := false
			if surface == "busy" || surface == "dialog" {
				m.beginOptimisticRun()
				m.cancelRun = func() { canceled = true }
			}
			if surface == "dialog" {
				m.startUserInput(protocol.UserInputRequest{ID: "input", Questions: []protocol.UserInputQuestion{{ID: "q", Question: "Answer"}}})
				m.userInputEditor.SetValue("draft")
				m.userInputEditor.CursorEnd()
			}
			for _, msg := range decodeTerminalBytes(t, "\x1b[1;2D") {
				m.Update(msg)
			}
			want := "t"
			if surface == "whole draft" {
				m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
				want = "draft"
			}
			copied := ""
			m.copySelectionToClipboard = func(text string) error { copied = text; return nil }
			beforeGeneration := m.runGeneration
			for _, msg := range decodeTerminalBytes(t, "\x1b[99;6u") {
				_, cmd := m.Update(msg)
				if cmd == nil {
					t.Fatal("copy returned no command")
				}
				runInputCommands(t, m, cmd)
			}
			if copied != want || canceled || m.runGeneration != beforeGeneration || m.editor.Value() != "draft" {
				t.Fatalf("copy=%q canceled=%v generation=%d", copied, canceled, m.runGeneration)
			}
			if surface == "dialog" && !m.userInputPending {
				t.Fatal("copy rejected the dialog")
			}
		})
	}
}

// Only execute input commands; heartbeat/blink timers need not run in this probe.
func runInputCommands(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, child := range msg {
			runInputCommands(t, m, child)
		}
	case tea.QuitMsg:
		t.Fatal("input command quit Snow")
	case transcriptSelectionCopiedMsg:
		m.Update(msg)
	}
}

func TestRepeatedClipboardReadKeepsOutstandingReply(t *testing.T) {
	for _, remote := range []bool{true, false} {
		for _, dialog := range []bool{false, true} {
			m := remoteClipboardModel(t)
			if !remote {
				t.Setenv("SSH_TTY", "")
				t.Setenv("SSH_CONNECTION", "")
			}
			if dialog {
				m.startUserInput(protocol.UserInputRequest{ID: "input", Questions: []protocol.UserInputQuestion{{ID: "q", Question: "Answer"}}})
			}
			if remote {
				m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
			} else {
				m.readClipboardText = func(context.Context) (string, error) { return "", errors.New("host unavailable") }
				m.Update(m.startClipboardTextRead()())
			}
			if m.terminalClipboard == nil {
				t.Fatal("no terminal request")
			}
			first, imageGeneration := *m.terminalClipboard, m.imagePasteGeneration
			m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
			if *m.terminalClipboard != first || m.imagePasteGeneration != imageGeneration || m.clipboardGeneration != first.generation {
				t.Fatal("second read invalidated the first request")
			}
			m.Update(tea.ClipboardMsg{Selection: 'c', Content: "clipboard contents"})
			got := m.editor.Value()
			if dialog {
				got = m.userInputEditor.Value()
			}
			if got != "clipboard contents" {
				t.Fatalf("paste lost: %q", got)
			}
		}
	}
}

func TestSelectionCopyUsesTerminalClipboardOverSSH(t *testing.T) {
	m := remoteClipboardModel(t)
	m.editor.SetValue("draft")
	m.editor.CursorEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
	m.copySelectionToClipboard = func(string) error { t.Fatal("SSH copy accessed host clipboard"); return nil }
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl | tea.ModShift})
	if cmd == nil {
		t.Fatal("missing copy command")
	}
	copied, ok := cmd().(transcriptSelectionCopiedMsg)
	if !ok || copied.terminalWrite == nil || copied.characters != 1 {
		t.Fatal("copy did not use terminal transport")
	}
	if _, write := m.Update(copied); write == nil {
		t.Fatal("terminal clipboard write was dropped")
	}
}
