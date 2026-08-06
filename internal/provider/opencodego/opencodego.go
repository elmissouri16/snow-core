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
	"os"
	"strings"
	"sync"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

// ProviderID is the stable provider identifier.
const ProviderID = "opencode-go"

// EnvAPIKey is the environment variable holding the OpenCode Go API key.
const EnvAPIKey = "OPENCODE_API_KEY"

// DefaultBaseURL is the OpenCode Go API base URL.
//
// NOTE: The production base URL must be verified against current OpenCode Go
// documentation at implement time (IMPLEMENTATION.md §5.6 verify checklist).
// This default is a placeholder that works against local/dev gateways and is
// overridable via Config.BaseURL.
const DefaultBaseURL = "https://opencode.ai/api/v1"

// DefaultModelID is the fallback model id used when neither the request nor
// config selects one. pi currently defaults OpenCode Go to "kimi-k2.6";
// this must be verified against the live catalog before release.
const DefaultModelID = "kimi-k2.6"

// Config controls the OpenCode Go adapter.
type Config struct {
	// BaseURL overrides DefaultBaseURL. Empty means default.
	BaseURL string
	// APIKey is a compile-time/config fallback credential (lowest priority).
	APIKey string
	// HTTPClient overrides http.DefaultClient. The client must not impose a
	// total timeout that would kill long-lived streams; callers cancel via ctx.
	HTTPClient *http.Client
	// DefaultModel overrides DefaultModelID.
	DefaultModel string
}

// Provider implements provider.Provider for OpenCode Go.
type Provider struct {
	baseURL      string
	apiKey       string
	client       *http.Client
	defaultModel string
}

// New validates and constructs the provider.
func New(cfg Config) (*Provider, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	model := cfg.DefaultModel
	if model == "" {
		model = DefaultModelID
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{baseURL: base, apiKey: cfg.APIKey, client: client, defaultModel: model}, nil
}

// ID implements provider.Provider.
func (p *Provider) ID() string { return ProviderID }

// resolveKey returns the first usable API key from the credential, the
// environment, then config, in that order.
func (p *Provider) resolveKey(creds auth.Credential) string {
	if creds.Key != "" {
		return creds.Key
	}
	if env := os.Getenv(EnvAPIKey); env != "" {
		return env
	}
	return p.apiKey
}

// Resolve implements provider.Provider: the credential is usable when any key
// source is present.
func (p *Provider) Resolve(_ context.Context, creds auth.Credential) error {
	if p.resolveKey(creds) != "" {
		return nil
	}
	return fmt.Errorf("opencode-go: no API key found: set %s, add a %q entry to the auth file, or pass a credential explicitly", EnvAPIKey, ProviderID)
}

// staticCatalog returns the guaranteed-available fallback catalog.
func (p *Provider) staticCatalog() []protocol.Model {
	return []protocol.Model{{
		Provider:         ProviderID,
		ID:               p.defaultModel,
		DisplayName:      "OpenCode Go Default",
		ContextWindow:    200000,
		SupportsTools:    true,
		SupportsThinking: true,
	}}
}

// ListModels implements provider.Provider. It returns the static catalog when
// the remote catalog cannot be fetched; it never fails on network errors.
func (p *Provider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	static := p.staticCatalog()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return static, nil
	}
	if key := p.resolveKey(auth.Credential{}); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return static, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return static, nil
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return static, nil
	}
	out := make([]protocol.Model, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID == "" {
			continue
		}
		out = append(out, protocol.Model{
			Provider:         ProviderID,
			ID:               m.ID,
			DisplayName:      "OpenCode Go " + m.ID,
			ContextWindow:    200000,
			SupportsTools:    true,
			SupportsThinking: true,
		})
	}
	if len(out) == 0 {
		return static, nil
	}
	return out, nil
}

