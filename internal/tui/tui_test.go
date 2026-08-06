package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
)

// buildAppForTest constructs the app synchronously and attaches it to the
// model so tests don't depend on the async Init path or a TTY.
func buildAppForTest(t *testing.T, m *Model) {
	t.Helper()
	a, err := app.New(context.Background(), app.Options{
		Provider:   "fake",
		NoSession:  true,
		Permission: "allow",
		CWD:        t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	m.app = a
	t.Cleanup(func() { a.Close() })
}

// TestModelSlashCommands verifies command parsing without a TTY.
func TestModelSlashCommands(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/help")
	_, quit := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if quit != nil {
		t.Fatal("help should not quit")
	}
	if len(m.lines) == 0 {
		t.Fatal("expected help output")
	}

	m.editor.SetValue("/quit")
	_, quit = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if quit == nil {
		t.Fatal("quit should return a quit command")
	}
}

// TestModelPermissionCommand verifies /permission updates the service mode.
func TestModelPermissionCommand(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/permission deny")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := string(m.app.Perm.Mode()); got != "deny" {
		t.Fatalf("mode = %s, want deny", got)
	}
}

// TestModelAgentEventUpdates verifies streaming events update the transcript.
func TestModelAgentEventUpdates(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "hello"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "bash"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolName: "bash", IsError: true, Message: "boom"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})

	view := m.View()
	if !strings.Contains(view, "hello") {
		t.Fatalf("view missing streamed text: %q", view)
	}
	if !strings.Contains(view, "bash") {
		t.Fatalf("view missing tool name: %q", view)
	}
}

// TestModelAbortOnCtrlC verifies the busy-abort path returns no quit cmd.
func TestModelAbortOnCtrlC(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	_, quit := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if quit != nil {
		t.Fatal("ctrl+c while busy should abort, not quit")
	}
	// busy is cleared when the EvAborted event arrives from the agent; a
	// second ctrl+c while idle should quit.
	m.busy = false
	_, quit = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if quit == nil {
		t.Fatal("ctrl+c while idle should quit")
	}
}
