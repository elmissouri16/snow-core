package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestLatePromptErrorDoesNotRestartSettledTurn(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	generation := m.beginOptimisticRun()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, TurnID: "failed", Message: "private failure"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "failed"})
	before := strings.Join(m.lines, "\n")
	m.Update(promptDoneMsg{generation: generation, turnID: "failed", admitted: true, err: errors.New("private failure")})
	if m.busy || m.activeTurnID != "" {
		t.Fatal("late prompt result restarted settled work")
	}
	if strings.Join(m.lines, "\n") != before {
		t.Fatal("late prompt result duplicated the authoritative error")
	}
}
