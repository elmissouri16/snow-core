package opencodego

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestListModelsRemoteMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q, want /models", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"data":[{
			"id":"reasoning-model",
			"name":"Reasoning Model",
			"context_window":131072,
			"max_output_tokens":8192,
			"pricing":{"currency":"USD","input_per_million":1.2},
			"supports_tools":false,
			"supports_thinking":true,
			"supports_vision":true,
			"thinking_levels":["low","high","xhigh","none"]
		}]}`)
	}))
	defer srv.Close()

	models, err := mustNew(t, srv.URL, "key").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %d, want 1", len(models))
	}
	model := models[0]
	if model.ID != "reasoning-model" || model.DisplayName != "Reasoning Model" || model.ContextWindow != 131072 || model.MaxOutputTokens != 8192 {
		t.Fatalf("metadata = %+v", model)
	}
	if model.SupportsTools || !model.SupportsThinking || !model.SupportsVision {
		t.Fatalf("capabilities = %+v", model)
	}
	if got := model.SupportedThinkingLevels(); len(got) != 4 || got[0] != protocol.ThinkingOff || got[1] != protocol.ThinkingLow || got[2] != protocol.ThinkingHigh || got[3] != protocol.ThinkingXHigh {
		t.Fatalf("thinking levels = %v", got)
	}
	if model.Pricing == nil || model.Pricing.InputPerMillion != 1.2 {
		t.Fatalf("pricing = %+v", model.Pricing)
	}
}

func TestListModelsEnrichesAvailabilityFromOpenCodeCatalog(t *testing.T) {
	catalogAuthorization := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			if got := r.Header.Get("Authorization"); got != "Bearer key" {
				t.Errorf("models authorization = %q", got)
			}
			_, _ = fmt.Fprint(w, `{"data":[{"id":"deepseek-v4-flash"},{"id":"direct-off","name":"Gateway Name","context_window":123,"supports_thinking":false}]}`)
		case "/catalog":
			catalogAuthorization <- r.Header.Get("Authorization")
			_, _ = fmt.Fprint(w, `{"opencode-go":{"models":{
				"deepseek-v4-flash":{
					"id":"deepseek-v4-flash","name":"DeepSeek V4 Flash","reasoning":true,
					"reasoning_options":[{"type":"effort","values":["low","high","max"]}],
					"tool_call":true,"modalities":{"input":["text"]},
					"limit":{"context":1000000,"output":384000},
					"cost":{"input":0.07,"output":0.14,"cache_read":0.0014}
				},
				"direct-off":{"id":"direct-off","name":"Catalog Name","reasoning":true,"reasoning_options":[{"type":"effort","values":["high"]}],"limit":{"context":999}}
			}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := New(Config{BaseURL: srv.URL, CatalogURL: srv.URL + "/catalog", APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := <-catalogAuthorization; got != "" {
		t.Fatalf("catalog request leaked authorization header %q", got)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}

	deepseek := models[0]
	if deepseek.DisplayName != "DeepSeek V4 Flash" || deepseek.ContextWindow != 1000000 || deepseek.MaxOutputTokens != 384000 {
		t.Fatalf("enriched metadata = %+v", deepseek)
	}
	if !deepseek.SupportsThinking || !deepseek.SupportsTools {
		t.Fatalf("enriched capabilities = %+v", deepseek)
	}
	if got := deepseek.SupportedThinkingLevels(); len(got) != 4 || got[0] != protocol.ThinkingOff || got[1] != protocol.ThinkingLow || got[2] != protocol.ThinkingHigh || got[3] != protocol.ThinkingMax {
		t.Fatalf("enriched thinking levels = %v", got)
	}
	if deepseek.Pricing == nil || deepseek.Pricing.InputPerMillion != 0.07 || deepseek.Pricing.CacheReadPerMillion != 0.0014 {
		t.Fatalf("enriched pricing = %+v", deepseek.Pricing)
	}

	direct := models[1]
	if direct.DisplayName != "Gateway Name" || direct.ContextWindow != 123 || direct.SupportsThinking || len(direct.ThinkingLevels) != 0 {
		t.Fatalf("direct gateway metadata did not win = %+v", direct)
	}
}

func TestListModelsMissingReasoningMetadataIsConservative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[{"id":"unknown-model"}]}`)
	}))
	defer srv.Close()

	models, err := mustNew(t, srv.URL, "key").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %d, want 1", len(models))
	}
	model := models[0]
	if model.SupportsThinking || len(model.ThinkingLevels) != 0 || !model.SupportsTools {
		t.Fatalf("missing metadata was guessed: %+v", model)
	}
	if model.ContextWindow != 200000 {
		t.Fatalf("context fallback = %d, want 200000", model.ContextWindow)
	}
}

func TestListModelsGenericReasoningCapabilityDoesNotInventLevels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[
			{"id":"parameter-model","reasoning":true,"supported_parameters":["reasoning_effort"]},
			{"id":"boolean-model","supports_reasoning_effort":true}
		]}`)
	}))
	defer srv.Close()

	models, err := mustNew(t, srv.URL, "key").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	for _, model := range models {
		if !model.SupportsThinking || len(model.ThinkingLevels) != 0 {
			t.Errorf("%s capability = %+v, want reasoning support without invented levels", model.ID, model)
		}
		levels := model.SupportedThinkingLevels()
		if len(levels) != 1 || levels[0] != protocol.ThinkingOff {
			t.Errorf("%s selectable levels = %v, want [off]", model.ID, levels)
		}
	}
}

