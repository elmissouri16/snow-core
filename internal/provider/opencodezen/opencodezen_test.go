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
	validData, _ := json.Marshal(catalogCache{Version: 1, BaseURL: "https://cache.invalid/v1", FetchedAt: time.Now().UnixMilli(), Models: []protocol.Model{validModel}})
	if err := os.WriteFile(cachePath, validData, 0o600); err != nil {
		t.Fatal(err)
	}
	cached, _ := New(Config{BaseURL: "https://cache.invalid/v1", CacheRoot: cacheRoot})
	models, err := cached.ListModels(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != validModel.ID {
		t.Fatalf("valid cache models=%v err=%v", modelIDs(models), err)
	}

	paidData, _ := json.Marshal(catalogCache{Version: 1, BaseURL: "https://cache.invalid/v1", FetchedAt: time.Now().UnixMilli(), Models: []protocol.Model{{Provider: ProviderID, ID: "gpt-5.4"}}})
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
	p, _ := New(Config{BaseURL: server.URL + "/v1", HTTPClient: server.Client(), RetryDelays: []time.Duration{}})
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

func TestChatUsesBearerAndRejectsUnknownModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer zen-secret" {
			t.Errorf("Authorization=%q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), RetryDelays: []time.Duration{}})
	stream, _ := p.Chat(context.Background(), auth.Credential{Key: "zen-secret"}, chatRequest("big-pickle"))
	drainText(t, stream)
	stream, _ = p.Chat(context.Background(), auth.Credential{}, chatRequest("gpt-5.4"))
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamError || !strings.Contains(event.Err.Error(), "maintained free catalog") {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestChatRetriesHTTP429BeforeOutput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call <= 3 {
			http.Error(w, `{"error":{"type":"FreeUsageLimitError","message":"later"}}`, http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), RetryDelays: []time.Duration{0, 0, 0}})
	stream, _ := p.Chat(context.Background(), auth.Credential{}, chatRequest("big-pickle"))
	if got := drainText(t, stream); got != "ok" || calls.Load() != 4 {
		t.Fatalf("text=%q calls=%d", got, calls.Load())
	}
}

func TestResponsesRetriesHTTP429(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		if calls.Add(1) == 1 {
			http.Error(w, "limit", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), RetryDelays: []time.Duration{0}})
	stream, _ := p.Chat(context.Background(), auth.Credential{}, chatRequest("muse-spark-1.2-contributor-free"))
	if got := drainText(t, stream); got != "ok" || calls.Load() != 2 {
		t.Fatalf("text=%q calls=%d", got, calls.Load())
	}
}

func TestChatStopsAfterRetryBudgetAndRedactsKey(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "limit zen-secret", http.StatusTooManyRequests)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), RetryDelays: []time.Duration{0, 0, 0}})
	stream, _ := p.Chat(context.Background(), auth.Credential{Key: "zen-secret"}, chatRequest("big-pickle"))
	event, err := stream.Next(context.Background())
	var limited *providerpkg.LimitError
	if err != nil || event.Type != protocol.EvStreamError || !errors.As(event.Err, &limited) || calls.Load() != 4 {
		t.Fatalf("event=%+v err=%v calls=%d", event, err, calls.Load())
	}
	if strings.Contains(event.Err.Error(), "zen-secret") || !strings.Contains(event.Err.Error(), "[redacted]") {
		t.Fatalf("secret not redacted: %v", event.Err)
	}
}

func TestRetryWaitHonorsCancellation(t *testing.T) {
	first := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first <- struct{}{}
		http.Error(w, "limit", http.StatusTooManyRequests)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), RetryDelays: []time.Duration{time.Hour}})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan protocol.EventStream, 1)
	go func() {
		stream, _ := p.Chat(ctx, auth.Credential{}, chatRequest("big-pickle"))
		result <- stream
	}()
	<-first
	cancel()
	stream := <-result
	event, err := stream.Next(context.Background())
	if !errors.Is(err, context.Canceled) && (event.Type != protocol.EvStreamError || !errors.Is(event.Err, context.Canceled)) {
		t.Fatalf("event=%+v err=%v", event, err)
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

func modelIDs(models []protocol.Model) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}
