package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestAgentEventMailboxCoalescesWithoutBlockingProducer(t *testing.T) {
	q := newAgentEventMailbox()
	const deltas = 10_000
	done := make(chan struct{})
	go func() {
		for range deltas {
			q.Push(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "x", TurnID: "turn"})
		}
		q.Push(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "bash"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("mailbox producer blocked without a consumer")
	}
	if got := q.len(); got != 2 {
		t.Fatalf("logical queue length=%d want 2", got)
	}
	batch := q.popBatch(maxAgentEventsPerUpdate)
	if len(batch) != 2 || batch[0].Type != protocol.EvTextDelta || batch[1].Type != protocol.EvToolStart {
		t.Fatalf("batch ordering=%+v", batch)
	}
	if batch[0].Text != strings.Repeat("x", deltas) {
		t.Fatalf("coalesced text bytes=%d want %d", len(batch[0].Text), deltas)
	}
}

func TestCoalesceRootSessionUpdatesKeepsLatestAndPreservesChildEvents(t *testing.T) {
	child := &protocol.AgentRef{ThreadID: "child", Path: "/root/child"}
	events := []protocol.AgentEvent{
		{Type: protocol.EvSessionUpdated, TurnID: "old"},
		{Type: protocol.EvToolStart, ToolName: "read"},
		{Type: protocol.EvSessionUpdated, Agent: child, TurnID: "child"},
		{Type: protocol.EvSessionUpdated, TurnID: "latest"},
		{Type: protocol.EvTurnDone, TurnID: "turn"},
	}
	got := coalesceRootSessionUpdates(events)
	if len(got) != 4 {
		t.Fatalf("events=%+v", got)
	}
	if got[0].Type != protocol.EvToolStart || got[1].Agent != child || got[2].TurnID != "latest" || got[3].Type != protocol.EvTurnDone {
		t.Fatalf("ordering=%+v", got)
	}
}

func TestAgentEventMailboxPreservesLifecycleAndPlanBoundaries(t *testing.T) {
	q := newAgentEventMailbox()
	q.Push(protocol.AgentEvent{Type: protocol.EvPlanDelta, Text: "a", Plan: &protocol.PlanItem{ID: "one"}})
	q.Push(protocol.AgentEvent{Type: protocol.EvPlanDelta, Text: "b", Plan: &protocol.PlanItem{ID: "one"}})
	q.Push(protocol.AgentEvent{Type: protocol.EvPlanDelta, Text: "c", Plan: &protocol.PlanItem{ID: "two"}})
	q.Push(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "turn"})
	q.Push(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "d"})
	batch := q.popBatch(maxAgentEventsPerUpdate)
	if len(batch) != 4 {
		t.Fatalf("batch length=%d want 4: %+v", len(batch), batch)
	}
	if batch[0].Text != "ab" || batch[0].Plan.ID != "one" || batch[1].Text != "c" || batch[1].Plan.ID != "two" || batch[2].Type != protocol.EvTurnDone || batch[3].Text != "d" {
		t.Fatalf("event ordering/coalescing changed: %+v", batch)
	}
}

func TestAgentEventMailboxNeverEvictsInteractionOrTerminalEvents(t *testing.T) {
	q := newAgentEventMailbox()
	protected := []protocol.AgentEvent{
		{Type: protocol.EvPermissionRequest, TurnID: "permission"},
		{Type: protocol.EvUserInputRequest, TurnID: "user-input"},
		{Type: protocol.EvTurnDone, TurnID: "done"},
		{Type: protocol.EvAborted, TurnID: "aborted"},
	}
	for _, event := range protected {
		q.Push(event)
	}
	for i := len(protected); i < maxMailboxQueuedItems+1; i++ {
		q.Push(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: string(rune(i + 1))})
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) != maxMailboxQueuedItems {
		t.Fatalf("mailbox items=%d want %d", len(q.items), maxMailboxQueuedItems)
	}
	for _, want := range protected {
		found := false
		for _, item := range q.items {
			if item.event.Type == want.Type && item.event.TurnID == want.TurnID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("protected event %s/%s was evicted", want.Type, want.TurnID)
		}
	}
}

