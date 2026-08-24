package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestQueuedAgentEventPacksTinyDeltaMetadata(t *testing.T) {
	var item queuedAgentEvent
	const fragments = 100_000
	for range fragments {
		item.appendText("x")
	}
	activeData := len(item.textData) - item.textDataHead
	activeMetadata := len(item.textLengths) - item.textLengthHead
	if item.textFragments != fragments || activeData != fragments {
		t.Fatalf("fragments=%d data=%d", item.textFragments, activeData)
	}
	if activeMetadata > activeData+16 {
		t.Fatalf("packed metadata=%d exceeds payload=%d", activeMetadata, activeData)
	}
	if got := item.joinedText(); len(got) != fragments || got[0] != 'x' || got[len(got)-1] != 'x' {
		t.Fatalf("joined tiny deltas length=%d", len(got))
	}
}

func TestAgentEventMailboxEvictsCompleteDeltaFragments(t *testing.T) {
	q := newAgentEventMailbox()
	first := strings.Repeat("a", 5<<20)
	second := strings.Repeat("b", 4<<20)
	q.Push(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: first, TurnID: "turn"})
	q.Push(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: second, TurnID: "turn"})
	q.Push(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "tail", TurnID: "turn"})
	batch := q.popBatch(maxAgentEventsPerUpdate)
	if len(batch) != 1 || batch[0].Text != second+"tail" {
		t.Fatalf("coalesced fragment tail length=%d", len(batch[0].Text))
	}
	if q.dropped != 1 {
		t.Fatalf("dropped=%d want 1", q.dropped)
	}
}

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

func TestCoalesceTUISnapshotEventsKeepsLatestAndPreservesChildEvents(t *testing.T) {
	child := &protocol.AgentRef{ThreadID: "child", Path: "/root/child"}
	events := []protocol.AgentEvent{
		{Type: protocol.EvSessionUpdated, TurnID: "old"},
		{Type: protocol.EvToolStart, ToolName: "read"},
		{Type: protocol.EvSessionUpdated, Agent: child, TurnID: "child"},
		{Type: protocol.EvSessionUpdated, TurnID: "latest"},
		{Type: protocol.EvTurnDone, TurnID: "turn"},
	}
	got := coalesceTUISnapshotEvents(events, false)
	if len(got) != 4 {
		t.Fatalf("events=%+v", got)
	}
	if got[0].Type != protocol.EvToolStart || got[1].Agent != child || got[2].TurnID != "latest" || got[3].Type != protocol.EvTurnDone {
		t.Fatalf("ordering=%+v", got)
	}
}

func TestCoalesceTUISnapshotEventsRespectsTurnAndDebugBoundaries(t *testing.T) {
	modelOne := protocol.Model{Provider: "p", ID: "one"}
	modelTwo := protocol.Model{Provider: "p", ID: "two"}
	events := []protocol.AgentEvent{
		{Type: protocol.EvUsage, TurnID: "turn-one", RootEpoch: 1, Usage: &protocol.Usage{Input: 1}},
		{Type: protocol.EvUsage, TurnID: "turn-one", RootEpoch: 1, Usage: &protocol.Usage{Input: 2}},
		{Type: protocol.EvTurnDone, TurnID: "turn-one", RootEpoch: 1},
		{Type: protocol.EvUsage, TurnID: "turn-two", RootEpoch: 1, Usage: &protocol.Usage{Input: 3}},
		{Type: protocol.EvModelChanged, RootEpoch: 1, Model: &modelOne},
		{Type: protocol.EvModelChanged, RootEpoch: 1, Model: &modelTwo},
	}
	got := coalesceTUISnapshotEvents(append([]protocol.AgentEvent(nil), events...), false)
	if len(got) != 4 || got[0].Usage.Input != 2 || got[1].Type != protocol.EvTurnDone || got[2].Usage.Input != 3 || got[3].Model.ID != "two" {
		t.Fatalf("snapshot coalescing crossed a boundary: %+v", got)
	}
	debug := coalesceTUISnapshotEvents(append([]protocol.AgentEvent(nil), events...), true)
	usageEvents := 0
	for _, event := range debug {
		if event.Type == protocol.EvUsage {
			usageEvents++
		}
	}
	if usageEvents != 3 {
		t.Fatalf("debug usage snapshots=%d want 3: %+v", usageEvents, debug)
	}
}

func TestAgentEventMailboxFastPathPreservesAttributedBoundaries(t *testing.T) {
	q := newAgentEventMailbox()
	first := &protocol.AgentRef{ThreadID: "one", Path: "/root/one", Role: "explorer", Depth: 1}
	same := *first
	other := *first
	other.ThreadID = "two"
	other.Path = "/root/two"
	q.Push(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "a", Agent: first, TurnID: "turn"})
	q.Push(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "b", Agent: &same, TurnID: "turn"})
	q.Push(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "c", Agent: &other, TurnID: "turn"})
	batch := q.popBatch(maxAgentEventsPerUpdate)
	if len(batch) != 2 || batch[0].Text != "ab" || batch[0].Agent.ThreadID != "one" || batch[1].Text != "c" || batch[1].Agent.ThreadID != "two" {
		t.Fatalf("attributed stream boundaries changed: %+v", batch)
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
