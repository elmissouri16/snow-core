package web

import (
	"encoding/json/v2"
	"strings"
	"testing"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func queueProjectionRuntime(t *testing.T) (*liveRuntime, *protocol.QueueControl) {
	t.Helper()
	r := &liveRuntime{queueSupported: true, ctx: t.Context(), cancel: func() {}, busy: true, assistant: -1, plan: -1, pendingUserID: "optimistic", snapshot: RuntimeSnapshot{SessionID: "session", Status: "running", CancelToken: "stop-only-token", Recovery: RecoveryHint{State: RecoveryAdmissionUnknown}, Messages: []RuntimeMessage{{ID: "optimistic", Role: "user", Text: "same text"}}}}
	q := &protocol.QueueControl{SessionID: "session", TurnID: "root", Revision: 1, Accepting: true, Change: protocol.QueueControlChange{Kind: "root_admitted", UserEntryID: "user-original"}}
	return r, q
}
func emitQueueProjection(r *liveRuntime, q *protocol.QueueControl) {
	r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvQueueUpdated, RootEpoch: 1, TurnID: q.TurnID, QueueControl: q}})
}
func emitQueueText(r *liveRuntime, text string) {
	r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTextDelta, RootEpoch: 1, TurnSequence: 1, TurnID: "root", Text: text}})
}

func TestQueueAuthorityWaitsForDurableRootAndCorrelatedPromptACK(t *testing.T) {
	for _, earlyACK := range []bool{false, true} {
		r, q := queueProjectionRuntime(t)
		if earlyACK {
			if err := r.promptAcknowledged(protocol.RPCResponse{ID: "42", Success: true}, nil, false); err != nil {
				t.Fatal(err)
			}
			if r.snapshot.Queue != nil {
				t.Fatal("ACK without source grants queue")
			}
		}
		emitQueueProjection(r, q)
		if !earlyACK {
			if r.snapshot.Queue.Token != "" || r.snapshot.Queue.CanEnqueue {
				t.Fatal("unacknowledged root grants queue")
			}
			if err := r.promptAcknowledged(protocol.RPCResponse{ID: "42", Success: true}, nil, false); err != nil {
				t.Fatal(err)
			}
		}
		if r.snapshot.Queue.Token == "" || r.snapshot.Queue.Token == r.snapshot.CancelToken || !r.snapshot.Queue.CanEnqueue || r.snapshot.Messages[0].SourceID != "user-original" {
			t.Fatalf("root capability: %+v", r.snapshot)
		}
		before := r.snapshot.Queue.Token
		r.snapshot.CancelRequested = true
		r.publishLocked()
		if r.snapshot.Queue.CanEnqueue {
			t.Fatal("Stop leaves queue authority")
		}
		r.snapshot.CancelRequested = false
		r.snapshot.Status = "permission"
		r.publishLocked()
		if !r.snapshot.Queue.CanEnqueue {
			t.Fatal("permission gate unnecessarily disables safe explicit queue")
		}
		r.snapshot.Status = "input"
		r.publishLocked()
		if !r.snapshot.Queue.CanEnqueue {
			t.Fatal("question gate unnecessarily disables safe explicit queue")
		}
		r.snapshot.Status = "idle"
		r.busy = false
		r.publishLocked()
		if r.snapshot.Queue.CanEnqueue || r.snapshot.Queue.Token != before {
			t.Fatal("idle root grants execution or drops review authority")
		}
	}
	r, q := queueProjectionRuntime(t)
	r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvQueueUpdated, TurnID: "root", QueueControl: q}})
	if r.snapshot.Queue != nil {
		t.Fatal("untagged root grants capability")
	}
	r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvQueueUpdated, RootEpoch: 1, TurnID: "root", QueueControl: q, Agent: &protocol.AgentRef{ThreadID: "child", Path: "/root/child", Depth: 1, ParentPath: "/root"}}})
	if r.snapshot.Queue != nil {
		t.Fatal("child grants root queue")
	}
}

