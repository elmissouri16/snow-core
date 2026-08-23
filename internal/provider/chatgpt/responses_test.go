package chatgpt

import (
	"bytes"
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

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"github.com/klauspost/compress/zstd"
)

func TestChatClassifiesThrottleAndQuotaWithoutLocalRetry(t *testing.T) {
	const access = "secret-access-token"
	for _, tc := range []struct {
		status int
		kind   providerpkg.RetryKind
		quota  bool
	}{{http.StatusTooManyRequests, providerpkg.RetryRateLimit, false}, {http.StatusPaymentRequired, "", true}} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				http.Error(w, "quota "+access, tc.status)
			}))
			defer server.Close()
			p := &Provider{baseURL: server.URL, client: server.Client()}
			stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: access, Extra: map[string]any{"account_id": "a"}}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
			if err != nil {
				t.Fatal(err)
			}
			ev, err := stream.Next(context.Background())
			if err != nil || ev.Err == nil || calls != 1 {
				t.Fatalf("event=%+v err=%v calls=%d", ev, err, calls)
			}
			if tc.quota {
				var limited providerpkg.UsageLimitedError
				if !errors.As(ev.Err, &limited) {
					t.Fatalf("quota error=%T %v", ev.Err, ev.Err)
				}
			} else if advice, ok := providerpkg.RetryAdviceFor(ev.Err); !ok || advice.Kind != tc.kind {
				t.Fatalf("advice=%+v ok=%v", advice, ok)
			}
			if strings.Contains(ev.Err.Error(), access) || !strings.Contains(ev.Err.Error(), "[redacted]") {
				t.Fatalf("credential leaked in error: %v", ev.Err)
			}
		})
	}
}

func TestChatStreamsCodexText(t *testing.T) {
	var gotModel string
	var gotAuth, gotAccount, gotSession, gotClientRequest, gotUserAgent string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/codex/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		gotSession = r.Header.Get("session-id")
		gotClientRequest = r.Header.Get("x-client-request-id")
		gotUserAgent = r.Header.Get("User-Agent")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotModel, _ = gotBody["model"].(string)
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
	temperature := 0.7
	affinity := strings.Repeat("a", 64)
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: token}, protocol.ChatRequest{
		Model: protocol.Model{ID: "gpt-5.4"}, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "hi"}}}},
		SessionAffinityKey: affinity, MaxTokens: 123, Temperature: &temperature,
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
	if gotSession != affinity || gotClientRequest != affinity || gotUserAgent != "snow" {
		t.Fatalf("affinity headers: session=%q client=%q user-agent=%q", gotSession, gotClientRequest, gotUserAgent)
	}
	if gotBody["prompt_cache_key"] != affinity || gotBody["tool_choice"] != "auto" || gotBody["parallel_tool_calls"] != true {
		t.Fatalf("Codex controls missing: %v", gotBody)
	}
	if _, ok := gotBody["temperature"]; ok {
		t.Fatalf("ChatGPT temperature was not suppressed: %v", gotBody)
	}
	if _, ok := gotBody["max_output_tokens"]; ok {
		t.Fatalf("ChatGPT max_output_tokens was not suppressed: %v", gotBody)
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

func TestCompressRequestBodyThresholdAndRoundTrip(t *testing.T) {
	small := []byte(`{"small":true}`)
	encoded, encoding := compressRequestBody(small)
	if encoding != "" || !bytes.Equal(encoded, small) {
		t.Fatalf("small body encoding=%q body=%q", encoding, encoded)
	}
	large := bytes.Repeat([]byte(`{"message":"long context"}`), 3000)
	encoded, encoding = compressRequestBody(large)
	if encoding != "zstd" || bytes.Equal(encoded, large) {
		t.Fatalf("large body encoding=%q compressed=%v", encoding, !bytes.Equal(encoded, large))
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(encoded, nil)
	if err != nil || !bytes.Equal(decoded, large) {
		t.Fatalf("decode err=%v equal=%v", err, bytes.Equal(decoded, large))
	}
}

func TestCompressRequestBodyConcurrent(t *testing.T) {
	body := bytes.Repeat([]byte(`{"message":"concurrent context"}`), 3000)
	const workers = 16
	encoded := make([][]byte, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			var encoding string
			encoded[i], encoding = compressRequestBody(body)
			if encoding != "zstd" {
				t.Errorf("worker %d encoding = %q", i, encoding)
			}
		}()
	}
	wg.Wait()
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	for i, compressed := range encoded {
		decoded, err := decoder.DecodeAll(compressed, nil)
		if err != nil || !bytes.Equal(decoded, body) {
			t.Fatalf("worker %d decode err=%v equal=%v", i, err, bytes.Equal(decoded, body))
		}
	}
}

func TestChatClassifiesTransientHTTPStatusesWithoutLocalRetry(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 599} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable","code":"server_error"}}`))
			}))
			defer server.Close()
			p := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
			stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
			if err != nil {
				t.Fatal(err)
			}
			event, err := stream.Next(context.Background())
			advice, ok := providerpkg.RetryAdviceFor(event.Err)
			if err != nil || event.Type != protocol.EvStreamError || !ok || advice.Kind != providerpkg.RetryTransient || advice.RetryAfter != 2*time.Second || calls != 1 {
				t.Fatalf("event=%+v err=%v advice=%+v ok=%v calls=%d", event, err, advice, ok, calls)
			}
		})
	}
}

