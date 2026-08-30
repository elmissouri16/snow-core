package opencodego

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type finalIdleReader struct {
	data []byte
}

func (r *finalIdleReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, providerpkg.ErrStreamIdle
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, providerpkg.ErrStreamIdle
}
func (*finalIdleReader) Close() error { return nil }

func TestReadSSEProcessesCompleteFinalLineBeforeIdleError(t *testing.T) {
	stream := newStream(context.Background(), 4, nil, ProviderID, "")
	go stream.readSSE(&http.Response{Body: &finalIdleReader{data: []byte("data: [DONE]")}})
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamDone {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

// sseChunk builds a "data: {...}" SSE line from a JSON object.
func sseChunk(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return "data: " + string(b) + "\n\n"
}

func chunkWith(delta map[string]any, finish string, usage map[string]any) map[string]any {
	choice := map[string]any{"index": 0}
	if delta != nil {
		choice["delta"] = delta
	}
	if finish != "" {
		choice["finish_reason"] = finish
	}
	m := map[string]any{"choices": []map[string]any{choice}}
	if usage != nil {
		m["usage"] = usage
	}
	return m
}

func mustNew(t *testing.T, base, key string) *Provider {
	t.Helper()
	p, err := New(Config{BaseURL: base, APIKey: key})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// TestLiveDefaultsPinned guards against regressions to the verified production
// constants: these were confirmed live against https://opencode.ai/zen/go/v1
// (GET /models → 200 OpenAI list; bad key on /chat/completions → 401 JSON).
func TestChatCompletionsEncodesImageContent(t *testing.T) {
	p := mustNew(t, "https://example.test/v1", "k")
	message := protocol.NewUserMessage("u", "", "describe")
	message.Content = append(message.Content, protocol.ContentBlock{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{1, 2, 3}})
	body, err := p.buildBody(protocol.ChatRequest{Model: protocol.Model{ID: "m", SupportsVision: true}, Messages: []protocol.Message{message}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Messages []struct {
			Content []struct {
				Type     string `json:"type"`
				ImageURL *struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Messages) != 1 || len(decoded.Messages[0].Content) != 2 || decoded.Messages[0].Content[1].ImageURL == nil || decoded.Messages[0].Content[1].ImageURL.URL != "data:image/png;base64,AQID" {
		t.Fatalf("messages=%+v body=%s", decoded.Messages, body)
	}
}

func TestStreamRejectsCumulativeTextAndReasoningOverflow(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		configure func(*stream)
		want      string
	}{
		{
			name:    "response text",
			payload: `{"choices":[{"delta":{"content":"x"}}]}`,
			configure: func(s *stream) {
				s.responseTextBytes = maxResponseTextBytes
			},
			want: "response text exceeds size limit",
		},
		{
			name:    "reasoning text",
			payload: `{"choices":[{"delta":{"reasoning_content":"x"}}]}`,
			configure: func(s *stream) {
				s.reasoningTextBytes = maxReasoningBytes
			},
			want: "reasoning text exceeds size limit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var chunk openAIChunk
			if err := json.Unmarshal([]byte(tt.payload), &chunk); err != nil {
				t.Fatal(err)
			}
			s := newStream(context.Background(), 1, nil, ProviderID, "")
			tt.configure(s)
			accums := make(map[int]*toolCallAccum)
			var order []int
			var finish protocol.StopReason
			err := s.processChunk(chunk, accums, &order, &finish)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("processChunk error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMapUsageDistinguishesExplicitZeroFromOmittedCacheRead(t *testing.T) {
	var explicit openAIUsage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":4,"prompt_tokens_details":{"cached_tokens":0}}`), &explicit); err != nil {
		t.Fatal(err)
	}
	if usage := mapUsage(explicit); !usage.CacheReadKnown || usage.CacheRead != 0 {
		t.Fatalf("explicit usage = %+v", usage)
	}
	var fallback openAIUsage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":4,"prompt_cache_hit_tokens":2}`), &fallback); err != nil {
		t.Fatal(err)
	}
	if usage := mapUsage(fallback); !usage.CacheReadKnown || usage.CacheRead != 2 {
		t.Fatalf("fallback usage = %+v", usage)
	}
	var fallbackZero openAIUsage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":4,"prompt_cache_hit_tokens":0}`), &fallbackZero); err != nil {
		t.Fatal(err)
	}
	if usage := mapUsage(fallbackZero); !usage.CacheReadKnown || usage.CacheRead != 0 {
		t.Fatalf("fallback zero usage = %+v", usage)
	}
	var omitted openAIUsage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":4,"prompt_tokens_details":{}}`), &omitted); err != nil {
		t.Fatal(err)
	}
	if usage := mapUsage(omitted); usage.CacheReadKnown {
		t.Fatalf("omitted usage = %+v", usage)
	}
}

func TestListModelsCachesAcrossProviderInstances(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/catalog" {
			_, _ = io.WriteString(w, `{"opencode-go":{"models":{"cached-model":{"id":"cached-model","name":"Cached","tool_call":true,"limit":{"context":64000,"output":4096}}}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"cached-model"}]}`)
	}))
	defer server.Close()
	cacheRoot := t.TempDir()
	newProvider := func() *Provider {
		provider, err := New(Config{BaseURL: server.URL, CatalogURL: server.URL + "/catalog", CacheRoot: cacheRoot})
		if err != nil {
			t.Fatal(err)
		}
		return provider
	}
	models, err := newProvider().ListModels(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "cached-model" {
		t.Fatalf("first models=%+v err=%v", models, err)
	}
	firstHits := hits.Load()
	models, err = newProvider().ListModels(context.Background())
	if err != nil || len(models) != 1 || hits.Load() != firstHits {
		t.Fatalf("cached models=%+v err=%v hits=%d want=%d", models, err, hits.Load(), firstHits)
	}
	info, err := os.Stat(filepath.Join(cacheRoot, "catalog.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode=%v err=%v", info, err)
	}
}

func TestLiveDefaultsPinned(t *testing.T) {
	if DefaultBaseURL != "https://opencode.ai/zen/go/v1" {
		t.Errorf("DefaultBaseURL = %q, want https://opencode.ai/zen/go/v1 (verified live)", DefaultBaseURL)
	}
	if DefaultModelID != "kimi-k2.6" {
		t.Errorf("DefaultModelID = %q, want kimi-k2.6 (verified in live /zen/go/v1/models catalog)", DefaultModelID)
	}
	if DefaultCatalogURL != "https://models.dev/api.json" {
		t.Errorf("DefaultCatalogURL = %q, want https://models.dev/api.json", DefaultCatalogURL)
	}
}

func drain(t *testing.T, s protocol.EventStream, ctx context.Context) []protocol.StreamEvent {
	t.Helper()
	defer s.Close()
	var out []protocol.StreamEvent
	for {
		ev, err := s.Next(ctx)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		out = append(out, ev)
	}
}

// TestChatStreamSequence verifies the full normalized event sequence: text
// delta, fragmented tool call across two chunks, tool_call_done with complete
// parsed arguments, then a separate continuation stream with text, usage,
// and a final done(stop) — mirroring the agent loop's two provider calls.
func TestChatStreamSequence(t *testing.T) {
	var mu sync.Mutex
	var sawRequest bool
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sawRequest = true
		callCount++
		n := callCount
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		write := func(s string) {
			_, _ = fmt.Fprint(w, s)
			if fl != nil {
				fl.Flush()
			}
		}
		if n == 1 {
			// Stream 1: text, fragmented tool call, finish tool_calls.
			write(sseChunk(chunkWith(map[string]any{"content": "Hello"}, "", nil)))
			write(sseChunk(chunkWith(map[string]any{"tool_calls": []map[string]any{{
				"index": 0, "id": "call_1", "type": "function",
				"function": map[string]any{"name": "read", "arguments": `{"path": "a`},
			}}}, "", nil)))
			write(sseChunk(chunkWith(map[string]any{"tool_calls": []map[string]any{{
				"index":    0,
				"function": map[string]any{"arguments": `bc.txt"}`},
			}}}, "", nil)))
			write(sseChunk(chunkWith(nil, "tool_calls", nil)))
		} else {
			// Stream 2: assistant text, usage, finish stop.
			write(sseChunk(chunkWith(map[string]any{"content": "Done reading"}, "", nil)))
			write(sseChunk(chunkWith(nil, "stop", map[string]any{
				"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15,
			})))
		}
		write("data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := mustNew(t, srv.URL, "test-key")
	ctx := t.Context()
	req := protocol.ChatRequest{
		Model:    protocol.Model{ID: "kimi-k2.6"},
		Messages: []protocol.Message{protocol.NewUserMessage("u1", "", "hi")},
	}

	// Stream 1: tool-call phase.
	s1, err := p.Chat(ctx, auth.Credential{Type: auth.CredentialAPIKey, Key: "test-key"}, req)
	if err != nil {
		t.Fatalf("Chat 1: %v", err)
	}
	events := drain(t, s1, ctx)

	var types []protocol.StreamEventType
	for _, e := range events {
		types = append(types, e.Type)
	}
	wantTypes := []protocol.StreamEventType{
		protocol.EvStreamTextDelta,
		protocol.EvStreamToolCallDelta,
		protocol.EvStreamToolCallDelta,
		protocol.EvStreamToolCallDone,
		protocol.EvStreamDone,
	}
	if len(types) != len(wantTypes) {
		t.Fatalf("stream1 event count mismatch: got %v want %v", types, wantTypes)
	}
	for i := range wantTypes {
		if types[i] != wantTypes[i] {
			t.Fatalf("stream1 event[%d] = %s, want %s (all: %v)", i, types[i], wantTypes[i], types)
		}
	}
	// First text delta
	if events[0].Text != "Hello" {
		t.Errorf("text delta = %q, want %q", events[0].Text, "Hello")
	}
	// tool_call_done must carry the complete parsed arguments JSON
	done := events[3]
	if done.Type != protocol.EvStreamToolCallDone {
		t.Fatalf("events[3] = %s, want tool_call_done", done.Type)
	}
	if done.ToolCallID != "call_1" || done.ToolName != "read" {
		t.Errorf("tool_call_done id/name = %q/%q, want call_1/read", done.ToolCallID, done.ToolName)
	}
	var args map[string]string
	if err := json.Unmarshal(done.Arguments, &args); err != nil {
		t.Fatalf("tool_call_done arguments not valid JSON: %v (%s)", err, done.Arguments)
	}
	if args["path"] != "abc.txt" {
		t.Errorf("tool_call_done args = %v, want path=abc.txt", args)
	}
	// done(tool_use)
	if events[4].Type != protocol.EvStreamDone || events[4].StopReason != protocol.StopToolUse {
		t.Fatalf("stream1 done = %+v, want done(tool_use)", events[4])
	}

	// Stream 2: continuation phase.
	s2, err := p.Chat(ctx, auth.Credential{Type: auth.CredentialAPIKey, Key: "test-key"}, req)
	if err != nil {
		t.Fatalf("Chat 2: %v", err)
	}
	events2 := drain(t, s2, ctx)
	var types2 []protocol.StreamEventType
	for _, e := range events2 {
		types2 = append(types2, e.Type)
	}
	wantTypes2 := []protocol.StreamEventType{
		protocol.EvStreamTextDelta,
		protocol.EvStreamUsage,
		protocol.EvStreamDone,
	}
	if len(types2) != len(wantTypes2) {
		t.Fatalf("stream2 event count mismatch: got %v want %v", types2, wantTypes2)
	}
	if events2[0].Text != "Done reading" {
		t.Errorf("stream2 text = %q, want %q", events2[0].Text, "Done reading")
	}
	u := events2[1].Usage
	if u == nil || u.Input != 10 || u.Output != 5 || u.Total != 15 {
		t.Errorf("usage = %+v, want input=10 output=5 total=15", u)
	}
	if events2[2].StopReason != protocol.StopStop {
		t.Errorf("stream2 done stop reason = %q, want stop", events2[2].StopReason)
	}
	mu.Lock()
	defer mu.Unlock()
	if !sawRequest {
		t.Error("server never saw the request")
	}
}

// TestChatToolUseDone verifies that a stream ending right after tool_calls
// produces a done(tool_use) event at EOF.
func TestChatToolUseDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"tool_calls": []map[string]any{{
			"index": 0, "id": "call_x", "type": "function",
			"function": map[string]any{"name": "bash", "arguments": `{"command":"ls"}`},
		}}}, "tool_calls", nil)))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
	}))
	defer srv.Close()

	p := mustNew(t, srv.URL, "k")
	s, err := p.Chat(context.Background(), auth.Credential{Key: "k"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, s, context.Background())
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3 (tool_call_delta, tool_call_done, done): %+v", len(events), events)
	}
	if events[0].Type != protocol.EvStreamToolCallDelta {
		t.Fatalf("events[0] = %s, want tool_call_delta", events[0].Type)
	}
	if events[1].Type != protocol.EvStreamToolCallDone {
		t.Fatalf("events[1] = %s, want tool_call_done", events[1].Type)
	}
	if events[2].Type != protocol.EvStreamDone || events[2].StopReason != protocol.StopToolUse {
		t.Fatalf("events[2] = %+v, want done(tool_use)", events[2])
	}
}

