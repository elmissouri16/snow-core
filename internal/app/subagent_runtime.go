package app

import (
	"github.com/snow-core/snow/internal/agent"
	"github.com/snow-core/snow/internal/session"
)

// childAgentRuntime owns the independent child transcript store. agent.Agent
// intentionally does not own/close its injected root store, so the manager's
// child runtime wrapper closes both in the required order.
type childAgentRuntime struct {
	*agent.Agent
	store session.Store
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
