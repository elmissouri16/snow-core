package opencodezen

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
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
	"github.com/elmissouri16/snow-core/internal/provider/modelsdev"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type zenCatalogResponse struct{ models, metadata []byte }

type zenCatalogFixture struct {
	server          *httptest.Server
	response        atomic.Pointer[zenCatalogResponse]
	offline         atomic.Bool
	metadataOffline atomic.Bool
	modelCalls      atomic.Int32
	chatPaths       chan string
}

func newZenCatalogFixture(t *testing.T) *zenCatalogFixture {
	t.Helper()
	f := &zenCatalogFixture{chatPaths: make(chan string, 8)}
	f.set(t, nil, nil)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			f.modelCalls.Add(1)
		}
		if f.offline.Load() || (f.metadataOffline.Load() && r.URL.Path == "/catalog") {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write(f.response.Load().models)
		case "/catalog":
			if r.Header.Get("Authorization") != "" {
				t.Error("metadata request included credentials")
			}
			_, _ = w.Write(f.response.Load().metadata)
		case "/v1/chat/completions":
			f.chatPaths <- r.URL.Path
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		case "/v1/responses":
			f.chatPaths <- r.URL.Path
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *zenCatalogFixture) set(t *testing.T, ids []string, metadata map[string]modelsdev.Model) {
	t.Helper()
	rows := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]string{"id": id})
	}
	models, err := json.Marshal(map[string]any{"data": rows})
	if err != nil {
		t.Fatal(err)
	}
	details, err := json.Marshal(modelsdev.Catalog{
		"opencode": {NPM: "@ai-sdk/openai-compatible", Models: metadata},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.response.Store(&zenCatalogResponse{models: models, metadata: details})
}

func (f *zenCatalogFixture) provider(t *testing.T, cacheRoot string) *Provider {
	t.Helper()
	p, err := New(Config{BaseURL: f.server.URL + "/v1", CatalogURL: f.server.URL + "/catalog", HTTPClient: f.server.Client(), CacheRoot: cacheRoot})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func freeMetadata() modelsdev.Model {
	return modelsdev.Model{
		Name: "New free model", ToolCall: new(true), Reasoning: new(true),
		ReasoningOptions: []modelsdev.ReasoningOption{{Type: "effort", Values: []string{"low", "high"}}},
		Limit:            modelsdev.Limit{Context: 262144, Input: 200000, Output: 32768},
		Cost:             &modelsdev.Cost{Input: new(0.0), Output: new(0.0)},
		Modalities:       modelsdev.Modalities{Input: []string{"text", "image"}, Output: []string{"text"}},
	}
}

func TestDynamicCatalogDiscoversFreeModelsAndRoutesBothProtocols(t *testing.T) {
	f := newZenCatalogFixture(t)
	chat, responses := freeMetadata(), freeMetadata()
	responses.Provider.NPM = "@ai-sdk/openai"
	paid, deprecated, unsupported, missingPrice := freeMetadata(), freeMetadata(), freeMetadata(), freeMetadata()
	paid.Cost.Input = new(1.0)
	deprecated.Status = "deprecated"
	unsupported.Provider.NPM = "@ai-sdk/anthropic"
	missingPrice.Cost.Output = nil
	f.set(t, []string{"future-chat", "future-responses", "paid-free", "deprecated-free", "unsupported-free", "unknown-free", "missing-price-free", "future-chat"}, map[string]modelsdev.Model{
		"future-chat": chat, "future-responses": responses, "paid-free": paid,
		"deprecated-free": deprecated, "unsupported-free": unsupported,
		"missing-price-free": missingPrice, "unavailable-free": freeMetadata(),
	})
	p, err := New(Config{BaseURL: f.server.URL + "/v1", CatalogURL: f.server.URL + "/catalog", HTTPClient: f.server.Client(), DefaultModel: "future-responses"})
	if err != nil {
		t.Fatal(err)
	}
	models, err := p.ListModelsWithCredential(t.Context(), auth.Credential{Key: "zen-test-key"})
	if err != nil || !slices.Equal(modelIDs(models), []string{"future-chat", "future-responses"}) {
		t.Fatalf("models=%v err=%v", modelIDs(models), err)
	}
	for _, model := range models {
		if model.ContextWindow != 200000 || model.MaxContextWindow != 262144 || model.MaxOutputTokens != 32768 ||
			!model.SupportsVision || !model.SupportsThinking || !model.SupportsTools || model.Pricing == nil ||
			!slices.Equal(model.ThinkingLevels, []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingHigh}) {
			t.Fatalf("incorrect dynamic metadata: %+v", model)
		}
		stream, err := p.Chat(t.Context(), auth.Credential{}, chatRequest(model.ID))
		if err != nil {
			t.Fatal(err)
		}
		drainText(t, stream)
		wantPath := "/v1/chat/completions"
		if model.ID == "future-responses" {
			wantPath = "/v1/responses"
		}
		if got := <-f.chatPaths; got != wantPath {
			t.Fatalf("%s path=%s want=%s", model.ID, got, wantPath)
		}
	}
	// Caller-supplied capabilities cannot turn a rejected ID into a free model.
	stream, _ := p.Chat(t.Context(), auth.Credential{}, chatRequest("paid-free"))
	if err := drainError(stream); err == nil {
		t.Fatal("paid route was accepted")
	}
	if p.DefaultModel().ID != "future-responses" || p.DefaultModel().ContextWindow != 200000 {
		t.Fatalf("dynamic default=%+v", p.DefaultModel())
	}
}

func TestDynamicCatalogCacheExpiryForcedRefreshAndOfflineRestart(t *testing.T) {
	f := newZenCatalogFixture(t)
	f.set(t, []string{"first"}, map[string]modelsdev.Model{"first": freeMetadata()})
	cacheRoot := t.TempDir()
	p := f.provider(t, cacheRoot)
	if _, err := p.ListModels(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.set(t, []string{"second"}, map[string]modelsdev.Model{"second": freeMetadata()})
	models, _ := p.ListModels(t.Context())
	if !slices.Equal(modelIDs(models), []string{"first"}) || p.ModelCatalogStale() || f.modelCalls.Load() != 1 {
		t.Fatalf("fresh cache=%v requests=%d", modelIDs(models), f.modelCalls.Load())
	}
	restarted := f.provider(t, cacheRoot)
	models, _ = restarted.ListModels(t.Context())
	if !slices.Equal(modelIDs(models), []string{"first"}) || f.modelCalls.Load() != 1 {
		t.Fatalf("disk cache=%v requests=%d", modelIDs(models), f.modelCalls.Load())
	}
	models, _ = restarted.RefreshModels(t.Context())
	if !slices.Equal(modelIDs(models), []string{"second"}) || f.modelCalls.Load() != 2 {
		t.Fatalf("forced refresh=%v requests=%d", modelIDs(models), f.modelCalls.Load())
	}
	f.set(t, []string{"third"}, map[string]modelsdev.Model{"third": freeMetadata()})
	restarted.catalogExpiresAt.Store(time.Now().Add(-time.Second).UnixMilli())
	models, _ = restarted.ListModels(t.Context())
	if !slices.Equal(modelIDs(models), []string{"third"}) || f.modelCalls.Load() != 3 {
		t.Fatalf("expired refresh=%v requests=%d", modelIDs(models), f.modelCalls.Load())
	}
	f.offline.Store(true)
	offline := f.provider(t, cacheRoot)
	models, _ = offline.RefreshModels(t.Context())
	if !slices.Equal(modelIDs(models), []string{"third"}) {
		t.Fatalf("offline restart lost verified dynamic model: %v", modelIDs(models))
	}
	if spec, ok := offline.modelSpec("third"); !ok || spec.Transport != transportChat {
		t.Fatalf("offline route=%+v ok=%v", spec, ok)
	}
}

func TestDynamicCatalogWithdrawalsAndPaidTransitionsRemainExcluded(t *testing.T) {
	for _, id := range []string{"future-free", "big-pickle"} {
		t.Run(id, func(t *testing.T) {
			f := newZenCatalogFixture(t)
			f.set(t, []string{id}, map[string]modelsdev.Model{id: freeMetadata()})
			cacheRoot := t.TempDir()
			p := f.provider(t, cacheRoot)
			if _, err := p.ListModels(t.Context()); err != nil {
				t.Fatal(err)
			}
			paid := freeMetadata()
			paid.Cost.Output = new(1.0)
			f.set(t, []string{id}, map[string]modelsdev.Model{id: paid})
			// Chat also refreshes an expired catalog before another inference.
			p.catalogExpiresAt.Store(time.Now().Add(-time.Second).UnixMilli())
			stream, _ := p.Chat(t.Context(), auth.Credential{}, chatRequest(id))
			if err := drainError(stream); err == nil || !strings.Contains(err.Error(), "verified free catalog") {
				t.Fatalf("paid transition accepted: %v", err)
			}
			f.offline.Store(true)
			restarted := f.provider(t, cacheRoot)
			models, err := restarted.RefreshModels(t.Context())
			if err != nil || len(models) != 0 {
				t.Fatalf("offline restart resurrected withdrawn model: %v err=%v", modelIDs(models), err)
			}
		})
	}
}

func TestDynamicCatalogMetadataFailureRetainsOnlyVerifiedAvailableModels(t *testing.T) {
	f := newZenCatalogFixture(t)
	f.set(t, []string{"verified"}, map[string]modelsdev.Model{"verified": freeMetadata()})
	p := f.provider(t, t.TempDir())
	_, _ = p.ListModels(t.Context())
	f.set(t, []string{"verified", "unverified-free"}, nil)
	f.metadataOffline.Store(true)
	models, err := p.RefreshModels(t.Context())
	if err != nil || !slices.Equal(modelIDs(models), []string{"verified"}) {
		t.Fatalf("metadata outage=%v err=%v", modelIDs(models), err)
	}
	f.set(t, nil, nil)
	models, err = p.RefreshModels(t.Context())
	if err != nil || len(models) != 0 {
		t.Fatalf("withdrawal during metadata outage=%v err=%v", modelIDs(models), err)
	}
	restarted := f.provider(t, p.cacheRoot)
	f.offline.Store(true)
	models, err = restarted.RefreshModels(t.Context())
	if err != nil || len(models) != 0 {
		t.Fatalf("stale disk resurrected unavailable models=%v err=%v", modelIDs(models), err)
	}
}

func TestDynamicCatalogRejectsUnverifiedCacheAndPreservesCancellation(t *testing.T) {
	f := newZenCatalogFixture(t)
	f.set(t, []string{"verified"}, map[string]modelsdev.Model{"verified": freeMetadata()})
	cacheRoot := t.TempDir()
	p := f.provider(t, cacheRoot)
	_, _ = p.ListModels(t.Context())
	data, err := os.ReadFile(filepath.Join(cacheRoot, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cached catalogCache
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatal(err)
	}
	delete(cached.Metadata, "verified")
	data, _ = json.Marshal(cached)
	if err := os.WriteFile(filepath.Join(cacheRoot, "catalog.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted := f.provider(t, cacheRoot)
	if models, _ := restarted.loadCatalogCache(time.Now()); models != nil {
		t.Fatalf("cache without free-pricing evidence accepted: %v", modelIDs(models))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.RefreshModels(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if models, _ := p.ListModels(t.Context()); !slices.Equal(modelIDs(models), []string{"verified"}) {
		t.Fatalf("cancellation changed catalog=%v", modelIDs(models))
	}
}
