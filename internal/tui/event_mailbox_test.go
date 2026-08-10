package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/pkg/protocol"
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
