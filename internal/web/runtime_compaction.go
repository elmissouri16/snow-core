package web

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"time"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeCompaction retains only bounded public counts and captured authority.
// Progress is deliberately distinct from terminal handle completion.
type RuntimeCompaction struct {
	State              string `json:"state"`
	RequestID          string `json:"request_id"`
	CompactionID       string `json:"compaction_id"`
	SessionID          string `json:"session_id"`
	BranchID           string `json:"branch_id"`
	ExpectedTipID      string `json:"expected_tip_id"`
	TurnID             string `json:"turn_id"`
	TurnOrigin         string `json:"turn_origin"`
	RootEpoch          uint64 `json:"root_epoch"`
	TurnSequence       uint64 `json:"turn_sequence"`
	ProgressDone       bool   `json:"progress_done"`
	SummarizedMessages int    `json:"summarized_messages"`
	RetainedMessages   int    `json:"retained_messages"`
	UsedFallback       bool   `json:"used_fallback"`
}

func (c *RuntimeCompaction) clone() *RuntimeCompaction {
	if c == nil {
		return nil
	}
	return new(*c)
}

// This receipt exists only on the returned HTTP clone, never on SSE snapshots.
type RuntimeCompactionACK = protocol.RPCCompactionAccepted

type RuntimeCompactionInput struct {
	protocol.RPCCompactionStartParams
	ExpectedRevision uint64
}
type runtimeCompactionState struct {
	pending, active, refreshed bool
	accepted                   protocol.RPCCompactionAccepted
	requestID                  string
	events                     []clientrpc.Event
	bytes                      int
	completion                 *protocol.RPCCompactionCompleted
}

