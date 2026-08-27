package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestTUIMouseCapturePreference(t *testing.T) {
	t.Run("default keeps wheel inside Snow", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.json")
		if !tuiMouseCaptureEnabled(app.Options{ConfigPath: path}) {
			t.Fatal("default config did not enable viewport mouse scrolling")
		}
	})

	t.Run("explicit app mode enables mouse capture", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"tui":{"mouse":true}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if !tuiMouseCaptureEnabled(app.Options{ConfigPath: path}) {
			t.Fatal("tui.mouse=true did not enable application mouse capture")
		}
	})

	t.Run("malformed config does not start", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"tui":`), 0o600); err != nil {
			t.Fatal(err)
		}
		if tuiMouseCaptureEnabled(app.Options{ConfigPath: path}) {
			t.Fatal("malformed config unexpectedly returned a usable preference")
		}
		if _, err := app.New(context.Background(), app.Options{ConfigPath: path, Provider: "fake", NoSession: true}); err == nil || !strings.Contains(err.Error(), "config: parse") {
			t.Fatalf("normal startup error = %v, want config parse error", err)
		}
	})
}

func TestComposerCtrlVPasteResultIsNotDropped(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 20
	m.layout()
	m.editor.SetValue("before ")
	m.editor.CursorEnd()
	pasted := "line one\n[<65;113;44M\nline three"
	m.pasteCmdOverride = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true}
	}

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("Ctrl+V discarded the textarea clipboard command")
	}
	result, ok := cmd().(textareaResultMsg)
	if !ok || result.target != textareaTargetComposer || result.requestID != "" || result.questionID != "" || result.pasteGeneration == 0 {
		t.Fatalf("composer paste result metadata = %#v", result)
	}
	_, _ = m.Update(result)
	if got := m.editor.Value(); got != "before "+pasted {
		t.Fatalf("composer paste = %q", got)
	}
	if m.busy || len(m.lines) != 0 {
		t.Fatalf("paste submitted prompt: busy=%v lines=%v", m.busy, m.lines)
	}
}

func TestPasteResultReturnsToInitiatingUserInputEditor(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 24
	m.editor.SetValue("hidden composer")
	m.startUserInput(protocol.UserInputRequest{ID: "paste-question", Questions: []protocol.UserInputQuestion{{
		ID: "answer", Header: "Answer", Question: "Paste details",
	}}})
	pasted := "first line\nsecond line"
	m.pasteCmdOverride = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true}
	}

	_, cmd := m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("free-form Ctrl+V discarded the textarea clipboard command")
	}
	result, ok := cmd().(textareaResultMsg)
	if !ok || result.target != textareaTargetUserInput || result.requestID != "paste-question" || result.questionID != "answer" {
		t.Fatalf("user-input paste result metadata = %#v", result)
	}
	_, _ = m.Update(result)
	if got := m.userInputEditor.Value(); got != pasted {
		t.Fatalf("user-input paste = %q", got)
	}
	if got := m.userInputDrafts["answer"]; got != pasted {
		t.Fatalf("user-input draft = %q", got)
	}
	if got := m.editor.Value(); got != "hidden composer" {
		t.Fatalf("paste leaked into composer: %q", got)
	}
}

func TestComposerPasteFailureIsVisibleAndRetryClearsIt(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.editor.Err = errors.New("clipboard unavailable")
	_, _ = m.Update(textareaResultMsg{
		target: textareaTargetComposer,
		msg:    struct{}{},
	})
	if len(m.lines) == 0 || !strings.Contains(stripANSI(m.lines[len(m.lines)-1]), "paste: clipboard unavailable") {
		t.Fatalf("paste failure was not visible: %v", m.lines)
	}

	m.pasteCmdOverride = func() tea.Msg { return struct{}{} }
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil || m.editor.Err != nil {
		t.Fatalf("paste retry did not clear prior error: cmd=%v err=%v", cmd != nil, m.editor.Err)
	}
}

func TestDelayedUserInputPasteDoesNotMoveToAnotherQuestion(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.startUserInput(protocol.UserInputRequest{ID: "multi-question", Questions: []protocol.UserInputQuestion{
		{ID: "first", Header: "First", Question: "First answer"},
		{ID: "second", Header: "Second", Question: "Second answer"},
	}})

	m.pasteCmdOverride = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("delayed paste"), Paste: true}
	}
	_, resultCmd := m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if resultCmd == nil {
		t.Fatal("free-form Ctrl+V returned no command")
	}
	result, ok := resultCmd().(textareaResultMsg)
	if !ok || result.requestID != "multi-question" || result.questionID != "first" {
		t.Fatalf("delayed paste metadata = %#v", result)
	}
	m.moveUserInputQuestion(1)
	_, _ = m.Update(result)
	if got := m.userInputEditor.Value(); got != "" {
		t.Fatalf("delayed paste leaked into second question: %q", got)
	}
	if got := m.userInputDrafts["first"]; got != "" {
		t.Fatalf("stale paste changed first-question draft: %q", got)
	}
}

func TestUserInputPasteFailureIsVisibleAndRetryClearsIt(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.startUserInput(protocol.UserInputRequest{ID: "paste-error", Questions: []protocol.UserInputQuestion{{
		ID: "answer", Header: "Answer", Question: "Paste details",
	}}})
	m.pasteCmdOverride = func() tea.Msg { return struct{}{} }

	_, cmd := m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	m.userInputEditor.Err = errors.New("clipboard unavailable")
	_, _ = m.Update(cmd())
	if m.userInputError != "paste: clipboard unavailable" {
		t.Fatalf("user-input paste error = %q", m.userInputError)
	}

	_, cmd = m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil || m.userInputEditor.Err != nil || m.userInputError != "" {
		t.Fatalf("user-input retry did not clear error: cmd=%v editorErr=%v error=%q", cmd != nil, m.userInputEditor.Err, m.userInputError)
	}
}

func TestTerminalBracketedPasteRemainsLiteralAndDoesNotSubmit(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	text := "alpha\n[<0;10;5M\nomega"
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	if got := m.editor.Value(); got != text {
		t.Fatalf("bracketed paste = %q, want %q", got, text)
	}
	if m.busy || len(m.lines) != 0 {
		t.Fatalf("bracketed paste submitted prompt: busy=%v lines=%v", m.busy, m.lines)
	}
}
