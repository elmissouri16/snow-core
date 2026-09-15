package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// CompactionRunHandle captures one manual operation. Identity and Done never
// change when a later turn is admitted. Release gates all provider work; Done
// closes only after usage, persistence and mailbox cleanup have finished.
type CompactionRunHandle struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	release             func()
	start               chan struct{}
	done                chan struct{}
	turn                TurnSnapshot
	sessionID, branchID string
	result              protocol.CompactionResult // immutable after done closes
	resultErr           error
	completion          protocol.RPCCompactionCompleted
}

func (h *CompactionRunHandle) ID() string            { return h.turn.ID }
func (h *CompactionRunHandle) SessionID() string     { return h.sessionID }
func (h *CompactionRunHandle) BranchID() string      { return h.branchID }
func (h *CompactionRunHandle) Turn() TurnSnapshot    { return h.turn }
func (h *CompactionRunHandle) Release()              { h.release() }
func (h *CompactionRunHandle) Cancel()               { h.cancel() }
func (h *CompactionRunHandle) Done() <-chan struct{} { return h.done }
func (h *CompactionRunHandle) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-h.done:
		return h.resultErr
	default:
	}
	select {
	case <-h.done:
		return h.resultErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Completion returns a bounded immutable public copy; false means cleanup has
// not finished. It deliberately excludes summary text and raw errors.
func (h *CompactionRunHandle) Completion() (protocol.RPCCompactionCompleted, bool) {
	select {
	case <-h.done:
		return h.completion, true
	default:
		return protocol.RPCCompactionCompleted{}, false
	}
}

func (h *CompactionRunHandle) Accepted() protocol.RPCCompactionAccepted {
	return protocol.RPCCompactionAccepted{CompactionID: h.ID(), SessionID: h.sessionID, BranchID: h.branchID, TurnID: h.turn.ID, TurnOrigin: h.turn.Origin, RootEpoch: h.turn.Epoch, TurnSequence: h.turn.Sequence}
}

// CompactionReadyAdmitted is read-only and never stops or defers automatic work.
// Caller holds admission. Plan mode allows this existing context-only operation;
// compaction does not execute tools or transition the collaboration mode.
func (a *Agent) CompactionReadyAdmitted() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed || a.running || a.autoRunning || a.goalRun != nil || a.compactionRun != nil {
		return errors.New("agent: manual compaction requires an idle agent")
	}
	if len(a.queuedInputs) != 0 || len(a.queueControl.review) != 0 {
		return errors.New("agent: manual compaction rejects pending or recovered input")
	}
	for _, item := range a.queueControl.items {
		if item.State == "pending" || item.State == "delivering" {
			return errors.New("agent: manual compaction rejects queued work")
		}
	}
	if a.opts.Session == nil {
		return errors.New("agent: no active session")
	}
	return nil
}

// StartCompaction reserves a captured operation, rejecting rather than stopping
// active automatic work. Apps should use their exact-binding admission facade.
func (a *Agent) StartCompaction(ctx context.Context) (*CompactionRunHandle, error) {
	unlock, err := a.LockAdmissionContext(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return a.StartCompactionAdmitted(ctx)
}

func (a *Agent) StartCompactionAdmitted(ctx context.Context) (*CompactionRunHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := a.CompactionReadyAdmitted(); err != nil {
		return nil, err
	}
	if a.opts.Goal != nil {
		goal, err := a.opts.Goal.Get()
		if err != nil {
			return nil, err
		}
		if goal != nil && !goal.Status.Terminal() {
			return nil, errors.New("agent: unfinished goal prevents manual compaction")
		}
	}
	return a.reserveCompactionAdmitted(ctx, false)
}

// reserveCompactionAdmitted is shared with synchronous legacy Compact, whose
// explicit control boundary historically pauses an active automatic goal.
func (a *Agent) reserveCompactionAdmitted(ctx context.Context, deferActiveGoal bool) (*CompactionRunHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, errors.New("agent: closed")
	}
	if a.running {
		return nil, errors.New("agent: cannot compact while running")
	}
	if len(a.queuedInputs) > 0 || len(a.queueControl.review) > 0 {
		return nil, errors.New("agent: undelivered queued input is waiting for recovery; call ClearPendingInputs first")
	}
	runCtx, cancel := context.WithCancel(ctx)
	a.running, a.queueAccepting, a.autoStop = true, false, false
	a.queuedInputs = nil
	a.admitTurnIdentityLocked("compact")
	h := &CompactionRunHandle{ctx: runCtx, cancel: cancel, start: make(chan struct{}), done: make(chan struct{}), turn: a.activeTurnSnapshotLocked(), sessionID: a.opts.Session.ID()}
	if branches, ok := a.opts.Session.(session.ActiveBranchStore); ok {
		h.branchID = branches.ActiveBranchID()
	}
	h.release = sync.OnceFunc(func() { close(h.start) })
	a.compactionRun = h
	a.activeCancel, a.activeDone = cancel, h.done
	a.turnWG.Go(func() { a.executeCompaction(h, deferActiveGoal) })
	return h, nil
}

