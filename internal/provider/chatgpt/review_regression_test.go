package chatgpt

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
)

func TestDeviceIntervalAcceptsStringOrNumber(t *testing.T) {
	for _, raw := range []string{`{"device_auth_id":"d","user_code":"u","interval":5}`, `{"device_auth_id":"d","user_code":"u","interval":"5"}`} {
		var response deviceCodeResponse
		if err := json.Unmarshal([]byte(raw), &response); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if response.Interval != 5 {
			t.Fatalf("interval=%d", response.Interval)
		}
	}
}

func TestRedirectPolicyRequiresExactOriginAndNoUserinfo(t *testing.T) {
	client := redirectSafeClient(&http.Client{})
	origin, _ := http.NewRequest(http.MethodGet, "https://chatgpt.com/start", nil)
	for _, target := range []string{
		"http://chatgpt.com/next",
		"https://other.example/next",
		"https://user@chatgpt.com/next",
		"https://chatgpt.com:443/next",
	} {
		req, _ := http.NewRequest(http.MethodGet, target, nil)
		if err := client.CheckRedirect(req, []*http.Request{origin}); err == nil {
			t.Fatalf("redirect %s accepted", target)
		}
	}
	same, _ := http.NewRequest(http.MethodGet, "https://chatgpt.com/next", nil)
	if err := client.CheckRedirect(same, []*http.Request{origin}); err != nil {
		t.Fatalf("same-origin redirect rejected: %v", err)
	}
	userinfo, _ := http.NewRequest(http.MethodGet, "https://user@chatgpt.com/next", nil)
	if _, err := client.Do(userinfo); err == nil {
		t.Fatal("initial request URL userinfo accepted")
	}
}

func TestForcedRefreshUsesNewerStoredCredential(t *testing.T) {
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshes.Add(1)
		http.Error(w, "unexpected", http.StatusBadRequest)
	}))
	defer server.Close()
	store := auth.NewMemoryStoreForTest()
	newer := auth.Credential{Type: auth.CredentialOAuth, Access: "new-access", Refresh: "new-refresh", AccountID: "acct"}
	if err := store.Put(ProviderID, newer); err != nil {
		t.Fatal(err)
	}
	p := New(Config{AuthBaseURL: server.URL, HTTPClient: server.Client(), Store: store})
	got, err := p.resolve(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "old-access", Refresh: "old-refresh", AccountID: "acct"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Access != newer.Access || refreshes.Load() != 0 {
		t.Fatalf("got=%+v refreshes=%d", got, refreshes.Load())
	}
}

