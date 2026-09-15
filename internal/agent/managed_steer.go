package agent

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var (
	ErrManagedSteerRejected = errors.New("agent: managed steering rejected")
	ErrManagedSteerStale    = errors.New("agent: managed steering root is stale")
)

// ManagedSteer checks the exact root while owning the native queue transaction.
// It never routes through queue_enqueue: accepted steering retains native
// priority, persistence, cancellation, and provider-failure recovery semantics.
func (a *Agent) ManagedSteer(p protocol.RPCManagedSteerParams) (protocol.RPCManagedSteerResult, error) {
	for _, id := range []string{p.SessionID, p.TurnID, p.RequestID} {
		if !validManagedSteerID(id) {
			return protocol.RPCManagedSteerResult{}, fmt.Errorf("%w: bounded nonempty identifiers required", ErrManagedSteerRejected)
		}
	}
	if !utf8.ValidString(p.Text) || strings.ContainsRune(p.Text, 0) || strings.TrimSpace(p.Text) == "" || len(p.Text) > protocol.RPCManagedSteerMaxTextBytes {
		return protocol.RPCManagedSteerResult{}, fmt.Errorf("%w: nonempty NUL-free UTF-8 text of at most %d bytes required", ErrManagedSteerRejected, protocol.RPCManagedSteerMaxTextBytes)
	}
	item, err := a.enqueueRootInputBound(protocol.QueuedInputSteer, p.Text, &p)
	if err != nil {
		return protocol.RPCManagedSteerResult{}, err
	}
	return protocol.RPCManagedSteerResult{SessionID: p.SessionID, TurnID: p.TurnID, RootEpoch: p.RootEpoch, RequestID: p.RequestID, ItemID: item.ID, Status: "accepted"}, nil
}

func validManagedSteerID(id string) bool {
	return len(id) > 0 && len(id) <= protocol.RPCManagedSteerMaxIDBytes && strings.TrimSpace(id) == id && utf8.ValidString(id) && !strings.ContainsAny(id, "\x00\r\n\t")
}

// enqueueRootInputBound is the native admission transaction. Legacy callers
// retain their established limits and blocking behavior. Managed submissions
// additionally enforce exact identity and the stricter shared manager budget.
func (a *Agent) enqueueRootInputBound(kind protocol.QueuedInputKind, text string, target *protocol.RPCManagedSteerParams) (protocol.QueuedInput, error) {
	if kind != protocol.QueuedInputSteer && kind != protocol.QueuedInputFollowUp {
		return protocol.QueuedInput{}, fmt.Errorf("agent: invalid queued input kind %q", kind)
	}
	if strings.TrimSpace(text) == "" {
		return protocol.QueuedInput{}, errors.New("agent: queued input is empty")
	}
	if len(text) > maxQueuedInputBytes {
		return protocol.QueuedInput{}, fmt.Errorf("agent: queued input exceeds %d bytes", maxQueuedInputBytes)
	}
	if target != nil {
		if !a.queuePublishMu.TryLock() {
			return protocol.QueuedInput{}, fmt.Errorf("%w: queue boundary busy; refresh before another explicit submission", ErrManagedSteerRejected)
		}
	} else {
		a.queuePublishMu.Lock()
	}
	defer a.queuePublishMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if target != nil {
		if a.opts.Session == nil || target.SessionID != a.opts.Session.ID() || target.TurnID != a.turnID || target.RootEpoch != a.rootEpoch {
			return protocol.QueuedInput{}, ErrManagedSteerStale
		}
		q := &a.queueControl
		if a.closed || a.goalRun != nil || a.compactionRun != nil || a.autoRunning || a.autoStop || a.goalAtTurn != nil || !a.running || !a.queueAccepting || a.turnOrigin != "user" || !q.ready || q.closed || q.sessionID != target.SessionID || q.turnID != target.TurnID {
			return protocol.QueuedInput{}, ErrManagedSteerRejected
		}
		if a.opts.Goal != nil {
			goal, err := a.opts.Goal.Get()
			if err != nil || (goal != nil && !goal.Status.Terminal()) {
				return protocol.QueuedInput{}, ErrManagedSteerRejected
			}
		}
		count, bytes := a.rootInputUsageLocked()
		if count >= protocol.RPCQueueMaxItems || bytes+len(text) > protocol.RPCQueueMaxTotalBytes {
			return protocol.QueuedInput{}, fmt.Errorf("%w: shared queue capacity reached", ErrManagedSteerRejected)
		}
	}
	if a.closed {
		return protocol.QueuedInput{}, errors.New("agent: closed")
	}
	if a.goalRun != nil || !a.running || !a.queueAccepting {
		return protocol.QueuedInput{}, ErrNotRunning
	}
	if len(a.queuedInputs) >= maxPendingRootInputs {
		return protocol.QueuedInput{}, fmt.Errorf("agent: queued input limit %d reached", maxPendingRootInputs)
	}
	a.queueSequence++
	item := protocol.QueuedInput{ID: newID(), Kind: kind, Text: text, Order: a.queueSequence}
	a.queuedInputs = append(a.queuedInputs, item)
	if target != nil {
		a.queueControl.revision++
		a.queueControl.change = protocol.QueueControlChange{Kind: "steer_accepted", ItemID: item.ID}
		a.publishQueueControlEventLocked(new(a.inputQueueLocked()))
	} else {
		snapshot := a.inputQueueLocked()
		a.mu.Unlock()
		a.publishInputQueue(snapshot)
		a.mu.Lock()
	}
	return item, nil
}

// Count each pending item once: manager follow-ups are also in queuedInputs.
// Review work is outside that native queue but consumes the same bounded budget.
func (a *Agent) rootInputUsageLocked() (count, bytes int) {
	count = len(a.queuedInputs) + len(a.queueControl.review)
	for _, item := range a.queuedInputs {
		bytes += len(item.Text)
	}
	for _, item := range a.queueControl.review {
		bytes += len(item.Text)
	}
	return count, bytes
}

// Called with queuePublishMu and mu held after the native queue has been
// cleared. Disappearance alone must never be interpreted as durable delivery.
func (a *Agent) publishDiscardedRootInputsLocked(cleared protocol.InputQueue) {
	q := &a.queueControl
	if q.turnID != a.turnID || q.sessionID != a.opts.Session.ID() {
		return
	}
	for _, item := range cleared.Items {
		q.revision++
		q.change = protocol.QueueControlChange{Kind: "discarded", ItemID: item.ID}
		a.publishQueueControlLocked()
	}
}
