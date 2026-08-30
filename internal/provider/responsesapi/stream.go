package responsesapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type codexStream struct {
	ch       chan protocol.StreamEvent
	done     chan struct{}
	ctx      context.Context
	body     io.ReadCloser
	once     sync.Once
	secrets  []string
	provider string
}

func NewStream(ctx context.Context, resp *http.Response, providerID string, secrets ...string) protocol.EventStream {
	return NewStreamWithIdleTimeout(ctx, resp, providerID, providerpkg.DefaultStreamIdleTimeout, secrets...)
}

func NewStreamWithIdleTimeout(ctx context.Context, resp *http.Response, providerID string, idleTimeout time.Duration, secrets ...string) protocol.EventStream {
	body := providerpkg.WrapIdleReadCloser(resp.Body, idleTimeout)
	s := &codexStream{ch: make(chan protocol.StreamEvent, 64), done: make(chan struct{}), ctx: ctx, body: body, secrets: slices.Clone(secrets), provider: providerLabel(providerID)}
	go s.read()
	return s
}

func (s *codexStream) prefix(message string) error { return errors.New(s.provider + ": " + message) }

func (s *codexStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	select {
	case ev, ok := <-s.ch:
		if !ok {
			return protocol.StreamEvent{}, io.EOF
		}
		return ev, nil
	case <-ctx.Done():
		return protocol.StreamEvent{}, ctx.Err()
	case <-s.ctx.Done():
		return protocol.StreamEvent{}, s.ctx.Err()
	case <-s.done:
		return protocol.StreamEvent{}, io.EOF
	}
}

func (s *codexStream) Close() error {
	s.once.Do(func() {
		if s.body != nil {
			_ = s.body.Close()
		}
		close(s.done)
	})
	return nil
}

func (s *codexStream) send(ev protocol.StreamEvent) {
	select {
	case s.ch <- ev:
	case <-s.ctx.Done():
	case <-s.done:
	}
}

type toolAccum struct {
	id   string
	name string
	args strings.Builder
}

type reasoningAccum struct {
	items            map[string]*strings.Builder
	totalBytes       int
	emitted          bool
	trailingNewlines int
}

func newReasoningAccum() *reasoningAccum {
	return &reasoningAccum{items: make(map[string]*strings.Builder)}
}

// append preserves raw per-item text for completed-snapshot reconciliation but
// inserts a visible paragraph boundary when the Responses API starts a new
// reasoning summary item. Without this, independently formatted summaries such
// as "Planning tasks" and "Designing workers" render as "tasksDesigning".
func (r *reasoningAccum) append(key, text string) string {
	if r == nil || text == "" {
		return text
	}
	builder, seen := r.items[key]
	if !seen {
		builder = &strings.Builder{}
		r.items[key] = builder
	}
	builder.WriteString(text)
	r.totalBytes += len(text)
	out := text
	if !seen && r.emitted {
		needed := max(0, 2-r.trailingNewlines)
		out = strings.Repeat("\n", needed) + strings.TrimLeft(text, "\r\n")
	}
	if out != "" {
		r.emitted = true
		r.trailingNewlines = trailingNewlineCount(out)
	}
	return out
}

func (r *reasoningAccum) canAppend(key, text string) error {
	if r == nil || text == "" {
		return nil
	}
	if len(key) > maxCodexIdentityBytes {
		return errors.New("reasoning identity exceeds size limit")
	}
	if _, exists := r.items[key]; !exists && len(r.items) >= maxCodexReasoningItems {
		return errors.New("reasoning item count exceeds limit")
	}
	if len(text) > maxCodexReasoningBytes-r.totalBytes {
		return errors.New("reasoning text exceeds total size limit")
	}
	return nil
}

func (r *reasoningAccum) text(key string) string {
	if r == nil || r.items[key] == nil {
		return ""
	}
	return r.items[key].String()
}

func trailingNewlineCount(value string) int {
	count := 0
	for i := len(value) - 1; i >= 0 && value[i] == '\n'; i-- {
		count++
	}
	return min(count, 2)
}

func (s *codexStream) read() {
	defer close(s.ch)
	defer s.body.Close()
	scanner := bufio.NewScanner(s.body)
	scanner.Buffer(make([]byte, 4096), maxCodexSSELineBytes)
	data := make([]byte, 0, 4096)
	dataBytes := 0
	dataFragments := 0
	calls := make(map[string]*toolAccum)
	event := make(map[string]any)
	bounds := &codexStreamBounds{}
	reasoning := newReasoningAccum()
	var finish protocol.StopReason
	sawTool := false
	terminal := false
	sendDone := func() {
		if finish == "" {
			if sawTool {
				finish = protocol.StopToolUse
			} else {
				finish = protocol.StopStop
			}
		}
		s.send(protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: finish})
	}
	flush := func() bool {
		if dataFragments == 0 {
			return false
		}
		payload := bytes.TrimSpace(data)
		reset := func() {
			data = data[:0]
			dataBytes = 0
			dataFragments = 0
		}
		if len(payload) == 0 {
			reset()
			return false
		}
		if bytes.Equal(payload, []byte("[DONE]")) {
			reset()
			terminal = true
			sendDone()
			return true
		}
		clear(event)
		if err := json.Unmarshal(payload, &event); err != nil {
			reset()
			s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("%s: invalid SSE event: %w", s.provider, err)})
			return true
		}
		stop := s.processEvent(event, calls, reasoning, bounds, &finish, &sawTool, &terminal)
		reset()
		return stop
	}
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			if flush() {
				return
			}
			continue
		}
		if after, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			fragment := bytes.TrimSpace(after)
			// Count a separator byte even for an empty fragment and independently
			// cap fragment count so buffer overhead cannot bypass the byte bound.
			if dataFragments >= maxCodexSSEEventFragments || len(fragment)+1 > maxCodexSSEEventBytes-dataBytes {
				s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: s.prefix("SSE event exceeds size limit")})
				return
			}
			if dataFragments > 0 {
				data = append(data, '\n')
			}
			data = append(data, fragment...)
			dataFragments++
			dataBytes += len(fragment) + 1
		}
	}
	scanErr := scanner.Err()
	if errors.Is(scanErr, providerpkg.ErrStreamIdle) && s.ctx.Err() == nil {
		s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: NewResponseError(s.provider, 0, "stream idle timeout", "stream_idle", "", s.secrets...)})
		return
	}
	if flush() {
		return
	}
	if scanErr != nil && s.ctx.Err() == nil {
		s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: NewResponseError(s.provider, 0, "stream read failed", "network_error", "", s.secrets...)})
		return
	}
	if !terminal && s.ctx.Err() == nil {
		s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: NewResponseError(s.provider, 0, "stream ended before a terminal response event", "stream_truncated", "", s.secrets...)})
	}
}
