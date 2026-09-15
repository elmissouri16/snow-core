package web

import (
	"encoding/json/v2"
	"strings"
	"testing"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func compactionProjectionRuntime(t *testing.T) *liveRuntime {
	t.Helper()
	return &liveRuntime{ctx: t.Context(), cancel: func() {}, instanceID: "instance", busy: true, assistant: -1, plan: -1, compaction: runtimeCompactionState{pending: true}, snapshot: RuntimeSnapshot{ProjectID: "project", InstanceID: "instance", SessionID: "session", Status: "running", CancelToken: "stop", Compaction: &RuntimeCompaction{State: "pending", SessionID: "session", BranchID: "main", ExpectedTipID: "tip"}}}
}
func compactionAccepted() protocol.RPCCompactionAccepted {
	return protocol.RPCCompactionAccepted{CompactionID: "compact-one", SessionID: "session", BranchID: "main", TurnID: "compact-one", TurnOrigin: "compact", RootEpoch: 1, TurnSequence: 1}
}
func compactionACK(t *testing.T, r *liveRuntime) RuntimeSnapshot {
	t.Helper()
	s, err := r.compactionAcknowledged(protocol.RPCResponse{ID: "42", Success: true, Data: compactionAccepted()}, protocol.RPCCompactionStartParams{SessionID: "session", BranchID: "main", ExpectedTipID: "tip"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func compactionProgress() clientrpc.Event {
	return clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: "compact-one", TurnOrigin: "compact", RootEpoch: 1, TurnSequence: 1, Text: "PRIVATE-NATIVE-TEXT", Compaction: &protocol.CompactionResult{Summary: "PRIVATE-SUMMARY", SummarizedMessages: 20, RetainedMessages: 8}}}
}
func compactionComplete(status string) clientrpc.Event {
	return clientrpc.Event{CompactionCompleted: &protocol.RPCCompactionCompleted{Type: protocol.RPCTypeCompactionCompleted, RequestID: "42", CompactionID: "compact-one", SessionID: "session", BranchID: "main", TurnID: "compact-one", TurnOrigin: "compact", RootEpoch: 1, TurnSequence: 1, Status: status, SummarizedMessages: 20, RetainedMessages: 8, UsedFallback: status == "fallback"}}
}
func compactionBuffer(t *testing.T, r *liveRuntime, e clientrpc.Event) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	consumed, valid := r.bufferCompactionEventLocked(e)
	if !consumed || !valid {
		t.Fatalf("buffer: consumed=%v valid=%v", consumed, valid)
	}
}

func TestCompactionACKOrderingProgressNeverCompletes(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(map[bool]string{false: "ACK-first", true: "progress-first"}[early], func(t *testing.T) {
			r := compactionProjectionRuntime(t)
			if early {
				compactionBuffer(t, r, compactionProgress())
			}
			receipt := compactionACK(t, r)
			if !early {
				r.consumeCompactionEvent(compactionProgress())
			}
			r.consumeCompactionEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "compact-one", TurnOrigin: "compact", RootEpoch: 1, TurnSequence: 1}})
			r.consumeCompactionEvent(clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{RequestID: "42", Status: protocol.RPCPromptCompletedStatus}})
			if !r.busy || !r.compaction.active || !r.snapshot.Compaction.ProgressDone || r.snapshot.Compaction.State != "running" || !r.turnCancelableLocked("stop") {
				t.Fatal("native progress/generic completion released manual operation")
			}
			if len(r.snapshot.Messages) != 0 {
				t.Fatal("compaction fabricated user/assistant text")
			}
			if receipt.CompactionACK == nil || r.snapshot.CompactionACK != nil {
				t.Fatal("receipt not clone-only")
			}
			receipt.CompactionACK.CompactionID = "changed"
			receipt.Compaction.State = "changed"
			if r.compaction.accepted.CompactionID != "compact-one" || r.snapshot.Compaction.State != "running" {
				t.Fatal("receipt aliases live state")
			}
		})
	}
}

