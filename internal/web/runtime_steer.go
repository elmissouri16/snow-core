package web

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeSteerACK proves native acceptance only. Private root IDs and epochs
// never leave this adapter. Token and RequestID bind the browser's exact intent.
type RuntimeSteerACK struct {
	Token     string `json:"live_steer_token"`
	RequestID string `json:"request_id"`
	ItemID    string `json:"item_id"`
	Status    string `json:"status"`
}

type RuntimeSteer struct {
	Token    string             `json:"live_steer_token"`
	Revision uint64             `json:"revision"`
	CanSteer bool               `json:"can_steer"`
	Items    []RuntimeSteerItem `json:"items"`
}

type RuntimeSteerItem struct {
	RequestID string `json:"request_id"`
	ItemID    string `json:"item_id,omitempty"`
	Text      string `json:"text"`
	Status    string `json:"status"` // accepted, delivered, discarded, uncertain
}

func (s *RuntimeSteer) clone() *RuntimeSteer {
	if s == nil {
		return nil
	}
	out := *s
	out.Items = slices.Clone(s.Items)
	return &out
}

func validSteerRequestID(id string) bool {
	return id != "" && len(id) <= protocol.RPCManagedSteerMaxIDBytes && strings.TrimSpace(id) == id && runtimeOption(id) && !strings.ContainsAny(id, "\r\n\t")
}

// SteerCurrentRun owns one bounded RPC mutation. Cancellation of the HTTP
// waiter never retries or revokes an already dispatched native operation.
func (m *RuntimeManager) SteerCurrentRun(ctx context.Context, projectID, instanceID, sessionID, token string, revision uint64, requestID, text string) (RuntimeSnapshot, error) {
	if ctx.Err() != nil || sessionID == "" || !runtimeIdentifier(sessionID) || token == "" || !runtimeOption(token) || revision == 0 || !validSteerRequestID(requestID) || !validMessageEditText(text) {
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
	r.mu.Lock()
	r.refreshSteerLocked()
	if !r.steerSupported || sessionID != r.snapshot.SessionID || token != r.steer.token || revision != r.steer.revision {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	if !r.steerActiveLocked() {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeBusy
	}
	if slices.ContainsFunc(r.steer.records, func(i runtimeSteerRecord) bool { return i.RequestID == requestID }) || !r.steerCapacityLocked(text) {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	p := protocol.RPCManagedSteerParams{SessionID: sessionID, TurnID: r.turnID, RootEpoch: r.rootEpoch, RequestID: requestID, Text: text}
	record := runtimeSteerRecord{RuntimeSteerItem: RuntimeSteerItem{RequestID: requestID, Text: text, Status: "uncertain"}, token: token, session: sessionID, turn: p.TurnID, epoch: p.RootEpoch, pending: true}
	r.trimSteerHistoryLocked(len(text))
	r.steer.records = append(r.steer.records, record)
	r.steer.revision++
	r.publishLocked()
	r.mu.Unlock()
	data, _ := json.Marshal(p)
	callCtx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	response, err := r.worker.Client.Call(callCtx, protocol.RPCRequest{Type: "managed_steer", Params: data})
	if err != nil {
		r.finishSteerUnknown(requestID, token)
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	if !response.Success {
		if response.ErrorCode == protocol.RPCManagedSteerRejectedErrorCode || response.ErrorCode == protocol.RPCManagedSteerStaleErrorCode {
			r.mu.Lock()
			r.steer.records = slices.DeleteFunc(r.steer.records, func(i runtimeSteerRecord) bool { return i.RequestID == requestID && i.token == token })
			r.steer.revision++
			r.publishLocked()
			r.mu.Unlock()
			return RuntimeSnapshot{}, ErrRuntimeInvalid
		}
		r.finishSteerUnknown(requestID, token)
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	var ack protocol.RPCManagedSteerResult
	data, err = json.Marshal(response.Data)
	if err != nil || json.Unmarshal(data, &ack) != nil || ack.SessionID != p.SessionID || ack.TurnID != p.TurnID || ack.RootEpoch != p.RootEpoch || ack.RequestID != requestID || ack.Status != "accepted" || !validSteerRequestID(ack.ItemID) {
		r.finishSteerUnknown(requestID, token)
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.reconcileSteerACKLocked(ack, token) {
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	r.publishLocked()
	result := r.snapshot.clone()
	result.SteerACK = &RuntimeSteerACK{Token: token, RequestID: requestID, ItemID: ack.ItemID, Status: "accepted"}
	return result, nil
}

func (r *liveRuntime) finishSteerUnknown(requestID, token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.steer.records {
		item := &r.steer.records[i]
		if item.RequestID == requestID && item.token == token {
			item.pending = false
		}
	}
	r.steer.revision++
	r.publishLocked()
}

var _ RuntimeSteerBackend = (*RuntimeManager)(nil)
