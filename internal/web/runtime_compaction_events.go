package web

import (
	"encoding/json/v2"
	"strings"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const compactionEventCount = 64
const compactionEventBytes = 32 << 10
const compactionPublicCountMax = 1 << 30

func compactionCountsValid(summarized, retained int) bool {
	return summarized >= 0 && retained >= 0 && summarized <= compactionPublicCountMax && retained <= compactionPublicCountMax
}
func compactionStatusValid(status string) bool {
	switch status {
	case "completed", "noop", "fallback", "canceled", "failed":
		return true
	}
	return false
}

// Never retain native Summary, opaque provider state, errors, or arbitrary text.
func publicCompactionEvent(event clientrpc.Event) (clientrpc.Event, bool, bool) {
	if c := event.CompactionCompleted; c != nil {
		if c.RequestID == "" || !runtimeOption(c.RequestID) || !runtimeIdentifier(c.SessionID) || c.BranchID == "" || !runtimeOption(c.BranchID) || !validCompactionAccepted(protocol.RPCCompactionAccepted{CompactionID: c.CompactionID, SessionID: c.SessionID, BranchID: c.BranchID, TurnID: c.TurnID, TurnOrigin: c.TurnOrigin, RootEpoch: c.RootEpoch, TurnSequence: c.TurnSequence}, protocol.RPCCompactionStartParams{SessionID: c.SessionID, BranchID: c.BranchID}) || !compactionStatusValid(c.Status) || !compactionCountsValid(c.SummarizedMessages, c.RetainedMessages) {
			return clientrpc.Event{}, false, false
		}
		out := &protocol.RPCCompactionCompleted{Type: protocol.RPCTypeCompactionCompleted, RequestID: strings.Clone(c.RequestID), CompactionID: strings.Clone(c.CompactionID), SessionID: strings.Clone(c.SessionID), BranchID: strings.Clone(c.BranchID), TurnID: strings.Clone(c.TurnID), TurnOrigin: "compact", RootEpoch: c.RootEpoch, TurnSequence: c.TurnSequence, Status: c.Status, SummarizedMessages: c.SummarizedMessages, RetainedMessages: c.RetainedMessages, UsedFallback: c.UsedFallback}
		return clientrpc.Event{CompactionCompleted: out}, true, true
	}
	e := event.AgentEvent
	if e == nil || !goalRootEvent(*e) || e.GoalRunID != "" || e.TurnOrigin != "compact" || (e.Type != protocol.EvCompactionStarted && e.Type != protocol.EvCompactionDone) {
		return clientrpc.Event{}, false, true
	}
	if e.TurnID == "" || !runtimeOption(e.TurnID) || e.RootEpoch == 0 || e.TurnSequence == 0 {
		return clientrpc.Event{}, false, false
	}
	out := &protocol.AgentEvent{Type: e.Type, TurnID: strings.Clone(e.TurnID), TurnOrigin: "compact", RootEpoch: e.RootEpoch, TurnSequence: e.TurnSequence}
	if e.Compaction != nil {
		c := e.Compaction
		if !compactionCountsValid(c.SummarizedMessages, c.RetainedMessages) {
			return clientrpc.Event{}, false, false
		}
		out.Compaction = &protocol.CompactionResult{SummarizedMessages: c.SummarizedMessages, RetainedMessages: c.RetainedMessages, UsedFallback: c.UsedFallback}
	}
	return clientrpc.Event{AgentEvent: out}, true, true
}

// Caller holds eventMu and mu. Everything else is dropped during pre-ACK
// ownership, so unrelated root events cannot bind or settle this operation.
func (r *liveRuntime) bufferCompactionEventLocked(event clientrpc.Event) (bool, bool) {
	if !r.compaction.pending {
		return false, true
	}
	public, keep, valid := publicCompactionEvent(event)
	if !valid {
		return true, false
	}
	if !keep {
		return true, true
	}
	data, err := json.Marshal(public)
	if err != nil || len(r.compaction.events) >= compactionEventCount || len(data) > compactionEventBytes-r.compaction.bytes {
		return true, false
	}
	// publicCompactionEvent has reconstructed and cloned every retained field.
	r.compaction.events = append(r.compaction.events, public)
	r.compaction.bytes += len(data)
	return true, true
}

// Called with eventMu held. Native progress and generic turn_done never release
// manual ownership; only the exact captured completion starts the final refresh.
func (r *liveRuntime) consumeCompactionEvent(event clientrpc.Event) bool {
	r.mu.Lock()
	if !r.compaction.active {
		owned := r.compaction.pending || event.CompactionCompleted != nil || event.AgentEvent != nil && event.AgentEvent.TurnOrigin == "compact"
		r.mu.Unlock()
		return owned
	}
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" || r.compaction.completion != nil {
		r.mu.Unlock()
		return true
	}
	a := r.compaction.accepted
	if c := event.CompactionCompleted; c != nil {
		if c.RequestID != r.compaction.requestID || c.CompactionID != a.CompactionID || c.SessionID != a.SessionID || c.BranchID != a.BranchID || c.TurnID != a.TurnID || c.TurnOrigin != a.TurnOrigin || c.RootEpoch != a.RootEpoch || c.TurnSequence != a.TurnSequence {
			r.mu.Unlock()
			return true
		}
		public, _, valid := publicCompactionEvent(event)
		if !valid {
			r.mu.Unlock()
			r.fail()
			return true
		}
		r.compaction.completion = public.CompactionCompleted
		r.mu.Unlock()
		if err := r.refreshCompaction(); err != nil {
			r.fail()
			return true
		}
		r.mu.Lock()
		if r.compaction.active && r.compaction.accepted == a && r.ctx.Err() == nil && r.snapshot.Status != "failed" && r.snapshot.Status != "closing" {
			r.compaction.refreshed = true
			r.finishCompactionLocked()
			r.publishLocked()
		}
		r.mu.Unlock()
		return true
	}
	e := event.AgentEvent
	if e == nil || e.TurnID != a.TurnID || e.TurnOrigin != a.TurnOrigin || e.RootEpoch != a.RootEpoch || e.TurnSequence != a.TurnSequence || e.GoalRunID != "" || !goalRootEvent(*e) {
		r.mu.Unlock()
		return true
	}
	public, keep, valid := publicCompactionEvent(event)
	if !valid {
		r.mu.Unlock()
		r.fail()
		return true
	}
	if keep && public.AgentEvent.Type == protocol.EvCompactionDone {
		c := r.snapshot.Compaction
		c.ProgressDone = true
		if p := public.AgentEvent.Compaction; p != nil {
			c.SummarizedMessages, c.RetainedMessages, c.UsedFallback = p.SummarizedMessages, p.RetainedMessages, p.UsedFallback
		}
		r.publishLocked()
	}
	r.mu.Unlock()
	return true
}

