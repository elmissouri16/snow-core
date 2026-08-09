package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/auth"
)

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
	status, err := Login(context.Background(), LoginOptions{Method: LoginBrowser, Store: store, AuthBaseURL: server.URL, HTTPClient: server.Client(), OpenBrowser: func(_ context.Context, target string) error {
		u, _ := url.Parse(target)
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

func TestRefreshRotatesOnceAcrossConcurrentResolvers(t *testing.T) {
	var calls atomic.Int32
	now := time.Now()
	access := testJWT(t, map[string]any{"exp": float64(now.Add(time.Hour).Unix()), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct"}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["refresh_token"] != "old" {
			t.Errorf("refresh=%q", body["refresh_token"])
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: access, RefreshToken: "rotated", ExpiresIn: 3600})
	}))
	defer server.Close()
	store := auth.NewMemoryStoreForTest()
	_ = store.Put(ProviderID, auth.Credential{Type: auth.CredentialOAuth, Access: "opaque", Refresh: "old", Expires: now.Add(-time.Minute).Unix(), AccountID: "acct"})
	p := New(Config{Store: store, AuthBaseURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := p.Resolve(context.Background(), auth.Credential{}); errs <- err }()
	}
	for i := 0; i < 2; i++ {
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
