package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
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
	case "read", "grep", "glob", "search_tools", "ask_user", "request_user_input", "update_plan", "get_goal", "create_goal", "update_goal":
		return permission.RiskRead
	case "write", "edit":
		return permission.RiskWrite
	case "bash":
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
		subs:      make(map[int]func(protocol.AgentEvent)),
		wake:      make(chan struct{}, 1),
		space:     make(chan struct{}, 1),
		closingCh: make(chan struct{}),
		maxItems:  maxItems,
		closed:    make(chan struct{}),
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

func (b *eventBus) dispatch() {
	atomic.StoreUint64(&b.dispatcherID, currentGoroutineID())
	defer close(b.closed)
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
			fns := make([]func(protocol.AgentEvent), 0, len(b.subs))
			if _, ok := item.(protocol.AgentEvent); ok {
				ids := make([]int, 0, len(b.subs))
				for id := range b.subs {
					ids = append(ids, id)
				}
				sort.Ints(ids)
				for _, id := range ids {
					fns = append(fns, b.subs[id])
				}
			}
			b.mu.Unlock()
			switch v := item.(type) {
			case protocol.AgentEvent:
				for _, fn := range fns {
					b.mu.Lock()
					if b.closing {
						b.mu.Unlock()
						break
					}
					b.inCallback = true
					b.mu.Unlock()
					func() {
						defer func() { _ = recover() }()
						fn(v.Clone())
					}()
					b.mu.Lock()
					b.inCallback = false
					b.mu.Unlock()
				}
			case eventBarrier:
				close(v.done)
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
	return b.inCallback && current != 0 && current == atomic.LoadUint64(&b.dispatcherID)
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
		b.subs = make(map[int]func(protocol.AgentEvent))
		b.signal()
	}
	b.mu.Unlock()
}

func (b *eventBus) Subscribe(fn func(protocol.AgentEvent)) func() {
	if fn == nil {
		return func() {}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return func() {}
	}
	id := b.next
	b.next++
	b.subs[id] = fn
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs, id)
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

func (b *eventBus) Publish(ev protocol.AgentEvent) {
	copyEvent := ev.Clone()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return
	}
	if len(b.items) >= b.maxItems {
		removeAt := -1
		for i, item := range b.items {
			queued, ok := item.(protocol.AgentEvent)
			if ok && coalescibleBusEvent(queued.Type) {
				removeAt = i
				break
			}
		}
		if removeAt < 0 && !coalescibleBusEvent(copyEvent.Type) {
			// Preserve drain barriers and make room for a lifecycle event by
			// evicting the oldest ordinary event.
			for i, item := range b.items {
				if _, ok := item.(protocol.AgentEvent); ok {
					removeAt = i
					break
				}
			}
		}
		if removeAt < 0 {
			return
		}
		copy(b.items[removeAt:], b.items[removeAt+1:])
		b.items[len(b.items)-1] = nil
		b.items = b.items[:len(b.items)-1]
	}
	b.items = append(b.items, copyEvent)
	b.signal()
}

func newID() string {
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(), atomic.AddUint64(&idCounter, 1))
}
