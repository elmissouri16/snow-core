package agent

import (
	"context"
	"errors"

	"github.com/elmissouri16/snow-core/internal/session"
)

// ErrBranchRollback marks a failed restoration after a branch mutation. A
// surface cannot safely interpret this as an unchanged session.
var ErrBranchRollback = errors.New("agent: branch rollback failed")

type messageEditTransaction struct {
	transition  func() error
	persisted   func(turnID, entryID string) error
	inputFailed func(entryID string, appendErr error) error
	release     func()
}

// MessageEditReadyAdmitted is read-only. Unlike ordinary controls, historical
// editing never preempts goals, consumes recovered input, or starts continuation.
// The caller must hold admission throughout validation and the transaction.
func (a *Agent) MessageEditReadyAdmitted() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed || a.running || a.autoRunning {
		return errors.New("agent: message edit requires an idle agent")
	}
	if len(a.queuedInputs) != 0 || len(a.queueControl.review) != 0 {
		return errors.New("agent: message edit rejects recovered queued input")
	}
	if a.opts.Goal != nil {
		goal, err := a.opts.Goal.Get()
		if err != nil {
			return err
		}
		if goal != nil && !goal.Status.Terminal() {
			return errors.New("agent: message edit rejects nonterminal goals")
		}
	}
	return nil
}

// PromptEditAdmitted uses the ordinary prompt validation, persistence and turn
// loop. transition runs only after prompt hooks/validation, while idle. persisted
// runs with the new turn claimed and its replacement user durable, before any
// provider work. release hands off caller-owned admission/plugin locks exactly
// then; on a pre-persistence error the caller retains ownership for rollback.
func (a *Agent) PromptEditAdmitted(ctx context.Context, text string, transition func() error, persisted func(string, string) error, inputFailed func(string, error) error, release func()) error {
	if transition == nil || persisted == nil || inputFailed == nil || release == nil {
		return errors.New("agent: incomplete message edit transaction")
	}
	if err := session.ValidateMessageEditText(text); err != nil {
		return err
	}
	if err := a.MessageEditReadyAdmitted(); err != nil {
		return err
	}
	return a.promptTransaction(ctx, text, nil, nil, &messageEditTransaction{transition: transition, persisted: persisted, inputFailed: inputFailed, release: release})
}

// RollbackMessageEditAdmitted restores runtime branch projections and removes
// only the abandoned branch record. Persisted tree entries remain append-only.
func (a *Agent) RollbackMessageEditAdmitted(created, original string) error {
	if err := a.SelectBranchAdmitted(original); err != nil {
		return err
	}
	a.mu.RLock()
	store := a.opts.Session
	a.mu.RUnlock()
	rollback, ok := store.(session.BranchRollbackStore)
	if !ok {
		return errors.New("agent: session cannot roll back message edit")
	}
	return rollback.DeleteBranchForRollback(created)
}
