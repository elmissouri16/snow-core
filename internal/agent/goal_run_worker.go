package agent

import (
	"context"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// runAutomaticGoal is the one native continuation worker shared by legacy
// ContinueGoal and explicit handles. Every provider request remains internalTurn
// (including its retries, tool chain, accounting and terminal cleanup).
func (a *Agent) runAutomaticGoal(ctx context.Context) error {
	a.mu.Lock()
	wrap := a.budgetWrap
	a.budgetWrap = false
	a.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		a.mu.RLock()
		stopped := a.autoStop
		a.mu.RUnlock()
		if stopped {
			return context.Canceled
		}
		if err := a.internalTurn(ctx, wrap); err != nil {
			return err
		}
		a.mu.Lock()
		crossed, reported := a.budgetWrap, a.budgetReportDone
		a.budgetWrap = false
		a.mu.Unlock()
		if crossed && !wrap && !reported {
			wrap = true
			continue
		}
		g, err := a.opts.Goal.Get()
		if err != nil {
			return err
		}
		if g == nil || g.Status != protocol.GoalActive {
			return nil
		}
		// A successful native turn can durably defer without changing its status.
		deferred, err := a.opts.Goal.Deferred()
		if err != nil {
			return err
		}
		if deferred {
			return nil
		}
		compacted, compactErr := a.autoCompactGoalBoundary(ctx)
		a.mu.Lock()
		crossed, stopped = a.budgetWrap, a.autoStop
		a.budgetWrap = false
		a.mu.Unlock()
		if stopped || ctx.Err() != nil {
			return errors.Join(context.Canceled, ctx.Err(), compactErr)
		}
		if crossed && !wrap {
			wrap = true
			continue
		}
		if compactErr != nil {
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "goal auto-compaction: " + compactErr.Error()})
			deferErr := a.opts.Goal.Defer(true)
			var statusErr error
			if deferErr == nil {
				_, statusErr = a.opts.Goal.SetStatusWithReason(g.GoalID, protocol.GoalBlocked, false, "Automatic compaction failed: "+compactErr.Error())
			}
			return errors.Join(compactErr, deferErr, statusErr)
		}
		if compacted {
			g, err = a.opts.Goal.Get()
			if err != nil {
				return err
			}
			if g == nil || g.Status != protocol.GoalActive {
				return nil
			}
		}
		timer := time.NewTimer(automaticTurnDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		wrap = false
	}
}
