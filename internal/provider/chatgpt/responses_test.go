package chatgpt

import (
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
	providerpkg "github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestChatClassifiesUsageLimit(t *testing.T) {
	const access = "secret-access-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "quota "+access, http.StatusTooManyRequests)
	}))
	defer server.Close()
	p := &Provider{baseURL: server.URL, client: server.Client()}
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: access, Extra: map[string]any{"account_id": "a"}}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var limited providerpkg.UsageLimitedError
	if ev.Err == nil || !errors.As(ev.Err, &limited) {
		t.Fatalf("event=%+v", ev)
	}
	if strings.Contains(ev.Err.Error(), access) || !strings.Contains(ev.Err.Error(), "[redacted]") {
		t.Fatalf("credential leaked in error: %v", ev.Err)
	}
}

func TestCodexStreamRejectsAggregateSSEEvent(t *testing.T) {
	fragment := strings.Repeat("x", 3<<20)
	body := "data: " + fragment + "\ndata: " + fragment + "\ndata: " + fragment + "\n"
	s := &codexStream{ch: make(chan protocol.StreamEvent, 4), done: make(chan struct{}), ctx: context.Background(), body: io.NopCloser(strings.NewReader(body))}
	s.read()
	ev := <-s.ch
	if ev.Type != protocol.EvStreamError || ev.Err == nil || !strings.Contains(ev.Err.Error(), "SSE event exceeds size limit") {
		t.Fatalf("event = %+v", ev)
	}
}

func TestCodexStreamRejectsTooManyEmptySSEFragments(t *testing.T) {
	body := strings.Repeat("data:\n", maxCodexSSEEventFragments+1)
	s := &codexStream{ch: make(chan protocol.StreamEvent, 4), done: make(chan struct{}), ctx: context.Background(), body: io.NopCloser(strings.NewReader(body))}
	s.read()
	ev := <-s.ch
	if ev.Type != protocol.EvStreamError || ev.Err == nil || !strings.Contains(ev.Err.Error(), "SSE event exceeds size limit") {
		t.Fatalf("event = %+v", ev)
	}
}

func TestCodexStreamBoundsToolCallsAndArguments(t *testing.T) {
	t.Run("per-call arguments", func(t *testing.T) {
		s := &codexStream{ch: make(chan protocol.StreamEvent, 2), done: make(chan struct{}), ctx: context.Background()}
		stopped := s.processEvent(map[string]any{
			"type": "response.function_call_arguments.delta", "item_id": "one", "delta": strings.Repeat("x", maxCodexToolArgumentBytes+1),
		}, map[string]*toolAccum{}, newReasoningAccum(), &codexStreamBounds{}, new(protocol.StopReason), new(bool))
		if !stopped {
			t.Fatal("oversized arguments did not stop stream")
		}
		ev := <-s.ch
		if ev.Type != protocol.EvStreamError || ev.Err == nil || !strings.Contains(ev.Err.Error(), "per-call size limit") {
			t.Fatalf("event = %+v", ev)
		}
	})
	t.Run("completed snapshot is authoritative", func(t *testing.T) {
		s := &codexStream{ch: make(chan protocol.StreamEvent, 8), done: make(chan struct{}), ctx: context.Background()}
		calls := make(map[string]*toolAccum)
		bounds := &codexStreamBounds{}
		if s.processEvent(map[string]any{"type": "response.function_call_arguments.delta", "item_id": "item", "delta": `{"old":true}`}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
			t.Fatal("delta stopped")
		}
		if s.processEvent(map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "function_call", "id": "item", "arguments": `{"new":true}`}}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
			t.Fatal("output item stopped")
		}
		var lastDone protocol.StreamEvent
		for len(s.ch) > 0 {
			ev := <-s.ch
			if ev.Type == protocol.EvStreamToolCallDone {
				lastDone = ev
			}
		}
		if string(lastDone.Arguments) != `{"new":true}` {
			t.Fatalf("last done arguments = %s", lastDone.Arguments)
		}
	})
	t.Run("completed snapshots contribute to aggregate", func(t *testing.T) {
		s := &codexStream{ch: make(chan protocol.StreamEvent, 16), done: make(chan struct{}), ctx: context.Background()}
		calls := make(map[string]*toolAccum)
		bounds := &codexStreamBounds{}
		for i := 0; i < 4; i++ {
			id := fmt.Sprintf("snapshot-%d", i)
			if s.processEvent(map[string]any{"type": "response.function_call_arguments.delta", "item_id": id, "delta": "x"}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
				t.Fatalf("delta %d stopped early", i)
			}
			if s.processEvent(map[string]any{"type": "response.function_call_arguments.done", "item_id": id, "arguments": strings.Repeat("x", maxCodexToolArgumentBytes)}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
				t.Fatalf("snapshot %d stopped early", i)
			}
		}
		if !s.processEvent(map[string]any{"type": "response.function_call_arguments.delta", "item_id": "overflow", "delta": "x"}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
			t.Fatal("aggregate snapshot growth did not stop later arguments")
		}
	})
	t.Run("tool count", func(t *testing.T) {
		s := &codexStream{ch: make(chan protocol.StreamEvent, 2), done: make(chan struct{}), ctx: context.Background()}
		calls := make(map[string]*toolAccum)
		bounds := &codexStreamBounds{}
		for i := 0; i <= maxCodexStreamToolCalls; i++ {
			stopped := s.processEvent(map[string]any{
				"type": "response.output_item.added",
				"item": map[string]any{"type": "function_call"},
			}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool))
			if stopped != (i == maxCodexStreamToolCalls) {
				t.Fatalf("event %d stopped = %v", i, stopped)
			}
		}
		ev := <-s.ch
		if ev.Type != protocol.EvStreamError || ev.Err == nil || !strings.Contains(ev.Err.Error(), "tool-call count") {
			t.Fatalf("event = %+v", ev)
		}
	})
}

