package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aymanbagabas/go-osc52/v2"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func remoteClipboardModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("SSH_TTY", "/dev/test")
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-ghostty")
	return prepareScrollableModel(t)
}

func TestTerminalClipboardReplyRequiresCurrentRequest(t *testing.T) {
	m := remoteClipboardModel(t)
	m.Update(tea.ClipboardMsg{Selection: 'c', Content: "unsolicited"})
	if m.editor.Value() != "" {
		t.Fatal("unsolicited clipboard inserted text")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd == nil || m.terminalClipboard == nil {
		t.Fatal("SSH paste did not query terminal")
	}
	m.Update(tea.ClipboardMsg{Selection: 'p', Content: "wrong selection"})
	if m.terminalClipboard == nil {
		t.Fatal("primary selection consumed system request")
	}
	m.Update(tea.ClipboardMsg{Selection: 'c', Content: "literal\n/help"})
	if m.terminalClipboard != nil || m.editor.Value() != "literal\n/help" || m.busy {
		t.Fatal("requested text did not paste literally")
	}
}

func TestTerminalClipboardTimeoutDrainsBeforeNextRequest(t *testing.T) {
	m := remoteClipboardModel(t)
	m.startClipboardTextRead()
	generation := m.terminalClipboard.generation
	m.Update(terminalClipboardTimeoutMsg(generation))
	if !m.terminalClipboard.canceled || !strings.Contains(m.lastStatus, "timed out") {
		t.Fatal("request did not time out")
	}
	if cmd := m.startClipboardTextRead(); cmd != nil {
		t.Fatal("overlapping request issued after timeout")
	}
	m.Update(tea.PasteMsg{Content: "native paste"})
	m.Update(tea.ClipboardMsg{Selection: 'c', Content: "late reply"})
	if m.editor.Value() != "native paste" || m.terminalClipboard != nil {
		t.Fatal("late response was not discarded and drained")
	}
	if cmd := m.startClipboardTextRead(); cmd == nil {
		t.Fatal("drained query prevented retry")
	}
	m.Update(tea.ClipboardMsg{Selection: 'c', Content: " next"})
	if m.editor.Value() != "native paste next" {
		t.Fatalf("retry text=%q", m.editor.Value())
	}
}

func TestTerminalClipboardCannotCrossModalRoundTrip(t *testing.T) {
	m := remoteClipboardModel(t)
	m.startClipboardTextRead()
	req := &protocol.UserInputRequest{ID: "dialog", Questions: []protocol.UserInputQuestion{{ID: "q", Question: "Answer"}}}
	m.Update(agentEventMsg{ev: protocol.AgentEvent{Type: protocol.EvUserInputRequest, UserInput: req}})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.userInputPending {
		t.Fatal("question did not close")
	}
	m.Update(tea.ClipboardMsg{Selection: 'c', Content: "old composer reply"})
	if m.editor.Value() != "" {
		t.Fatal("canceled clipboard survived a modal round trip")
	}
}

func TestTerminalClipboardQuestionOwnershipAndMaskedLogin(t *testing.T) {
	m := remoteClipboardModel(t)
	m.startUserInput(protocol.UserInputRequest{ID: "dialog", Questions: []protocol.UserInputQuestion{{ID: "one", Question: "First"}, {ID: "two", Question: "Second"}}})
	m.startClipboardTextRead()
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.ClipboardMsg{Selection: 'c', Content: "first answer"})
	if m.userInputEditor.Value() != "" {
		t.Fatal("reply crossed question boundary")
	}
	m.clearUserInput()
	m.beginKeyCapture("openai")
	m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	m.Update(tea.ClipboardMsg{Selection: 'c', Content: "nonsecret-test-key"})
	if m.secretBuf.String() != "nonsecret-test-key" || strings.Contains(m.viewContent(), "nonsecret-test-key") {
		t.Fatal("masked login paste routing failed")
	}
}

func TestLocalClipboardFailureFallsBackAndStaleReadIsIgnored(t *testing.T) {
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	m := prepareScrollableModel(t)
	m.readClipboardText = func(ctx context.Context) (string, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > terminalClipboardTimeout {
			t.Error("host read has no bounded deadline")
		}
		return "", errors.New("unavailable")
	}
	cmd := m.startClipboardTextRead()
	_, query := m.Update(cmd())
	if query == nil || m.terminalClipboard == nil {
		t.Fatal("local failure did not fall back to OSC52")
	}
	m.Update(tea.ClipboardMsg{Selection: 'c', Content: "fallback"})
	m.readClipboardText = func(context.Context) (string, error) { return "stale", nil }
	cmd = m.startClipboardTextRead()
	m.Update(tea.PasteMsg{Content: " newer"})
	m.Update(cmd())
	if m.editor.Value() != "fallback newer" {
		t.Fatalf("stale host read inserted: %q", m.editor.Value())
	}
}

func TestTerminalClipboardOversizeAndMultiplexerQueries(t *testing.T) {
	m := remoteClipboardModel(t)
	m.startClipboardTextRead()
	m.Update(tea.ClipboardMsg{Selection: 'c', Content: strings.Repeat("x", maxClipboardTextBytes+1)})
	if m.editor.Value() != "" || !strings.Contains(m.lastStatus, "1 MiB") {
		t.Fatal("oversized clipboard was inserted")
	}
	for _, tc := range []struct{ term, tmux, prefix string }{{"screen", "", "\x1bP\x1b]52;"}, {"tmux-256color", "test", "\x1bPtmux;\x1b\x1b]52;"}} {
		t.Setenv("TERM", tc.term)
		t.Setenv("TMUX", tc.tmux)
		if got := wrapTerminalClipboard(osc52.Query()); !strings.HasPrefix(got, tc.prefix) || !strings.Contains(got, "?") {
			t.Fatalf("wrapped query=%q", got)
		}
	}
}

func TestShiftEnterNewlineAndSavedOverrides(t *testing.T) {
	m := prepareScrollableModel(t)
	for _, msg := range decodeTerminalBytes(t, "\x1b[13;2u") {
		m.Update(msg)
	}
	if m.editor.Value() != "\n" || m.busy {
		t.Fatal("Shift+Enter submitted instead of newline")
	}
	m.startUserInput(protocol.UserInputRequest{ID: "input", Questions: []protocol.UserInputQuestion{{ID: "q", Question: "Answer"}}})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if m.userInputEditor.Value() != "\n" || !m.userInputPending {
		t.Fatal("Shift+Enter submitted answer")
	}
	keys, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"newline": {"ctrl+j"}})
	if err != nil || keyMatches(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}, keys.Newline) {
		t.Fatal("migration replaced saved newline override")
	}
	if name, err := keyNameFromMessage(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}); err != nil || name != "shift+enter" {
		t.Fatalf("capture=%q err=%v", name, err)
	}
}
