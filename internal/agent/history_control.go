package agent

import (
	"context"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ErrHistoryControlUnknown marks an uncertain durable commit outcome.
var ErrHistoryControlUnknown = session.ErrHistoryControlUnknown

// ManagedForkBranchAdmitted is the passive fork path. The caller holds admission
// and the app/plugin gates, and has checked exact source/target saved tips and
// persisted nonterminal goals. Unlike ForkWithOptionsAdmitted this never stops
// automatic work, reads/copies managed goal files, or activates skill tools.
func (a *Agent) ManagedForkBranchAdmitted(ctx context.Context, opts protocol.RPCManagedBranchForkParams, mode protocol.CollaborationMode) (protocol.SessionBranch, error) {
	if err := a.BranchRestoreReadyAdmitted(); err != nil {
		return protocol.SessionBranch{}, err
	}
	a.mu.RLock()
	store := a.opts.Session
	manager, ok := store.(session.HistoryControlStore)
	a.mu.RUnlock()
	if !ok {
		return protocol.SessionBranch{}, errors.New("agent: managed branch fork unsupported")
	}
	if err := ctx.Err(); err != nil {
		return protocol.SessionBranch{}, err
	}
	branch, err := manager.ForkHistoryBranch(ctx, opts, mode)
	if err != nil {
		if !errors.Is(err, ErrHistoryControlUnknown) || branch.ID == "" {
			return branch, err
		}
		// A lost commit acknowledgement may leave a committed new branch with a
		// stale cached store cursor. Reconcile only an authoritatively confirmed
		// generated branch, and still report unknown so callers never retry it.
		versions, ok := store.(session.BranchVersionStore)
		if !ok {
			return branch, err
		}
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		expected := session.BranchVersionIdentity{SessionID: opts.SessionID, BranchID: branch.ID, TipID: opts.TargetTipID}
		actual, probeErr := versions.ProbeBranchVersion(probeCtx)
		if probeErr != nil || actual != expected {
			return branch, errors.Join(err, probeErr)
		}
		if adoptErr := versions.AdoptBranchVersion(probeCtx, expected); adoptErr != nil {
			return branch, errors.Join(err, adoptErr)
		}
	}
	a.mu.Lock()
	a.mode, a.turnMode = mode, mode
	// Match passive Versions restore: historical skills are not trusted runtime
	// activations. Explicit later skill use follows ordinary permission gates.
	a.activeSkills = make(map[string]string)
	a.resetTurnIdentityLocked()
	a.latestContextTokens = 0
	a.latestRequestEstimate = 0
	a.latestContextReport = nil
	effort := a.effectiveThinkingLocked(mode)
	a.mu.Unlock()
	a.resetMailboxUnread()
	a.publish(protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{Mode: mode, ReasoningEffort: effort}})
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	return branch, err
}
