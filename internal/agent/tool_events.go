package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func editDiffPreview(details []any) (string, bool) {
	for _, detail := range details {
		switch d := detail.(type) {
		case tools.DiffDetails:
			if d.Diff != "" {
				return d.Diff, true
			}
		case *tools.DiffDetails:
			if d != nil && d.Diff != "" {
				return d.Diff, true
			}
		}
	}
	return "", false
}

func toolStartMessage(name string, rawArgs json.RawMessage) string {
	switch name {
	case "bash":
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(rawArgs, &input); err == nil && input.Command != "" {
			return boundEventText(input.Command, 2*1024)
		}
	case "edit", "write":
		var input struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(rawArgs, &input); err == nil && input.Path != "" {
			return boundEventText(input.Path, 2*1024)
		}
	}
	return "running"
}

func boundEventText(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	body := []byte(text)[:maxBytes]
	for len(body) > 0 && !utf8.Valid(body) {
		body = body[:len(body)-1]
	}
	return string(body) + "\n… [tool output preview truncated]"
}

// riskFor maps tool names to permission risk classes.
func riskFor(name string) permission.Risk {
	switch name {
	case "read", "grep", "glob", "search_tools", "ask_user", "request_user_input", "update_plan", "get_goal", "create_goal", "update_goal", "process_status", "process_logs", "process_list":
		return permission.RiskRead
	case "write", "edit":
		return permission.RiskWrite
	case "bash", "process_start", "process_stop":
		return permission.RiskExec
	case "webfetch":
		return permission.RiskNet
	default:
		return permission.RiskExec
	}
}

// extractPaths pulls likely path fields from tool args.
func extractPaths(args map[string]any) []string {
	var paths []string
	for _, k := range []string{"path", "file", "dir", "paths"} {
		switch v := args[k].(type) {
		case string:
			paths = append(paths, v)
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					paths = append(paths, s)
				}
			}
		}
	}
	return paths
}

func assistantResponseContentWithProviderData(thinking string, providerData, blocks []protocol.ContentBlock) []protocol.ContentBlock {
	content := make([]protocol.ContentBlock, 0, len(providerData)+len(blocks)+1)
	if thinking != "" {
		content = append(content, protocol.ContentBlock{Type: protocol.BlockThinking, Text: thinking})
	}
	// Opaque reasoning continuity is persisted before output/function calls and
	// is deliberately never published on the AgentEvent bus.
	content = append(content, providerData...)
	return append(content, blocks...)
}

func strings_trim(s string) string { return strings.TrimSpace(s) }

func newEventBus() *eventBus { return newEventBusWithCap(eventBusMaxItems) }

func newEventBusWithCap(maxItems int) *eventBus {
	if maxItems < 1 {
		maxItems = 1
	}
	b := &eventBus{
		subs:        make(map[int]*eventSubscriber),
		callbackIDs: make(map[uint64]struct{}),
		wake:        make(chan struct{}, 1),
		space:       make(chan struct{}, 1),
		closingCh:   make(chan struct{}),
		maxItems:    maxItems,
		closed:      make(chan struct{}),
	}
	go b.dispatch()
	return b
}

func (b *eventBus) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *eventBus) runSubscriber(sub *eventSubscriber) {
	id := currentGoroutineID()
	b.mu.Lock()
	b.callbackIDs[id] = struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.callbackIDs, id)
		b.mu.Unlock()
	}()
	for {
		select {
		case task := <-sub.tasks:
			func() {
				defer func() { _ = recover() }()
				sub.fn(task.event)
			}()
			// The event-local channel is buffered for every dispatched subscriber,
			// so a late completion after timeout never strands the worker.
			task.completed <- eventSubscriberCompletion{id: sub.id, generation: task.generation}
		case <-sub.stop:
			return
		}
	}
}

