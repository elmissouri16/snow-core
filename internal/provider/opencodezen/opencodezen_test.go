package opencodezen

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
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCatalogURLDefaultsOnlyForOpenCodeZenEndpoint(t *testing.T) {
	provider, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.catalogURL != DefaultCatalogURL || DefaultCatalogURL != "https://models.dev/api.json" {
		t.Fatalf("default catalog URL=%q", provider.catalogURL)
	}

	custom, err := New(Config{BaseURL: "https://gateway.example/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if custom.catalogURL != "" {
		t.Fatalf("custom gateway unexpectedly uses catalog %q", custom.catalogURL)
	}
}

func TestListModelsFiltersToMaintainedFreeCatalogAndOptionalAuth(t *testing.T) {
	for _, tc := range []struct {
		name, key, wantAuth string
	}{
		{name: "anonymous"},
		{name: "key", key: "zen-secret", wantAuth: "Bearer zen-secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" {
					t.Errorf("path=%q", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != tc.wantAuth {
					t.Errorf("Authorization=%q want %q", got, tc.wantAuth)
				}
				_, _ = io.WriteString(w, `{"data":[{"id":"big-pickle"},{"id":"muse-spark-1.2-contributor-free"},{"id":"deepseek-v4-flash-free"},{"id":"gpt-5.4"}]}`)
			}))
			defer server.Close()
			p, err := New(Config{BaseURL: server.URL + "/v1", HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			models, err := p.ListModelsWithCredential(context.Background(), auth.Credential{Key: tc.key})
			if err != nil {
				t.Fatal(err)
			}
			if got := modelIDs(models); !slices.Equal(got, []string{"big-pickle", "muse-spark-1.2-contributor-free"}) {
				t.Fatalf("models=%v", got)
			}
			if models[0].Description == "" || models[1].Description == "" {
				t.Fatalf("privacy descriptions missing: %+v", models)
			}
		})
	}
}

func TestListModelsFallsBackToNonDeprecatedFreeCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	p, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), CacheRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil || len(models) != 7 || models[0].ID != DefaultModelID {
		t.Fatalf("fallback=%v err=%v", modelIDs(models), err)
	}
	for _, model := range models {
		if model.ID == "deepseek-v4-flash-free" || model.ID == "laguna-s-2.1-free" {
			t.Fatalf("deprecated model in fallback: %s", model.ID)
		}
	}
}

func TestListModelsValidEmptyLiveCatalogIsAuthoritative(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.4"}]}`)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	models, err := p.ListModels(context.Background())
	if err != nil || len(models) != 0 {
		t.Fatalf("models=%v err=%v", modelIDs(models), err)
	}
}

