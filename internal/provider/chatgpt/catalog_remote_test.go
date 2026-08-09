package chatgpt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestRemoteCatalogMappingETagAndAccountCache(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if r.URL.Path != "/codex/models" || r.URL.Query().Get("client_version") != CatalogCompatibilityVersion {
			t.Errorf("url=%s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer access" || r.Header.Get("chatgpt-account-id") != "acct" || r.Header.Get("originator") != "snow" {
			t.Errorf("headers=%v", r.Header)
		}
		if n > 1 {
			if r.Header.Get("If-None-Match") != "tag" {
				t.Errorf("etag=%q", r.Header.Get("If-None-Match"))
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", "tag")
		_ = json.NewEncoder(w).Encode(modelsResponse{Models: []modelRecord{
			{Slug: "visible", DisplayName: "Visible", Description: "desc", Visibility: "list", Priority: 2, ContextWindow: 1000, MaxContextWindow: 2000, EffectiveContextWindowPercent: 95, SupportVerbosity: true, SupportsReasoningSummaryParameter: boolPointer(true), InputModalities: []string{"text", "image"}, DefaultReasoningLevel: "medium", SupportedReasoningLevels: []reasoningLevelRecord{{"low"}, {"xhigh"}, {"high"}}, Upgrade: &modelUpgradeRecord{Model: "next", MigrationMarkdown: "move"}},
			{Slug: "hidden", Visibility: "hide", Priority: 1},
			{Slug: "spark", Visibility: "list", Priority: 1, SupportedInAPI: false, ContextWindow: 128000, SupportsReasoningSummaryParameter: boolPointer(false), SupportedReasoningLevels: []reasoningLevelRecord{{"medium"}}},
		}})
	}))
	defer server.Close()
	store := auth.NewMemoryStoreForTest()
	_ = store.Put(ProviderID, auth.Credential{Type: auth.CredentialOAuth, Access: "access", Refresh: "refresh", AccountID: "acct"})
	root := t.TempDir()
	p := New(Config{BaseURL: server.URL, Store: store, CacheRoot: root, HTTPClient: server.Client()})
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "spark" || models[1].ID != "visible" {
		t.Fatalf("models=%+v", models)
	}
	m := models[1]
	if m.ContextWindow != 950 || m.MaxContextWindow != 2000 || !m.SupportsVision || !m.SupportsVerbosity || m.SupportsReasoningSummary == nil || !*m.SupportsReasoningSummary || m.DefaultThinking != protocol.ThinkingMedium || m.Upgrade == nil || m.Upgrade.Model != "next" {
		t.Fatalf("mapped=%+v", m)
	}
	if models[0].SupportsReasoningSummary == nil || *models[0].SupportsReasoningSummary {
		t.Fatalf("Spark-like explicit summary capability was not preserved: %+v", models[0])
	}
	if got := m.SupportedThinkingLevels(); len(got) != 3 || got[1] != protocol.ThinkingLow || got[2] != protocol.ThinkingHigh {
		t.Fatalf("levels=%v", got)
	}
	if _, err = p.RefreshModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d", calls.Load())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache entries=%v err=%v", entries, err)
	}
	info, _ := entries[0].Info()
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode=%o", info.Mode().Perm())
	}
	if strings.Contains(entries[0].Name(), "acct") {
		t.Fatal("cache filename exposed account id")
	}
}

func boolPointer(value bool) *bool { return &value }