func TestAgentEventMailboxBackpressuresProtectedOverflow(t *testing.T) {
	q := newAgentEventMailbox()
	defer q.Close()
	for i := 0; i < maxMailboxQueuedItems; i++ {
		q.Push(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: string(rune(i + 1))})
	}
	pushed := make(chan struct{})
	go func() {
		q.Push(protocol.AgentEvent{Type: protocol.EvAborted, TurnID: "new"})
		close(pushed)
	}()
	select {
	case <-pushed:
		t.Fatal("protected overflow was dropped instead of backpressured")
	case <-time.After(20 * time.Millisecond):
	}
	if batch := q.popBatch(1); len(batch) != 1 {
		t.Fatalf("popped batch len=%d", len(batch))
	}
	select {
	case <-pushed:
	case <-time.After(time.Second):
		t.Fatal("protected producer did not resume after mailbox space opened")
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	found := false
	for _, item := range q.items {
		if item.event.Type == protocol.EvAborted && item.event.TurnID == "new" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("resumed protected event was not retained")
	}
}

func TestAgentEventMailboxDoesNotLeaveStaleWakeAfterConsumerRace(t *testing.T) {
	q := newAgentEventMailbox()
	q.Push(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "one"})
	// Coordinate the race: the waiter consumed the wake but has not popped yet;
	// a producer appends while the logical queue is still non-empty.
	<-q.wake
	q.Push(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolName: "two"})
	batch := q.popBatch(maxAgentEventsPerUpdate)
	if len(batch) != 2 {
		t.Fatalf("batch len=%d", len(batch))
	}
	select {
	case <-q.wake:
		t.Fatal("producer left a stale wake for a non-empty→non-empty append")
	default:
	}

	// Even if a stale wake is injected defensively, wait must skip it rather
	// than return nil and permanently disarm Bubble Tea delivery.
	q.wake <- struct{}{}
	result := make(chan tea.Msg, 1)
	go func() { result <- q.wait() }()
	select {
	case msg := <-result:
		t.Fatalf("stale wake returned %T", msg)
	case <-time.After(20 * time.Millisecond):
	}
	q.Push(protocol.AgentEvent{Type: protocol.EvTurnDone})
	select {
	case msg := <-result:
		batch, ok := msg.(agentEventBatchMsg)
		if !ok || len(batch.events) != 1 || batch.events[0].Type != protocol.EvTurnDone {
			t.Fatalf("post-stale result=%T %+v", msg, msg)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not re-arm after stale wake")
	}
}

func TestAgentEventMailboxCloseReleasesWaiterAndDropsLateEvents(t *testing.T) {
	q := newAgentEventMailbox()
	result := make(chan tea.Msg, 1)
	go func() { result <- q.wait() }()
	q.Close()
	q.Close()
	select {
	case msg := <-result:
		if msg != nil {
			t.Fatalf("closed waiter returned %T", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not release waiter")
	}
	q.Push(protocol.AgentEvent{Type: protocol.EvTurnDone})
	if q.len() != 0 {
		t.Fatal("push after close queued an event")
	}
}

func TestAgentEventMailboxRearmsBoundedBatches(t *testing.T) {
	q := newAgentEventMailbox()
	for i := 0; i < maxAgentEventsPerUpdate+7; i++ {
		q.Push(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: string(rune(i + 1))})
	}
	first, ok := q.wait().(agentEventBatchMsg)
	if !ok || len(first.events) != maxAgentEventsPerUpdate {
		t.Fatalf("first batch=%T len=%d", first, len(first.events))
	}
	second, ok := q.wait().(agentEventBatchMsg)
	if !ok || len(second.events) != 7 {
		t.Fatalf("second batch=%T len=%d", second, len(second.events))
	}
}