func TestStaticCatalogPublishesPolicyMetadataWithoutReasoningClaims(t *testing.T) {
	want := map[string]struct{ context, maximum, output int }{
		"big-pickle":                      {context: 160000, maximum: 200000, output: 32000},
		"x-preview-f-free":                {context: 1000000, output: 131072},
		"mimo-v2.5-free":                  {context: 200000, output: 32000},
		"hy3-free":                        {context: 190000, output: 64000},
		"nemotron-3-ultra-free":           {context: 1000000, output: 128000},
		"nemotron-3.5-lightning-free":     {context: 262144, output: 262144},
		"muse-spark-1.2-contributor-free": {context: 1048576, output: 131072},
	}
	for _, model := range staticCatalog() {
		expected, ok := want[model.ID]
		if !ok {
			t.Fatalf("unexpected model %q", model.ID)
		}
		if model.ContextWindow != expected.context || model.MaxContextWindow != expected.maximum || model.MaxOutputTokens != expected.output {
			t.Errorf("%s limits=%d/%d/%d want=%d/%d/%d", model.ID, model.ContextWindow, model.MaxContextWindow, model.MaxOutputTokens, expected.context, expected.maximum, expected.output)
		}
		if !model.SupportsTools || model.SupportsThinking || len(model.ThinkingLevels) != 0 {
			t.Errorf("%s static capabilities guessed reasoning: %+v", model.ID, model)
		}
		if got := model.SupportedThinkingLevels(); !slices.Equal(got, []protocol.ThinkingLevel{protocol.ThinkingOff}) {
			t.Errorf("%s thinking levels=%v want=[off]", model.ID, got)
		}
		delete(want, model.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing metadata for %v", want)
	}
}

func TestListModelsLoadsReasoningMetadataFromModelsDev(t *testing.T) {
	catalogAuthorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			if got := r.Header.Get("Authorization"); got != "Bearer zen-secret" {
				t.Errorf("models Authorization=%q", got)
			}
			_, _ = io.WriteString(w, `{"data":[
				{"id":"big-pickle"},{"id":"x-preview-f-free"},{"id":"hy3-free"},
				{"id":"muse-spark-1.2-contributor-free"},{"id":"gpt-5.4"}
			]}`)
		case "/catalog":
			catalogAuthorization <- r.Header.Get("Authorization")
			_, _ = io.WriteString(w, `{"opencode":{"models":{
				"big-pickle":{"reasoning":true,"reasoning_options":[{"type":"toggle"}]},
				"x-preview-f-free":{"reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high","max"]}]},
				"hy3-free":{"reasoning":true,"reasoning_options":[{"type":"toggle"},{"type":"effort","values":["low","medium","high"]}]},
				"muse-spark-1.2-contributor-free":{"reasoning":true,"reasoning_options":[{"type":"effort","values":["minimal","low","medium","high","xhigh"]}]},
				"gpt-5.4":{"reasoning":true,"reasoning_options":[{"type":"effort","values":["ultra"]}]}
			}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p, err := New(Config{BaseURL: server.URL + "/v1", CatalogURL: server.URL + "/catalog", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	models, err := p.ListModelsWithCredential(context.Background(), auth.Credential{Key: "zen-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-catalogAuthorization; got != "" {
		t.Fatalf("catalog request leaked Authorization=%q", got)
	}
	want := map[string][]protocol.ThinkingLevel{
		"big-pickle":                      {protocol.ThinkingOff},
		"x-preview-f-free":                {protocol.ThinkingOff, protocol.ThinkingLow, protocol.ThinkingHigh, protocol.ThinkingMax},
		"hy3-free":                        {protocol.ThinkingOff, protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh},
		"muse-spark-1.2-contributor-free": {protocol.ThinkingOff, protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh, protocol.ThinkingXHigh},
	}
	if len(models) != len(want) {
		t.Fatalf("models=%v", modelIDs(models))
	}
	for _, model := range models {
		levels, ok := want[model.ID]
		if !ok {
			t.Fatalf("unexpected model %q", model.ID)
		}
		if !model.SupportsThinking || !slices.Equal(model.SupportedThinkingLevels(), levels) {
			t.Errorf("%s supports=%v levels=%v want=%v", model.ID, model.SupportsThinking, model.SupportedThinkingLevels(), levels)
		}
	}
}

func TestListModelsUsesCachedReasoningWhenMetadataRefreshFails(t *testing.T) {
	cacheRoot := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"x-preview-f-free"}]}`)
		case "/catalog":
			http.Error(w, "offline", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cachedModel := freeModels()[1].Model
	cachedModel.SupportsThinking = true
	cachedModel.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingHigh}
	cachedAt := time.Now().Add(-time.Hour)
	data, _ := json.Marshal(catalogCache{
		Version: catalogCacheVersion, BaseURL: server.URL + "/v1", CatalogURL: server.URL + "/catalog",
		FetchedAt: cachedAt.UnixMilli(), Models: []protocol.Model{cachedModel},
	})
	if err := os.WriteFile(filepath.Join(cacheRoot, "catalog.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{
		BaseURL: server.URL + "/v1", CatalogURL: server.URL + "/catalog",
		HTTPClient: server.Client(), CacheRoot: cacheRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil || len(models) != 1 || !models[0].SupportsThinking || !slices.Equal(models[0].ThinkingLevels, cachedModel.ThinkingLevels) {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	persisted, err := os.ReadFile(filepath.Join(cacheRoot, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cache catalogCache
	if json.Unmarshal(persisted, &cache) != nil || cache.FetchedAt != cachedAt.UnixMilli() {
		t.Fatalf("failed metadata refresh replaced verified cache: %+v", cache)
	}
}

func TestCatalogCacheValidationAndPermissions(t *testing.T) {
	cacheRoot := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"big-pickle"}]}`)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), CacheRoot: cacheRoot})
	if _, err := p.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(cacheRoot, "catalog.json")
	info, err := os.Stat(cachePath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache info=%v err=%v", info, err)
	}

	validModel := freeModels()[1].Model
	validModel.SupportsThinking = true
	validModel.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingHigh}
	validData, _ := json.Marshal(catalogCache{
		Version: catalogCacheVersion, BaseURL: "https://cache.invalid/v1",
		FetchedAt: time.Now().UnixMilli(), Models: []protocol.Model{validModel},
	})
	if err := os.WriteFile(cachePath, validData, 0o600); err != nil {
		t.Fatal(err)
	}
	cached, _ := New(Config{BaseURL: "https://cache.invalid/v1", CacheRoot: cacheRoot})
	models, err := cached.ListModels(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != validModel.ID || !models[0].SupportsThinking || !slices.Equal(models[0].ThinkingLevels, validModel.ThinkingLevels) {
		t.Fatalf("valid cache models=%+v err=%v", models, err)
	}

	legacyData, _ := json.Marshal(catalogCache{
		Version: 1, BaseURL: "https://cache.invalid/v1",
		FetchedAt: time.Now().UnixMilli(), Models: []protocol.Model{validModel},
	})
	if err := os.WriteFile(cachePath, legacyData, 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, _ := New(Config{BaseURL: "https://cache.invalid/v1", CacheRoot: cacheRoot})
	if models, _ := legacy.loadCatalogCache(time.Now()); len(models) != 0 {
		t.Fatalf("legacy cache retained pinned reasoning: %+v", models)
	}

	paidData, _ := json.Marshal(catalogCache{
		Version: catalogCacheVersion, BaseURL: "https://cache.invalid/v1",
		FetchedAt: time.Now().UnixMilli(), Models: []protocol.Model{{Provider: ProviderID, ID: "gpt-5.4"}},
	})
	if err := os.WriteFile(cachePath, paidData, 0o600); err != nil {
		t.Fatal(err)
	}
	rejected, _ := New(Config{BaseURL: "https://cache.invalid/v1", CacheRoot: cacheRoot, DiscoveryTimeout: time.Millisecond})
	models, err = rejected.ListModels(context.Background())
	if err != nil || len(models) != 7 {
		t.Fatalf("paid cache was not rejected: models=%v err=%v", modelIDs(models), err)
	}
}

func TestChatRoutesTransportAndOmitsAnonymousAuthorization(t *testing.T) {
	var chatCalls, responsesCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("anonymous Authorization=%q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] == "" || body["stream"] != true {
			t.Errorf("body=%v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch r.URL.Path {
		case "/v1/chat/completions":
			chatCalls++
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"chat\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		case "/v1/responses":
			responsesCalls++
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"responses\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL + "/v1", HTTPClient: server.Client()})
	for _, tc := range []struct{ model, want string }{
		{model: "big-pickle", want: "chat"},
		{model: "muse-spark-1.2-contributor-free", want: "responses"},
	} {
		stream, err := p.Chat(context.Background(), auth.Credential{}, chatRequest(tc.model))
		if err != nil {
			t.Fatal(err)
		}
		if got := drainText(t, stream); got != tc.want {
			t.Fatalf("%s text=%q", tc.model, got)
		}
	}
	if chatCalls != 1 || responsesCalls != 1 {
		t.Fatalf("chat=%d responses=%d", chatCalls, responsesCalls)
	}
}

func TestChatSerializesAdvertisedThinkingEfforts(t *testing.T) {
	wantEffort := map[string]string{
		"x-preview-f-free":                "max",
		"hy3-free":                        "medium",
		"muse-spark-1.2-contributor-free": "xhigh",
	}
	seen := make(map[string]bool, len(wantEffort))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog":
			_, _ = io.WriteString(w, `{"opencode":{"models":{
				"x-preview-f-free":{"reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high","max"]}]},
				"hy3-free":{"reasoning":true,"reasoning_options":[{"type":"effort","values":["low","medium","high"]}]},
				"muse-spark-1.2-contributor-free":{"reasoning":true,"reasoning_options":[{"type":"effort","values":["minimal","low","medium","high","xhigh"]}]}
			}}}`)
			return
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"x-preview-f-free"},{"id":"hy3-free"},{"id":"muse-spark-1.2-contributor-free"}]}`)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		model, _ := body["model"].(string)
		want, ok := wantEffort[model]
		if !ok {
			t.Errorf("unexpected model in request body: %v", body)
		}
		var got string
		switch r.URL.Path {
		case "/v1/chat/completions":
			got, _ = body["reasoning_effort"].(string)
		case "/v1/responses":
			reasoning, _ := body["reasoning"].(map[string]any)
			got, _ = reasoning["effort"].(string)
		default:
			http.NotFound(w, r)
			return
		}
		if got != want {
			t.Errorf("%s reasoning effort=%q want=%q; body=%v", model, got, want, body)
		}
		seen[model] = true
		w.Header().Set("Content-Type", "text/event-stream")
		if r.URL.Path == "/v1/responses" {
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	p, err := New(Config{
		BaseURL: server.URL + "/v1", CatalogURL: server.URL + "/catalog",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	advertised := make(map[string]protocol.Model, len(models))
	for _, model := range models {
		advertised[model.ID] = model
	}
	for _, tc := range []struct {
		model    string
		thinking protocol.ThinkingLevel
	}{
		{model: "x-preview-f-free", thinking: protocol.ThinkingMax},
		{model: "hy3-free", thinking: protocol.ThinkingMedium},
		{model: "muse-spark-1.2-contributor-free", thinking: protocol.ThinkingXHigh},
	} {
		model, ok := advertised[tc.model]
		if !ok {
			t.Fatalf("missing dynamically advertised model %q", tc.model)
		}
		request := chatRequest(tc.model)
		request.Model = model
		request.Thinking = tc.thinking
		stream, err := p.Chat(context.Background(), auth.Credential{}, request)
		if err != nil {
			t.Fatal(err)
		}
		if got := drainText(t, stream); got != "ok" {
			t.Fatalf("%s text=%q", tc.model, got)
		}
	}
	if len(seen) != len(wantEffort) {
		t.Fatalf("models seen=%v want=%v", seen, wantEffort)
	}
}

func TestChatSurfacesSuccessfulEmptyCompletions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		payload  string
		contains string
	}{
		{name: "empty", payload: `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`, contains: "returned an empty completion"},
		{name: "reasoning-only", payload: `data: {"choices":[{"delta":{"reasoning_content":"thinking"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`, contains: "without producing a final answer"},
		{name: "output-limit", payload: `data: {"choices":[{"delta":{},"finish_reason":"length"}]}

data: [DONE]

`, contains: "reached its output limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.payload)
			}))
			defer server.Close()
			p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
			stream, err := p.Chat(context.Background(), auth.Credential{}, chatRequest("big-pickle"))
			if err != nil {
				t.Fatal(err)
			}
			streamErr := drainError(stream)
			if streamErr == nil || !strings.Contains(streamErr.Error(), tc.contains) || !strings.Contains(streamErr.Error(), "big-pickle") {
				t.Fatalf("error=%v, want %q", streamErr, tc.contains)
			}
		})
	}
}

