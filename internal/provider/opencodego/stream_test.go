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
	"testing"
	"time"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

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
