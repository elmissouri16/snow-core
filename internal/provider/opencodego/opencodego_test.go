package opencodego

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

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
	ctx := context.Background()
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

// TestThinkingSkipped verifies thinking blocks are not serialized to the wire
// and that reasoning_content deltas surface as thinking events.
func TestThinkingSkipped(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"reasoning_content": "hmm"}, "", nil)))
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"content": "answer"}, "stop", nil)))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
	}))
	defer srv.Close()

	p := mustNew(t, srv.URL, "k")
	userMsg := protocol.Message{
		ID:   "u1",
		Role: protocol.RoleUser,
		Content: []protocol.ContentBlock{
			{Type: protocol.BlockThinking, Text: "secret reasoning"},
			{Type: protocol.BlockText, Text: "visible prompt"},
		},
	}
	s, err := p.Chat(context.Background(), auth.Credential{Key: "k"}, protocol.ChatRequest{
		Model:    protocol.Model{ID: "m"},
		Messages: []protocol.Message{userMsg},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, s, context.Background())

	// The wire body must not contain the thinking text.
	if strings.Contains(string(gotBody), "secret reasoning") {
		t.Error("request body leaked thinking block content")
	}
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(req.Messages) != 1 || req.Messages[0].Content != "visible prompt" {
		t.Errorf("messages = %+v, want only visible text content", req.Messages)
	}

	// Response reasoning_content must map to thinking delta.
	if len(events) < 2 || events[0].Type != protocol.EvStreamThinkingDelta {
		t.Fatalf("events = %+v, want thinking_delta first", events)
	}
	if events[0].Text != "hmm" {
		t.Errorf("thinking delta = %q, want hmm", events[0].Text)
	}
}

func TestReadBoundedSSELineRejectsOversizedRecord(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(strings.Repeat("x", 128)))
	if _, err := readBoundedSSELine(reader, 64); err == nil {
		t.Fatal("oversized SSE record was accepted")
	}
}

func TestListModelsDiscoveryTimeoutFallsBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	provider, err := New(Config{BaseURL: server.URL, DiscoveryTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	models, err := provider.ListModels(context.Background())
	if err != nil || len(models) == 0 {
		t.Fatalf("fallback models=%+v err=%v", models, err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("catalog timeout took %s", elapsed)
	}
}

// TestListModelsFallback verifies the static catalog is returned when the
// remote catalog endpoint fails.
func TestListModelsFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "boom")
	}))
	defer srv.Close()

	p := mustNew(t, srv.URL, "k")
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels must not fail: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("static catalog = %d models, want 1", len(models))
	}
	m := models[0]
	if m.Provider != ProviderID || m.ID != DefaultModelID || !m.SupportsTools || !m.SupportsThinking {
		t.Errorf("static model = %+v, want provider=%s id=%s tools+thinking", m, ProviderID, DefaultModelID)
	}
}