func TestChatRedactsActiveKeyFromProviderError(t *testing.T) {
	const key = "super-secret-api-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "backend echoed %s", key)
	}))
	defer server.Close()
	provider := mustNew(t, server.URL, key)
	stream, err := provider.Chat(context.Background(), auth.Credential{Key: key}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, stream, context.Background())
	if len(events) != 1 || events[0].Err == nil || strings.Contains(events[0].Err.Error(), key) || !strings.Contains(events[0].Err.Error(), "[redacted]") {
		t.Fatalf("unredacted provider error = %+v", events)
	}
}

// TestChatUnauthorized verifies 401 produces a descriptive EvStreamError.
func TestChatUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":{"message":"Incorrect API key provided"}}`)
	}))
	defer srv.Close()

	p := mustNew(t, srv.URL, "bad-key")
	s, err := p.Chat(context.Background(), auth.Credential{Key: "bad-key"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatalf("Chat should not fail before stream: %v", err)
	}
	events := drain(t, s, context.Background())
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 error event", len(events))
	}
	ev := events[0]
	if ev.Type != protocol.EvStreamError {
		t.Fatalf("event type = %s, want error", ev.Type)
	}
	if ev.Err == nil {
		t.Fatal("error event missing Err")
	}
	if !strings.Contains(ev.Err.Error(), "401") || !strings.Contains(ev.Err.Error(), "API key") {
		t.Errorf("error message = %q, want mention of 401 and API key", ev.Err.Error())
	}
}