func (a *Agent) executeCompaction(h *CompactionRunHandle, deferActiveGoal bool) {
	select {
	case <-h.ctx.Done():
		h.resultErr = h.ctx.Err()
	case <-h.start:
		h.resultErr = h.ctx.Err()
		if h.resultErr == nil {
			h.result, h.resultErr = a.compactActiveContext(h.ctx, compactionManual)
		}
	}
	// Legacy goal deferral and all mailbox persistence precede the terminal
	// result, admission release and Done. Stop/Close can wait without admission.
	if deferActiveGoal && h.ctx.Err() != nil && a.opts.Goal != nil {
		h.resultErr = errors.Join(h.resultErr, a.opts.Goal.Defer(true))
	}
	h.resultErr = errors.Join(h.resultErr, h.ctx.Err())
	h.cancel()
	a.finishCompaction(h)
}

func (a *Agent) finishCompaction(h *CompactionRunHandle) {
	// Producers also hold mailboxPersistMu, so no envelope can sneak between
	// the final drain and running=false. Unlike finishTurnMailbox, ownership
	// stays running while this last batch and its events are being committed.
	a.mailboxPersistMu.Lock()
	defer a.mailboxPersistMu.Unlock()
	a.mailboxMu.Lock()
	batch := slices.Clone(a.mailbox)
	a.mailbox, a.mailboxBytes = nil, 0
	a.mailboxMu.Unlock()
	h.resultErr = errors.Join(h.resultErr, a.persistMailboxBatchLocked(batch))
	status := "completed"
	switch {
	case errors.Is(h.resultErr, context.Canceled), errors.Is(h.resultErr, context.DeadlineExceeded):
		status = "canceled"
	case h.resultErr != nil:
		status = "failed"
	case h.result.SummarizedMessages == 0:
		status = "noop"
	case h.result.UsedFallback:
		status = "fallback"
	}
	h.completion = protocol.RPCCompactionCompleted{Type: protocol.RPCTypeCompactionCompleted, CompactionID: h.ID(), SessionID: h.sessionID, BranchID: h.branchID, TurnID: h.turn.ID, TurnOrigin: h.turn.Origin, RootEpoch: h.turn.Epoch, TurnSequence: h.turn.Sequence, Status: status, SummarizedMessages: h.result.SummarizedMessages, RetainedMessages: h.result.RetainedMessages, UsedFallback: h.result.UsedFallback}
	a.mu.Lock()
	a.running, a.queueAccepting = false, false
	a.activeCancel, a.activeDone, a.goalAtTurn = nil, nil, nil
	a.compactionRun = nil
	close(h.done)
	a.mu.Unlock()
}

// compactManualCaptured is the synchronous compatibility facade. It keeps the
// historical goal-pause behavior; the explicit StartCompaction API never does.
func (a *Agent) compactManualCaptured(ctx context.Context, turn *TurnSnapshot) (protocol.CompactionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	unlock, err := a.LockAdmissionContext(ctx)
	if err != nil {
		return protocol.CompactionResult{}, err
	}
	h, err := a.startLegacyCompactionAdmitted(ctx)
	unlock()
	if err != nil {
		return protocol.CompactionResult{}, err
	}
	if turn != nil {
		*turn = h.Turn()
	}
	h.Release()
	// Once admitted, synchronous Compact waits through cleanup even when the
	// caller context is canceled, just as the original implementation did.
	err = h.Wait(context.Background())
	return h.result, err
}

func (a *Agent) startLegacyCompactionAdmitted(ctx context.Context) (*CompactionRunHandle, error) {
	a.mu.RLock()
	owned := a.goalRun != nil || a.compactionRun != nil
	a.mu.RUnlock()
	if owned {
		return nil, errors.New("agent: cannot compact while an explicit run owns the runtime")
	}
	deferActiveGoal := false
	if a.opts.Goal != nil {
		goal, err := a.opts.Goal.Get()
		if err != nil {
			return nil, fmt.Errorf("agent: inspect goal before compact: %w", err)
		}
		deferActiveGoal = goal != nil && goal.Status == protocol.GoalActive
	}
	if err := a.stopAutomaticForControl(ctx, "compact"); err != nil {
		if deferActiveGoal {
			_ = a.opts.Goal.Defer(true)
		}
		return nil, err
	}
	if deferActiveGoal {
		if err := a.opts.Goal.Defer(true); err != nil {
			return nil, fmt.Errorf("agent: pause goal after compact: %w", err)
		}
	}
	return a.reserveCompactionAdmitted(ctx, deferActiveGoal)
}