func TestQueueDeliveryOnlyCreatesExactSourcesAndACKCannotHideIt(t *testing.T) {
	r, q := queueProjectionRuntime(t)
	emitQueueProjection(r, q)
	if err := r.promptAcknowledged(protocol.RPCResponse{ID: "42", Success: true}, nil, false); err != nil {
		t.Fatal(err)
	}
	emitQueueText(r, "same answer")
	q = q.Clone()
	q.Revision = 2
	q.Change = protocol.QueueControlChange{Kind: "reply", UserEntryID: "user-original", ReplyEntryID: "answer-original"}
	emitQueueProjection(r, q)
	q = q.Clone()
	q.Revision = 3
	q.Items = []protocol.QueueControlItem{{ID: "queued-1", Text: "same text", State: "pending"}}
	q.Change = protocol.QueueControlChange{Kind: "enqueued", ItemID: "queued-1"}
	emitQueueProjection(r, q)
	if len(r.snapshot.Messages) != 2 {
		t.Fatal("enqueue creates optimistic user")
	}
	// A later remove/enqueue ACK has already arrived, before the older delivery
	// event is drained. It can replace pending state, never consume source events.
	future := q.Clone()
	future.Revision = 6
	future.Items = []protocol.QueueControlItem{{ID: "queued-2", Text: "next", State: "pending"}}
	future.Change = protocol.QueueControlChange{Kind: "enqueued", ItemID: "queued-2"}
	if !r.applyQueueControlLocked(future, false) {
		t.Fatal("future ACK rejected")
	}
	delivered := q.Clone()
	delivered.Revision = 5
	delivered.Items = nil
	delivered.Change = protocol.QueueControlChange{Kind: "delivered", ItemID: "queued-1", UserEntryID: "user-queued", PreviousUserEntryID: "user-original", PrecedingReplyEntryID: "answer-original", SpanID: "input-span", Text: "same text"}
	emitQueueProjection(r, delivered)
	if len(r.snapshot.Messages) != 3 || r.snapshot.Messages[2].SourceID != "user-queued" || !r.snapshot.Messages[2].CanEdit || !r.snapshot.Messages[1].CanRegenerate || r.snapshot.Messages[1].SourceID != "answer-original" {
		t.Fatalf("exact source delivery lost: %+v", r.snapshot.Messages)
	}
	if len(r.snapshot.Queue.Items) != 1 || r.snapshot.Queue.Items[0].ID != "queued-2" {
		t.Fatal("old source event overwrote newer ACK queue")
	}
	emitQueueProjection(r, delivered)
	if len(r.snapshot.Messages) != 3 {
		t.Fatal("duplicate durable delivery duplicates user")
	}
	emitQueueText(r, "same answer")
	if r.snapshot.Messages[3].SourceTurnID != "root" || r.snapshot.Messages[3].SourceSpanID != "input-span" {
		t.Fatal("queued assistant inherited original root source")
	}
	reply := future.Clone()
	reply.Revision = 7
	reply.Change = protocol.QueueControlChange{Kind: "reply", UserEntryID: "user-queued", ReplyEntryID: "answer-queued"}
	emitQueueProjection(r, reply)
	closed := reply.Clone()
	closed.Revision = 8
	closed.Accepting = false
	closed.Items = nil
	closed.ReviewItems = []protocol.QueueControlItem{{ID: "queued-2", Text: "next", State: "held"}}
	closed.Change = protocol.QueueControlChange{Kind: "retained"}
	emitQueueProjection(r, closed)
	r.consumeEvent(clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{RequestID: "42", Status: protocol.RPCPromptCompletedStatus}})
	if !r.snapshot.Messages[3].CanRegenerate || r.snapshot.Messages[3].SourceID != "answer-queued" || r.snapshot.Queue.CanEnqueue || r.snapshot.Queue.Items[0].State != "held" {
		t.Fatalf("queued reply and held source: %+v", r.snapshot)
	}
}

