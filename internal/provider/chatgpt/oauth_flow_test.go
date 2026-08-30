package chatgpt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
)

func TestDevicePollIntervalClampsBeforeDurationConversion(t *testing.T) {
	if got := devicePollInterval(flexibleSeconds(math.MaxInt64)); got != maxDevicePollInterval {
		t.Fatalf("huge interval = %s", got)
	}
	if got := devicePollInterval(0); got != 5*time.Second {
		t.Fatalf("default interval = %s", got)
	}
	if got := devicePollInterval(7); got != 7*time.Second {
		t.Fatalf("normal interval = %s", got)
	}
}

func TestPKCEChallengeAndEntropy(t *testing.T) {
	verifier, err := randomURLString(64)
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) < 43 || len(verifier) > 128 || strings.Contains(verifier, "=") {
		t.Fatalf("invalid verifier shape: len=%d", len(verifier))
	}
	if got := pkceChallenge("test"); got != "n4bQgYhMfWWaL-qgxVrQFaO_TxsrC4Is0V1sFbDwCgg" {
		t.Fatalf("challenge=%q", got)
	}
}

func TestBrowserLoginValidatesStateAndPersists(t *testing.T) {
	access := testJWT(t, map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct"}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			t.Errorf("path=%s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code_verifier") == "" {
			t.Errorf("form=%v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: access, RefreshToken: "refresh", ExpiresIn: 3600})
	}))
	defer server.Close()
	store := auth.NewMemoryStoreForTest()
	status, err := Login(context.Background(), LoginOptions{Method: LoginBrowser, Store: store, AuthBaseURL: server.URL, HTTPClient: server.Client(), AllowedWorkspaceIDs: []string{"acct"}, OpenBrowser: func(_ context.Context, target string) error {
		u, _ := url.Parse(target)
		if got := u.Query().Get("scope"); got != "openid profile email offline_access api.connectors.read api.connectors.invoke" {
			t.Errorf("scope=%q", got)
		}
		if got := u.Query().Get("allowed_workspace_id"); got != "acct" {
			t.Errorf("allowed_workspace_id=%q", got)
		}
		state := u.Query().Get("state")
		go func() { _, _ = http.Get("http://127.0.0.1:1455/auth/callback?code=ok&state=" + url.QueryEscape(state)) }()
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Authenticated || status.AccountID != "acct" {
		t.Fatalf("status=%+v", status)
	}
	if got, ok := store.Get(ProviderID); !ok || got.Refresh != "refresh" {
		t.Fatalf("stored=%+v ok=%v", got, ok)
	}
	if _, ok := parseCallbackURL("/auth/callback?code=x&state=wrong", "right"); ok {
		t.Fatal("wrong state accepted")
	}
}

func TestAllowedWorkspaceRejectsDifferentOAuthAccount(t *testing.T) {
	cred := auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "actual"}
	if err := ensureAllowedWorkspace(cred, []string{"wanted"}); err == nil || !strings.Contains(err.Error(), "not saved") {
		t.Fatalf("workspace mismatch error=%v", err)
	}
	if err := ensureAllowedWorkspace(cred, []string{"actual", "actual", ""}); err != nil {
		t.Fatalf("matching workspace rejected: %v", err)
	}
}

type coordinatedMemoryStore struct{ *auth.MemoryStore }

func (s *coordinatedMemoryStore) WithRefreshLock(_ string, fn func() error) error { return fn() }

func TestCoordinatedRefreshDoesNotResurrectLogout(t *testing.T) {
	now := time.Now()
	store := &coordinatedMemoryStore{MemoryStore: auth.NewMemoryStoreForTest()}
	_ = store.Put(ProviderID, auth.Credential{Type: auth.CredentialOAuth, Access: "old", Refresh: "refresh", Expires: now.Add(-time.Minute).Unix(), AccountID: "acct"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := store.Delete(ProviderID); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "new", RefreshToken: "new-refresh", ExpiresIn: 3600})
	}))
	defer server.Close()
	provider := New(Config{Store: store, AuthBaseURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if _, err := provider.Resolve(context.Background(), auth.Credential{}); !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("Resolve error=%v, want ErrLoginRequired", err)
	}
	if _, ok := store.Get(ProviderID); ok {
		t.Fatal("in-flight refresh resurrected deleted credential")
	}
}

func TestRefreshRotatesOnceAcrossConcurrentResolvers(t *testing.T) {
	var calls atomic.Int32
	now := time.Now()
	access := testJWT(t, map[string]any{"exp": float64(now.Add(time.Hour).Unix()), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct"}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("content-type=%q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("refresh_token") != "old" || r.Form.Get("client_id") != OAuthClientID {
			t.Errorf("refresh form=%v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: access, RefreshToken: "rotated", ExpiresIn: 3600})
	}))
	defer server.Close()
	store := auth.NewMemoryStoreForTest()
	_ = store.Put(ProviderID, auth.Credential{Type: auth.CredentialOAuth, Access: "opaque", Refresh: "old", Expires: now.Add(-time.Minute).Unix(), AccountID: "acct"})
	p := New(Config{Store: store, AuthBaseURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }})
	errs := make(chan error, 2)
	for range 2 {
		go func() { _, err := p.Resolve(context.Background(), auth.Credential{}); errs <- err }()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls=%d", calls.Load())
	}
	got, _ := store.Get(ProviderID)
	if got.Refresh != "rotated" {
		t.Fatalf("stored=%+v", got)
	}
}

func TestBrowserLoginTimeoutAndRefreshErrorRedaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error_description":"echo super-secret-refresh"}`)
	}))
	defer server.Close()
	_, err := Login(context.Background(), LoginOptions{Method: LoginBrowser, Store: auth.NewMemoryStoreForTest(), AuthBaseURL: server.URL, HTTPClient: server.Client(), BrowserTimeout: 20 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout err=%v", err)
	}
	store := auth.NewMemoryStoreForTest()
	now := time.Now()
	_ = store.Put(ProviderID, auth.Credential{Type: auth.CredentialOAuth, Access: "old", Refresh: "super-secret-refresh", Expires: now.Add(-time.Minute).Unix(), AccountID: "acct"})
	p := New(Config{Store: store, AuthBaseURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }})
	_, err = p.Resolve(context.Background(), auth.Credential{})
	if err == nil || strings.Contains(err.Error(), "super-secret-refresh") {
		t.Fatalf("refresh error leaked secret: %v", err)
	}
}

func TestDevicePendingHonorsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/usercode") {
			fmt.Fprint(w, `{"device_auth_id":"d","user_code":"CODE","interval":1}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	_, err := Login(context.Background(), LoginOptions{Method: LoginDevice, Store: auth.NewMemoryStoreForTest(), AuthBaseURL: server.URL, HTTPClient: server.Client(), DeviceTimeout: 20 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("device timeout err=%v", err)
	}
}

func TestDeviceLoginImmediateAuthorization(t *testing.T) {
	access := testJWT(t, map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-device"}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			fmt.Fprint(w, `{"device_auth_id":"d","user_code":"CODE","interval":1}`)
		case "/api/accounts/deviceauth/token":
			fmt.Fprint(w, `{"authorization_code":"a","code_verifier":"v"}`)
		case "/oauth/token":
			_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: access, RefreshToken: "r", ExpiresIn: 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	store := auth.NewMemoryStoreForTest()
	status, err := Login(ctx, LoginOptions{Method: LoginDevice, Store: store, AuthBaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if status.AccountID != "acct-device" {
		t.Fatalf("status=%+v", status)
	}
}