// Chat implements provider.Provider: starts an SSE streaming chat request and
// returns a normalized EventStream.
func (p *Provider) Chat(ctx context.Context, creds auth.Credential, req protocol.ChatRequest) (protocol.EventStream, error) {
	key := p.resolveKey(creds)
	if key == "" {
		return errorStream(ctx, fmt.Errorf("opencode-go: no API key found: set %s, add a %q entry to the auth file, or pass a credential explicitly", EnvAPIKey, ProviderID)), nil
	}

	body, err := p.buildBody(req)
	if err != nil {
		return errorStream(ctx, fmt.Errorf("opencode-go: build request: %w", err)), nil
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return errorStream(ctx, fmt.Errorf("opencode-go: create request: %w", err)), nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return errorStream(ctx, fmt.Errorf("opencode-go: request failed: %w", err)), nil
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		msg := fmt.Sprintf("opencode-go: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
		if resp.StatusCode == http.StatusUnauthorized {
			msg = "opencode-go: invalid API key (HTTP 401)"
		}
		return errorStream(ctx, errors.New(msg)), nil
	}

	s := newStream(ctx, 64, func() { _ = resp.Body.Close() })
	go s.readSSE(resp)
	return s, nil
}

// ---------------------------------------------------------------------------
// Request building
// ---------------------------------------------------------------------------

type openAIChatRequest struct {
	Model         string          `json:"model"`
	Messages      []openAIMessage `json:"messages"`
	Stream        bool            `json:"stream"`
	StreamOptions *streamOptions  `json:"stream_options,omitempty"`
	Tools         []openAITool    `json:"tools,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	MaxTokens     *int            `json:"max_tokens,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function openAIToolCallFunction `json:"function,omitempty"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// buildBody maps a ChatRequest into the OpenAI wire format.
func (p *Provider) buildBody(req protocol.ChatRequest) ([]byte, error) {
	model := req.Model.ID
	if model == "" {
		model = p.defaultModel
	}
	oreq := openAIChatRequest{
		Model:         model,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}
	if req.System != "" {
		oreq.Messages = append(oreq.Messages, openAIMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		om, ok := mapMessage(m)
		if !ok {
			continue
		}
		oreq.Messages = append(oreq.Messages, om)
	}
	for _, t := range req.Tools {
		params := t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		oreq.Tools = append(oreq.Tools, openAITool{
			Type: "function",
			Function: openAIToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	if req.MaxTokens > 0 {
		v := req.MaxTokens
		oreq.MaxTokens = &v
	}
	if req.Temperature != nil {
		oreq.Temperature = req.Temperature
	}
	return json.Marshal(oreq)
}

// mapMessage converts a protocol message to the OpenAI wire format. The bool
// result is false for message roles that cannot be represented.
func mapMessage(m protocol.Message) (openAIMessage, bool) {
	switch m.Role {
	case protocol.RoleUser:
		return openAIMessage{Role: "user", Content: textContent(m)}, true
	case protocol.RoleAssistant:
		om := openAIMessage{Role: "assistant", Content: textContent(m)}
		for _, b := range m.Content {
			if b.Type != protocol.BlockToolCall {
				continue
			}
			args := string(b.Arguments)
			if args == "" {
				args = "{}"
			}
			om.ToolCalls = append(om.ToolCalls, openAIToolCall{
				ID:   b.ToolCallID,
				Type: "function",
				Function: openAIToolCallFunction{
					Name:      b.Name,
					Arguments: args,
				},
			})
		}
		return om, true
	case protocol.RoleTool:
		return openAIMessage{Role: "tool", ToolCallID: m.ToolCallID, Content: textContent(m)}, true
	case protocol.RoleCustom:
		// Harness notes surface as assistant text.
		return openAIMessage{Role: "assistant", Content: textContent(m)}, true
	default:
		return openAIMessage{}, false
	}
}

// textContent joins the representable text of a message. Thinking blocks are
// skipped for OpenCode Go (no reasoning_content support is assumed). Images
// are not yet supported by this adapter.
func textContent(m protocol.Message) string {
	var sb strings.Builder
	for _, b := range m.Content {
		switch b.Type {
		case protocol.BlockText:
			sb.WriteString(b.Text)
		case protocol.BlockThinking:
			// Skipped.
		default:
			// Tool call blocks contribute no text; images unsupported.
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// SSE response parsing
// ---------------------------------------------------------------------------

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
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func mapUsage(u openAIUsage) *protocol.Usage {
	out := &protocol.Usage{
		Input:     u.PromptTokens,
		Output:    u.CompletionTokens,
		CacheRead: u.PromptTokensDetails.CachedTokens,
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
	ch      chan protocol.StreamEvent
	done    chan struct{}
	reqCtx  context.Context
	closeFn func()
	once    sync.Once
}

func newStream(ctx context.Context, buf int, closeFn func()) *stream {
	return &stream{
		ch:      make(chan protocol.StreamEvent, buf),
		done:    make(chan struct{}),
		reqCtx:  ctx,
		closeFn: closeFn,
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
	s := newStream(ctx, 1, nil)
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
		eventName string
		accums    = make(map[int]*toolCallAccum)
		order     []int // insertion order of tool call indices
		finish    protocol.StopReason
		doneSent  bool
		errored   bool
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
		line, err := r.ReadString('\n')
		if line != "" {
			stop := s.handleLine(strings.TrimSpace(line), &eventName, accums, &order, &finish, markError)
			if stop {
				sendDone()
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				sendDone()
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
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("opencode-go: stream read failed: %w", err)})
			return
		}
	}
}

// handleLine returns true when the stream should stop (e.g. after [DONE]).
func (s *stream) handleLine(line string, eventName *string, accums map[int]*toolCallAccum, order *[]int, finish *protocol.StopReason, markError func(protocol.StreamEvent)) bool {
	switch {
	case strings.HasPrefix(line, "event:"):
		*eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
	case strings.HasPrefix(line, "data:"):
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			return false
		}
		if *eventName == "error" {
			*eventName = ""
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: errors.New("opencode-go: " + data)})
			return false
		}
		*eventName = ""
		if data == "[DONE]" {
			// [DONE] terminates the stream; some servers keep the connection
			// open afterwards, so signal the caller to stop reading.
			return true
		}
		var chunk openAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			var errPayload struct {
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if uerr := json.Unmarshal([]byte(data), &errPayload); uerr == nil && errPayload.Error != nil && errPayload.Error.Message != "" {
				markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: errors.New("opencode-go: " + errPayload.Error.Message)})
			} else {
				// Malformed chunk: surface it rather than silently dropping.
				markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("opencode-go: unparseable SSE data: %s", truncateStr(data, 500))})
			}
			return false
		}
		s.processChunk(chunk, accums, order, finish)
	}
	return false
}

func (s *stream) processChunk(chunk openAIChunk, accums map[int]*toolCallAccum, order *[]int, finish *protocol.StopReason) {
	if chunk.Usage != nil {
		s.send(protocol.StreamEvent{Type: protocol.EvStreamUsage, Usage: mapUsage(*chunk.Usage)})
	}
	for _, ch := range chunk.Choices {
		d := ch.Delta
		if d.Content != "" {
			s.send(protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: d.Content})
		}
		if d.ReasoningContent != "" {
			s.send(protocol.StreamEvent{Type: protocol.EvStreamThinkingDelta, Text: d.ReasoningContent})
		}
		for _, tc := range d.ToolCalls {
			acc, ok := accums[tc.Index]
			if !ok {
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
			// Deltas carry only the fragment; the agent appends fragments.
			s.send(protocol.StreamEvent{
				Type:       protocol.EvStreamToolCallDelta,
				ToolCallID: acc.id,
				ToolName:   acc.name,
				Arguments:  json.RawMessage(tc.Function.Arguments),
			})
			if tc.Function.Arguments != "" {
				acc.argsBuf.WriteString(tc.Function.Arguments)
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
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