func TestCompactionExactRootAndRequestFence(t *testing.T) {
	r := compactionProjectionRuntime(t)
	compactionACK(t, r)
	mutations := []func(*protocol.RPCCompactionCompleted){func(c *protocol.RPCCompactionCompleted) { c.RequestID = "foreign" }, func(c *protocol.RPCCompactionCompleted) { c.CompactionID = "foreign" }, func(c *protocol.RPCCompactionCompleted) { c.SessionID = "foreign" }, func(c *protocol.RPCCompactionCompleted) { c.BranchID = "foreign" }, func(c *protocol.RPCCompactionCompleted) { c.TurnID = "foreign" }, func(c *protocol.RPCCompactionCompleted) { c.TurnOrigin = "prompt" }, func(c *protocol.RPCCompactionCompleted) { c.RootEpoch++ }, func(c *protocol.RPCCompactionCompleted) { c.TurnSequence++ }}
	for _, mutate := range mutations {
		e := compactionComplete("completed")
		mutate(e.CompactionCompleted)
		r.consumeCompactionEvent(e)
	}
	for _, mutate := range []func(*protocol.AgentEvent){func(e *protocol.AgentEvent) { e.GoalRunID = "goal" }, func(e *protocol.AgentEvent) { e.TurnOrigin = "prompt" }, func(e *protocol.AgentEvent) { e.TurnID = "foreign" }, func(e *protocol.AgentEvent) { e.RootEpoch++ }, func(e *protocol.AgentEvent) { e.TurnSequence++ }, func(e *protocol.AgentEvent) {
		e.Agent = &protocol.AgentRef{ThreadID: "child", Path: "/root/child", ParentPath: "/root", Depth: 1}
	}} {
		e := compactionProgress()
		mutate(e.AgentEvent)
		r.consumeCompactionEvent(e)
	}
	if r.compaction.completion != nil || r.snapshot.Compaction.ProgressDone || !r.busy {
		t.Fatal("foreign root affected captured operation")
	}
}

func TestCompactionCompletionRequiresRefreshAndCancelRetirement(t *testing.T) {
	for _, status := range []string{"completed", "noop", "fallback", "canceled", "failed"} {
		t.Run(status, func(t *testing.T) {
			r := compactionProjectionRuntime(t)
			compactionACK(t, r)
			r.compaction.completion = compactionComplete(status).CompactionCompleted
			r.finishCompactionLocked()
			if !r.busy {
				t.Fatal("released before authoritative refresh")
			}
			r.compaction.refreshed = true
			r.snapshot.CancelRequested = true
			r.cancelTaskToken = "stop"
			r.finishCompactionLocked()
			if !r.busy {
				t.Fatal("released before cancel retirement")
			}
			r.cancelTaskToken = ""
			r.finishCompactionLocked()
			if r.busy || r.snapshot.Status != "idle" || r.snapshot.Compaction.State != status || r.snapshot.CancelRequested || r.snapshot.CancelToken != "" {
				t.Fatalf("terminal projection: %+v", r.snapshot)
			}
			if status == "failed" && r.snapshot.Error == "" {
				t.Fatal("failure lost")
			}
			r.uncertainCompactionLocked()
			if r.snapshot.Compaction.State != status {
				t.Fatal("late EOF rewrote finished operation")
			}
		})
	}
}

func TestCompactionFailedClosingCannotReviveOnLateACKOrCompletion(t *testing.T) {
	for _, status := range []string{"failed", "closing"} {
		t.Run(status, func(t *testing.T) {
			r := compactionProjectionRuntime(t)
			r.snapshot.Status = status
			r.uncertainCompactionLocked()
			_, err := r.compactionAcknowledged(protocol.RPCResponse{ID: "42", Success: true, Data: compactionAccepted()}, protocol.RPCCompactionStartParams{SessionID: "session", BranchID: "main"})
			if err == nil || r.snapshot.Compaction.State != "uncertain" || r.snapshot.Status != status {
				t.Fatal("late ACK revived failed runtime")
			}
		})
	}
	r := compactionProjectionRuntime(t)
	compactionACK(t, r)
	r.snapshot.Status = "failed"
	r.compaction.completion = compactionComplete("completed").CompactionCompleted
	r.compaction.refreshed = true
	r.finishCompactionLocked()
	if !r.busy || r.snapshot.Status != "failed" {
		t.Fatal("late completion revived failed runtime")
	}
}

