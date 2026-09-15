package agent

import (
	"context"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RestoreBranchVersionAdmitted never executes a turn, restores an old provider
// selection, runs skill tools, preempts a goal or consumes queued input. The
// caller owns admission and all app/plugin transition gates.
func (a *Agent) RestoreBranchVersionAdmitted(ctx context.Context, p protocol.RPCBranchRestorePrepareParams, mode protocol.CollaborationMode) error {
	if err := a.BranchRestoreReadyAdmitted(); err != nil {
		return err
	}
	a.mu.RLock()
	store, ok := a.opts.Session.(session.BranchVersionStore)
	a.mu.RUnlock()
	if !ok {
		return errors.New("agent: bounded branch restore unsupported")
	}
	err := store.RestoreBranchVersion(ctx, p, mode)
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	actual, probeErr := store.ProbeBranchVersion(probeCtx)
	source := session.BranchVersionIdentity{SessionID: p.SessionID, BranchID: p.SourceBranchID, TipID: p.SourceTipID}
	target := session.BranchVersionIdentity{SessionID: p.SessionID, BranchID: p.TargetBranchID, TipID: p.TargetTipID}
	if err != nil && probeErr == nil && actual == source {
		return err
	}
	if probeErr != nil || actual != target {
		return errors.Join(session.ErrBranchRestoreUnknown, err, probeErr)
	}
	// A confirmed target must also reconcile any stale cached store identity left
	// by an uncertain SQL commit. This is read-only with respect to durable state.
	if adoptErr := store.AdoptBranchVersion(probeCtx, target); adoptErr != nil {
		return errors.Join(session.ErrBranchRestoreUnknown, err, adoptErr)
	}
	a.mu.Lock()
	a.mode = mode
	a.turnMode = mode
	effort := a.effectiveThinkingLocked(mode)
	a.resetTurnIdentityLocked()
	a.latestContextTokens = 0
	a.latestRequestEstimate = 0
	a.latestContextReport = nil
	// Do not reactivate historical skills through tools during restore. Explicit
	// future skill use follows the normal permissioned path.
	a.activeSkills = make(map[string]string)
	a.mu.Unlock()
	a.resetMailboxUnread()
	a.publish(protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{Mode: mode, ReasoningEffort: effort}})
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	if err != nil {
		return errors.Join(session.ErrBranchRestoreUnknown, err)
	}
	return nil
}

// BranchRestoreReadyAdmitted inspects only live control state. Both persisted
// source and target goal predicates are checked by the bounded version reader
// and again under the store's atomic selection reservation, without loading
// arbitrary goal objectives or historical cost records.
func (a *Agent) BranchRestoreReadyAdmitted() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed || a.running || a.autoRunning || a.goalRun != nil {
		return errors.New("agent: branch restore requires an idle agent")
	}
	if len(a.queuedInputs) != 0 || len(a.queueControl.items) != 0 || len(a.queueControl.review) != 0 {
		return errors.New("agent: branch restore rejects pending, review or recovered input")
	}
	return nil
}

// BranchRestoreThinking reads the effective reasoning level for a prepared
// target mode without changing the current runtime selection.
func (a *Agent) BranchRestoreThinking(mode protocol.CollaborationMode) protocol.ThinkingLevel {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.effectiveThinkingLocked(mode)
}
