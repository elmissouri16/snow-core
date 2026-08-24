package tui

import (
	"encoding/binary"
	"encoding/json"
	"reflect"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/pkg/protocol"
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
	event protocol.AgentEvent
	bytes int

	// Stream text is packed separately from the event envelope. Varint fragment
	// lengths preserve complete-fragment tail eviction without retaining one
	// 16-byte string header per provider delta.
	textData       []byte
	textLengths    []byte
	textDataHead   int
	textLengthHead int
	textFragments  int
}

func (item *queuedAgentEvent) appendText(text string) {
	if text == "" && item.textFragments > 0 {
		return
	}
	item.textData = append(item.textData, text...)
	item.textLengths = binary.AppendUvarint(item.textLengths, uint64(len(text)))
	item.textFragments++
}

func (item *queuedAgentEvent) dropOldestTextFragment() (int, bool) {
	if item.textFragments <= 1 {
		return 0, false
	}
	length, width := binary.Uvarint(item.textLengths[item.textLengthHead:])
	if width <= 0 || length > uint64(len(item.textData)-item.textDataHead) {
		return 0, false
	}
	item.textLengthHead += width
	item.textDataHead += int(length)
	item.textFragments--
	item.compactTextStorage()
	return int(length), true
}

func (item *queuedAgentEvent) compactTextStorage() {
	const compactThreshold = 64 << 10
	if item.textDataHead >= compactThreshold && item.textDataHead*2 >= len(item.textData) {
		copy(item.textData, item.textData[item.textDataHead:])
		item.textData = item.textData[:len(item.textData)-item.textDataHead]
		item.textDataHead = 0
	}
	if item.textLengthHead >= compactThreshold && item.textLengthHead*2 >= len(item.textLengths) {
		copy(item.textLengths, item.textLengths[item.textLengthHead:])
		item.textLengths = item.textLengths[:len(item.textLengths)-item.textLengthHead]
		item.textLengthHead = 0
	}
}

func (item *queuedAgentEvent) joinedText() string {
	return string(item.textData[item.textDataHead:])
}

// coalesceTUISnapshotEvents keeps only root snapshots superseded within one UI
// batch. Session updates carry no payload; usage and model events are current
// state snapshots. Child events remain independently observable, turn IDs keep
// accounting boundaries intact, and debug usage history remains complete.
// Compaction is in-place because the batch is owned by the Bubble Tea message.
func coalesceTUISnapshotEvents(events []protocol.AgentEvent, preserveUsageHistory bool) []protocol.AgentEvent {
	writeAt := 0
	for i := range events {
		if tuiSnapshotSuperseded(events[i], events[i+1:], preserveUsageHistory) {
			continue
		}
		events[writeAt] = events[i]
		writeAt++
	}
	clear(events[writeAt:])
	return events[:writeAt]
}

func tuiSnapshotSuperseded(event protocol.AgentEvent, later []protocol.AgentEvent, preserveUsageHistory bool) bool {
	if event.Agent != nil {
		return false
	}
	switch event.Type {
	case protocol.EvSessionUpdated:
		for _, candidate := range later {
			if candidate.Type == protocol.EvSessionUpdated && candidate.Agent == nil {
				return true
			}
		}
	case protocol.EvUsage:
		if preserveUsageHistory {
			return false
		}
		for _, candidate := range later {
			if candidate.Type == protocol.EvUsage && candidate.Agent == nil &&
				candidate.TurnID == event.TurnID && candidate.RootEpoch == event.RootEpoch {
				return true
			}
		}
	case protocol.EvModelChanged:
		for _, candidate := range later {
			if candidate.Type == protocol.EvModelChanged && candidate.Agent == nil &&
				candidate.TurnID == event.TurnID && candidate.RootEpoch == event.RootEpoch {
				return true
			}
		}
	}
	return false
}

// agentEventMailbox is an ordered handoff from agent callbacks to the Bubble
// Tea update loop. Coalescible and snapshot events are shed at the hard bound;
// interaction requests and terminal turn boundaries apply backpressure instead
// of being lost. Adjacent stream deltas are represented as parts and joined
// only when a batch is popped, avoiding quadratic concatenation while busy.
type agentEventMailbox struct {
	mu      sync.Mutex
	items   []queuedAgentEvent
	bytes   int
	dropped int
	wake    chan struct{}
	space   chan struct{}
	done    chan struct{}
	closed  bool
}

func newAgentEventMailbox() *agentEventMailbox {
	return &agentEventMailbox{wake: make(chan struct{}, 1), space: make(chan struct{}), done: make(chan struct{})}
}