func TestQueueExactEditableTextBoundsAndHeldSurviveNextRoot(t *testing.T) {
	r, q := queueProjectionRuntime(t)
	emitQueueProjection(r, q)
	_ = r.promptAcknowledged(protocol.RPCResponse{ID: "42", Success: true}, nil, false)
	original := "exact\x1b[31m\r\t text\n\u00a0"
	held := q.Clone()
	held.Revision = 2
	held.Accepting = false
	held.ReviewItems = []protocol.QueueControlItem{{ID: "held", Text: original, State: "held"}}
	held.Change = protocol.QueueControlChange{Kind: "retained"}
	emitQueueProjection(r, held)
	r.consumeEvent(clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{RequestID: "42", Status: protocol.RPCPromptCanceledStatus}})
	oldToken := r.snapshot.Queue.Token
	r.busy = true
	r.promptID = ""
	r.turnID = ""
	r.snapshot.Status = "running"
	r.publishLocked()
	if r.snapshot.Queue.Items[0].Text != original || r.snapshot.Queue.CanEnqueue {
		t.Fatal("new admission discarded/retargeted exact held text")
	}
	next := held.Clone()
	next.Revision = 3
	next.TurnID = "new-root"
	next.Accepting = true
	next.Change = protocol.QueueControlChange{Kind: "root_admitted", UserEntryID: "new-user"}
	emitQueueProjection(r, next)
	_ = r.promptAcknowledged(protocol.RPCResponse{ID: "43", Success: true}, nil, false)
	if r.snapshot.Queue.Token == oldToken || r.snapshot.Queue.Token == "" || len(r.snapshot.Queue.Items) != 1 || r.snapshot.Queue.Items[0].State != "held" || r.snapshot.Queue.Items[0].Text != original {
		t.Fatalf("held preservation: %+v", r.snapshot.Queue)
	}
	raw, _ := json.Marshal(r.snapshot.Queue)
	var copied RuntimeQueue
	if err := json.Unmarshal(raw, &copied); err != nil || copied.Items[0].Text != original {
		t.Fatal("JSON transformed editable queue text")
	}
	for _, invalid := range []string{"", "\xff", "x\x00y", strings.Repeat("x", protocol.RPCQueueMaxTextBytes+1)} {
		bad := next.Clone()
		bad.ReviewItems[0].Text = invalid
		if validQueueControl(bad) {
			t.Fatal("invalid queued text allowed")
		}
	}
	big := next.Clone()
	big.ReviewItems = nil
	big.Items = make([]protocol.QueueControlItem, 9)
	for i := range big.Items {
		big.Items[i] = protocol.QueueControlItem{ID: strings.Repeat("i", i+1), Text: "text", State: "pending"}
	}
	if validQueueControl(big) {
		t.Fatal("queue count unbounded")
	}
	big.Items = big.Items[:5]
	for i := range big.Items {
		big.Items[i].Text = strings.Repeat("x", 64<<10)
	}
	if validQueueControl(big) {
		t.Fatal("queue bytes unbounded")
	}
}

func TestQueueEmptySnapshotDoesNotMeanDeliveryAndFailureIsUncertain(t *testing.T) {
	r, q := queueProjectionRuntime(t)
	emitQueueProjection(r, q)
	_ = r.promptAcknowledged(protocol.RPCResponse{ID: "42", Success: true}, nil, false)
	q = q.Clone()
	q.Revision = 2
	q.Items = []protocol.QueueControlItem{{ID: "pending", Text: "pending text", State: "pending"}}
	q.ReviewItems = []protocol.QueueControlItem{{ID: "held", Text: "held text", State: "held"}}
	q.Change = protocol.QueueControlChange{Kind: "enqueued", ItemID: "pending"}
	emitQueueProjection(r, q)
	r.fail()
	if r.snapshot.Queue.CanEnqueue || r.snapshot.Queue.Items[0].State != "uncertain" || r.snapshot.Queue.Items[1].State != "held" {
		t.Fatalf("failure claims unsent pending or loses held certainty: %+v", r.snapshot.Queue)
	}
	empty := q.Clone()
	empty.Revision = 3
	empty.Items = nil
	empty.Change = protocol.QueueControlChange{Kind: "removed", ItemID: "pending"}
	emitQueueProjection(r, empty)
	if len(r.snapshot.Messages) != 1 {
		t.Fatal("empty queue inferred durable user")
	}
}