func TestChatUsesBearerAndRejectsUnknownModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer zen-secret" {
			t.Errorf("Authorization=%q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, _ := p.Chat(context.Background(), auth.Credential{Key: "zen-secret"}, chatRequest("big-pickle"))
	drainText(t, stream)
	stream, _ = p.Chat(context.Background(), auth.Credential{}, chatRequest("gpt-5.4"))
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamError || !strings.Contains(event.Err.Error(), "maintained free catalog") {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestChatClassifiesHTTP429WithoutLocalRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3")
		http.Error(w, `{"error":{"type":"FreeUsageLimitError","message":"later"}}`, http.StatusTooManyRequests)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, _ := p.Chat(context.Background(), auth.Credential{}, chatRequest("big-pickle"))
	event, err := stream.Next(context.Background())
	advice, ok := providerpkg.RetryAdviceFor(event.Err)
	if err != nil || !ok || advice.Kind != providerpkg.RetryRateLimit || advice.RetryAfter != 3*time.Second || calls.Load() != 1 {
		t.Fatalf("event=%+v err=%v advice=%+v calls=%d", event, err, advice, calls.Load())
	}
}

func TestResponsesClassifiesHTTP429WithoutLocalRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		http.Error(w, "limit", http.StatusTooManyRequests)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, _ := p.Chat(context.Background(), auth.Credential{}, chatRequest("muse-spark-1.2-contributor-free"))
	event, err := stream.Next(context.Background())
	advice, ok := providerpkg.RetryAdviceFor(event.Err)
	if err != nil || !ok || advice.Kind != providerpkg.RetryRateLimit || calls.Load() != 1 {
		t.Fatalf("event=%+v err=%v advice=%+v calls=%d", event, err, advice, calls.Load())
	}
}

