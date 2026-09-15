package agent

import (
	"errors"
	"fmt"
	"slices"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var (
	ErrQueueRejected = errors.New("agent: queue control rejected")
	ErrQueueStale    = errors.New("agent: queue control stale")
	ErrQueueUnknown  = errors.New("agent: queue delivery outcome unknown")
)

type queueControlState struct {
	sessionID, turnID string
	revision          uint64
	ready, closed     bool
	items             []protocol.QueueControlItem
	review            []protocol.QueueControlItem
	change            protocol.QueueControlChange
	userID, replyID   string
}

// QueueControlSnapshot is read-only, including while delivery owns the queue.
func (a *Agent) QueueControlSnapshot(p protocol.RPCQueueListParams) (protocol.QueueControl, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if p.SessionID == "" || p.TurnID == "" || p.SessionID != a.opts.Session.ID() || p.SessionID != a.queueControl.sessionID || p.TurnID != a.queueControl.turnID {
		return protocol.QueueControl{}, ErrQueueStale
	}
	return a.queueControlLocked(), nil
}

// MutateQueueControl never waits for delivery or plugin hooks. Selection remains
// the legacy priority boundary: a racing mutation is rejected, never reordered.
// It does not take admission/state/plugin locks or invoke any callback inline.
func (a *Agent) MutateQueueControl(command string, p protocol.RPCQueueUpdateParams) (protocol.QueueControl, error) {
	if command != "queue_enqueue" && command != "queue_update" && command != "queue_remove" {
		return protocol.QueueControl{}, ErrQueueRejected
	}
	if command != "queue_remove" {
		if err := session.ValidateMessageEditText(p.Text); err != nil {
			return protocol.QueueControl{}, errors.Join(ErrQueueRejected, err)
		}
	}
	if !a.queuePublishMu.TryLock() {
		return protocol.QueueControl{}, fmt.Errorf("%w: delivery owns queue; refresh before retrying explicitly", ErrQueueRejected)
	}
	defer a.queuePublishMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	q := &a.queueControl
	if p.SessionID == "" || p.TurnID == "" || p.SessionID != a.opts.Session.ID() || p.SessionID != q.sessionID || p.TurnID != q.turnID || p.Revision != q.revision {
		return protocol.QueueControl{}, ErrQueueStale
	}
	if a.closed || a.goalRun != nil {
		return protocol.QueueControl{}, ErrQueueRejected
	}
	index := slices.IndexFunc(q.items, func(i protocol.QueueControlItem) bool { return i.ID == p.ItemID })
	review := slices.IndexFunc(q.review, func(i protocol.QueueControlItem) bool { return i.ID == p.ItemID })
	if command == "queue_remove" && review >= 0 {
		q.review = slices.Delete(q.review, review, review+1)
	} else {
		if !q.ready || q.closed || !a.running || !a.queueAccepting || a.turnOrigin != "user" || a.turnID != q.turnID {
			return protocol.QueueControl{}, ErrQueueRejected
		}
		if a.opts.Goal != nil {
			g, err := a.opts.Goal.Get()
			if err != nil || (g != nil && !g.Status.Terminal()) {
				return protocol.QueueControl{}, ErrQueueRejected
			}
		}
		if _, ok := a.opts.Session.(session.BatchStore); !ok {
			return protocol.QueueControl{}, fmt.Errorf("%w: atomic queued input history unsupported", ErrQueueRejected)
		}
		count, bytes := a.rootInputUsageLocked()
		if command == "queue_enqueue" {
			if count >= protocol.RPCQueueMaxItems || len(a.queuedInputs) >= maxPendingRootInputs {
				return protocol.QueueControl{}, fmt.Errorf("%w: queue capacity reached", ErrQueueRejected)
			}
			if bytes+len(p.Text) > protocol.RPCQueueMaxTotalBytes {
				return protocol.QueueControl{}, fmt.Errorf("%w: queue byte capacity reached", ErrQueueRejected)
			}
			a.queueSequence++
			item := protocol.QueuedInput{ID: newID(), Kind: protocol.QueuedInputFollowUp, Text: p.Text, Order: a.queueSequence}
			a.queuedInputs = append(a.queuedInputs, item)
			q.items = append(q.items, protocol.QueueControlItem{ID: item.ID, Text: item.Text, State: "pending"})
			p.ItemID = item.ID
		} else {
			if index < 0 || q.items[index].State != "pending" {
				return protocol.QueueControl{}, ErrQueueStale
			}
			pending := slices.IndexFunc(a.queuedInputs, func(i protocol.QueuedInput) bool { return i.ID == p.ItemID })
			if pending < 0 {
				return protocol.QueueControl{}, ErrQueueStale
			}
			if command == "queue_update" {
				if bytes-len(a.queuedInputs[pending].Text)+len(p.Text) > protocol.RPCQueueMaxTotalBytes {
					return protocol.QueueControl{}, fmt.Errorf("%w: queue byte capacity reached", ErrQueueRejected)
				}
				q.items[index].Text = p.Text
				a.queuedInputs[pending].Text = p.Text
			} else {
				q.items = slices.Delete(q.items, index, index+1)
				a.queuedInputs = slices.Delete(a.queuedInputs, pending, pending+1)
			}
		}
	}
	kinds := map[string]string{"queue_enqueue": "enqueued", "queue_update": "updated", "queue_remove": "removed"}
	q.revision++
	q.change = protocol.QueueControlChange{Kind: kinds[command], ItemID: p.ItemID}
	result := a.queueControlLocked()
	a.publishQueueControlEventLocked(new(a.inputQueueLocked()))
	return result, nil
}

func (a *Agent) queueControlLocked() protocol.QueueControl {
	q := &a.queueControl
	count, bytes := a.rootInputUsageLocked()
	return protocol.QueueControl{SessionID: q.sessionID, TurnID: q.turnID, Revision: q.revision, Accepting: a.goalRun == nil && q.ready && !q.closed && a.running && a.queueAccepting && !a.closed && count < protocol.RPCQueueMaxItems && bytes < protocol.RPCQueueMaxTotalBytes, Items: append([]protocol.QueueControlItem{}, q.items...), ReviewItems: append([]protocol.QueueControlItem{}, q.review...), Change: q.change}
}

// Caller owns queuePublishMu and mu. Publish without mu so bounded event-bus
// backpressure cannot deadlock a subscriber's read-only queue_list. Mutating
// controls TryLock queuePublishMu, so reentrant mutations reject immediately.
func (a *Agent) publishQueueControlEventLocked(legacy *protocol.InputQueue) {
	q := a.queueControlLocked()
	event := protocol.AgentEvent{Type: protocol.EvQueueUpdated, Queue: legacy, QueueControl: &q, TurnID: q.TurnID, TurnOrigin: "user", RootEpoch: a.rootEpoch, TurnSequence: a.activeTurnSequence}
	if a.goalRun != nil {
		event.GoalRunID = a.goalRun.id
	}
	a.mu.Unlock()
	a.bus.Publish(event)
	a.mu.Lock()
}
func (a *Agent) publishQueueControlLocked() { a.publishQueueControlEventLocked(nil) }

func (a *Agent) queueControlRootReady(userID string) {
	a.queuePublishMu.Lock()
	defer a.queuePublishMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	q := &a.queueControl
	q.sessionID = a.opts.Session.ID()
	q.turnID = a.turnID
	q.ready = true
	q.closed = false
	q.userID = userID
	q.replyID = ""
	q.items = nil
	q.revision++
	q.change = protocol.QueueControlChange{Kind: "root_admitted", UserEntryID: userID}
	a.publishQueueControlLocked()
}
func (a *Agent) queueControlReply(message protocol.Message) {
	a.queuePublishMu.Lock()
	defer a.queuePublishMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	q := &a.queueControl
	if q.turnID != a.turnID || !q.ready {
		return
	}
	q.replyID = ""
	if message.IsRegeneratableReply() {
		q.replyID = message.ID
	}
	q.revision++
	q.change = protocol.QueueControlChange{Kind: "reply", UserEntryID: q.userID, ReplyEntryID: q.replyID}
	a.publishQueueControlLocked()
}

// Caller owns queuePublishMu and mu. Move only web work out of the legacy
// recovery/retry queue; it remains explicitly discardable, never scheduled.
func (a *Agent) holdQueueControlLocked() {
	q := &a.queueControl
	if !q.ready || q.closed {
		return
	}
	q.closed = true
	q.ready = false
	for _, item := range q.items {
		item.State = "held"
		q.review = append(q.review, item)
		a.queuedInputs = slices.DeleteFunc(a.queuedInputs, func(i protocol.QueuedInput) bool { return i.ID == item.ID })
	}
	q.items = nil
	q.revision++
	q.change = protocol.QueueControlChange{Kind: "closed"}
	if len(q.review) > 0 {
		q.change.Kind = "retained"
	}
	a.publishQueueControlLocked()
}
func (a *Agent) holdQueueControl() {
	a.queuePublishMu.Lock()
	defer a.queuePublishMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.holdQueueControlLocked()
}

// QueueControlTransitionReady prevents identity-changing operations from
// stranding bounded review work or preempting an explicit goal-run owner.
// It never clears or retargets that work.
func (a *Agent) QueueControlTransitionReady() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.goalRun != nil {
		return errors.New("agent: cannot change lifecycle identity while an explicit goal run owns the runtime")
	}
	if len(a.queueControl.items) > 0 || len(a.queueControl.review) > 0 {
		return fmt.Errorf("%w: explicitly discard queued/review work before changing lifecycle identity", ErrQueueRejected)
	}
	if a.running && a.turnOrigin == "user" {
		return errors.New("agent: cannot change lifecycle identity while running")
	}
	return nil
}
