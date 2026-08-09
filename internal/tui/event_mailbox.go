package tui

import (
	"reflect"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/pkg/protocol"
)

const maxAgentEventsPerUpdate = 256

type agentEventBatchMsg struct {
	events []protocol.AgentEvent
}

type queuedAgentEvent struct {
	event     protocol.AgentEvent
	textParts []string
}

// agentEventMailbox is a lossless, ordered handoff from agent callbacks to the
// Bubble Tea update loop. Producers never wait for the UI consumer. Adjacent
// stream deltas are represented as parts and joined only when a batch is
// popped, avoiding quadratic concatenation while the UI is busy rendering.
type agentEventMailbox struct {
	mu    sync.Mutex
	items []queuedAgentEvent
	wake  chan struct{}
}

func newAgentEventMailbox() *agentEventMailbox {
	return &agentEventMailbox{wake: make(chan struct{}, 1)}
}

func (q *agentEventMailbox) Push(ev protocol.AgentEvent) {
	if q == nil {
		return
	}
	copy := ev.Clone()
	q.mu.Lock()
	wasEmpty := len(q.items) == 0
	if len(q.items) > 0 && compatibleStreamDeltas(q.items[len(q.items)-1].event, copy) {
		q.items[len(q.items)-1].textParts = append(q.items[len(q.items)-1].textParts, copy.Text)
	} else {
		item := queuedAgentEvent{event: copy}
		if isCoalescibleStreamDelta(copy.Type) {
			item.textParts = []string{copy.Text}
			item.event.Text = ""
		}
		q.items = append(q.items, item)
	}
	if wasEmpty {
		q.signalLocked()
	}
	q.mu.Unlock()
}

func (q *agentEventMailbox) signalLocked() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *agentEventMailbox) popBatch(limit int) []protocol.AgentEvent {
	if q == nil || limit <= 0 {
		return nil
	}
	q.mu.Lock()
	count := min(limit, len(q.items))
	if count == 0 {
		q.mu.Unlock()
		return nil
	}
	items := append([]queuedAgentEvent(nil), q.items[:count]...)
	clear(q.items[:count])
	q.items = q.items[count:]
	if len(q.items) == 0 {
		q.items = nil
	}
	if len(q.items) > 0 {
		q.signalLocked()
	}
	q.mu.Unlock()
	// Materialize potentially large merged strings after releasing the producer
	// lock; an agent callback never waits for UI-side concatenation.
	events := make([]protocol.AgentEvent, 0, len(items))
	for _, item := range items {
		ev := item.event
		if item.textParts != nil {
			ev.Text = strings.Join(item.textParts, "")
		}
		events = append(events, ev)
	}
	return events
}

func (q *agentEventMailbox) wait() tea.Msg {
	if q == nil {
		return nil
	}
	for {
		<-q.wake
		events := q.popBatch(maxAgentEventsPerUpdate)
		if len(events) > 0 {
			return agentEventBatchMsg{events: events}
		}
		// A defensive stale wake must never return nil: Bubble Tea would not
		// run Update and therefore would not install the next waiter.
	}
}

func (q *agentEventMailbox) len() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func isCoalescibleStreamDelta(kind protocol.AgentEventType) bool {
	switch kind {
	case protocol.EvTextDelta, protocol.EvThinkingDelta, protocol.EvPlanDelta:
		return true
	default:
		return false
	}
}

func compatibleStreamDeltas(previous, next protocol.AgentEvent) bool {
	if previous.Type != next.Type || !isCoalescibleStreamDelta(next.Type) {
		return false
	}
	// Coalesce only the Text field. Every other envelope field must be exactly
	// equivalent so metadata is never silently replaced or discarded.
	previous.Text = ""
	next.Text = ""
	return reflect.DeepEqual(previous, next)
}

func waitForEvent(q *agentEventMailbox) tea.Cmd {
	return func() tea.Msg { return q.wait() }
}