func TestChatRateLimitRedactsKey(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "limit zen-secret", http.StatusTooManyRequests)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, _ := p.Chat(context.Background(), auth.Credential{Key: "zen-secret"}, chatRequest("big-pickle"))
	event, err := stream.Next(context.Background())
	advice, ok := providerpkg.RetryAdviceFor(event.Err)
	if err != nil || event.Type != protocol.EvStreamError || !ok || advice.Kind != providerpkg.RetryRateLimit || calls.Load() != 1 {
		t.Fatalf("event=%+v err=%v advice=%+v calls=%d", event, err, advice, calls.Load())
	}
	if strings.Contains(event.Err.Error(), "zen-secret") || !strings.Contains(event.Err.Error(), "[redacted]") {
		t.Fatalf("secret not redacted: %v", event.Err)
	}
}

func chatRequest(model string) protocol.ChatRequest {
	return protocol.ChatRequest{
		Model:    protocol.Model{Provider: ProviderID, ID: model, SupportsTools: true},
		Messages: []protocol.Message{protocol.NewUserMessage("u", "", "hello")},
	}
}

func drainText(t *testing.T, stream protocol.EventStream) string {
	t.Helper()
	defer stream.Close()
	var text strings.Builder
	for {
		event, err := stream.Next(context.Background())
		if errors.Is(err, io.EOF) {
			return text.String()
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Type == protocol.EvStreamError {
			t.Fatalf("stream error: %v", event.Err)
		}
		if event.Type == protocol.EvStreamTextDelta {
			text.WriteString(event.Text)
		}
	}
}

func drainError(stream protocol.EventStream) error {
	defer stream.Close()
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			return err
		}
		if event.Type == protocol.EvStreamError {
			return event.Err
		}
		if event.Type == protocol.EvStreamDone {
			return nil
		}
	}
}

func modelIDs(models []protocol.Model) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}