func (b *eventBus) dispatch() {
	b.dispatcherID.Store(currentGoroutineID())
	defer close(b.closed)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var generation uint64
	for range b.wake {
		for {
			b.mu.Lock()
			if len(b.items) == 0 {
				b.mu.Unlock()
				break
			}
			item := b.items[0]
			b.items[0] = nil
			b.items = b.items[1:]
			if len(b.items) == 0 {
				b.items = nil
			}
			select {
			case b.space <- struct{}{}:
			default:
			}
			subscribers := make([]*eventSubscriber, 0, len(b.subs))
			if _, ok := item.(protocol.AgentEvent); ok {
				ids := make([]int, 0, len(b.subs))
				for id := range b.subs {
					ids = append(ids, id)
				}
				slices.Sort(ids)
				for _, id := range ids {
					subscribers = append(subscribers, b.subs[id])
				}
			}
			b.mu.Unlock()

			switch event := item.(type) {
			case protocol.AgentEvent:
				generation++
				active := make([]*eventSubscriber, 0, len(subscribers))
				for _, sub := range subscribers {
					b.mu.Lock()
					current, subscribed := b.subs[sub.id]
					closing := b.closing
					b.mu.Unlock()
					if closing {
						break
					}
					if subscribed && current == sub {
						active = append(active, sub)
					}
				}
				completed := make(chan eventSubscriberCompletion, len(active))
				pending := make(map[int]*eventSubscriber, len(active))
				for i, sub := range active {
					payload := event
					if i+1 < len(active) {
						payload = event.Clone()
					}
					task := eventSubscriberTask{event: payload, generation: generation, completed: completed}
					select {
					case sub.tasks <- task:
						pending[sub.id] = sub
					case <-sub.stop:
					}
				}
				if len(pending) == 0 {
					continue
				}
				timer.Reset(eventSubscriberTimeout)
				for len(pending) > 0 {
					select {
					case done := <-completed:
						if done.generation == generation {
							delete(pending, done.id)
						}
					case <-timer.C:
						b.mu.Lock()
						for id, sub := range pending {
							if b.subs[id] == sub {
								delete(b.subs, id)
							}
						}
						b.mu.Unlock()
						for _, sub := range pending {
							sub.close()
						}
						pending = nil
					}
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case eventBarrier:
				close(event.done)
			case eventStop:
				return
			}
		}
	}
}

func currentGoroutineID() uint64 {
	var stack [64]byte
	n := runtime.Stack(stack[:], false)
	const prefix = "goroutine "
	if n <= len(prefix) || string(stack[:len(prefix)]) != prefix {
		return 0
	}
	end := len(prefix)
	for end < n && stack[end] >= '0' && stack[end] <= '9' {
		end++
	}
	id, _ := strconv.ParseUint(string(stack[len(prefix):end]), 10, 64)
	return id
}

func (b *eventBus) InCallback() bool {
	current := currentGoroutineID()
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.callbackIDs[current]
	return current != 0 && ok
}

func (b *eventBus) Drain(ctx context.Context) error {
	if b.InCallback() {
		return ErrReentrantDrain
	}
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	for {
		b.mu.Lock()
		if b.closing {
			b.mu.Unlock()
			return nil
		}
		if len(b.items) < b.maxItems {
			b.items = append(b.items, eventBarrier{done})
			b.signal()
			b.mu.Unlock()
			break
		}
		b.mu.Unlock()
		select {
		case <-b.space:
		case <-b.closingCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *eventBus) Wait() {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-b.closed:
	case <-timer.C:
	}
}

func (b *eventBus) Close() {
	var subscribers []*eventSubscriber
	b.mu.Lock()
	if !b.closing {
		b.closing = true
		close(b.closingCh)
		// Closing suppresses future callbacks. Release callers already waiting
		// on a drain barrier before terminating the dispatcher.
		for _, item := range b.items {
			if barrier, ok := item.(eventBarrier); ok {
				close(barrier.done)
			}
		}
		b.items = []any{eventStop{}}
		for _, sub := range b.subs {
			subscribers = append(subscribers, sub)
		}
		b.subs = make(map[int]*eventSubscriber)
		b.signal()
	}
	b.mu.Unlock()
	for _, sub := range subscribers {
		sub.close()
	}
}

func (b *eventBus) Subscribe(fn func(protocol.AgentEvent)) func() {
	if fn == nil {
		return func() {}
	}
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return func() {}
	}
	id := b.next
	b.next++
	sub := &eventSubscriber{id: id, fn: fn, tasks: make(chan eventSubscriberTask), stop: make(chan struct{})}
	b.subs[id] = sub
	b.mu.Unlock()
	go b.runSubscriber(sub)
	return func() {
		b.mu.Lock()
		if b.subs[id] == sub {
			delete(b.subs, id)
		}
		b.mu.Unlock()
		sub.close()
	}
}

func coalescibleBusEvent(kind protocol.AgentEventType) bool {
	switch kind {
	case protocol.EvTextDelta, protocol.EvThinkingDelta, protocol.EvPlanDelta,
		protocol.EvToolProgress, protocol.EvUsage, protocol.EvSessionUpdated,
		protocol.EvQueueUpdated:
		return true
	default:
		return false
	}
}

func protectedBusEvent(kind protocol.AgentEventType) bool {
	switch kind {
	case protocol.EvToolStart, protocol.EvToolEnd, protocol.EvRunStatsUpdated,
		protocol.EvTurnDone, protocol.EvProviderRetry, protocol.EvError, protocol.EvAborted,
		protocol.EvPermissionRequest, protocol.EvUserInputRequest,
		protocol.EvPlanStarted, protocol.EvPlanCompleted,
		protocol.EvCompactionStarted, protocol.EvCompactionDone:
		return true
	default:
		return false
	}
}

func (b *eventBus) Publish(ev protocol.AgentEvent) {
	copyEvent := ev.Clone()
	for {
		b.mu.Lock()
		if b.closing {
			b.mu.Unlock()
			return
		}
		if len(b.items) < b.maxItems {
			b.items = append(b.items, copyEvent)
			b.signal()
			b.mu.Unlock()
			return
		}
		removeAt := -1
		for i, item := range b.items {
			queued, ok := item.(protocol.AgentEvent)
			if ok && coalescibleBusEvent(queued.Type) {
				removeAt = i
				break
			}
		}
		if removeAt < 0 && protectedBusEvent(copyEvent.Type) {
			for i, item := range b.items {
				queued, ok := item.(protocol.AgentEvent)
				if ok && !protectedBusEvent(queued.Type) {
					removeAt = i
					break
				}
			}
		}
		if removeAt >= 0 {
			copy(b.items[removeAt:], b.items[removeAt+1:])
			b.items[len(b.items)-1] = nil
			b.items = b.items[:len(b.items)-1]
			b.items = append(b.items, copyEvent)
			b.signal()
			b.mu.Unlock()
			return
		}
		if !protectedBusEvent(copyEvent.Type) {
			b.mu.Unlock()
			return
		}
		// Protected terminal/pairing events apply bounded backpressure rather
		// than dropping either the queued boundary or the new one. The dispatcher
		// signals space as soon as it takes an item, independently of callbacks.
		b.mu.Unlock()
		select {
		case <-b.space:
		case <-b.closingCh:
			return
		}
	}
}

func newID() string {
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(), idCounter.Add(1))
}
