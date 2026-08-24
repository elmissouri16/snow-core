package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestWithoutProviderReasoningReusesCleanRequest(t *testing.T) {
	request := protocol.ChatRequest{Messages: []protocol.Message{{ID: "plain", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{protocol.NewTextBlock("answer")}}}}
	got := withoutProviderReasoning(request)
	if len(got.Messages) != 1 || &got.Messages[0] != &request.Messages[0] {
		t.Fatal("reasoning-free request was copied")
	}
}

func TestWithoutProviderReasoningCopiesOnlyAffectedMessages(t *testing.T) {
	request := protocol.ChatRequest{Messages: []protocol.Message{
		{ID: "plain", Role: protocol.RoleUser, Content: []protocol.ContentBlock{protocol.NewTextBlock("question")}},
		{ID: "reasoning", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{
			{Type: protocol.BlockProviderData, Data: []byte(`{"opaque":true}`)},
			protocol.NewTextBlock("answer"),
		}},
	}}
	got := withoutProviderReasoning(request)
	if len(got.Messages) != 2 || &got.Messages[0] == &request.Messages[0] {
		t.Fatal("request message headers were not isolated")
	}
	if &got.Messages[0].Content[0] != &request.Messages[0].Content[0] {
		t.Fatal("unaffected message content was unnecessarily cloned")
	}
	if len(got.Messages[1].Content) != 1 || got.Messages[1].Content[0].Type != protocol.BlockText {
		t.Fatalf("provider data was not removed: %+v", got.Messages[1].Content)
	}
	if len(request.Messages[1].Content) != 2 || request.Messages[1].Content[0].Type != protocol.BlockProviderData {
		t.Fatal("reasoning filter mutated its input")
	}
}

func TestNormalizeEndpoints(t *testing.T) {
	for _, tc := range []struct{ input, responses, models string }{
		{"https://example.com/v1", "https://example.com/v1/responses", "https://example.com/v1/models"},
		{"https://example.com/v1/", "https://example.com/v1/responses", "https://example.com/v1/models"},
		{"http://localhost:11434/v1/responses", "http://localhost:11434/v1/responses", "http://localhost:11434/v1/models"},
		{"http://localhost:11434/v1/chat/completions", "http://localhost:11434/v1/responses", "http://localhost:11434/v1/models"},
	} {
		responses, models, err := normalizeEndpoints(tc.input)
		if err != nil || responses != tc.responses || models != tc.models {
			t.Fatalf("normalizeEndpoints(%q)=(%q,%q,%v)", tc.input, responses, models, err)
		}
	}
	for _, raw := range []string{"example.com/v1", "ftp://example.com/v1", "https://user:pass@example.com/v1", "https://example.com/v1?q=x", "https://example.com/v1#x"} {
		if _, _, err := normalizeEndpoints(raw); err == nil {
			t.Fatalf("invalid endpoint accepted: %s", raw)
		}
	}
}

func TestChatOptionalBearerAndResponsesBody(t *testing.T) {
	for _, tc := range []struct{ name, key, wantAuth string }{{"keyless", "", ""}, {"key", "secret-key", "Bearer secret-key"}} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/responses" {
					t.Errorf("path=%s", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != tc.wantAuth {
					t.Errorf("authorization=%q", got)
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3}}}\n\n")
			}))
			defer server.Close()
			p, err := New(Config{BaseURL: server.URL + "/v1", HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			stream, err := p.Chat(context.Background(), auth.Credential{Key: tc.key}, protocol.ChatRequest{Model: protocol.Model{Provider: ProviderID, ID: "model", SupportsTools: true}, Messages: []protocol.Message{protocol.NewUserMessage("u", "", "hi")}})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			var text string
			for {
				ev, nextErr := stream.Next(context.Background())
				if nextErr != nil {
					break
				}
				if ev.Type == protocol.EvStreamTextDelta {
					text += ev.Text
				}
			}
			if text != "ok" {
				t.Fatalf("text=%q", text)
			}
			if body["stream"] != true || body["store"] != false || body["model"] != "model" {
				t.Fatalf("body=%v", body)
			}
			if _, ok := body["text"]; ok {
				t.Fatalf("unsupported verbosity sent: %v", body)
			}
			if _, ok := body["reasoning"]; ok {
				t.Fatalf("unsupported reasoning sent: %v", body)
			}
		})
	}
}