func TestCodexStreamBoundsCompletedReasoningItems(t *testing.T) {
	s := &codexStream{ch: make(chan protocol.StreamEvent, maxCodexReasoningItems+2), done: make(chan struct{}), ctx: context.Background()}
	calls := make(map[string]*toolAccum)
	bounds := &codexStreamBounds{}
	for i := 0; i <= maxCodexReasoningItems; i++ {
		stopped := s.processEvent(map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{"type": "reasoning", "id": fmt.Sprintf("reasoning-%d", i), "summary": []any{}},
		}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool))
		if stopped != (i == maxCodexReasoningItems) {
			t.Fatalf("item %d stopped = %v", i, stopped)
		}
	}
	var last protocol.StreamEvent
	for len(s.ch) > 0 {
		last = <-s.ch
	}
	if last.Type != protocol.EvStreamError || last.Err == nil || !strings.Contains(last.Err.Error(), "completed reasoning") {
		t.Fatalf("last event = %+v", last)
	}
}

func TestChatStreamsCodexText(t *testing.T) {
	var gotModel string
	var gotAuth, gotAccount string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/codex/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotModel = body.Model
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	token := testJWT(t, map[string]any{
		"exp":                         float64(time.Now().Add(time.Hour).Unix()),
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-test"},
	})
	p := &Provider{baseURL: server.URL, client: server.Client()}
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: token}, protocol.ChatRequest{
		Model:    protocol.Model{ID: "gpt-5.4"},
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "hi"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var text string
	var usage *protocol.Usage
	var done bool
	for {
		ev, err := stream.Next(context.Background())
		if err != nil {
			if strings.Contains(err.Error(), "EOF") {
				break
			}
			t.Fatal(err)
		}
		switch ev.Type {
		case protocol.EvStreamTextDelta:
			text += ev.Text
		case protocol.EvStreamUsage:
			usage = ev.Usage
		case protocol.EvStreamDone:
			done = true
		}
	}
	if text != "hello" || !done || usage == nil || usage.Total != 5 {
		t.Fatalf("stream text=%q done=%v usage=%+v", text, done, usage)
	}
	if gotModel != "gpt-5.4" || gotAccount != "acct-test" || !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("request headers/model: model=%q account=%q auth=%q", gotModel, gotAccount, gotAuth)
	}
}

func TestChatStreamsReasoningAndMergesCompletedSnapshots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		events := []string{
			`{"type":"response.reasoning_summary_text.delta","item_id":"rs-1","summary_index":0,"delta":"**Inspecting"}`,
			`{"type":"response.reasoning_summary_text.delta","item_id":"rs-1","summary_index":0,"delta":" repository**"}`,
			`{"type":"response.reasoning_summary_text.done","item_id":"rs-1","summary_index":0,"text":"**Inspecting repository**"}`,
			`{"type":"response.reasoning_summary_part.done","item_id":"rs-1","summary_index":0,"part":{"type":"summary_text","text":"**Inspecting repository**"}}`,
			`{"type":"response.reasoning_text.done","item_id":"rs-2","content_index":0,"text":"\nChecking files."}`,
			`{"type":"response.completed","response":{"status":"completed"}}`,
		}
		for _, event := range events {
			_, _ = w.Write([]byte("data: " + event + "\n\n"))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	token := testJWT(t, map[string]any{
		"exp":                         float64(time.Now().Add(time.Hour).Unix()),
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-test"},
	})
	p := &Provider{baseURL: server.URL, client: server.Client()}
	model := protocol.Model{
		ID:               "gpt-5.6-luna",
		SupportsThinking: true,
		ThinkingLevels:   []protocol.ThinkingLevel{protocol.ThinkingHigh},
	}
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: token}, protocol.ChatRequest{
		Model: model, Thinking: protocol.ThinkingHigh,
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "inspect"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	var chunks []string
	for {
		ev, err := stream.Next(context.Background())
		if err != nil {
			if strings.Contains(err.Error(), "EOF") {
				break
			}
			t.Fatal(err)
		}
		if ev.Type == protocol.EvStreamThinkingDelta {
			chunks = append(chunks, ev.Text)
		}
	}
	want := []string{"**Inspecting", " repository**", "\n\nChecking files."}
	if strings.Join(chunks, "|") != strings.Join(want, "|") {
		t.Fatalf("thinking chunks = %#v, want %#v", chunks, want)
	}
}

