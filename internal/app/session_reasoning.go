package app

import (
	"context"
	"errors"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// SessionReasoning returns effective settings without network discovery or any
// host/project configuration or history write. Lock order matches model/session
// compound transactions: app state first, then agent admission, then agent mu.
func (a *App) SessionReasoning(ctx context.Context, p protocol.RPCSessionReasoningGetParams) (protocol.RPCSessionReasoning, error) {
	if a == nil || a.Agent == nil {
		return protocol.RPCSessionReasoning{}, errors.New("app: session reasoning unavailable")
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return protocol.RPCSessionReasoning{}, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return protocol.RPCSessionReasoning{}, err
	}
	if a.Session == nil || a.Session.ID() != p.SessionID {
		return protocol.RPCSessionReasoning{}, agent.ErrSessionReasoningStale
	}
	if a.Subagents != nil && a.Subagents.HasActive() {
		return protocol.RPCSessionReasoning{}, agent.ErrSessionReasoningBusy
	}
	return a.Agent.SessionReasoningAdmitted(p.SessionID)
}

// SetSessionReasoning does not call UpdateRPCSettings, config.Update, or any
// legacy persisted setter. Only one mode-aware live preference is changed.
func (a *App) SetSessionReasoning(ctx context.Context, p protocol.RPCSessionReasoningSetParams) (protocol.RPCSessionReasoning, error) {
	if a == nil || a.Agent == nil {
		return protocol.RPCSessionReasoning{}, errors.New("app: session reasoning unavailable")
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return protocol.RPCSessionReasoning{}, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return protocol.RPCSessionReasoning{}, err
	}
	if a.Session == nil || a.Session.ID() != p.Expected.SessionID {
		return protocol.RPCSessionReasoning{}, agent.ErrSessionReasoningStale
	}
	if a.Subagents != nil && a.Subagents.HasActive() {
		return protocol.RPCSessionReasoning{}, agent.ErrSessionReasoningBusy
	}
	result, err := a.Agent.SetSessionReasoningAdmitted(p)
	if err == nil || errors.Is(err, agent.ErrSessionReasoningUnknown) {
		// Prepared history controls were authorized under a different response
		// configuration. Neither edits/regenerations nor restores may reuse them.
		clear(a.messageEdits)
		clear(a.branchRestores)
	}
	return result, err
}