func TestListModelsRejectsCacheFromInferredEffortSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	cacheRoot := t.TempDir()
	stale := catalogCacheFile{
		Version:   1,
		BaseURL:   srv.URL,
		FetchedAt: time.Now().UnixMilli(),
		Models: []protocol.Model{{
			Provider: ProviderID, ID: "guessed-model", SupportsThinking: true,
			ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh},
		}},
	}
	data, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheRoot, "catalog.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	p, err := New(Config{BaseURL: stale.BaseURL, CacheRoot: cacheRoot})
	if err != nil {
		t.Fatal(err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != DefaultModelID {
		t.Fatalf("models = %+v, want static fallback instead of v1 cache", models)
	}
	if got := models[0].SupportedThinkingLevels(); len(got) != 1 || got[0] != protocol.ThinkingOff {
		t.Fatalf("fallback thinking levels = %v, want [off]", got)
	}
}

func TestListModelsMalformedFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `not-json`)
	}))
	defer srv.Close()

	models, err := mustNew(t, srv.URL, "key").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != DefaultModelID {
		t.Fatalf("malformed response did not use static fallback: %+v", models)
	}
}

func TestBuildBodyThinkingMapping(t *testing.T) {
	model := protocol.Model{
		Provider:         ProviderID,
		ID:               "reasoning-model",
		SupportsThinking: true,
		ThinkingLevels: []protocol.ThinkingLevel{
			protocol.ThinkingMinimal,
			protocol.ThinkingLow,
			protocol.ThinkingMedium,
			protocol.ThinkingHigh,
			protocol.ThinkingXHigh,
			protocol.ThinkingMax,
			protocol.ThinkingUltra,
		},
	}
	want := map[protocol.ThinkingLevel]string{
		protocol.ThinkingOff:     "",
		protocol.ThinkingMinimal: "minimal",
		protocol.ThinkingLow:     "low",
		protocol.ThinkingMedium:  "medium",
		protocol.ThinkingHigh:    "high",
		protocol.ThinkingXHigh:   "xhigh",
		protocol.ThinkingMax:     "max",
		protocol.ThinkingUltra:   "ultra",
	}
	p := mustNew(t, "http://unused", "key")
	for level, native := range want {
		body, err := p.buildBody(protocol.ChatRequest{Model: model, Thinking: level})
		if err != nil {
			t.Fatalf("buildBody(%q): %v", level, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		got, ok := decoded["reasoning_effort"].(string)
		if native == "" {
			if ok {
				t.Fatalf("level %q serialized reasoning_effort=%q, want omitted", level, got)
			}
		} else if !ok || got != native {
			t.Fatalf("level %q serialized reasoning_effort=%q/%v, want %q", level, got, ok, native)
		}
	}
}

func TestUnsupportedThinkingDoesNotMakeHTTPRequest(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := mustNew(t, srv.URL, "key")
	stream, err := p.Chat(context.Background(), auth.Credential{Key: "key"}, protocol.ChatRequest{
		Model: protocol.Model{
			Provider:         ProviderID,
			ID:               "low-only",
			SupportsThinking: true,
			ThinkingLevels:   []protocol.ThinkingLevel{protocol.ThinkingLow},
		},
		Thinking: protocol.ThinkingHigh,
	})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, stream, context.Background())
	if calls != 0 {
		t.Fatalf("HTTP calls = %d, want 0", calls)
	}
	if len(events) != 1 || events[0].Type != protocol.EvStreamError || events[0].Err == nil || !strings.Contains(events[0].Err.Error(), "low-only") || !strings.Contains(events[0].Err.Error(), "off|low") {
		t.Fatalf("events = %+v, want actionable unsupported-effort error", events)
	}
}