func TestChatClassifiesNetworkFailureWithoutLocalRetry(t *testing.T) {
	sentinel := errors.New("temporary dial failure")
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, sentinel
	})}
	p := New(Config{BaseURL: "https://example.test", HTTPClient: client})
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	event, err := stream.Next(context.Background())
	advice, ok := providerpkg.RetryAdviceFor(event.Err)
	if err != nil || !ok || advice.Kind != providerpkg.RetryTransient || !errors.Is(event.Err, sentinel) || calls != 1 {
		t.Fatalf("event=%+v err=%v advice=%+v calls=%d", event, err, advice, calls)
	}
}

func TestChatNextCallContextTimeoutWhileWaitingForHeadersIsNonterminal(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		close(started)
		<-release
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	p := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	callCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := stream.Next(callCtx); done <- err }()
	<-started
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Next err=%v", err)
	}
	close(release)
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamDone {
		t.Fatalf("second Next event=%+v err=%v", event, err)
	}
	if calls != 1 {
		t.Fatalf("request was restarted: calls=%d", calls)
	}
}

func TestChatNextCallContextTimeoutDoesNotTerminateStream(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-release
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	p := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	callCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := stream.Next(callCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Next err=%v", err)
	}
	close(release)
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamDone {
		t.Fatalf("second Next event=%+v err=%v", event, err)
	}
}

func TestChatClassifiesPreOutputStreamReadFailureWithoutLocalRetry(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: &readErrorBody{err: errors.New("read failed")}}, nil
	})}
	p := New(Config{BaseURL: "https://example.test", HTTPClient: client})
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	event, err := stream.Next(context.Background())
	advice, ok := providerpkg.RetryAdviceFor(event.Err)
	if err != nil || !ok || advice.Kind != providerpkg.RetryTransient || calls != 1 {
		t.Fatalf("event=%+v err=%v advice=%+v calls=%d", event, err, advice, calls)
	}
}

func TestChatDoesNotRetryStreamReadFailureAfterActivity(t *testing.T) {
	calls := 0
	body := &readErrorBody{data: []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"), err: errors.New("read failed")}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}, nil
	})}
	p := New(Config{BaseURL: "https://example.test", HTTPClient: client})
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	first, err := stream.Next(context.Background())
	if err != nil || first.Type != protocol.EvStreamTextDelta {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := stream.Next(context.Background())
	if err != nil || second.Type != protocol.EvStreamError {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestChatClassifiesImmediateStreamFailureAndTruncation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first string
	}{
		{name: "overload", first: "data: {\"type\":\"error\",\"code\":\"server_overloaded\",\"message\":\"servers overloaded\"}\n\n"},
		{name: "truncation", first: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(tc.first))
			}))
			defer server.Close()
			p := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
			stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
			if err != nil {
				t.Fatal(err)
			}
			event, err := stream.Next(context.Background())
			advice, ok := providerpkg.RetryAdviceFor(event.Err)
			if err != nil || !ok || advice.Kind != providerpkg.RetryTransient || calls != 1 {
				t.Fatalf("event=%+v err=%v advice=%+v calls=%d", event, err, advice, calls)
			}
		})
	}
}

