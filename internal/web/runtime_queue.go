package web

import (
	"context"
	"encoding/json/v2"
	"slices"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *RuntimeManager) EnqueueFollowUp(ctx context.Context, projectID, instanceID, sessionID, token string, revision uint64, text string) (RuntimeSnapshot, error) {
	return m.mutateQueue(ctx, projectID, instanceID, sessionID, token, revision, "", text, "queue_enqueue")
}
func (m *RuntimeManager) UpdateQueuedFollowUp(ctx context.Context, projectID, instanceID, sessionID, token string, revision uint64, itemID, text string) (RuntimeSnapshot, error) {
	return m.mutateQueue(ctx, projectID, instanceID, sessionID, token, revision, itemID, text, "queue_update")
}
func (m *RuntimeManager) RemoveQueuedFollowUp(ctx context.Context, projectID, instanceID, sessionID, token string, revision uint64, itemID string) (RuntimeSnapshot, error) {
	return m.mutateQueue(ctx, projectID, instanceID, sessionID, token, revision, itemID, "", "queue_remove")
}

func (r *liveRuntime) queueActiveLocked() bool {
	q := r.queue.control
	if r.compaction.pending || r.compaction.active || r.goal.pending || r.goal.active || q == nil || !q.Accepting || q.SessionID != r.snapshot.SessionID || q.TurnID != r.turnID || r.promptID == "" || r.queue.sourceUserID == "" || r.ctx.Err() != nil || !r.busy || r.transitioning || r.snapshot.CancelRequested {
		return false
	}
	switch r.snapshot.Status {
	case "running", "permission", "input":
		return true
	default:
		return false
	}
}

func (m *RuntimeManager) mutateQueue(ctx context.Context, projectID, instanceID, sessionID, token string, revision uint64, itemID, text, command string) (RuntimeSnapshot, error) {
	if ctx.Err() != nil || sessionID == "" || !runtimeIdentifier(sessionID) || token == "" || !runtimeOption(token) || (command != "queue_enqueue" && (itemID == "" || !runtimeOption(itemID))) || (command != "queue_remove" && !validMessageEditText(text)) {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	r, err := m.runtime(projectID, instanceID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	if !r.control.TryLock() {
		return RuntimeSnapshot{}, ErrRuntimeBusy
	}
	defer r.control.Unlock()
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return RuntimeSnapshot{}, err
	}
	if !r.supports("queue_next") {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	r.mu.Lock()
	if r.compaction.pending || r.compaction.active || r.goal.pending || r.goal.active {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeBusy
	}
	q := r.queue.control
	if q == nil || token != r.queue.token || revision != r.queue.viewRevision || sessionID != r.snapshot.SessionID || sessionID != q.SessionID || q.TurnID != r.turnID {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	index := queueItemIndex(q, itemID)
	review := slices.IndexFunc(q.ReviewItems, func(i protocol.QueueControlItem) bool { return i.ID == itemID })
	if r.queue.dismissed[itemID] {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	// A failed worker cannot receive controls. Explicit removal only dismisses
	// review/copy text locally; it cannot revoke or retry uncertain execution.
	if command == "queue_remove" && (r.ctx.Err() != nil || r.snapshot.Status == "failed") && (index >= 0 || review >= 0) {
		if r.queue.dismissed == nil {
			r.queue.dismissed = make(map[string]bool)
		}
		r.queue.dismissed[itemID] = true
		r.queue.viewRevision++
		r.publishLocked()
		result := r.snapshot.clone()
		r.mu.Unlock()
		return result, nil
	}
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" || r.snapshot.CancelRequested || r.transitioning {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeBusy
	}
	active := r.queueActiveLocked()
	if (command != "queue_remove" || review < 0) && !active {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeBusy
	}
	if command != "queue_enqueue" && review < 0 && (index < 0 || q.Items[index].State != "pending") {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	if command == "queue_update" && review >= 0 {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	steerCount, total := r.steerPendingUsageLocked()
	for _, items := range [][]protocol.QueueControlItem{q.Items, q.ReviewItems} {
		for _, item := range items {
			total += len(item.Text)
		}
	}
	if command == "queue_enqueue" && (len(q.Items)+len(q.ReviewItems)+steerCount >= protocol.RPCQueueMaxItems || total+len(text) > protocol.RPCQueueMaxTotalBytes) || command == "queue_update" && total-len(q.Items[index].Text)+len(text) > protocol.RPCQueueMaxTotalBytes {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	turn, coreRevision := q.TurnID, q.Revision
	r.mu.Unlock()
	var params any
	switch command {
	case "queue_enqueue":
		params = protocol.RPCQueueEnqueueParams{SessionID: sessionID, TurnID: turn, Revision: coreRevision, Text: text}
	case "queue_update":
		params = protocol.RPCQueueUpdateParams{SessionID: sessionID, TurnID: turn, Revision: coreRevision, ItemID: itemID, Text: text}
	case "queue_remove":
		params = protocol.RPCQueueRemoveParams{SessionID: sessionID, TurnID: turn, Revision: coreRevision, ItemID: itemID}
	}
	data, _ := json.Marshal(params)
	callCtx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	response, err := r.worker.Client.Call(callCtx, protocol.RPCRequest{Type: command, Params: data})
	if err != nil {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	if !response.Success {
		if response.ErrorCode == protocol.RPCQueueRejectedErrorCode || response.ErrorCode == protocol.RPCQueueStaleErrorCode {
			return RuntimeSnapshot{}, ErrRuntimeInvalid
		}
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	var result protocol.QueueControl
	data, err = json.Marshal(response.Data)
	if err != nil || json.Unmarshal(data, &result) != nil || !validQueueControl(&result) || result.TurnID != turn || result.SessionID != sessionID || !validQueueMutationAck(result, command, coreRevision, itemID, text) {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	r.eventMu.Lock()
	r.mu.Lock()
	// Never overwrite a newer event projection with a slow HTTP/RPC waiter.
	valid := r.applyQueueControlLocked(&result, false)
	snapshot := r.snapshot.clone()
	terminal := r.ctx.Err() != nil || snapshot.Status == "failed" || snapshot.Status == "closing"
	r.mu.Unlock()
	r.eventMu.Unlock()
	if !valid {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	if terminal {
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	return snapshot, nil
}

var _ RuntimeQueueNextBackend = (*RuntimeManager)(nil)

func validQueueMutationAck(q protocol.QueueControl, command string, before uint64, id, text string) bool {
	if q.Revision != before+1 {
		return false
	}
	kind := map[string]string{"queue_enqueue": "enqueued", "queue_update": "updated", "queue_remove": "removed"}[command]
	if q.Change.Kind != kind || q.Change.ItemID == "" || (command != "queue_enqueue" && q.Change.ItemID != id) {
		return false
	}
	index := queueItemIndex(&q, q.Change.ItemID)
	if command == "queue_remove" {
		return index < 0 && !slices.ContainsFunc(q.ReviewItems, func(item protocol.QueueControlItem) bool { return item.ID == id })
	}
	return index >= 0 && q.Items[index].State == "pending" && q.Items[index].Text == text
}
