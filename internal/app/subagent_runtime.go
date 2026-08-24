package app

import (
	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// childAgentRuntime owns the independent child transcript store. agent.Agent
// intentionally does not own/close its injected root store, so the manager's
// child runtime wrapper closes both in the required order.
type childAgentRuntime struct {
	*agent.Agent
	store session.Store
}

func (r *childAgentRuntime) LatestAssistantMessage() (protocol.Message, bool, error) {
	if r == nil || r.store == nil {
		return protocol.Message{}, false, nil
	}
	if latest, ok := r.store.(session.LatestAssistantStore); ok {
		return latest.LatestAssistantMessage()
	}
	return protocol.Message{}, false, nil
}

func (r *childAgentRuntime) Close() {
	if r == nil {
		return
	}
	if r.Agent != nil {
		r.Agent.Close()
	}
	if r.store != nil {
		_ = r.store.Close()
	}
}
