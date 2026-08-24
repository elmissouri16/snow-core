// Package opencodego implements the OpenCode Go provider adapter: an
// OpenAI-compatible Chat Completions streaming client with bearer-token auth.
//
// The provider id is "opencode-go" (matching the auth.json key and the
// OPENCODE_API_KEY environment variable convention).
package opencodego

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type openAIChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

type openAIUsage struct {
	PromptTokens         int  `json:"prompt_tokens"`
	CompletionTokens     int  `json:"completion_tokens"`
	TotalTokens          int  `json:"total_tokens"`
	PromptCacheHitTokens *int `json:"prompt_cache_hit_tokens"`
	PromptTokensDetails  struct {
		CachedTokens        *int `json:"cached_tokens"`
		CacheCreationTokens int  `json:"cache_creation_input_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func mapUsage(u openAIUsage) *protocol.Usage {
	out := &protocol.Usage{
		Input:      u.PromptTokens,
		Output:     u.CompletionTokens,
		Reasoning:  u.CompletionTokensDetails.ReasoningTokens,
		CacheWrite: u.PromptTokensDetails.CacheCreationTokens,
	}
	cachedTokens := u.PromptTokensDetails.CachedTokens
	if cachedTokens == nil {
		cachedTokens = u.PromptCacheHitTokens
	}
	if cachedTokens != nil {
		out.CacheRead = *cachedTokens
		out.CacheReadKnown = true
	}
	out.Total = u.TotalTokens
	if out.Total == 0 {
		out.Total = out.Input + out.Output
	}
	return out
}

type toolCallAccum struct {
	index   int
	id      string
	name    string
	argsBuf strings.Builder
}

// finalArgs returns the complete arguments, wrapping malformed fragments.
func (a *toolCallAccum) finalArgs() json.RawMessage {
	raw := strings.TrimSpace(a.argsBuf.String())
	if raw == "" {
		raw = "{}"
	}
	if !json.Valid([]byte(raw)) {
		wrapped, _ := json.Marshal(map[string]string{"_raw": raw})
		return wrapped
	}
	return json.RawMessage(raw)
}

// stream is the channel-backed EventStream returned by Chat.
type stream struct {
	ch                 chan protocol.StreamEvent
	done               chan struct{}
	reqCtx             context.Context
	closeFn            func()
	once               sync.Once
	totalArgs          int
	responseTextBytes  int
	reasoningTextBytes int
	provider           string
	secret             string
}

func newStream(ctx context.Context, buf int, closeFn func(), provider, secret string) *stream {
	return &stream{
		ch:       make(chan protocol.StreamEvent, buf),
		done:     make(chan struct{}),
		reqCtx:   ctx,
		closeFn:  closeFn,
		provider: provider,
		secret:   secret,
	}
}

// Next implements protocol.EventStream.
func (s *stream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	select {
	case ev, ok := <-s.ch:
		if !ok {
			return protocol.StreamEvent{}, io.EOF
		}
		return ev, nil
	case <-ctx.Done():
		return protocol.StreamEvent{}, ctx.Err()
	case <-s.reqCtx.Done():
		return protocol.StreamEvent{}, s.reqCtx.Err()
	case <-s.done:
		return protocol.StreamEvent{}, io.EOF
	}
}

// Close implements protocol.EventStream.
func (s *stream) Close() error {
	s.once.Do(func() {
		if s.closeFn != nil {
			s.closeFn()
		}
		close(s.done)
	})
	return nil
}

// send delivers an event unless the stream is being torn down.
func (s *stream) send(ev protocol.StreamEvent) {
	select {
	case s.ch <- ev:
	case <-s.reqCtx.Done():
	case <-s.done:
	}
}

// errorStream returns a stream whose first event is EvStreamError, then EOF.
func errorStream(ctx context.Context, err error) protocol.EventStream {
	s := newStream(ctx, 1, nil, ProviderID, "")
	s.ch <- protocol.StreamEvent{Type: protocol.EvStreamError, Err: err}
	close(s.ch)
	return s
}

// readSSE consumes the response body and normalizes events into the stream.
// Only the user's Close() closes s.done; this goroutine only closes s.ch so
// buffered events are always drained before EOF is reported.
func (s *stream) readSSE(resp *http.Response) {
	defer close(s.ch)
	defer func() { _ = resp.Body.Close() }()

	r := bufio.NewReader(resp.Body)
	var (
		eventName        string
		accums           = make(map[int]*toolCallAccum)
		order            []int // insertion order of tool call indices
		finish           protocol.StopReason
		doneSent         bool
		errored          bool
		terminalObserved bool
	)
	markError := func(ev protocol.StreamEvent) {
		errored = true
		s.send(ev)
	}
	sendDone := func() {
		if doneSent || errored {
			return
		}
		doneSent = true
		if finish == "" {
			finish = protocol.StopStop
		}
		s.send(protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: finish})
	}

	for {
		line, err := readBoundedSSELine(r, maxSSELineBytes)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, providerpkg.ErrStreamIdle) {
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("%s: SSE line exceeds limit or is unreadable: %w", s.provider, err)})
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			stop := s.handleLine(line, &eventName, accums, &order, &finish, &terminalObserved, markError)
			if stop {
				sendDone()
				return
			}
		}
		if errors.Is(err, providerpkg.ErrStreamIdle) {
			cause := fmt.Errorf("%s: stream idle timeout: %w", s.provider, err)
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: &providerpkg.AdvisedError{Err: cause, Advice: providerpkg.RetryAdvice{Kind: providerpkg.RetryTransient}}})
			return
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if terminalObserved {
					sendDone()
				} else {
					cause := fmt.Errorf("%s: stream truncated before terminal event", s.provider)
					markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: &providerpkg.AdvisedError{Err: cause, Advice: providerpkg.RetryAdvice{Kind: providerpkg.RetryTransient}}})
				}
				return
			}
			select {
			case <-s.done:
				return // user closed the stream; not an error
			default:
			}
			if s.reqCtx.Err() != nil {
				return // cancellation; Next() surfaces ctx.Err()
			}
			cause := fmt.Errorf("%s: stream read failed: %w", s.provider, err)
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: &providerpkg.AdvisedError{Err: cause, Advice: providerpkg.RetryAdvice{Kind: providerpkg.RetryTransient}}})
			return
		}
	}
}

// handleLine returns true when the stream should stop (e.g. after [DONE]).
func (s *stream) handleLine(line []byte, eventName *string, accums map[int]*toolCallAccum, order *[]int, finish *protocol.StopReason, terminalObserved *bool, markError func(protocol.StreamEvent)) bool {
	switch {
	case bytes.HasPrefix(line, []byte("event:")):
		*eventName = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("event:"))))
	case bytes.HasPrefix(line, []byte("data:")):
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 {
			return false
		}
		if *eventName == "error" {
			*eventName = ""
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: errors.New(s.provider + ": " + providerpkg.RedactSecrets(string(data), s.secret))})
			return false
		}
		*eventName = ""
		if bytes.Equal(data, []byte("[DONE]")) {
			// [DONE] terminates the stream; some servers keep the connection
			// open afterwards, so signal the caller to stop reading.
			*terminalObserved = true
			return true
		}
		var chunk openAIChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			var errPayload struct {
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if uerr := json.Unmarshal(data, &errPayload); uerr == nil && errPayload.Error != nil && errPayload.Error.Message != "" {
				markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: errors.New(s.provider + ": " + providerpkg.RedactSecrets(errPayload.Error.Message, s.secret))})
			} else {
				// Malformed chunk: surface it rather than silently dropping.
				markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("%s: unparseable SSE data: %s", s.provider, truncateStr(providerpkg.RedactSecrets(string(data), s.secret), 500))})
			}
			return false
		}
		if err := s.processChunk(chunk, accums, order, finish); err != nil {
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: err})
			return true
		}
		if *finish != "" {
			*terminalObserved = true
		}
	}
	return false
}

func (s *stream) processChunk(chunk openAIChunk, accums map[int]*toolCallAccum, order *[]int, finish *protocol.StopReason) error {
	if chunk.Usage != nil {
		s.send(protocol.StreamEvent{Type: protocol.EvStreamUsage, Usage: mapUsage(*chunk.Usage)})
	}
	for _, ch := range chunk.Choices {
		d := ch.Delta
		if d.Content != "" {
			if len(d.Content) > maxResponseTextBytes-s.responseTextBytes {
				return fmt.Errorf("%s: response text exceeds size limit", s.provider)
			}
			s.responseTextBytes += len(d.Content)
			s.send(protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: d.Content})
		}
		if d.ReasoningContent != "" {
			if len(d.ReasoningContent) > maxReasoningBytes-s.reasoningTextBytes {
				return fmt.Errorf("%s: reasoning text exceeds size limit", s.provider)
			}
			s.reasoningTextBytes += len(d.ReasoningContent)
			s.send(protocol.StreamEvent{Type: protocol.EvStreamThinkingDelta, Text: d.ReasoningContent})
		}
		for _, tc := range d.ToolCalls {
			acc, ok := accums[tc.Index]
			if !ok {
				if len(accums) >= maxStreamToolCalls {
					return fmt.Errorf("%s: too many streamed tool calls", s.provider)
				}
				acc = &toolCallAccum{index: tc.Index}
				accums[tc.Index] = acc
				*order = append(*order, tc.Index)
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			// Sticky fallback id: some compatible servers defer/omit ids.
			if acc.id == "" {
				acc.id = fmt.Sprintf("tc-%d", acc.index)
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			fragmentBytes := len(tc.Function.Arguments)
			if acc.argsBuf.Len()+fragmentBytes > maxToolArgumentBytes || s.totalArgs+fragmentBytes > maxTotalToolArgumentBytes {
				return fmt.Errorf("%s: streamed tool arguments exceed limit", s.provider)
			}
			// Deltas carry only the fragment; the agent appends fragments.
			s.send(protocol.StreamEvent{
				Type:       protocol.EvStreamToolCallDelta,
				ToolCallID: acc.id,
				ToolName:   acc.name,
				Arguments:  json.RawMessage(tc.Function.Arguments),
			})
			if tc.Function.Arguments != "" {
				acc.argsBuf.WriteString(tc.Function.Arguments)
				s.totalArgs += fragmentBytes
			}
		}
		// finish_reason is first-wins: a trailing chunk must not overwrite an
		// earlier tool_calls/stop decision.
		switch ch.FinishReason {
		case "stop":
			if *finish == "" {
				*finish = protocol.StopStop
			}
		case "length":
			if *finish == "" {
				*finish = protocol.StopLength
			}
		case "tool_calls":
			if *finish == "" {
				*finish = protocol.StopToolUse
			}
			if *finish == protocol.StopToolUse {
				for _, idx := range *order {
					acc := accums[idx]
					if acc == nil {
						continue
					}
					s.send(protocol.StreamEvent{
						Type:       protocol.EvStreamToolCallDone,
						ToolCallID: acc.id,
						ToolName:   acc.name,
						Arguments:  acc.finalArgs(),
					})
				}
			}
		}
	}
	return nil
}

func readBoundedSSELine(reader *bufio.Reader, maxBytes int) ([]byte, error) {
	part, err := reader.ReadSlice('\n')
	if len(part) > maxBytes {
		return nil, fmt.Errorf("line exceeds %d bytes", maxBytes)
	}
	if err != bufio.ErrBufferFull {
		// The borrowed reader buffer remains valid until the next read, and the
		// caller processes it synchronously before advancing the stream.
		return part, err
	}
	line := append([]byte(nil), part...)
	for {
		part, err = reader.ReadSlice('\n')
		if len(line)+len(part) > maxBytes {
			return nil, fmt.Errorf("line exceeds %d bytes", maxBytes)
		}
		line = append(line, part...)
		if err != bufio.ErrBufferFull {
			return line, err
		}
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
