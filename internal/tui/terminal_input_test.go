package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"strings"
	"testing"
	"testing/iotest"
)

func prepareScrollableModel(t *testing.T) *Model {
	t.Helper()
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 12
	m.layout()
	for i := range 60 {
		m.lines = append(m.lines, fmt.Sprintf("line %02d", i))
	}
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscript()
	m.transcript.SetYOffset(9)
	return m
}

// Exercise the same streaming decoder Bubble Tea uses, including reads split
// inside escape sequences and UTF-8 code points.
func decodeTerminalBytes(t *testing.T, input string) []tea.Msg {
	t.Helper()
	reader := uv.NewTerminalReader(iotest.OneByteReader(strings.NewReader(input)), "xterm-ghostty")
	events := make(chan uv.Event, len(input)+1)
	if err := reader.StreamEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	close(events)
	var messages []tea.Msg
	for event := range events {
		switch event := event.(type) {
		case uv.KeyPressEvent:
			messages = append(messages, tea.KeyPressMsg(event))
		case uv.KeyReleaseEvent:
			messages = append(messages, tea.KeyReleaseMsg(event))
		case uv.MouseWheelEvent:
			messages = append(messages, tea.MouseWheelMsg(event))
		case uv.MouseClickEvent:
			messages = append(messages, tea.MouseClickMsg(event))
		case uv.MouseMotionEvent:
			messages = append(messages, tea.MouseMotionMsg(event))
		case uv.MouseReleaseEvent:
			messages = append(messages, tea.MouseReleaseMsg(event))
		case uv.PasteEvent:
			messages = append(messages, tea.PasteMsg{Content: event.Content})
		}
	}
	return messages
}

func TestTerminalDecoderHandlesLegacyAndEnhancedKeys(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"\x1b\r", "alt+enter"}, {"\x1b[Z", "shift+tab"}, {"\x1b", "esc"},
		{"\x1b[13;2u", "shift+enter"}, {"\x1b[1;5B", "ctrl+down"}, {"\x1bm", "alt+m"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			messages := decodeTerminalBytes(t, tc.input)
			if len(messages) != 1 {
				t.Fatalf("events = %#v", messages)
			}
			key, ok := messages[0].(tea.KeyPressMsg)
			if !ok || key.String() != tc.want {
				t.Fatalf("key = %#v, want %q", messages[0], tc.want)
			}
		})
	}
}

func TestFragmentedTerminalMouseDoesNotEditOrCrossModal(t *testing.T) {
	for _, modal := range []bool{false, true} {
		m := prepareScrollableModel(t)
		if modal {
			m.startUserInput(protocol.UserInputRequest{ID: "input", Questions: []protocol.UserInputQuestion{{ID: "answer", Question: "Answer"}}})
		}
		before := m.transcript.YOffset()
		for _, msg := range decodeTerminalBytes(t, "\x1b[<65;8;4M\x1b[<65;8;4M") {
			m.Update(msg)
		}
		want := before
		if !modal {
			want += 2 * m.transcript.MouseWheelDelta
		}
		if m.transcript.YOffset() != want || m.editor.Value() != "" || m.userInputEditor.Value() != "" {
			t.Fatalf("modal=%v offset=%d want=%d editor=%q answer=%q", modal, m.transcript.YOffset(), want, m.editor.Value(), m.userInputEditor.Value())
		}
	}
}

func TestFragmentedBracketedPasteIsLiteral(t *testing.T) {
	m := prepareScrollableModel(t)
	text := "alpha\n[<65;8;4M\n日本語 👨‍👩‍👧‍👦 é\n/help"
	for _, msg := range decodeTerminalBytes(t, "\x1b[200~"+text+"\x1b[201~") {
		m.Update(msg)
	}
	if m.editor.Value() != text || m.busy {
		t.Fatalf("paste=%q busy=%v", m.editor.Value(), m.busy)
	}
}

func TestKeyReleaseCannotSubmitOrToggleMode(t *testing.T) {
	m := prepareScrollableModel(t)
	m.editor.SetValue("draft")
	before := m.app.Agent.Mode()
	m.Update(tea.KeyReleaseMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyReleaseMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.busy || m.editor.Value() != "draft" || m.app.Agent.Mode() != before {
		t.Fatal("key release performed an action")
	}
}

func TestViewDeclaresTerminalModes(t *testing.T) {
	m := prepareScrollableModel(t)
	view := m.View()
	if !view.AltScreen || !view.ReportFocus || view.DisableBracketedPasteMode || view.Cursor != nil || view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("terminal modes = %+v", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF6})
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("native mouse mode was not declared")
	}
}
