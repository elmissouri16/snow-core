package tui

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/pkg/protocol"
)

const (
	maxAgentEventsPerUpdate = 256
	maxMailboxQueuedItems   = 4096
	maxMailboxQueuedBytes   = 32 << 20
	maxMailboxDeltaBytes    = 8 << 20
)

type agentEventBatchMsg struct {
	events []protocol.AgentEvent
}

type queuedAgentEvent struct {
	event     protocol.AgentEvent
	textParts []string
	bytes     int
}

// coalesceRootSessionUpdates keeps only the latest root-session invalidation in
// one UI batch. Session updates carry no payload: reading the latest store state
// satisfies every earlier invalidation, while child-session lifecycle events
// remain independently observable.
func coalesceRootSessionUpdates(events []protocol.AgentEvent) []protocol.AgentEvent {
	last := -1
	count := 0
	for i, event := range events {
		if event.Type == protocol.EvSessionUpdated && event.Agent == nil {
			last = i
			count++
		}
	}
	if count < 2 {
		return events
	}
	coalesced := make([]protocol.AgentEvent, 0, len(events)-count+1)
	for i, event := range events {
		if event.Type == protocol.EvSessionUpdated && event.Agent == nil && i != last {
			continue
		}
		coalesced = append(coalesced, event)
	}
	return coalesced
}

// agentEventMailbox is a lossless, ordered handoff from agent callbacks to the
// Bubble Tea update loop. Producers never wait for the UI consumer. Adjacent
// stream deltas are represented as parts and joined only when a batch is
// popped, avoiding quadratic concatenation while the UI is busy rendering.
type agentEventMailbox struct {
	mu      sync.Mutex
	items   []queuedAgentEvent
	bytes   int
	dropped int
	wake    chan struct{}
	done    chan struct{}
	closed  bool
}

func newAgentEventMailbox() *agentEventMailbox {
	return &agentEventMailbox{wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func (q *agentEventMailbox) Push(ev protocol.AgentEvent) {
	if q == nil {
		return
	}
	copy := ev.Clone()
	if isCoalescibleStreamDelta(copy.Type) && len(copy.Text) > maxMailboxDeltaBytes {
		copy.Text = boundedUTF8Tail(copy.Text, maxMailboxDeltaBytes)
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	wasEmpty := len(q.items) == 0
	if len(q.items) > 0 && compatibleStreamDeltas(q.items[len(q.items)-1].event, copy) {
		item := &q.items[len(q.items)-1]
		item.textParts = append(item.textParts, copy.Text)
		item.bytes += len(copy.Text)
		q.bytes += len(copy.Text)
		for item.bytes > maxMailboxDeltaBytes && len(item.textParts) > 1 {
			removed := len(item.textParts[0])
			item.textParts[0] = ""
			item.textParts = item.textParts[1:]
			item.bytes -= removed
			q.bytes -= removed
			q.dropped++
		}
	} else {
		item := queuedAgentEvent{event: copy, bytes: approximateAgentEventBytes(copy)}
		if isCoalescibleStreamDelta(copy.Type) {
			item.textParts = []string{copy.Text}
			item.bytes = len(copy.Text)
			item.event.Text = ""
		}
		q.items = append(q.items, item)
		q.bytes += item.bytes
	}
	q.trimLocked()
	if wasEmpty && len(q.items) > 0 {
		q.signalLocked()
	}
	q.mu.Unlock()
}

func approximateAgentEventBytes(ev protocol.AgentEvent) int {
	if isCoalescibleStreamDelta(ev.Type) {
		return len(ev.Text)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return 1024 + len(ev.Text) + len(ev.Message) + len(ev.ToolOutput)
	}
	return len(data)
}

func mailboxSnapshotEvent(typ protocol.AgentEventType) bool {
	switch typ {
	case protocol.EvSessionUpdated, protocol.EvUsage, protocol.EvQueueUpdated, protocol.EvToolProgress:
		return true
	default:
		return false
	}
}

func (q *agentEventMailbox) trimLocked() {
	for len(q.items) > maxMailboxQueuedItems || q.bytes > maxMailboxQueuedBytes {
		removeAt := -1
		for i := range q.items {
			if isCoalescibleStreamDelta(q.items[i].event.Type) {
				removeAt = i
				break
			}
		}
		if removeAt < 0 {
			for i := range q.items {
				if mailboxSnapshotEvent(q.items[i].event.Type) {
					removeAt = i
					break
				}
			}
		}
		if removeAt < 0 {
			// A pathological flood of lifecycle events must still have a hard
			// memory bound. Preserve the newest state when no coalescible item exists.
			removeAt = 0
		}
		q.bytes -= q.items[removeAt].bytes
		copy(q.items[removeAt:], q.items[removeAt+1:])
		q.items[len(q.items)-1] = queuedAgentEvent{}
		q.items = q.items[:len(q.items)-1]
		q.dropped++
	}
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
	for i := range q.items[:count] {
		q.bytes -= q.items[i].bytes
	}
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
		select {
		case <-q.wake:
		case <-q.done:
		}
		events := q.popBatch(maxAgentEventsPerUpdate)
		if len(events) > 0 {
			return agentEventBatchMsg{events: events}
		}
		q.mu.Lock()
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return nil
		}
		// A defensive stale wake must never return nil: Bubble Tea would not
		// run Update and therefore would not install the next waiter.
	}
}

// Close releases blocked Bubble Tea commands. Queued events may still be
// drained once; later pushes are dropped.
func (q *agentEventMailbox) Close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.done)
	}
	q.mu.Unlock()
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
