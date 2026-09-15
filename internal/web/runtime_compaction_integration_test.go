package web

import (
	"testing"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCompactionAndSteerSnapshotClonesOwnTheirReceipts(t *testing.T) {
	s := RuntimeSnapshot{Compaction: &RuntimeCompaction{State: "running"}, CompactionACK: new(compactionAccepted()), Steer: &RuntimeSteer{Items: []RuntimeSteerItem{{RequestID: "request", Status: "accepted"}}}, SteerACK: &RuntimeSteerACK{RequestID: "request", Status: "accepted"}}
	clone := s.clone()
	clone.Compaction.State = "changed"
	clone.CompactionACK.TurnID = "changed"
	clone.Steer.Items[0].Status = "changed"
	clone.SteerACK.Status = "changed"
	if s.Compaction.State != "running" || s.CompactionACK.TurnID != "compact-one" || s.Steer.Items[0].Status != "accepted" || s.SteerACK.Status != "accepted" {
		t.Fatal("snapshot retained a mutable projection or receipt alias")
	}
}

func TestCompactionGenericEventPipelineCannotReleaseOwnership(t *testing.T) {
	r := compactionProjectionRuntime(t)
	compactionACK(t, r)
	r.consumeEvent(compactionProgress())
	r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "compact-one", TurnOrigin: "compact", RootEpoch: 1, TurnSequence: 1}})
	r.consumeEvent(clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{RequestID: "42", Status: protocol.RPCPromptCompletedStatus}})
	if !r.busy || !r.compaction.active || r.snapshot.Status != "running" || !r.snapshot.Compaction.ProgressDone {
		t.Fatal("generic event pipeline bypassed captured manual ownership")
	}
	r.cancelTaskToken = "stop"
	r.snapshot.CancelRequested = true
	r.compaction.completion = compactionComplete("canceled").CompactionCompleted
	r.compaction.refreshed = true
	r.retireTurnCancel("instance", "session", "stop")
	if r.busy || r.compaction.active || r.snapshot.Status != "idle" || r.snapshot.Compaction.State != "canceled" || r.snapshot.CancelRequested {
		t.Fatal("cancel task retirement did not settle refreshed completion")
	}
}

func TestCompactionFailHookMarksUnknownWithoutOverwritingTerminal(t *testing.T) {
	r := compactionProjectionRuntime(t)
	r.fail()
	if r.snapshot.Status != "failed" || r.snapshot.Compaction.State != "uncertain" {
		t.Fatal("worker failure retained pending success appearance")
	}
	r = compactionProjectionRuntime(t)
	compactionACK(t, r)
	r.compaction.completion = compactionComplete("noop").CompactionCompleted
	r.compaction.refreshed = true
	r.finishCompactionLocked()
	r.fail()
	if r.snapshot.Status != "failed" || r.snapshot.Compaction.State != "noop" {
		t.Fatal("late worker failure rewrote completed operation")
	}
}
