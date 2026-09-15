package agent

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"sync"
	"uuid"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// GoalRunHandle owns the serial worker across native turns, retries, compaction
// and the intervals between them. Done closes exactly once after durable cleanup.
// Release is the transport's provider-start gate, not a second agent loop.
type GoalRunHandle struct {
	id, goalID string
	ctx        context.Context
	cancel     context.CancelFunc
	start      chan struct{}
	done       chan struct{}
	release    sync.Once
	resultErr  error // immutable after done closes
	goalStatus protocol.ThreadGoalStatus
}

func (h *GoalRunHandle) ID() string            { return h.id }
func (h *GoalRunHandle) GoalID() string        { return h.goalID }
func (h *GoalRunHandle) Release()              { h.release.Do(func() { close(h.start) }) }
func (h *GoalRunHandle) Cancel()               { h.cancel() }
func (h *GoalRunHandle) Done() <-chan struct{} { return h.done }
func (h *GoalRunHandle) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-h.done:
		return h.resultErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (h *GoalRunHandle) GoalStatus() protocol.ThreadGoalStatus {
	select {
	case <-h.done:
		return h.goalStatus
	default:
		return ""
	}
}

// GoalRunReadyAdmitted performs read-only validation; the caller must retain
// admission through durable goal mutation and StartGoalRunAdmitted.
func (a *Agent) GoalRunReadyAdmitted() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.opts.ManagedExplicitGoals {
		return errors.New("agent: explicit goal policy is not enabled")
	}
	if a.closed || a.running || a.autoRunning || a.goalRun != nil {
		return errors.New("agent: goal run requires an idle agent")
	}
	if a.mode != protocol.ModeDefault {
		return errors.New("agent: goal run is unavailable in Plan mode")
	}
	if a.opts.Goal == nil || !a.goalToolsAvailableLocked() {
		return errors.New("agent: goal capabilities unavailable")
	}
	if len(a.queuedInputs) != 0 || len(a.queueControl.review) != 0 {
		return errors.New("agent: goal run rejects pending or recovered input")
	}
	for _, item := range a.queueControl.items {
		if item.State == "pending" || item.State == "delivering" {
			return errors.New("agent: goal run rejects queued work")
		}
	}
	if !a.model.SupportsThinkingLevel(a.effectiveThinkingLocked(a.mode)) {
		return errors.New("agent: model does not support selected thinking level")
	}
	return nil
}

// StartGoalRunAdmitted reserves ownership synchronously. The goal must already
// be durably deferred. No request or compaction can run until Release.
func (a *Agent) StartGoalRunAdmitted(ctx context.Context, goalID string) (*GoalRunHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := a.GoalRunReadyAdmitted(); err != nil {
		return nil, err
	}
	goal, err := a.opts.Goal.Get()
	if err != nil {
		return nil, err
	}
	if goal == nil || goal.GoalID != goalID || goal.Status != protocol.GoalActive {
		return nil, errors.New("agent: goal run binding changed")
	}
	deferred, err := a.opts.Goal.Deferred()
	if err != nil {
		return nil, err
	}
	if !deferred {
		return nil, errors.New("agent: goal run must be durably deferred before admission")
	}
	runCtx, cancel := context.WithCancel(ctx)
	h := &GoalRunHandle{id: "goal-run-" + uuid.New().String(), goalID: goalID, ctx: runCtx, cancel: cancel, start: make(chan struct{}), done: make(chan struct{})}
	a.ResetGoalAudit()
	a.mu.Lock()
	a.goalRun = h
	a.autoRunning, a.autoStop, a.autoPending = true, false, false
	a.autoDone = h.done
	a.budgetWrap, a.budgetReportDone = false, false
	a.autoWG.Go(func() { a.executeGoalRun(h) })
	a.mu.Unlock()
	return h, nil
}

func (a *Agent) executeGoalRun(h *GoalRunHandle) {
	var err error
	select {
	case <-h.ctx.Done():
		err = h.ctx.Err()
	case <-h.start:
		err = h.ctx.Err()
		if err == nil {
			err = a.opts.Goal.Defer(false)
		}
		if err == nil {
			err = a.runAutomaticGoal(h.ctx)
		}
	}
	// Fail closed on every exit, even transport loss before the first request.
	// Successful status completion and budget cleanup remain native goal behavior.
	err = errors.Join(err, h.ctx.Err(), a.opts.Goal.Defer(true))
	goal, goalErr := a.opts.Goal.Get()
	err = errors.Join(err, goalErr)
	if goal != nil {
		h.goalStatus = goal.Status
	}
	h.resultErr = err
	h.cancel()
	a.mu.Lock()
	if a.goalRun == h {
		a.goalRun = nil
		a.autoRunning, a.autoPending = false, false
		a.autoDone = nil
	}
	close(h.done)
	a.mu.Unlock()
}

func (a *Agent) GoalRunID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.goalRun != nil {
		return a.goalRun.id
	}
	return ""
}

// ManagedGoalMutationAllowedAdmitted protects legacy control paths from
// changing the goal underneath an explicit owner. Legacy policy is unchanged.
func (a *Agent) ManagedGoalMutationAllowedAdmitted() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.opts.ManagedExplicitGoals {
		return errors.New("agent: managed goals require explicit goal_run controls")
	}
	return nil
}

func (a *Agent) managedGoalToolAllowed(name string) bool {
	if name != "create_goal" && name != "get_goal" && name != "update_goal" {
		return true
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.opts.ManagedExplicitGoals {
		return true
	}
	return name != "create_goal" && a.goalRun != nil && a.turnOrigin == "goal"
}

// checkManagedGoalToolCall must run after plugin argument transforms and before
// permission/tool dispatch. Schema filtering alone is not an authority boundary.
func (a *Agent) checkManagedGoalToolCall(name string, raw []byte) error {
	if !a.managedGoalToolAllowed(name) {
		return fmt.Errorf("goal: %s requires an explicitly owned goal run (creation is manager-only)", name)
	}
	a.mu.RLock()
	managed, owner := a.opts.ManagedExplicitGoals, a.goalRun
	a.mu.RUnlock()
	if !managed || (name != "get_goal" && name != "update_goal") {
		return nil
	}
	goal, err := a.opts.Goal.Get()
	if err != nil {
		return err
	}
	if owner == nil || goal == nil || goal.GoalID != owner.goalID {
		return errors.New("goal: run no longer owns this goal")
	}
	if name == "update_goal" {
		var args struct {
			GoalID string `json:"goal_id"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return err
		}
		if args.GoalID != owner.goalID {
			return errors.New("goal: update does not match owned goal")
		}
	}
	return nil
}