// StartCompaction reserves the shared root before dispatching one native handle.
// Browser cancellation after reservation cannot cancel or replay the operation.
func (m *RuntimeManager) StartCompaction(ctx context.Context, projectID, instanceID string, input RuntimeCompactionInput) (RuntimeSnapshot, error) {
	p := input.RPCCompactionStartParams
	if !runtimeIdentifier(p.SessionID) || p.BranchID == "" || !runtimeOption(p.BranchID) || !runtimeOption(p.ExpectedTipID) || input.ExpectedRevision == 0 {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	defer r.control.Unlock()
	if err := r.idle(); err != nil {
		return RuntimeSnapshot{}, err
	}
	project := r.project
	project.checkIdentity()
	if !project.Available {
		return RuntimeSnapshot{}, ErrProjectInvalid
	}
	if !r.supports("compaction_run") || !r.supports("messages_public_history") || !r.supports("goal_run") {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	if err := r.reserveCompaction(input); err != nil {
		return RuntimeSnapshot{}, err
	}
	data, _ := json.Marshal(p)
	callCtx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	response, callErr := r.worker.Client.Call(callCtx, protocol.RPCRequest{Type: "compaction_start", Params: data})
	cancel()
	if callErr != nil {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	return r.compactionAcknowledged(response, p)
}

// reserveCompaction is called with the shared control gate held.
func (r *liveRuntime) reserveCompaction(input RuntimeCompactionInput) error {
	p := input.RPCCompactionStartParams
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snapshot.Queue != nil && len(r.snapshot.Queue.Items) != 0 {
		return ErrRuntimeQueueReview
	}
	g := r.snapshot.Goal
	if r.compaction.pending || r.compaction.active || r.goal.pending || r.goal.active || r.snapshot.CancelRequested || r.snapshot.Permission != nil || r.snapshot.Input != nil {
		return ErrRuntimeBusy
	}
	if input.ExpectedRevision != r.snapshot.Revision || r.snapshot.SessionID != p.SessionID || g == nil || g.SessionID != p.SessionID || g.BranchID != p.BranchID || g.TipID != p.ExpectedTipID {
		return ErrRuntimeInvalid
	}
	if g.Running || g.GoalID != "" && !protocol.ThreadGoalStatus(g.Status).Terminal() {
		return ErrRuntimeBusy
	}
	if r.ctx.Err() != nil || r.snapshot.Status != "idle" {
		return ErrRuntimeUnavailable
	}
	r.compaction = runtimeCompactionState{pending: true}
	r.snapshot.Compaction = &RuntimeCompaction{State: "pending", SessionID: p.SessionID, BranchID: p.BranchID, ExpectedTipID: p.ExpectedTipID}
	r.busy, r.transitioning = true, false
	r.snapshot.Status, r.snapshot.Error = "running", ""
	r.snapshot.CancelToken, r.snapshot.CancelRequested = rand.Text(), false
	r.promptID, r.earlyCompletion, r.turnID = "", "", ""
	r.pendingUserID, r.pendingRegenerateReplyID = "", ""
	r.assistant, r.plan = -1, -1
	r.assistantHasPlan = false
	r.publishLocked()
	return nil
}

func validCompactionAccepted(a protocol.RPCCompactionAccepted, p protocol.RPCCompactionStartParams) bool {
	return a.CompactionID != "" && runtimeOption(a.CompactionID) && a.TurnID == a.CompactionID && a.SessionID == p.SessionID && a.BranchID == p.BranchID && a.TurnOrigin == "compact" && a.RootEpoch > 0 && a.TurnSequence > 0
}

func (r *liveRuntime) compactionAcknowledged(response protocol.RPCResponse, p protocol.RPCCompactionStartParams) (RuntimeSnapshot, error) {
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	if !response.Success {
		r.mu.Lock()
		definitive := response.ErrorCode == protocol.RPCCompactionRejectedErrorCode && len(r.compaction.events) == 0 && r.ctx.Err() == nil && r.snapshot.Status != "failed" && r.snapshot.Status != "closing"
		if definitive {
			r.compaction = runtimeCompactionState{}
			r.snapshot.Compaction.State = "failed"
			r.snapshot.Error = "Manual compaction was rejected. Refresh and review the current conversation."
			r.busy = false
			r.snapshot.Status = "idle"
			r.clearTurnCancelLocked()
			r.publishLocked()
		}
		r.mu.Unlock()
		if !definitive {
			r.fail()
			return RuntimeSnapshot{}, ErrRuntimeUnavailable
		}
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	var a protocol.RPCCompactionAccepted
	data, err := json.Marshal(response.Data)
	if err != nil || json.Unmarshal(data, &a) != nil || response.ID == "" || !runtimeOption(response.ID) || !validCompactionAccepted(a, p) {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	r.mu.Lock()
	if !r.compaction.pending || r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" || a.RootEpoch <= r.retiredEpoch || a.RootEpoch < r.rootEpoch || a.TurnSequence <= r.turnSequence {
		r.mu.Unlock()
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	events := r.compaction.events
	r.compaction.events, r.compaction.bytes = nil, 0
	r.compaction.pending, r.compaction.active = false, true
	r.compaction.accepted, r.compaction.requestID = a, response.ID
	c := r.snapshot.Compaction
	c.State, c.RequestID, c.CompactionID = "running", response.ID, a.CompactionID
	c.TurnID, c.TurnOrigin, c.RootEpoch, c.TurnSequence = a.TurnID, a.TurnOrigin, a.RootEpoch, a.TurnSequence
	r.turnID, r.rootEpoch, r.turnSequence = a.TurnID, a.RootEpoch, a.TurnSequence
	r.publishLocked()
	r.mu.Unlock()
	for _, event := range events {
		r.consumeCompactionEvent(event)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	result := r.snapshot.clone()
	result.CompactionACK = new(a)
	return result, nil
}

// Caller holds mu. Transport loss must not report success or revive a dead worker.
func (r *liveRuntime) uncertainCompactionLocked() {
	if (r.compaction.pending || r.compaction.active) && r.snapshot.Compaction != nil {
		r.snapshot.Compaction.State = "uncertain"
	}
}
