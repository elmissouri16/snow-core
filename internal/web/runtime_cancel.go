package web

import (
	"context"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeTurnCancelBackend is an optional, generation-bound cancellation capability.
// CancelTurn latches intent, not completion. A successful call never promises idle;
// completion and cancellation-task retirement together release readiness. Legacy
// Abort is unchanged.
// cancelToken comes from the current snapshot, never a provider or RPC-private ID.
type RuntimeTurnCancelBackend interface {
	CancelTurn(ctx context.Context, projectID, instanceID, cancelToken string) error
}

var _ RuntimeTurnCancelBackend = (*RuntimeManager)(nil)

// CancelTurn deliberately bypasses the nonqueued admission gate: Prompt holds it
// through its RPC acknowledgment. Register exactly one worker-lifetime task while
// holding the state lock, then let that task serialize with admission and controls.
// Browser disconnection after admission cannot revoke or retry this mutation.
func (m *RuntimeManager) CancelTurn(ctx context.Context, projectID, instanceID, cancelToken string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return ErrRuntimeClosed
	}
	r := m.workers[projectID]
	if r == nil {
		return ErrRuntimeClosed
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() != nil || instanceID == "" || r.instanceID != instanceID || !r.turnCancelableLocked(cancelToken) {
		return ErrRuntimeInvalid
	}
	if r.snapshot.CancelRequested {
		return nil
	}
	r.snapshot.CancelRequested = true
	r.cancelTaskToken = cancelToken
	sessionID := r.snapshot.SessionID
	r.publishLocked()
	r.cancelTasks.Go(func() { m.dispatchTurnCancel(r, projectID, instanceID, sessionID, cancelToken) })
	return nil
}

// Caller holds mu. Unknown status, unbound sessions and transitions fail closed.
// The locally generated token exists before any provider event or admission ack,
// so a current admission is cancelable without trusting a provider turn identity.
func (r *liveRuntime) turnCancelableLocked(token string) bool {
	if r.ctx.Err() != nil || !r.busy || r.transitioning || r.snapshot.SessionID == "" || token == "" || token != r.snapshot.CancelToken {
		return false
	}
	switch r.snapshot.Status {
	case "running", "permission", "input":
		return true
	default:
		return false
	}
}

func (m *RuntimeManager) dispatchTurnCancel(r *liveRuntime, projectID, instanceID, sessionID, token string) {
	// Registered first so retirement runs AFTER the later control.Unlock defer.
	defer r.retireTurnCancel(instanceID, sessionID, token)
	// A cancelable wait also lets stop join tasks while CloseProject holds control.
	// Only this one latched task waits; ordinary controls and prompts never queue.
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !r.control.TryLock() {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
		}
	}
	defer r.control.Unlock()
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return
	}
	r.mu.Lock()
	valid := r.turnCancelableLocked(token) && r.snapshot.CancelRequested && r.snapshot.SessionID == sessionID
	r.mu.Unlock()
	if !valid {
		return
	}
	// Holding control prevents any next prompt/session admission between the final
	// validation and this single bounded RPC. Completion may win, but a later turn
	// cannot be aborted. Never clear attention or claim idle from an abort ack.
	if err := r.call(protocol.RPCRequest{Type: "abort"}, nil, nil); err != nil {
		r.fail()
	}
}

// Completion may precede the abort RPC acknowledgment. Keep the public latch
// until the dispatcher has released control, otherwise readers advertise Send
// while the mutation gate still rejects it. Caller holds mu.
func (r *liveRuntime) clearTurnCancelLocked() {
	if r.cancelTaskToken != "" && r.cancelTaskToken == r.snapshot.CancelToken {
		return
	}
	r.snapshot.CancelToken = ""
	r.snapshot.CancelRequested = false
}

// Called only after the dispatcher's control gate has been released (or its
// cancelable wait ended). A retired task owns only its original generation; it
// must never clear a replacement session/turn's cancellation latch.
func (r *liveRuntime) retireTurnCancel(instanceID, sessionID, token string) {
	r.mu.Lock()
	if r.cancelTaskToken != token {
		r.mu.Unlock()
		return
	}
	r.cancelTaskToken = ""
	if r.instanceID != instanceID || r.snapshot.SessionID != sessionID || r.snapshot.CancelToken != token {
		r.mu.Unlock()
		return
	}
	wasGoalActive := r.goal.active
	r.finishGoalRunLocked()
	r.finishCompactionLocked()
	goalFinished := wasGoalActive && !r.goal.active
	if !r.busy && r.snapshot.Status == "idle" && r.ctx.Err() == nil {
		r.clearTurnCancelLocked()
		r.publishLocked()
	}
	r.mu.Unlock()
	if goalFinished {
		r.persistRecovery()
	}
}