func TestChatStreamsEncryptedReasoningOnlyAsOpaqueProviderData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"type":"response.reasoning_summary_text.delta","item_id":"reasoning-1","summary_index":0,"delta":"safe summary"}`,
			`{"type":"response.output_item.done","item":{"type":"reasoning","id":"reasoning-1","summary":[],"content":[{"type":"reasoning_text","text":"opaque detail","provider_internal":"drop"}],"encrypted_content":"encrypted-secret","status":"completed"}}`,
			`{"type":"response.completed","response":{"status":"completed"}}`,
		} {
			_, _ = w.Write([]byte("data: " + event + "\n\n"))
		}
	}))
	defer server.Close()
	p := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var visible string
	var opaque *protocol.ContentBlock
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			break
		}
		visible += event.Text
		if event.Type == protocol.EvStreamProviderData {
			opaque = event.ProviderData
		}
	}
	wantWire := `{"type":"reasoning","id":"reasoning-1","summary":[],"content":[{"text":"opaque detail","type":"reasoning_text"}],"encrypted_content":"encrypted-secret"}`
	if strings.Contains(visible, "encrypted-secret") || opaque == nil || string(opaque.Data) != wantWire || opaque.Name != "reasoning-1" {
		t.Fatalf("provider reasoning was not sanitized to the complete official wire item; visible leaked=%v opaque nil=%v", strings.Contains(visible, "encrypted-secret"), opaque == nil)
	}
}

func TestReasoningAccumSeparatesDistinctSummaryItems(t *testing.T) {
	reasoning := newReasoningAccum()
	chunks := []string{
		reasoning.append("summary:item-1:0", "**Planning tasks**"),
		reasoning.append("summary:item-1:0", " carefully"),
		reasoning.append("summary:item-2:0", "**Designing workers**"),
		reasoning.append("text:item-3:0", "\nMonitoring execution"),
	}
	got := strings.Join(chunks, "")
	want := "**Planning tasks** carefully\n\n**Designing workers**\n\nMonitoring execution"
	if got != want {
		t.Fatalf("reasoning stream = %q, want %q", got, want)
	}
	if reasoning.text("summary:item-2:0") != "**Designing workers**" {
		t.Fatalf("raw item text was polluted by display separator: %q", reasoning.text("summary:item-2:0"))
	}
}

func TestMissingReasoningSuffixIsMonotonic(t *testing.T) {
	for _, tc := range []struct {
		name      string
		streamed  string
		completed string
		want      string
	}{
		{name: "fallback", completed: "complete", want: "complete"},
		{name: "suffix", streamed: "com", completed: "complete", want: "plete"},
		{name: "duplicate", streamed: "complete", completed: "complete"},
		{name: "shorter", streamed: "complete", completed: "com"},
		{name: "divergent", streamed: "first", completed: "second"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := missingReasoningSuffix(tc.streamed, tc.completed); got != tc.want {
				t.Fatalf("missingReasoningSuffix(%q, %q) = %q, want %q", tc.streamed, tc.completed, got, tc.want)
			}
		})
	}
}

func TestBuildResponsesBodyOmitsHostDiscoveryMetadata(t *testing.T) {
	body, err := buildResponsesBody(protocol.ChatRequest{
		Model: protocol.Model{ID: "gpt-5.4"},
		Tools: []protocol.ToolSchema{{
			Name: "mail_find", Description: "Find mail", Parameters: json.RawMessage(`{"type":"object"}`),
			Discovery: &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Namespace: "mail", Keywords: []string{"unread"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"discovery"`) || strings.Contains(string(body), `"namespace":"mail"`) {
		t.Fatalf("host discovery metadata leaked to provider: %s", body)
	}
	if !strings.Contains(string(body), `"name":"mail_find"`) {
		t.Fatalf("tool schema missing from provider body: %s", body)
	}
}