func TestCredentialFromTokensUsesIDTokenClaimsWithoutPersistingToken(t *testing.T) {
	idToken := testJWT(t, map[string]any{
		"chatgpt_account_id": "id-account",
		"chatgpt_plan_type":  "team",
	})
	cred, err := credentialFromTokens(tokenResponse{AccessToken: "opaque-access", RefreshToken: "refresh", IDToken: idToken}, auth.Credential{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if cred.AccountID != "id-account" || cred.Extra["plan_type"] != "team" {
		t.Fatalf("credential metadata=%+v", cred)
	}
	encoded, _ := json.Marshal(cred)
	if strings.Contains(string(encoded), idToken) {
		t.Fatal("raw id_token was persisted")
	}
}

func TestRefreshFailureClassificationIsSecretFree(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		permanent bool
	}{
		{name: "invalid grant", status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"refresh-secret"}`, permanent: true},
		{name: "server failure", status: http.StatusServiceUnavailable, body: `refresh-secret`, permanent: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			p := New(Config{AuthBaseURL: server.URL, HTTPClient: server.Client()})
			_, err := p.refresh(context.Background(), auth.Credential{Type: auth.CredentialOAuth, Access: "access", Refresh: "refresh-secret", AccountID: "acct"})
			if err == nil || strings.Contains(err.Error(), "refresh-secret") {
				t.Fatalf("unsafe error=%v", err)
			}
			if errors.Is(err, ErrLoginRequired) != tc.permanent || errors.Is(err, ErrRefreshFailed) == tc.permanent {
				t.Fatalf("classification=%v", err)
			}
		})
	}
}

func TestCatalogCacheMetadataIsolationAndValidation(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	p := New(Config{BaseURL: "https://one.example/backend-api", CacheRoot: root, ClientVersion: "v1", Now: func() time.Time { return now }})
	entry := catalogCache{Version: catalogCacheVersion, BackendOrigin: p.backendOrigin(), AccountID: "acct", FetchedAt: now, ClientVersion: "v1", Models: []modelRecord{{Slug: "m", Visibility: "list"}}}
	if err := p.saveCatalogCache("acct", entry); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.loadCatalogCache("acct"); !ok {
		t.Fatal("valid cache rejected")
	}
	otherOrigin := New(Config{BaseURL: "https://two.example/backend-api", CacheRoot: root, ClientVersion: "v1", Now: func() time.Time { return now }})
	if p.catalogCachePath("acct") == otherOrigin.catalogCachePath("acct") {
		t.Fatal("backend origins share a cache key")
	}
	if _, ok := p.loadCatalogCache("other"); ok {
		t.Fatal("cache accepted for another account")
	}

	path := p.catalogCachePath("acct")
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "malformed", data: []byte(`{`)},
		{name: "oversized", data: make([]byte, maxCatalogBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, ok := p.loadCatalogCache("acct"); ok {
				t.Fatal("invalid cache accepted")
			}
		})
	}
	for _, mutate := range []func(*catalogCache){
		func(c *catalogCache) { c.Version++ },
		func(c *catalogCache) { c.ClientVersion = "old" },
		func(c *catalogCache) { c.AccountID = "other" },
		func(c *catalogCache) { c.FetchedAt = time.Time{} },
	} {
		bad := entry
		mutate(&bad)
		data, _ := json.Marshal(bad)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := p.loadCatalogCache("acct"); ok {
			t.Fatalf("invalid metadata accepted: %+v", bad)
		}
	}
	if filepath.Base(path) == "acct.json" {
		t.Fatal("account ID exposed in cache path")
	}
}

func TestStaleCatalogCacheTriggersRefresh(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(modelsResponse{Models: []modelRecord{{Slug: "live", Visibility: "list"}}})
	}))
	defer server.Close()
	store := auth.NewMemoryStoreForTest()
	_ = store.Put(ProviderID, auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"})
	now := time.Now().UTC()
	p := New(Config{BaseURL: server.URL, Store: store, CacheRoot: t.TempDir(), HTTPClient: server.Client(), Now: func() time.Time { return now }})
	stale := catalogCache{Version: catalogCacheVersion, BackendOrigin: p.backendOrigin(), AccountID: "acct", FetchedAt: now.Add(-catalogFreshness - time.Second), ClientVersion: p.clientVersion, Models: []modelRecord{{Slug: "cached", Visibility: "list"}}}
	if err := p.saveCatalogCache("acct", stale); err != nil {
		t.Fatal(err)
	}
	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(models) != 1 || models[0].ID != "live" {
		t.Fatalf("stale cache was treated as fresh: calls=%d models=%+v", calls.Load(), models)
	}
}

func TestBrowserLoginCanUsePasteOnlyWhenCallbackPortOccupied(t *testing.T) {
	listener, err := netListenCallbackPort()
	if err != nil {
		t.Skipf("callback port already unavailable: %v", err)
	}
	defer listener.Close()
	// Exercise only the setup/state path: cancellation proves the occupied port
	// no longer causes an immediate failure when a paste callback is available.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := &loginClient{opts: LoginOptions{PasteCallback: func(context.Context) (string, error) { return "", context.Canceled }}, base: "https://auth.example", client: http.DefaultClient, now: time.Now}
	_, err = c.browser(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("paste-only browser flow failed before context handling: %v", err)
	}
}

func netListenCallbackPort() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:1455")
}