func TestQueueRevisionBufferPreservesPublicRootSourceEvents(t *testing.T) {
	r, q := queueProjectionRuntime(t)
	r.messageEdit.pending = true
	event := clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvQueueUpdated, TurnID: "root", RootEpoch: 1, QueueControl: q, Message: "PRIVATE", ToolOutput: "PRIVATE"}}
	if consumed, valid := r.bufferMessageEditEventLocked(event); !consumed || !valid {
		t.Fatal("valid queue source event lost from revision buffer")
	}
	data, _ := json.Marshal(r.messageEdit.events)
	if strings.Contains(string(data), "PRIVATE") {
		t.Fatal("private event payload retained")
	}
	ack := protocol.RPCMessageEditCommitted{TurnID: "root", History: protocol.RPCMessagesPage{Messages: []protocol.Message{{ID: "user-original", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "original"}}}}}}
	after, err := r.publishMessageEdit("42", ack)
	if err != nil || after.Queue == nil || !after.Queue.CanEnqueue || after.Queue.Token == "" || len(after.Messages) != 1 || after.Messages[0].SourceID != "user-original" {
		t.Fatalf("ACK replay queue source: %+v %v", after, err)
	}
	bad := q.Clone()
	bad.Items = make([]protocol.QueueControlItem, 9)
	event.AgentEvent.QueueControl = bad
	r.messageEdit.pending = true
	if consumed, valid := r.bufferMessageEditEventLocked(event); !consumed || valid {
		t.Fatal("unbounded queue snapshot accepted into buffer")
	}
	unsupported, known := queueProjectionRuntime(t)
	unsupported.queueSupported = false
	emitQueueProjection(unsupported, known)
	if unsupported.snapshot.Queue != nil || unsupported.queue.control != nil {
		t.Fatal("unadvertised queue source grants capability")
	}
}

func TestQueueStartingIsKnownLockedProgressUntilOutcome(t *testing.T) {
	r, q := queueProjectionRuntime(t)
	emitQueueProjection(r, q)
	if err := r.promptAcknowledged(protocol.RPCResponse{ID: "42", Success: true}, nil, false); err != nil {
		t.Fatal(err)
	}
	selected := q.Clone()
	selected.Revision = 2
	selected.Items = []protocol.QueueControlItem{{ID: "starting-item", Text: "exact submitted text", State: "delivering"}}
	selected.Change = protocol.QueueControlChange{Kind: "selected", ItemID: "starting-item"}
	emitQueueProjection(r, selected)
	if len(r.snapshot.Queue.Items) != 1 || r.snapshot.Queue.Items[0].State != "starting" || r.snapshot.Queue.Items[0].Text != "exact submitted text" || len(r.snapshot.Messages) != 1 {
		t.Fatalf("known starting progress misrepresented: %+v", r.snapshot)
	}
	r.snapshot.CancelRequested = true
	r.publishLocked()
	if r.snapshot.Queue.Items[0].State != "starting" || r.snapshot.Queue.CanEnqueue {
		t.Fatal("Stop alone fabricated an unknown outcome")
	}
	r.snapshot.CancelRequested = false
	r.snapshot.Status = "failed"
	r.publishLocked()
	if r.snapshot.Queue.Items[0].State != "uncertain" {
		t.Fatal("failed starting delivery still claims known progress")
	}

	healthy, root := queueProjectionRuntime(t)
	emitQueueProjection(healthy, root)
	unknown := root.Clone()
	unknown.Revision = 2
	unknown.ReviewItems = []protocol.QueueControlItem{{ID: "unknown-item", Text: "exact uncertain text", State: "delivery_unknown"}}
	unknown.Change = protocol.QueueControlChange{Kind: "delivery_unknown", ItemID: "unknown-item"}
	emitQueueProjection(healthy, unknown)
	if healthy.snapshot.Status != "running" || healthy.snapshot.Queue.Items[0].State != "uncertain" {
		t.Fatal("explicit unknown outcome mislabeled as normal starting progress")
	}
}
