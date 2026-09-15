package web

import (
	"crypto/rand"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type runtimeSteerRecord struct {
	RuntimeSteerItem
	token, session, turn string
	epoch                uint64
	pending              bool
}

type runtimeSteerChange struct {
	session, turn, item, status string
	epoch, revision             uint64
}

type runtimeSteerState struct {
	token, session, turn string
	epoch, revision      uint64
	active               bool
	records              []runtimeSteerRecord
	// queue events may precede the RPC waiter. Only item identity plus its exact
	// private root may reconcile an acceptance receipt; text equality never does.
	changes []runtimeSteerChange
}

func (r *liveRuntime) steerActiveLocked() bool {
	q := r.queue.control
	return r.steerSupported && !r.compaction.pending && !r.compaction.active && !r.goalBlocksHistoryLocked() && r.snapshot.Permission == nil && r.snapshot.Input == nil && q != nil && q.Accepting && q.SessionID == r.snapshot.SessionID && q.TurnID == r.turnID && r.turnID != "" && r.rootEpoch > 0 && r.promptID != "" && r.queue.sourceUserID != "" && r.busy && !r.transitioning && !r.snapshot.CancelRequested && r.snapshot.Status == "running" && r.ctx.Err() == nil
}

// Counts native pending steering, separately from the follow-up/review list.
// Unknown outcomes remain conservatively occupied until a native terminal event.
func (r *liveRuntime) steerPendingUsageLocked() (count, bytes int) {
	for _, item := range r.steer.records {
		if item.session == r.snapshot.SessionID && item.turn == r.turnID && item.epoch == r.rootEpoch && !steerTerminal(item.Status) {
			count++
			bytes += len(item.Text)
		}
	}
	return count, bytes
}

func (r *liveRuntime) steerCapacityLocked(text string) bool {
	count, bytes := r.steerPendingUsageLocked()
	if q := r.queue.control; q != nil {
		for _, items := range [][]protocol.QueueControlItem{q.Items, q.ReviewItems} {
			count += len(items)
			for _, item := range items {
				bytes += len(item.Text)
			}
		}
	}
	return count < protocol.RPCQueueMaxItems && bytes+len(text) <= protocol.RPCQueueMaxTotalBytes
}

func steerTerminal(status string) bool { return status == "delivered" || status == "discarded" }

func (r *liveRuntime) trimSteerHistoryLocked(incoming int) {
	bytes := incoming
	for _, item := range r.steer.records {
		bytes += len(item.Text)
	}
	for len(r.steer.records) >= protocol.RPCQueueMaxItems || bytes > protocol.RPCQueueMaxTotalBytes {
		// Prefer completed records; retain pending/current-root unknown intents.
		index := slices.IndexFunc(r.steer.records, func(item runtimeSteerRecord) bool {
			return !item.pending && (steerTerminal(item.Status) || item.turn != r.turnID || item.epoch != r.rootEpoch)
		})
		if index < 0 {
			return
		} // admission's shared capacity check prevents this
		bytes -= len(r.steer.records[index].Text)
		r.steer.records = slices.Delete(r.steer.records, index, index+1)
	}
}

func (r *liveRuntime) refreshSteerLocked() {
	if !r.steerSupported {
		r.snapshot.Steer = nil
		return
	}
	active := r.steerActiveLocked()
	if active && (r.steer.token == "" || r.steer.session != r.snapshot.SessionID || r.steer.turn != r.turnID || r.steer.epoch != r.rootEpoch) {
		r.steer.token, r.steer.session, r.steer.turn, r.steer.epoch = rand.Text(), r.snapshot.SessionID, r.turnID, r.rootEpoch
		r.steer.revision++
	}
	if active != r.steer.active {
		r.steer.active = active
		r.steer.revision++
	}
	// Stop/completion/failure is not an explicit native discard. In particular,
	// provider failure can leave accepted input deliverable in the native loop.
	uncertain := r.snapshot.CancelRequested || !r.busy || r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing"
	for i := range r.steer.records {
		item := &r.steer.records[i]
		if item.Status == "accepted" && (uncertain || item.session != r.snapshot.SessionID || item.turn != r.turnID || item.epoch != r.rootEpoch) {
			item.Status = "uncertain"
			r.steer.revision++
		}
	}
	// At least one nonblank byte must fit before granting public admission.
	// Empty text is never a valid submission, even when its byte cost fits.
	out := &RuntimeSteer{Token: r.steer.token, Revision: r.steer.revision, CanSteer: active && r.steerCapacityLocked("x"), Items: make([]RuntimeSteerItem, 0, len(r.steer.records))}
	for _, item := range r.steer.records {
		out.Items = append(out.Items, item.RuntimeSteerItem)
	}
	r.snapshot.Steer = out
}

// Called only for a validated exact-root queue event (never an HTTP ACK). It
// does not manufacture user rows or translate steering into follow-up delivery.
func (r *liveRuntime) applySteerControlLocked(q *protocol.QueueControl) bool {
	if q == nil || q.SessionID != r.snapshot.SessionID || q.TurnID != r.turnID {
		return false
	}
	var status string
	switch q.Change.Kind {
	case "steer_accepted":
		status = "accepted"
	case "delivered":
		status = "delivered"
	case "discarded":
		status = "discarded"
	default:
		return true
	}
	if !validSteerRequestID(q.Change.ItemID) || q.Revision == 0 || (status != "delivered" && q.Change.Text != "") || (status == "delivered" && !validMessageEditText(q.Change.Text)) {
		return false
	}
	// Generic delivery/discard also describes Queue next. Only a previously
	// observed steering acceptance or an ACK-bound steering item belongs here.
	if status != "accepted" && !slices.ContainsFunc(r.steer.changes, func(c runtimeSteerChange) bool {
		return c.session == q.SessionID && c.turn == q.TurnID && c.epoch == r.rootEpoch && c.item == q.Change.ItemID
	}) && !slices.ContainsFunc(r.steer.records, func(i runtimeSteerRecord) bool {
		return i.session == q.SessionID && i.turn == q.TurnID && i.epoch == r.rootEpoch && i.ItemID == q.Change.ItemID
	}) {
		return true
	}
	change := runtimeSteerChange{session: q.SessionID, turn: q.TurnID, epoch: r.rootEpoch, item: q.Change.ItemID, status: status, revision: q.Revision}
	index := slices.IndexFunc(r.steer.changes, func(c runtimeSteerChange) bool {
		return c.session == change.session && c.turn == change.turn && c.epoch == change.epoch && c.item == change.item
	})
	if index >= 0 {
		old := r.steer.changes[index]
		if old.revision >= change.revision || steerTerminal(old.status) {
			return true
		}
		r.steer.changes[index] = change
	} else {
		if len(r.steer.changes) >= 2*protocol.RPCQueueMaxItems {
			r.steer.changes = slices.Delete(r.steer.changes, 0, 1)
		}
		r.steer.changes = append(r.steer.changes, change)
	}
	for i := range r.steer.records {
		item := &r.steer.records[i]
		if item.ItemID == change.item && item.session == change.session && item.turn == change.turn && item.epoch == change.epoch && !steerTerminal(item.Status) {
			// Acceptance arriving after Stop must not erase uncertainty. Only native
			// delivery/discard resolves it, and ACKs cannot regress either terminal.
			if status != "accepted" || item.Status != "uncertain" {
				item.Status = status
				r.steer.revision++
			}
		}
	}
	return true
}

func (r *liveRuntime) reconcileSteerACKLocked(ack protocol.RPCManagedSteerResult, token string) bool {
	index := slices.IndexFunc(r.steer.records, func(item runtimeSteerRecord) bool {
		return item.RequestID == ack.RequestID && item.token == token && item.session == ack.SessionID && item.turn == ack.TurnID && item.epoch == ack.RootEpoch
	})
	if index < 0 {
		return false
	}
	item := &r.steer.records[index]
	if item.ItemID != "" && item.ItemID != ack.ItemID {
		return false
	}
	// Reject an ACK that aliases a different explicitly submitted request.
	if slices.ContainsFunc(r.steer.records, func(other runtimeSteerRecord) bool {
		return other.RequestID != ack.RequestID && other.ItemID == ack.ItemID
	}) {
		return false
	}
	item.ItemID, item.pending = ack.ItemID, false
	if !steerTerminal(item.Status) {
		item.Status = "accepted"
		for _, change := range r.steer.changes {
			if change.item == ack.ItemID && change.session == ack.SessionID && change.turn == ack.TurnID && change.epoch == ack.RootEpoch {
				item.Status = change.status
				break
			}
		}
		if item.Status == "accepted" && (r.turnID != item.turn || r.rootEpoch != item.epoch || !r.busy || r.snapshot.CancelRequested || r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing") {
			item.Status = "uncertain"
		}
	}
	r.steer.revision++
	return true
}
