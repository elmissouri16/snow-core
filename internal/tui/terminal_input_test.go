package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func prepareScrollableModel(t *testing.T) *Model {
	t.Helper()
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 12
	m.layout()
	for i := 0; i < 60; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line %02d", i))
	}
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscript()
	m.transcript.SetYOffset(9)
	return m
}

func deliverTerminalCmd(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	_, next := m.Update(msg)
	return next
}

func TestLeakedSGRWheelReportsScrollWithoutEditing(t *testing.T) {
	m := prepareScrollableModel(t)
	before := m.transcript.YOffset
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<65;113;44M[<65;113;44M")})
	deliverTerminalCmd(t, m, cmd)
	if got := m.transcript.YOffset; got != before+2*m.transcript.MouseWheelDelta {
		t.Fatalf("wheel-down offset=%d want %d", got, before+2*m.transcript.MouseWheelDelta)
	}
	if got := m.editor.Value(); got != "" {
		t.Fatalf("mouse report leaked into editor: %q", got)
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<64;113;44M")})
	deliverTerminalCmd(t, m, cmd)
	if got := m.transcript.YOffset; got != before+m.transcript.MouseWheelDelta {
		t.Fatalf("wheel-up offset=%d want %d", got, before+m.transcript.MouseWheelDelta)
	}
}

func TestLeakedSGRReportDoesNotEnterBlockingUserInput(t *testing.T) {
	m := prepareScrollableModel(t)
	m.startUserInput(protocol.UserInputRequest{ID: "terminal", Questions: []protocol.UserInputQuestion{{
		ID: "answer", Header: "Answer", Question: "Type an answer",
	}}})
	before := m.transcript.YOffset
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<65;42;7M")})
	deliverTerminalCmd(t, m, cmd)
	if got := m.userInputEditor.Value(); got != "" {
		t.Fatalf("mouse report leaked into user input: %q", got)
	}
	if got := m.transcript.YOffset; got != before {
		t.Fatalf("mouse report scrolled behind user input: offset=%d want %d", got, before)
	}
	if m.transcriptSelection.anchor != nil || m.transcriptSelection.pressActive {
		t.Fatalf("mouse report selected behind user input: %+v", m.transcriptSelection)
	}
}

func TestLeakedSGRFragmentsRecoverAcrossInputMessages(t *testing.T) {
	tests := []struct {
		name  string
		parts []tea.KeyMsg
	}{
		{name: "after escape", parts: []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune("[<65;113;44M")}}},
		{name: "after csi", parts: []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("["), Alt: true}, {Type: tea.KeyRunes, Runes: []rune("<65;113;44M")}}},
		{name: "inside parameters", parts: []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("[<65;113")}, {Type: tea.KeyRunes, Runes: []rune(";44M")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := prepareScrollableModel(t)
			before := m.transcript.YOffset
			var cmd tea.Cmd
			for _, part := range tt.parts {
				_, cmd = m.Update(part)
			}
			deliverTerminalCmd(t, m, cmd)
			if got := m.transcript.YOffset; got != before+m.transcript.MouseWheelDelta {
				t.Fatalf("offset=%d want %d", got, before+m.transcript.MouseWheelDelta)
			}
			if got := m.editor.Value(); got != "" {
				t.Fatalf("fragment leaked into editor: %q", got)
			}
		})
	}
}

func TestInvalidTerminalFragmentTimeoutReplaysLiteralInput(t *testing.T) {
	m := prepareScrollableModel(t)
	fragment := "[<65;113"
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(fragment)})
	if cmd == nil || m.terminalInput.raw != fragment {
		t.Fatalf("fragment was not retained: raw=%q cmd=%v", m.terminalInput.raw, cmd != nil)
	}
	_, _ = m.Update(clearMetaEnterMsg(m.metaEnterSeq))
	if got := m.editor.Value(); got != fragment {
		t.Fatalf("replayed editor value=%q want %q", got, fragment)
	}
}

func TestFragmentedShiftTabRetainsModalNavigation(t *testing.T) {
	m := prepareScrollableModel(t)
	m.compVisible = true
	m.compMatches = []string{"/allow", "/default", "/plan"}
	m.compIndex = 1
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[Z")})
	deliverTerminalCmd(t, m, cmd)
	if m.compIndex != 0 {
		t.Fatalf("fragmented Shift+Tab index=%d want 0", m.compIndex)
	}
	if got := m.editor.Value(); got != "" {
		t.Fatalf("fragmented Shift+Tab leaked into editor: %q", got)
	}
}

func TestFragmentedShiftTabIsAppliedBeforeFollowingEnter(t *testing.T) {
	m := prepareScrollableModel(t)
	m.editor.SetValue("run after mode switch")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, modeCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[Z")})
	if !m.modeSwitching {
		t.Fatal("fragmented Shift+Tab was not applied synchronously")
	}
	_, promptCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if promptCmd != nil || m.busy || m.editor.Value() != "run after mode switch" {
		t.Fatalf("Enter raced mode switch: cmd=%v busy=%v editor=%q", promptCmd != nil, m.busy, m.editor.Value())
	}
	applyModeToggleCommand(t, m, modeCmd)
	if m.app.Agent.Mode() != protocol.ModePlan {
		t.Fatalf("mode=%q", m.app.Agent.Mode())
	}
}

func TestTerminalFragmentWindowPreservesEscapeAndOptionReturn(t *testing.T) {
	t.Run("modal escape", func(t *testing.T) {
		m := prepareScrollableModel(t)
		m.compVisible = true
		m.compMatches = []string{"/help"}
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if !m.compVisible {
			t.Fatal("modal Escape fired before fragment window expired")
		}
		_, _ = m.Update(clearMetaEnterMsg(m.metaEnterSeq))
		if m.compVisible {
			t.Fatal("modal Escape was not replayed after fragment timeout")
		}
	})

	t.Run("active escape", func(t *testing.T) {
		m := prepareScrollableModel(t)
		m.busy = true
		m.runStartedAt = m.currentTime()
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if len(m.lines) != 60 {
			t.Fatal("active Escape fired before fragment window expired")
		}
		_, _ = m.Update(clearMetaEnterMsg(m.metaEnterSeq))
		if len(m.lines) != 61 || strings.TrimSpace(stripANSI(m.lines[len(m.lines)-1])) != "aborted" {
			t.Fatalf("active Escape was not replayed: lines=%d", len(m.lines))
		}
	})

	t.Run("split option return", func(t *testing.T) {
		m := prepareScrollableModel(t)
		m.editor.SetValue("line")
		m.editor.CursorEnd()
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		deliverTerminalCmd(t, m, cmd)
		if got := m.editor.Value(); got != "line\n" {
			t.Fatalf("split Option+Return=%q", got)
		}
	})
}

func TestPastedMouseLikeTextRemainsLiteral(t *testing.T) {
	m := prepareScrollableModel(t)
	text := "[<65;113;44M"
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	if got := m.editor.Value(); got != text {
		t.Fatalf("pasted mouse-like text=%q want %q", got, text)
	}
}

func TestValidNonWheelSGRReportIsConsumed(t *testing.T) {
	m := prepareScrollableModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<0;10;5M")})
	deliverTerminalCmd(t, m, cmd)
	if got := m.editor.Value(); got != "" {
		t.Fatalf("mouse press leaked into editor: %q", got)
	}
}
