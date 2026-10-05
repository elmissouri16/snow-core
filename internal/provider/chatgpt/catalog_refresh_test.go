package chatgpt

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestAstraCatalogCompatibilityReplacesFreshLegacyCache(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("originator") != "snow" {
			t.Error("catalog client must identify as Snow")
		}
		switch r.URL.Query().Get("client_version") {
		case "0.147.0":
			w.Header().Set("ETag", "legacy")
			fmt.Fprint(w, `{"models":[{"slug":"gpt-5.6-sol","visibility":"list"}]}`)
		case "0.160.0":
			if r.Header.Get("If-None-Match") != "" {
				t.Error("incompatible cache ETag was reused")
			}
			fmt.Fprint(w, `{"models":[
				{"slug":"gpt-6-astra","display_name":"GPT-6-Astra","visibility":"list",
				 "context_window":272000,"max_context_window":872000,"effective_context_window_percent":95,
				 "input_modalities":["text","image"],"support_verbosity":true,"tool_mode":"code_mode_only",
				 "default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"},{"effort":"max"},{"effort":"ultra"}]},
				{"slug":"hidden","visibility":"hide"}]}`)
		default:
			http.Error(w, "unexpected compatibility contract", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	credential := auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}
	cfg := Config{BaseURL: server.URL, HTTPClient: server.Client(), CacheRoot: t.TempDir(), ClientVersion: "0.147.0"}
	legacy := New(cfg)
	if _, err := legacy.ListModelsWithCredential(t.Context(), credential); err != nil {
		t.Fatal(err)
	}
	cfg.ClientVersion = ""
	p := New(cfg)
	models, err := p.ListModelsWithCredential(t.Context(), credential)
	if err != nil || len(models) != 1 || models[0].ID != "gpt-6-astra" || calls.Load() != 2 {
		t.Fatalf("new catalog=%+v calls=%d err=%v", models, calls.Load(), err)
	}
	m := models[0]
	if m.ContextWindow != 258400 || m.MaxContextWindow != 872000 || !m.SupportsTools || !m.SupportsVision ||
		!m.SupportsVerbosity || m.DefaultThinking != protocol.ThinkingMedium ||
		!slices.Equal(m.ThinkingLevels, []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax}) {
		t.Fatalf("Astra capabilities=%+v", m)
	}
	revision := p.ModelCatalogRevision()
	if _, err := p.ListModelsWithCredential(t.Context(), credential); err != nil || calls.Load() != 2 || p.ModelCatalogRevision() != revision {
		t.Fatalf("cache hit changed snapshot: calls=%d err=%v", calls.Load(), err)
	}
}

func TestCatalogExpiryRevalidationAndFailureRetry(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if code := int(status.Load()); code != http.StatusOK {
			if code == http.StatusNotModified && r.Header.Get("If-None-Match") != "current" {
				t.Error("revalidation omitted ETag")
			}
			w.WriteHeader(code)
			return
		}
		w.Header().Set("ETag", "current")
		fmt.Fprint(w, `{"models":[{"slug":"gpt-6-astra","visibility":"list"}]}`)
	}))
	defer server.Close()
	now := time.Now().UTC()
	p := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), CacheRoot: t.TempDir(), Now: func() time.Time { return now }})
	credential := auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}
	if !p.ModelCatalogStale() {
		t.Fatal("unloaded catalog must be stale")
	}
	if _, err := p.ListModelsWithCredential(t.Context(), credential); err != nil || p.ModelCatalogStale() {
		t.Fatalf("fresh catalog err=%v", err)
	}
	revision := p.ModelCatalogRevision()
	now = now.Add(catalogFreshness - time.Second)
	if _, err := p.ListModelsWithCredential(t.Context(), credential); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if !p.ModelCatalogStale() {
		t.Fatal("reading cache renewed its original deadline")
	}
	status.Store(http.StatusNotModified)
	if _, err := p.ListModelsWithCredential(t.Context(), credential); err != nil || p.ModelCatalogStale() || p.ModelCatalogRevision() != revision {
		t.Fatalf("304 must renew freshness without changing records: err=%v", err)
	}
	now = now.Add(catalogFreshness)
	status.Store(http.StatusServiceUnavailable)
	models, err := p.ListModelsWithCredential(t.Context(), credential)
	if err == nil || len(models) != 1 || models[0].ID != "gpt-6-astra" || p.ModelCatalogStale() {
		t.Fatalf("same-account outage fallback=%+v err=%v", models, err)
	}
	cached, ok := p.loadCatalogCache("acct", auth.AccountIdentity(credential))
	if !ok || !cached.FetchedAt.Equal(now.Add(-catalogFreshness)) {
		t.Fatal("failed discovery renewed the disk cache")
	}
	now = now.Add(catalogRetryDelay)
	if !p.ModelCatalogStale() {
		t.Fatal("failed discovery did not become retryable")
	}
	credential.AccountID = "another-account"
	models, err = p.ListModelsWithCredential(t.Context(), credential)
	if err == nil || len(models) != 0 || p.ModelCatalogRevision() == revision {
		t.Fatalf("failed account switch retained prior inventory: models=%+v err=%v", models, err)
	}
}

func TestCatalogAccountSwitchInvalidatesBeforeFetch(t *testing.T) {
	var p *Provider
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("chatgpt-account-id") == "first-account" {
			fmt.Fprint(w, `{"models":[{"slug":"first-account-model","visibility":"list"}]}`)
			return
		}
		if p.DefaultModel().ID == "first-account-model" || !p.ModelCatalogStale() {
			t.Error("account switch exposed prior inventory while discovery was pending")
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	p = New(Config{BaseURL: server.URL, HTTPClient: server.Client(), CacheRoot: t.TempDir()})
	credential := auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "first-account"}
	if _, err := p.ListModelsWithCredential(t.Context(), credential); err != nil {
		t.Fatal(err)
	}
	if p.DefaultModel().ID != "first-account-model" {
		t.Fatal("initial catalog was not retained")
	}
	credential.AccountID = "second-account"
	models, err := p.ListModelsWithCredential(t.Context(), credential)
	if err == nil || len(models) != 0 || p.DefaultModel().ID == "first-account-model" {
		t.Fatalf("failed switch retained another account's catalog: models=%+v err=%v", models, err)
	}
}

func TestCatalogEarlierAccountRequestCannotRestoreInventory(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{}, 1)
	defer close(release)
	var firstCalls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		account := r.Header.Get("chatgpt-account-id")
		if account == "first" && firstCalls.Add(1) == 2 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"models":[{"slug":"%s-model","visibility":"list"}]}`, account)))}, nil
	})}
	p := New(Config{HTTPClient: client, CacheRoot: t.TempDir()})
	first := auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "first"}
	if _, err := p.ListModelsWithCredential(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := p.RefreshModelsWithCredential(t.Context(), first)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first account refresh did not start")
	}
	second := first
	second.AccountID = "second"
	if _, err := p.ListModelsWithCredential(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p.DefaultModel().ID != "second-model" {
		t.Fatalf("late earlier-account request restored catalog: default=%s", p.DefaultModel().ID)
	}
}