func (q *agentEventMailbox) Push(ev protocol.AgentEvent) {
	if q == nil {
		return
	}
	copyEvent := ev.Clone()
	if isCoalescibleStreamDelta(copyEvent.Type) && len(copyEvent.Text) > maxMailboxDeltaBytes {
		copyEvent.Text = boundedUTF8Tail(copyEvent.Text, maxMailboxDeltaBytes)
	}
	for {
		q.mu.Lock()
		if q.closed {
			q.mu.Unlock()
			return
		}
		wasEmpty := len(q.items) == 0
		if len(q.items) > 0 && compatibleStreamDeltas(q.items[len(q.items)-1].event, copyEvent) {
			item := &q.items[len(q.items)-1]
			item.appendText(copyEvent.Text)
			item.bytes += len(copyEvent.Text)
			q.bytes += len(copyEvent.Text)
			for item.bytes > maxMailboxDeltaBytes && item.textFragments > 1 {
				removed, ok := item.dropOldestTextFragment()
				if !ok {
					break
				}
				item.bytes -= removed
				q.bytes -= removed
				q.dropped++
			}
			q.trimLocked()
			if wasEmpty && len(q.items) > 0 {
				q.signalLocked()
			}
			q.mu.Unlock()
			return
		}

		item := queuedAgentEvent{event: copyEvent, bytes: approximateAgentEventBytes(copyEvent)}
		if isCoalescibleStreamDelta(copyEvent.Type) {
			item.appendText(copyEvent.Text)
			item.bytes = len(copyEvent.Text)
			item.event.Text = ""
		}
		if !q.makeRoomLocked(item.bytes) {
			if !mailboxProtectedEvent(copyEvent.Type) {
				q.dropped++
				q.mu.Unlock()
				return
			}
			space := q.space
			q.mu.Unlock()
			select {
			case <-space:
				continue
			case <-q.done:
				return
			}
		}
		q.items = append(q.items, item)
		q.bytes += item.bytes
		if wasEmpty {
			q.signalLocked()
		}
		q.mu.Unlock()
		return
	}
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

func mailboxProtectedEvent(typ protocol.AgentEventType) bool {
	switch typ {
	case protocol.EvPermissionRequest, protocol.EvUserInputRequest,
		protocol.EvTurnDone, protocol.EvAborted:
		return true
	default:
		return false
	}
}

func (q *agentEventMailbox) makeRoomLocked(newBytes int) bool {
	for len(q.items) >= maxMailboxQueuedItems || (len(q.items) > 0 && q.bytes+newBytes > maxMailboxQueuedBytes) {
		if !q.evictOneLocked() {
			return false
		}
	}
	return true
}

func (q *agentEventMailbox) trimLocked() {
	for len(q.items) > maxMailboxQueuedItems || q.bytes > maxMailboxQueuedBytes {
		if !q.evictOneLocked() {
			return
		}
	}
}

func (q *agentEventMailbox) evictOneLocked() bool {
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
		for i := range q.items {
			if !mailboxProtectedEvent(q.items[i].event.Type) {
				removeAt = i
				break
			}
		}
	}
	if removeAt < 0 {
		return false
	}
	q.bytes -= q.items[removeAt].bytes
	copy(q.items[removeAt:], q.items[removeAt+1:])
	q.items[len(q.items)-1] = queuedAgentEvent{}
	q.items = q.items[:len(q.items)-1]
	q.dropped++
	return true
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
	close(q.space)
	q.space = make(chan struct{})
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
		if item.textFragments > 0 {
			ev.Text = item.joinedText()
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
	// Root and attributed stream deltas normally carry only scalar correlation
	// fields plus an optional AgentRef. Avoid boxing two large AgentEvent values
	// through reflect.DeepEqual for every token; the reflective path remains for
	// unusual extension payloads so coalescing never drops metadata.
	if simpleStreamDeltaEnvelope(previous) && simpleStreamDeltaEnvelope(next) {
		return previous.ToolCallID == next.ToolCallID &&
			previous.ToolName == next.ToolName &&
			previous.Message == next.Message &&
			previous.ToolOutput == next.ToolOutput &&
			previous.ToolDurationMS == next.ToolDurationMS &&
			previous.TurnID == next.TurnID &&
			previous.TurnOrigin == next.TurnOrigin &&
			previous.TurnSequence == next.TurnSequence &&
			previous.RootEpoch == next.RootEpoch &&
			previous.GoalContinuing == next.GoalContinuing &&
			previous.IsError == next.IsError &&
			equalAgentRef(previous.Agent, next.Agent)
	}
	// Coalesce only the Text field. Every other envelope field must be exactly
	// equivalent so metadata is never silently replaced or discarded.
	previous.Text = ""
	next.Text = ""
	return reflect.DeepEqual(previous, next)
}

func simpleStreamDeltaEnvelope(event protocol.AgentEvent) bool {
	return event.ToolProgress == nil && event.ToolRouting == nil && event.ProviderRetry == nil &&
		event.Usage == nil && event.Model == nil && event.Mode == nil && event.Plan == nil &&
		event.PlanUpdate == nil && event.Compaction == nil && event.Permission == nil &&
		event.UserInput == nil && event.Queue == nil && event.ThreadGoal == nil &&
		event.Subagent == nil && event.AgentMessage == nil
}

func equalAgentRef(left, right *protocol.AgentRef) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func waitForEvent(q *agentEventMailbox) tea.Cmd {
	return func() tea.Msg { return q.wait() }
}