func TestChatDoesNotRetryStreamFailureAfterActivity(t *testing.T) {
	cases := []struct {
		name  string
		first string
		want  protocol.StreamEventType
	}{
		{"text", `{"type":"response.output_text.delta","delta":"partial"}`, protocol.EvStreamTextDelta},
		{"thinking", `{"type":"response.reasoning_summary_text.delta","item_id":"r","summary_index":0,"delta":"thinking"}`, protocol.EvStreamThinkingDelta},
		{"tool", `{"type":"response.function_call_arguments.delta","item_id":"i","call_id":"c","name":"read","delta":"{}"}`, protocol.EvStreamToolCallDelta},
		{"provider_data", `{"type":"response.output_item.done","item":{"type":"reasoning","id":"r","summary":[],"encrypted_content":"opaque"}}`, protocol.EvStreamProviderData},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: {\"type\":\"error\",\"code\":\"server_overloaded\",\"message\":\"overloaded\"}\n\n", tc.first)
			}))
			defer server.Close()
			p := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
			stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			first, err := stream.Next(context.Background())
			if err != nil || first.Type != tc.want {
				t.Fatalf("first=%+v err=%v", first, err)
			}
			second, err := stream.Next(context.Background())
			if err != nil || second.Type != protocol.EvStreamError {
				t.Fatalf("second=%+v err=%v", second, err)
			}
			if calls != 1 {
				t.Fatalf("retried after activity: calls=%d", calls)
			}
		})
	}
}

func TestChatDoesNotRetryValidationErrorWithTransientWords(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"error\",\"code\":\"invalid_request\",\"message\":\"temporarily unavailable field\"}\n\n"))
	}))
	defer server.Close()
	p := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamError {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestChatPreservesHTTPDiagnosticsWithoutLocalRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("x-request-id", "req-final")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable","code":"service_unavailable"}}`))
	}))
	defer server.Close()
	p := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, err := p.Chat(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamError || event.Err == nil {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	message := event.Err.Error()
	for _, want := range []string{"HTTP 503", "service_unavailable", "req-final"} {
		if !strings.Contains(message, want) {
			t.Fatalf("missing %q in %q", want, message)
		}
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestChatProviderAttemptIsContextCancellable(t *testing.T) {
	started := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	p := New(Config{BaseURL: "https://example.test", HTTPClient: client})
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := p.Chat(ctx, auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	done := make(chan error, 1)
	go func() { _, err := stream.Next(ctx); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestResponseRequestIDRedactsBeforeBounding(t *testing.T) {
	secret := strings.Repeat("s", 300)
	header := http.Header{"X-Request-Id": []string{secret}}
	got := responseRequestID(header, secret)
	if got != "[redacted]" || strings.Contains(got, secret[:8]) {
		t.Fatalf("request id=%q", got)
	}
	if got := responseRequestID(http.Header{"X-Request-Id": []string{"\u009breq"}}); got != "req" {
		t.Fatalf("C1 request id=%q", got)
	}
}

func consumeSuccessfulStream(stream protocol.EventStream) error {
	for {
		event, err := stream.Next(context.Background())
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if event.Type == protocol.EvStreamError {
			return event.Err
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

type readErrorBody struct {
	data []byte
	err  error
}

func (b *readErrorBody) Read(dst []byte) (int, error) {
	if len(b.data) > 0 {
		n := copy(dst, b.data)
		b.data = b.data[n:]
		return n, nil
	}
	return 0, b.err
}

func (*readErrorBody) Close() error { return nil }

func TestBuildResponsesBodyOmitsHostDiscoveryMetadata(t *testing.T) {
	body, err := buildResponsesBody(protocol.ChatRequest{
		Model: protocol.Model{ID: "gpt-5.4", SupportsTools: true},
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