func TestChatFallsBackToChatCompletionsAndCachesCapability(t *testing.T) {
	var responsesCalls, chatCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			responsesCalls++
			http.NotFound(w, r)
		case "/v1/chat/completions":
			chatCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer compatible-key" {
				t.Errorf("authorization=%q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["model"] != "chat-model" || body["stream"] != true || body["messages"] == nil {
				t.Errorf("body=%v", body)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p, err := New(Config{BaseURL: server.URL + "/v1", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		stream, err := p.Chat(context.Background(), auth.Credential{Key: "compatible-key"}, protocol.ChatRequest{Model: protocol.Model{Provider: ProviderID, ID: "chat-model", SupportsTools: true}, Messages: []protocol.Message{protocol.NewUserMessage("u", "", "hi")}})
		if err != nil {
			t.Fatal(err)
		}
		var text string
		for {
			event, nextErr := stream.Next(context.Background())
			if nextErr != nil {
				break
			}
			if event.Type == protocol.EvStreamError {
				t.Fatalf("stream error: %v", event.Err)
			}
			if event.Type == protocol.EvStreamTextDelta {
				text += event.Text
			}
		}
		_ = stream.Close()
		if text != "ok" {
			t.Fatalf("text=%q", text)
		}
	}
	if responsesCalls != 1 || chatCalls != 2 {
		t.Fatalf("responses calls=%d chat calls=%d", responsesCalls, chatCalls)
	}
}

func TestChatCompletionsFallbackStatuses(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusNotImplemented} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var chatCalls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/responses":
					w.WriteHeader(status)
				case "/v1/chat/completions":
					chatCalls++
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			p, _ := New(Config{BaseURL: server.URL + "/v1", HTTPClient: server.Client()})
			stream, err := p.Chat(context.Background(), auth.Credential{}, protocol.ChatRequest{Model: protocol.Model{Provider: ProviderID, ID: "m", SupportsTools: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			var sawDone bool
			for {
				event, nextErr := stream.Next(context.Background())
				if nextErr != nil {
					break
				}
				sawDone = sawDone || event.Type == protocol.EvStreamDone
			}
			if chatCalls != 1 || !sawDone {
				t.Fatalf("chat calls=%d done=%t", chatCalls, sawDone)
			}
		})
	}
}

func TestChatCompletionsFallbackDoesNotLeakOpenCodeKey(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "unrelated-opencode-key")
	var chatAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			http.NotFound(w, r)
		case "/v1/chat/completions":
			chatAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL + "/v1", HTTPClient: server.Client()})
	stream, err := p.Chat(context.Background(), auth.Credential{}, protocol.ChatRequest{Model: protocol.Model{Provider: ProviderID, ID: "m", SupportsTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	for {
		if _, err := stream.Next(context.Background()); err != nil {
			break
		}
	}
	if chatAuth != "" {
		t.Fatalf("unrelated OpenCode credential leaked: %q", chatAuth)
	}
}

func TestResolveKeyPrecedenceAndOptionalKey(t *testing.T) {
	t.Setenv(EnvAPIKey, "env-key")
	p, _ := New(Config{APIKey: "stored-or-explicit-key"})
	resolved, err := p.Resolve(context.Background(), auth.Credential{})
	if err != nil || resolved.Key != "stored-or-explicit-key" {
		t.Fatalf("configured resolve=%+v err=%v", resolved, err)
	}
	p, _ = New(Config{})
	resolved, err = p.Resolve(context.Background(), auth.Credential{})
	if err != nil || resolved.Key != "env-key" {
		t.Fatalf("env resolve=%+v err=%v", resolved, err)
	}
	resolved, _ = p.Resolve(context.Background(), auth.Credential{Key: "direct"})
	if resolved.Key != "direct" {
		t.Fatalf("direct resolve=%+v", resolved)
	}
	t.Setenv(EnvAPIKey, "")
	p, _ = New(Config{})
	resolved, err = p.Resolve(context.Background(), auth.Credential{})
	if err != nil || resolved.Key != "" {
		t.Fatalf("keyless resolve=%+v err=%v", resolved, err)
	}
}

func TestDiscoveryCredentialPrecedence(t *testing.T) {
	t.Setenv(EnvAPIKey, "env-key")
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
	}))
	defer server.Close()
	p, err := New(Config{BaseURL: server.URL, APIKey: "explicit-key", DiscoveryAPIKey: "stored-key", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer explicit-key" {
		t.Fatalf("authorization = %q", got)
	}
}

func TestDiscoveryCredentialIsNotRuntimeFallback(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	var modelsAuth, chatAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			modelsAuth = r.Header.Get("Authorization")
			_, _ = fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
		case "/v1/responses":
			chatAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p, err := New(Config{BaseURL: server.URL + "/v1", DiscoveryAPIKey: "stored-key", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	stream, err := p.Chat(context.Background(), auth.Credential{}, protocol.ChatRequest{Model: protocol.Model{Provider: ProviderID, ID: "m", SupportsTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	for {
		if _, err := stream.Next(context.Background()); err != nil {
			break
		}
	}
	if modelsAuth != "Bearer stored-key" || chatAuth != "" {
		t.Fatalf("models auth=%q chat auth=%q", modelsAuth, chatAuth)
	}
}

func TestListModelsDiscoveryFailuresUseConfiguredFallback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		timeout time.Duration
	}{
		{name: "malformed", handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `not-json`) }},
		{name: "oversized", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, strings.Repeat("x", maxModelsResponseBytes+1))
		}},
		{name: "timeout", timeout: 10 * time.Millisecond, handler: func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			p, err := New(Config{BaseURL: server.URL, DefaultModel: "configured", HTTPClient: server.Client(), DiscoveryTimeout: tc.timeout})
			if err != nil {
				t.Fatal(err)
			}
			models, err := p.ListModels(context.Background())
			if err != nil || len(models) != 1 || models[0].ID != "configured" {
				t.Fatalf("models=%+v err=%v", models, err)
			}
		})
	}
}

func TestListModelsMetadataAndFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"plain"},{"id":"rich","display_name":"Rich","context_window":1000,"max_output_tokens":100,"supports_vision":true,"supports_thinking":true,"thinking_levels":["low","XHIGH","max","ultra","xhigh"],"supports_verbosity":true,"supports_reasoning_summary":false,"pricing":{"currency":"USD","input_per_million":1}}]}`)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL + "/v1", HTTPClient: server.Client()})
	models, err := p.ListModels(context.Background())
	if err != nil || len(models) != 2 {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	if !models[0].SupportsTools || models[0].SupportsThinking || models[0].SupportsVision {
		t.Fatalf("plain=%+v", models[0])
	}
	rich := models[1]
	if !rich.SupportsVision || !rich.SupportsThinking || !rich.SupportsVerbosity || rich.SupportsReasoningSummary == nil || *rich.SupportsReasoningSummary {
		t.Fatalf("rich=%+v", rich)
	}
	wantLevels := []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingXHigh, protocol.ThinkingMax, protocol.ThinkingUltra}
	if !slices.Equal(rich.ThinkingLevels, wantLevels) {
		t.Fatalf("rich thinking levels=%v, want %v", rich.ThinkingLevels, wantLevels)
	}
	negative, ok := normalizeModel(modelRecord{
		ID: "negative", SupportsVision: boolPtr(false), SupportsThinking: boolPtr(false), Reasoning: boolPtr(true),
		Input: []string{"image"}, ThinkingLevels: []string{"high"},
		Capabilities: &modelCapabilities{Vision: boolPtr(true), Thinking: boolPtr(true), ThinkingLevels: []string{"high"}},
	})
	if !ok || negative.SupportsVision || negative.SupportsThinking || len(negative.ThinkingLevels) != 0 {
		t.Fatalf("explicit negative metadata was overridden: %+v", negative)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", 500) }))
	defer bad.Close()
	withFallback, _ := New(Config{BaseURL: bad.URL, DefaultModel: "configured", HTTPClient: bad.Client(), DiscoveryTimeout: time.Second})
	models, err = withFallback.ListModels(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "configured" {
		t.Fatalf("fallback=%+v err=%v", models, err)
	}
	withoutFallback, _ := New(Config{BaseURL: bad.URL, HTTPClient: bad.Client()})
	if _, err = withoutFallback.ListModels(context.Background()); err == nil {
		t.Fatal("missing discovery error")
	}
}

func boolPtr(value bool) *bool { return &value }

func TestChatRejectsNonStreamSuccess(t *testing.T) {
	const key = "nonstream-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"error":{"message":"bad %s"}}`, key)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, err := p.Chat(context.Background(), auth.Credential{Key: key}, protocol.ChatRequest{Model: protocol.Model{ID: "m", SupportsTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ev.Err == nil || !strings.Contains(ev.Err.Error(), "content type") || strings.Contains(ev.Err.Error(), key) {
		t.Fatalf("event=%+v", ev)
	}
}

func TestChatClassifiesLimitsAndRedactsSecrets(t *testing.T) {
	const key = "active-secret"
	for _, status := range []int{http.StatusPaymentRequired, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, "quota "+key)
			}))
			defer server.Close()
			p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
			stream, err := p.Chat(context.Background(), auth.Credential{Key: key}, protocol.ChatRequest{Model: protocol.Model{ID: "m"}})
			if err != nil {
				t.Fatal(err)
			}
			ev, err := stream.Next(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if status == http.StatusPaymentRequired {
				var limited providerpkg.UsageLimitedError
				if ev.Err == nil || !errors.As(ev.Err, &limited) {
					t.Fatalf("event=%+v", ev)
				}
			} else if advice, ok := providerpkg.RetryAdviceFor(ev.Err); !ok || advice.Kind != providerpkg.RetryRateLimit {
				t.Fatalf("event=%+v advice=%+v ok=%v", ev, advice, ok)
			}
			if strings.Contains(ev.Err.Error(), key) || !strings.Contains(ev.Err.Error(), "[redacted]") {
				t.Fatalf("secret leak: %v", ev.Err)
			}
		})
	}
}

func TestChatRejectsCrossOriginRedirectWithoutForwardingKey(t *testing.T) {
	const key = "redirect-secret"
	forwarded := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		forwarded <- r.Header.Get("Authorization")
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	p, err := New(Config{BaseURL: source.URL, HTTPClient: source.Client()})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := p.Chat(context.Background(), auth.Credential{Key: key}, protocol.ChatRequest{Model: protocol.Model{ID: "m", SupportsTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := stream.Next(context.Background())
	if err != nil || ev.Err == nil || !strings.Contains(ev.Err.Error(), "network request failed") {
		t.Fatalf("event=%+v err=%v", ev, err)
	}
	select {
	case got := <-forwarded:
		t.Fatalf("redirect target received authorization %q", got)
	default:
	}
}