// TestListModelsRemote verifies successful remote catalog parsing.
func TestListModelsRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization = %q, want Bearer k", got)
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"model-a"},{"id":"model-b"}]}`)
	}))
	defer srv.Close()

	p := mustNew(t, srv.URL, "k")
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2", len(models))
	}
	if models[0].ID != "model-a" || models[0].Provider != ProviderID {
		t.Errorf("models[0] = %+v, want model-a/opencode-go", models[0])
	}
}

// TestResolve verifies credential resolution behavior.
func TestResolve(t *testing.T) {
	p := mustNew(t, "http://unused", "cfg-key")
	if _, err := p.Resolve(context.Background(), auth.Credential{}); err != nil {
		t.Errorf("Resolve with config key should pass, got %v", err)
	}
	p2 := mustNew(t, "http://unused", "")
	if _, err := p2.Resolve(context.Background(), auth.Credential{Key: "direct"}); err != nil {
		t.Errorf("Resolve with credential key should pass, got %v", err)
	}
	if _, err := p2.Resolve(context.Background(), auth.Credential{}); err == nil {
		t.Error("Resolve with no key should fail")
	}
}

// TestChatCancellation verifies cancelling the context mid-stream surfaces
// the context error from Next.
func TestChatCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"content": "a"}, "", nil)))
		if fl != nil {
			fl.Flush()
		}
		<-r.Context().Done() // hold the stream open until the client cancels
	}))
	defer srv.Close()

	p := mustNew(t, srv.URL, "k")
	s, err := p.Chat(ctx, auth.Credential{Key: "k"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ev, err := s.Next(ctx)
	if err != nil {
		t.Fatalf("first Next: %v", err)
	}
	if ev.Type != protocol.EvStreamTextDelta || ev.Text != "a" {
		t.Fatalf("first event = %+v, want text_delta 'a'", ev)
	}
	cancel()
	_, err = s.Next(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Next after cancel = %v, want context.Canceled", err)
	}
}

// TestErrorEvent verifies OpenAI-style error events become EvStreamError.
func TestErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "event: error\n")
		_, _ = fmt.Fprint(w, `data: {"error":{"message":"rate limit exceeded"}}`+"\n\n")
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
	if len(events) != 1 || events[0].Type != protocol.EvStreamError {
		t.Fatalf("events = %+v, want one error event", events)
	}
	if !strings.Contains(events[0].Err.Error(), "rate limit") {
		t.Errorf("error = %q, want mention of rate limit", events[0].Err.Error())
	}
}

func TestChatRejectsNonSSESuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer srv.Close()
	p := mustNew(t, srv.URL, "k")
	stream, err := p.Chat(context.Background(), auth.Credential{Key: "k"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, stream, context.Background())
	if len(events) != 1 || events[0].Type != protocol.EvStreamError || !strings.Contains(events[0].Err.Error(), "content type") {
		t.Fatalf("events=%+v", events)
	}
}

func TestChatRejectsTruncatedSSEWithoutTerminalSignal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"content": "partial"}, "", nil)))
	}))
	defer srv.Close()
	p := mustNew(t, srv.URL, "k")
	stream, err := p.Chat(context.Background(), auth.Credential{Key: "k"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, stream, context.Background())
	if len(events) != 2 || events[0].Type != protocol.EvStreamTextDelta || events[1].Type != protocol.EvStreamError || !strings.Contains(events[1].Err.Error(), "truncated") {
		t.Fatalf("events=%+v", events)
	}
}

func TestChatAcceptsFinishReasonWithoutDoneSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"content": "complete"}, "stop", nil)))
	}))
	defer srv.Close()
	p := mustNew(t, srv.URL, "k")
	stream, err := p.Chat(context.Background(), auth.Credential{Key: "k"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, stream, context.Background())
	if len(events) != 2 || events[0].Type != protocol.EvStreamTextDelta || events[1].Type != protocol.EvStreamDone {
		t.Fatalf("events=%+v", events)
	}
}

// TestChatDoneThenKeepalive: after "data: [DONE]" some servers keep the
// connection open. drain must complete with done(stop) without hanging.
func TestChatDoneThenKeepalive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"content": "hello"}, "", nil)))
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(nil, "stop", nil)))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
		// Keep the connection open (simulating a keepalive server) until the
		// client tears down. The read loop must have already stopped on DONE.
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := mustNew(t, srv.URL, "k")
	s, err := p.Chat(context.Background(), auth.Credential{Key: "k"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events := drain(t, s, ctx)
	if len(events) != 2 {
		t.Fatalf("events = %+v, want [text_delta, done]", events)
	}
	if events[0].Type != protocol.EvStreamTextDelta || events[0].Text != "hello" {
		t.Fatalf("events[0] = %+v, want text_delta 'hello'", events[0])
	}
	if events[1].Type != protocol.EvStreamDone || events[1].StopReason != protocol.StopStop {
		t.Fatalf("events[1] = %+v, want done(stop)", events[1])
	}
}

// TestChatEmptyToolCallID: tool calls without an "id" field must get a
// sticky fallback id shared by delta and done events.
func TestChatEmptyToolCallID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		// No "id" field: only index + function.
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"tool_calls": []map[string]any{{
			"index":    0,
			"type":     "function",
			"function": map[string]any{"name": "read", "arguments": `{"path":"a.txt"}`},
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

	var deltaID, doneID string
	var doneArgs json.RawMessage
	for _, e := range events {
		switch e.Type {
		case protocol.EvStreamToolCallDelta:
			deltaID = e.ToolCallID
		case protocol.EvStreamToolCallDone:
			doneID = e.ToolCallID
			doneArgs = e.Arguments
		}
	}
	if deltaID == "" || doneID == "" {
		t.Fatalf("sticky fallback id missing: delta=%q done=%q", deltaID, doneID)
	}
	if deltaID != doneID {
		t.Fatalf("delta id %q != done id %q", deltaID, doneID)
	}
	if doneID != "tc-0" {
		t.Fatalf("fallback id = %q, want tc-0", doneID)
	}
	var args map[string]string
	if err := json.Unmarshal(doneArgs, &args); err != nil || args["path"] != "a.txt" {
		t.Fatalf("done args = %s, want path=a.txt", doneArgs)
	}
}

// TestChatFinishFirstWins: a trailing finish_reason "stop" after an earlier
// "tool_calls" must NOT overwrite the tool_use stop reason.
func TestChatFinishFirstWins(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		// 1. tool call with finish_reason tool_calls
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"tool_calls": []map[string]any{{
			"index": 0, "id": "c1", "type": "function",
			"function": map[string]any{"name": "read", "arguments": `{"path":"a"}`},
		}}}, "tool_calls", nil)))
		// 2. trailing chunk claims stop (some servers emit this; must be ignored)
		_, _ = fmt.Fprint(w, sseChunk(chunkWith(nil, "stop", nil)))
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
	var done *protocol.StreamEvent
	for i := range events {
		if events[i].Type == protocol.EvStreamDone {
			done = &events[i]
			break
		}
	}
	if done == nil {
		t.Fatalf("no done event: %+v", events)
	}
	if done.StopReason != protocol.StopToolUse {
		t.Fatalf("done stop reason = %q, want tool_use (first wins)", done.StopReason)
	}
}
