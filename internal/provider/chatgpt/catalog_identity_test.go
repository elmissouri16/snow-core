package chatgpt

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
)

func catalogUserCredential(t *testing.T, subject string, expires time.Time) auth.Credential {
	t.Helper()
	return auth.Credential{Type: auth.CredentialOAuth, Access: testJWT(t, map[string]any{
		"sub": subject, "exp": expires.Unix(),
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "shared-workspace"},
	}), AccountID: "shared-workspace"}
}

func TestCatalogSeparatesUsersInSameWorkspace(t *testing.T) {
	first := catalogUserCredential(t, "first-user", time.Now().Add(time.Hour))
	second := catalogUserCredential(t, "second-user", time.Now().Add(time.Hour))
	var calls atomic.Int32
	var p *Provider
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.Header.Get("chatgpt-account-id") != "shared-workspace" {
			t.Error("identity hash replaced upstream workspace header")
		}
		user := ""
		switch r.Header.Get("Authorization") {
		case "Bearer " + first.Access:
			user = "first"
		case "Bearer " + second.Access:
			user = "second"
		default:
			t.Error("unexpected credential")
		}
		if call == 2 && (p.DefaultModel().ID == "first-model" || !p.ModelCatalogStale()) {
			t.Error("prior user catalog retained during same-workspace switch")
		}
		wantETag := ""
		if call > 2 {
			wantETag = user + "-etag"
		}
		if r.Header.Get("If-None-Match") != wantETag {
			t.Errorf("call %d: conditional catalog request reused another user's ETag", call)
		}
		w.Header().Set("ETag", user+"-etag")
		fmt.Fprintf(w, `{"models":[{"slug":"%s-model","visibility":"list"}]}`, user)
	}))
	defer server.Close()
	cfg := Config{BaseURL: server.URL, HTTPClient: server.Client(), CacheRoot: t.TempDir()}
	p = New(cfg)
	for _, credential := range []auth.Credential{first, second} {
		if _, err := p.ListModelsWithCredential(t.Context(), credential); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 || p.DefaultModel().ID != "second-model" {
		t.Fatalf("same-workspace users shared fresh inventory: calls=%d default=%s", calls.Load(), p.DefaultModel().ID)
	}
	if p.catalogCachePath(auth.AccountIdentity(first)) == p.catalogCachePath(auth.AccountIdentity(second)) {
		t.Fatal("same-workspace users share disk cache path")
	}
	for _, credential := range []auth.Credential{second, first} {
		if _, err := p.RefreshModelsWithCredential(t.Context(), credential); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 4 {
		t.Fatalf("forced identity-scoped refreshes=%d, want 4", calls.Load())
	}
	// A cold provider must still load each user's own fresh cache, and token
	// rotation must preserve the stable workspace/user cache identity.
	cold := New(cfg)
	models, err := cold.ListModelsWithCredential(t.Context(), second)
	if err != nil || len(models) != 1 || models[0].ID != "second-model" || calls.Load() != 4 {
		t.Fatalf("cold second-user cache: models=%+v calls=%d err=%v", models, calls.Load(), err)
	}
	rotated := catalogUserCredential(t, "first-user", time.Now().Add(2*time.Hour))
	models, err = cold.ListModelsWithCredential(t.Context(), rotated)
	if err != nil || len(models) != 1 || models[0].ID != "first-model" || calls.Load() != 4 {
		t.Fatalf("rotated first-user cache: models=%+v calls=%d err=%v", models, calls.Load(), err)
	}
}

func TestCatalogLegacyWorkspaceCacheCannotEstablishUserEntitlement(t *testing.T) {
	for _, placement := range []string{"workspace-only path", "identity path"} {
		t.Run(placement, func(t *testing.T) {
			credential := catalogUserCredential(t, "known-user", time.Now().Add(time.Hour))
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("If-None-Match") != "" {
					t.Error("legacy workspace cache supplied a user-scoped ETag")
				}
				fmt.Fprint(w, `{"models":[{"slug":"verified-user-model","visibility":"list"}]}`)
			}))
			defer server.Close()
			p := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), CacheRoot: t.TempDir()})
			legacy := catalogCache{Version: 1, BackendOrigin: p.backendOrigin(), AccountID: "shared-workspace",
				FetchedAt: time.Now().UTC(), ETag: "legacy-etag", ClientVersion: p.clientVersion,
				Models: []modelRecord{{Slug: "unverified-workspace-model", Visibility: "list"}}}
			data, err := json.Marshal(legacy)
			if err != nil {
				t.Fatal(err)
			}
			key := "shared-workspace"
			if placement == "identity path" {
				key = auth.AccountIdentity(credential)
			}
			if err := os.WriteFile(p.catalogCachePath(key), data, 0o600); err != nil {
				t.Fatal(err)
			}
			models, err := p.ListModelsWithCredential(t.Context(), credential)
			if err != nil || len(models) != 1 || models[0].ID != "verified-user-model" || calls.Load() != 1 {
				t.Fatalf("legacy catalog reused: models=%+v calls=%d err=%v", models, calls.Load(), err)
			}
		})
	}
}

func TestCatalogRefreshCannotChangeUserIdentity(t *testing.T) {
	first := catalogUserCredential(t, "first-user", time.Now().Add(time.Hour))
	second := catalogUserCredential(t, "second-user", time.Now().Add(time.Hour))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	p := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), CacheRoot: t.TempDir()})
	p.SetAuthRefresh(func(context.Context, auth.Credential) (auth.Credential, error) {
		return second, nil
	})
	models, err := p.ListModelsWithCredential(t.Context(), first)
	if err == nil || len(models) != 0 || calls.Load() != 1 {
		t.Fatalf("discovery retried under another user: models=%+v calls=%d err=%v", models, calls.Load(), err)
	}
	if _, ok := p.loadCatalogCache("shared-workspace", auth.AccountIdentity(first)); ok {
		t.Fatal("changed identity populated the original user's disk cache")
	}
}
