package tui

import (
	"errors"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestLatePromptErrorKeepsTerminalCompletionSettled(t *testing.T) {
	m := modelPickerTestModel(t, 80, 24)
	m.terminal.unfocused = true
	generation := m.beginOptimisticRun()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, TurnID: "failed", Message: "private failure"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "failed"})
	m.Update(promptDoneMsg{generation: generation, turnID: "failed", admitted: true, err: errors.New("private failure")})
	if m.busy || m.terminal.heartbeat {
		t.Fatal("late prompt result restarted settled work and terminal progress")
	}
	if got := terminalTestAlert(m); got != "\a"+ansi.Notify("Snow encountered an error") {
		t.Fatalf("late prompt result lost completion alert: %q", got)
	}
}
