package tui

import (
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func promptTurnEvent(t *testing.T, runtime *agent.Agent, text string) protocol.AgentEvent {
	t.Helper()
	started := make(chan protocol.AgentEvent, 1)
	unsubscribe := runtime.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvTurnDone {
			started <- event
		}
	})
	defer unsubscribe()
	if err := runtime.Prompt(t.Context(), text); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-started:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for turn-started event")
		return protocol.AgentEvent{}
	}
}