// TestChatRequestBody verifies the wire format: auth header, model, tools
// mapping, and message role mapping including assistant tool_calls and tool
// role with tool_call_id.
func TestChatRequestBody(t *testing.T) {
	var (
		mu      sync.Mutex
		gotAuth string
		gotBody []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"content": "ok"}, "stop", nil)))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
	}))
	defer srv.Close()

	p := mustNew(t, srv.URL, "cfg-key")
	msg := protocol.Message{
		ID:   "a1",
		Role: protocol.RoleAssistant,
		Content: []protocol.ContentBlock{
			{Type: protocol.BlockText, Text: "Let me check"},
			{Type: protocol.BlockToolCall, ToolCallID: "call_9", Name: "read", Arguments: json.RawMessage(`{"path":"x.go"}`)},
		},
	}
	toolRes := protocol.NewToolResultMessage("t1", "a1", "call_9", "read",
		[]protocol.ContentBlock{protocol.NewTextBlock("contents")}, false)

	s, err := p.Chat(context.Background(), auth.Credential{Key: ""}, protocol.ChatRequest{
		Model:  protocol.Model{ID: "model-42"},
		System: "be careful",
		Messages: []protocol.Message{
			protocol.NewUserMessage("u1", "", "please"),
			msg,
			toolRes,
		},
		Tools: []protocol.ToolSchema{{
			Name:        "edit",
			Description: "Edit a file",
			Parameters:  json.RawMessage(`{"type":"object","required":["path"]}`),
			Discovery:   &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Namespace: "files"},
		}},
		MaxTokens: 512,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	_ = drain(t, s, context.Background())

	mu.Lock()
	defer mu.Unlock()
	if gotAuth != "Bearer cfg-key" {
		t.Errorf("Authorization = %q, want Bearer cfg-key", gotAuth)
	}

	var req struct {
		Model         string `json:"model"`
		Stream        bool   `json:"stream"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
		MaxTokens int `json:"max_tokens"`
		Messages  []struct {
			Role       string `json:"role"`
			Content    any    `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request body: %v\n%s", err, gotBody)
	}
	if req.Model != "model-42" {
		t.Errorf("model = %q, want model-42", req.Model)
	}
	if !req.Stream || !req.StreamOptions.IncludeUsage {
		t.Errorf("stream=%v include_usage=%v, want true/true", req.Stream, req.StreamOptions.IncludeUsage)
	}
	if req.MaxTokens != 512 {
		t.Errorf("max_tokens = %d, want 512", req.MaxTokens)
	}
	if len(req.Tools) != 1 || req.Tools[0].Type != "function" {
		t.Fatalf("tools = %+v, want one function tool", req.Tools)
	}
	tool := req.Tools[0].Function
	if tool.Name != "edit" || tool.Description != "Edit a file" {
		t.Errorf("tool function = %q/%q, want edit/Edit a file", tool.Name, tool.Description)
	}
	if !json.Valid(tool.Parameters) || !strings.Contains(string(tool.Parameters), `"required"`) {
		t.Errorf("tool parameters = %s, want valid schema", tool.Parameters)
	}
	if strings.Contains(string(gotBody), `"discovery"`) || strings.Contains(string(gotBody), `"namespace":"files"`) {
		t.Errorf("host discovery metadata leaked to provider: %s", gotBody)
	}
	// roles: system, user, assistant, tool
	if len(req.Messages) != 4 {
		t.Fatalf("messages = %d, want 4: %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("messages[0] role = %q, want system", req.Messages[0].Role)
	}
	if req.Messages[1].Role != "user" {
		t.Errorf("messages[1] role = %q, want user", req.Messages[1].Role)
	}
	if req.Messages[2].Role != "assistant" {
		t.Errorf("messages[2] role = %q, want assistant", req.Messages[2].Role)
	}
	asst := req.Messages[2]
	if len(asst.ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls = %d, want 1", len(asst.ToolCalls))
	}
	tc := asst.ToolCalls[0]
	if tc.ID != "call_9" || tc.Type != "function" || tc.Function.Name != "read" {
		t.Errorf("assistant tool_call = %+v, want call_9/function/read", tc)
	}
	if tc.Function.Arguments != `{"path":"x.go"}` {
		t.Errorf("tool_call arguments = %q, want raw JSON preserved", tc.Function.Arguments)
	}
	if req.Messages[3].Role != "tool" || req.Messages[3].ToolCallID != "call_9" {
		t.Errorf("messages[3] = role %q tool_call_id %q, want tool/call_9", req.Messages[3].Role, req.Messages[3].ToolCallID)
	}
}