func TestCompactionPreACKPrivacyAndBounds(t *testing.T) {
	r := compactionProjectionRuntime(t)
	e := compactionProgress()
	compactionBuffer(t, r, e)
	e.AgentEvent.Compaction.SummarizedMessages = 999
	compactionBuffer(t, r, clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTextDelta, TurnOrigin: "compact", Text: strings.Repeat("PRIVATE", 100000)}})
	compactionBuffer(t, r, compactionComplete("fallback"))
	data, err := json.Marshal(r.compaction.events)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PRIVATE") || len(r.compaction.events) != 2 || r.compaction.events[0].AgentEvent.Compaction.SummarizedMessages != 20 {
		t.Fatal("private/aliased buffer")
	}
	for len(r.compaction.events) < compactionEventCount {
		compactionBuffer(t, r, compactionProgress())
	}
	if _, valid := r.bufferCompactionEventLocked(compactionProgress()); valid {
		t.Fatal("unbounded preACK event buffer")
	}
	r = compactionProjectionRuntime(t)
	r.compaction.bytes = compactionEventBytes
	if _, valid := r.bufferCompactionEventLocked(compactionProgress()); valid {
		t.Fatal("unbounded preACK byte buffer")
	}
	for _, n := range []int{-1, compactionPublicCountMax + 1} {
		e := compactionProgress()
		e.AgentEvent.Compaction.RetainedMessages = n
		if _, _, valid := publicCompactionEvent(e); valid {
			t.Fatal("invalid counts retained")
		}
	}
}

func TestCompactionReservationCASAndPlanMode(t *testing.T) {
	fresh := func() *liveRuntime {
		r := compactionProjectionRuntime(t)
		r.busy = false
		r.compaction = runtimeCompactionState{}
		r.snapshot.Status = "idle"
		r.snapshot.Revision = 7
		r.snapshot.Mode = "plan"
		r.snapshot.Goal = &RuntimeGoal{SessionID: "session", BranchID: "main", TipID: "tip", Status: "none"}
		return r
	}
	input := RuntimeCompactionInput{SessionID: "session", BranchID: "main", ExpectedTipID: "tip", ExpectedRevision: 7}
	for _, mutate := range []func(*RuntimeCompactionInput){func(i *RuntimeCompactionInput) { i.ExpectedRevision = 0 }, func(i *RuntimeCompactionInput) { i.ExpectedRevision = 6 }, func(i *RuntimeCompactionInput) { i.SessionID = "foreign" }, func(i *RuntimeCompactionInput) { i.BranchID = "foreign" }, func(i *RuntimeCompactionInput) { i.ExpectedTipID = "foreign" }} {
		r := fresh()
		p := input
		mutate(&p)
		if r.reserveCompaction(p) == nil || r.busy {
			t.Fatal("stale admission reserved")
		}
	}
	for _, mutate := range []func(*liveRuntime){func(r *liveRuntime) { r.snapshot.Queue = &RuntimeQueue{Items: []RuntimeQueueItem{{ID: "held"}}} }, func(r *liveRuntime) { r.snapshot.Goal.GoalID = "goal"; r.snapshot.Goal.Status = "paused" }, func(r *liveRuntime) { r.goal.active = true }, func(r *liveRuntime) { r.compaction.pending = true }, func(r *liveRuntime) { r.snapshot.CancelRequested = true }, func(r *liveRuntime) { r.snapshot.Permission = &RuntimePermission{ID: "permission"} }, func(r *liveRuntime) { r.snapshot.Input = &protocol.UserInputRequest{ID: "input"} }} {
		r := fresh()
		mutate(r)
		if r.reserveCompaction(input) == nil {
			t.Fatal("busy admission reserved")
		}
	}
	r := fresh()
	if err := r.reserveCompaction(input); err != nil {
		t.Fatal(err)
	}
	if !r.busy || r.transitioning || !r.compaction.pending || r.snapshot.CancelToken == "" || r.snapshot.Mode != "plan" || len(r.snapshot.Messages) != 0 {
		t.Fatal("Plan reservation changed mode/history or lost Stop")
	}
}
