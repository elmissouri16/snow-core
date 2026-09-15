package web

import (
	"crypto/rand"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type runtimeQueueState struct {
	control                                   *protocol.QueueControl
	token                                     string
	eventRevision                             uint64
	viewRevision                              uint64
	dismissed                                 map[string]bool
	sourceUserID, spanID, replyID, replyRowID string
}

// Queue text is editable source, not a clipped transcript preview. Preserve it
// exactly; only structural/UTF-8/byte bounds authorize this public projection.
func validQueueControl(q *protocol.QueueControl) bool {
	if q == nil || q.SessionID == "" || !runtimeIdentifier(q.SessionID) || q.TurnID == "" || !runtimeOption(q.TurnID) || q.Revision == 0 || len(q.Items)+len(q.ReviewItems) > protocol.RPCQueueMaxItems {
		return false
	}
	seen := make(map[string]bool)
	total := 0
	for _, items := range [][]protocol.QueueControlItem{q.Items, q.ReviewItems} {
		for _, item := range items {
			if item.ID == "" || !runtimeOption(item.ID) || seen[item.ID] || !validMessageEditText(item.Text) {
				return false
			}
			switch item.State {
			case "pending", "delivering", "held", "delivery_unknown":
			default:
				return false
			}
			seen[item.ID] = true
			total += len(item.Text)
		}
	}
	if total > protocol.RPCQueueMaxTotalBytes {
		return false
	}
	c := q.Change
	for _, id := range []string{c.ItemID, c.UserEntryID, c.PreviousUserEntryID, c.PrecedingReplyEntryID, c.ReplyEntryID, c.SpanID} {
		if id != "" && !runtimeOption(id) {
			return false
		}
	}
	switch c.Kind {
	case "root_admitted":
		return c.UserEntryID != "" && c.Text == ""
	case "reply":
		return c.UserEntryID != "" && c.Text == ""
	case "delivered":
		return c.ItemID != "" && c.UserEntryID != "" && c.SpanID != "" && validMessageEditText(c.Text)
	case "enqueued", "updated", "removed", "selected", "delivering", "delivery_unknown", "steer_accepted", "discarded":
		return c.ItemID != "" && c.Text == ""
	case "held", "closed", "retained":
		return c.Text == ""
	default:
		return false
	}
}

// ACKs may overtake the event consumer. Snapshot revision and source-event
// revision are separate so a newer ACK never suppresses an unseen delivery.
func (r *liveRuntime) applyQueueControlLocked(q *protocol.QueueControl, sourceEvent bool) bool {
	if !validQueueControl(q) || q.SessionID != r.snapshot.SessionID || q.TurnID != r.turnID {
		return false
	}
	if sourceEvent && q.Revision > r.queue.eventRevision {
		if !r.projectQueueSourceLocked(q) {
			return false
		}
		r.queue.eventRevision = q.Revision
	}
	if r.queue.control == nil || q.Revision > r.queue.control.Revision {
		if r.queue.control == nil || r.queue.control.TurnID != q.TurnID {
			r.queue.token = ""
		}
		r.queue.control = q.Clone()
		r.queue.viewRevision++
	}
	r.publishLocked()
	return true
}

func (r *liveRuntime) projectQueueSourceLocked(q *protocol.QueueControl) bool {
	c := q.Change
	switch c.Kind {
	case "root_admitted":
		r.queue.sourceUserID = c.UserEntryID
		r.queue.spanID = q.TurnID
		r.queue.replyID = ""
		r.queue.replyRowID = ""
		for i := range r.snapshot.Messages {
			row := &r.snapshot.Messages[i]
			if row.Role == "user" && ((r.pendingUserID != "" && row.ID == r.pendingUserID) || row.SourceID == c.UserEntryID) {
				row.SourceID = strings.Clone(c.UserEntryID)
				row.SourceTurnID = ""
				row.CanEdit = !row.Truncated
				r.pendingUserID = ""
				break
			}
		}
	case "reply":
		if c.UserEntryID != r.queue.sourceUserID {
			return false
		}
		r.queue.replyID = c.ReplyEntryID
		r.queue.replyRowID = ""
		if c.ReplyEntryID != "" && r.assistant >= 0 && r.assistant < len(r.snapshot.Messages) {
			row := &r.snapshot.Messages[r.assistant]
			if row.Role == "assistant" && row.SourceTurnID == q.TurnID && row.SourceSpanID == r.queue.spanID {
				row.SourceID = strings.Clone(c.ReplyEntryID)
				r.queue.replyRowID = row.ID
			}
		}
	case "delivered":
		if c.PreviousUserEntryID != r.queue.sourceUserID {
			return false
		}
		if c.PrecedingReplyEntryID != "" && c.PrecedingReplyEntryID == r.queue.replyID {
			r.finishQueueReplyLocked()
		}
		r.queue.sourceUserID = c.UserEntryID
		r.queue.spanID = c.SpanID
		r.queue.replyID = ""
		r.queue.replyRowID = ""
		// The exact persisted delivery alone creates a new visible user. Queue list
		// disappearance, enqueue acceptance and text equality never do this.
		r.addMessage(RuntimeMessage{Role: "user", SourceID: strings.Clone(c.UserEntryID), Text: c.Text})
		r.assistant = -1
		r.plan = -1
		r.assistantHasPlan = false
		r.pendingRegenerateReplyID = ""
	}
	return true
}

func (r *liveRuntime) finishQueueReplyLocked() {
	if r.queue.replyID == "" || r.queue.replyRowID == "" || r.assistantHasPlan {
		return
	}
	for i := range r.snapshot.Messages {
		row := &r.snapshot.Messages[i]
		if row.ID == r.queue.replyRowID && row.SourceID == r.queue.replyID && row.Role == "assistant" && !row.Truncated && strings.TrimSpace(row.Text) != "" && len(row.Tools) == 0 {
			row.CanRegenerate = true
			return
		}
	}
}

// Called at every public snapshot commit so Stop, failure and admission races
// cannot leave stale enqueue authority. A queue token is minted only after both
// correlated RPC admission and an exact, durable root_admitted event.
func (r *liveRuntime) refreshQueueLocked() {
	q := r.queue.control
	if q == nil || !r.queueSupported {
		return
	}
	bound := q.SessionID == r.snapshot.SessionID && q.TurnID == r.turnID
	admitted := bound && r.promptID != "" && r.queue.sourceUserID != ""
	if r.queue.token == "" && admitted {
		r.queue.token = rand.Text()
	}
	active := admitted && r.queueActiveLocked()
	switch r.snapshot.Status {
	case "running", "permission", "input":
	default:
		active = false
	}
	count, bytes := r.steerPendingUsageLocked()
	for _, items := range [][]protocol.QueueControlItem{q.Items, q.ReviewItems} {
		count += len(items)
		for _, item := range items {
			bytes += len(item.Text)
		}
	}
	result := &RuntimeQueue{Token: r.queue.token, Revision: r.queue.viewRevision, CanEnqueue: active && count < protocol.RPCQueueMaxItems && bytes < protocol.RPCQueueMaxTotalBytes, Items: make([]RuntimeQueueItem, 0, len(q.Items)+len(q.ReviewItems))}
	for _, items := range [][]protocol.QueueControlItem{q.Items, q.ReviewItems} {
		for _, item := range items {
			if r.queue.dismissed[item.ID] {
				continue
			}
			state := item.State
			switch state {
			case "delivering":
				state = "starting"
			case "delivery_unknown":
				state = "uncertain"
			}
			if (r.snapshot.Status == "failed" || r.snapshot.Status == "closing" || r.ctx.Err() != nil) && (state == "pending" || state == "starting") {
				state = "uncertain"
			}
			result.Items = append(result.Items, RuntimeQueueItem{ID: item.ID, Text: item.Text, State: state})
		}
	}
	r.snapshot.Queue = result
}

func (r *liveRuntime) queueSourceSpanLocked() string {
	if r.queue.sourceUserID != "" && r.queue.spanID != "" && r.queue.control != nil && r.queue.control.TurnID == r.turnID {
		return r.queue.spanID
	}
	return ""
}

func queueItemIndex(q *protocol.QueueControl, id string) int {
	return slices.IndexFunc(q.Items, func(item protocol.QueueControlItem) bool { return item.ID == id })
}