// Core has finished cleanup. Keep shared admission reserved through these
// authoritative read-only refreshes. Exact history is not a provider transcript.
func (r *liveRuntime) refreshCompaction() error {
	var page protocol.RPCMessagesPage
	if err := r.call(protocol.RPCRequest{Type: "messages_page"}, protocol.RPCMessagesPageParams{Limit: runtimeHistoryCount, MaxBytes: runtimeHistoryBytes, PublicHistory: true}, &page); err != nil {
		return err
	}
	if page.HistoryTools == nil {
		page.HistoryTools = make(map[string][]protocol.RPCHistoryTool)
	}
	r.mu.Lock()
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		r.mu.Unlock()
		return ErrRuntimeUnavailable
	}
	r.snapshot.Messages = nil
	r.assistant, r.plan = -1, -1
	r.projectHistory(page)
	r.publishLocked()
	r.mu.Unlock()
	if err := r.refreshGoal(); err != nil {
		return err
	}
	return r.refreshTelemetry()
}

// Called with mu held both on terminal refresh and cancellation task retirement.
func (r *liveRuntime) finishCompactionLocked() {
	c := r.compaction.completion
	if !r.compaction.active || c == nil || !r.compaction.refreshed || r.cancelTaskToken != "" || r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		return
	}
	r.compaction.active = false
	r.busy, r.transitioning = false, false
	if r.permissionAgent == nil {
		r.clearPermissionLocked()
	}
	r.snapshot.Input = nil
	if r.snapshot.Permission != nil {
		r.snapshot.Status = "permission"
	} else {
		r.snapshot.Status = "idle"
	}
	r.clearTurnCancelLocked()
	out := r.snapshot.Compaction
	out.State, out.SummarizedMessages, out.RetainedMessages, out.UsedFallback = c.Status, c.SummarizedMessages, c.RetainedMessages, c.UsedFallback
	if c.Status == "failed" {
		r.snapshot.Error = "Manual compaction failed. The saved conversation and usage were refreshed; nothing will be retried automatically."
	}
}
