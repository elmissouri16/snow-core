package chatgpt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
)

func TestExpiredStoredCredentialRefreshesBeforeValidation(t *testing.T) {
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" {
			t.Errorf("unexpected refresh request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("refresh_token") != "old-refresh" {
			t.Errorf("invalid refresh form: %v", err)
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600})
	}))
	defer server.Close()

	store := auth.NewMemoryStore()
	if err := store.Put(ProviderID, auth.Credential{
		Type: auth.CredentialOAuth, Access: "old-access", Refresh: "old-refresh",
		Expires: time.Now().Add(-time.Hour).Unix(), AccountID: "acct",
	}); err != nil {
		t.Fatal(err)
	}
	service := auth.NewService(store)
	if err := service.Register(NewAuthDriver(New(Config{AuthBaseURL: server.URL, HTTPClient: server.Client()}))); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		resolved, err := service.Resolve(t.Context(), ProviderID)
		if err != nil || resolved.Access != "new-access" || resolved.Refresh != "new-refresh" {
			t.Fatalf("resolved access=%q refresh=%q err=%v", resolved.Access, resolved.Refresh, err)
		}
	}
	stored, _ := store.Get(ProviderID)
	if stored.Access != "new-access" || stored.Refresh != "new-refresh" || refreshes.Load() != 1 {
		t.Fatalf("stored access=%q refresh=%q refreshes=%d", stored.Access, stored.Refresh, refreshes.Load())
	}

	// An explicit expired override must not silently fall back to the store.
	service.SetExplicit(ProviderID, auth.Credential{
		Type: auth.CredentialOAuth, Access: "expired-override", Refresh: "override-refresh",
		Expires: time.Now().Add(-time.Hour).Unix(), AccountID: "acct",
	})
	if _, err := service.Resolve(t.Context(), ProviderID); err == nil {
		t.Fatal("expired explicit override was accepted")
	}
	if refreshes.Load() != 1 {
		t.Fatalf("explicit override triggered %d refreshes", refreshes.Load())
	}
}

func TestLazyAuthDriverDefersProviderForLocalOperations(t *testing.T) {
	var builds atomic.Int32
	driver := NewLazyAuthDriver(func() (*Provider, error) {
		builds.Add(1)
		return New(), nil
	})
	if descriptor := driver.Descriptor(); descriptor.ProviderID != ProviderID {
		t.Fatalf("Descriptor() provider = %q", descriptor.ProviderID)
	}
	if _, err := driver.Inspect(auth.Credential{Type: auth.CredentialOAuth}); err == nil {
		t.Fatal("Inspect accepted an incomplete OAuth credential")
	}
	if _, err := driver.Refresh(context.Background(), auth.Credential{}, auth.RefreshExpiring); err == nil {
		t.Fatal("Refresh accepted a missing refresh token")
	}
	if _, err := driver.Login(context.Background(), auth.LoginRequest{Method: "invalid"}, nil); err == nil {
		t.Fatal("Login accepted an invalid method")
	}
	if builds.Load() != 0 {
		t.Fatalf("local auth operations constructed provider %d times", builds.Load())
	}
	first, err := driver.configuredProvider()
	if err != nil {
		t.Fatal(err)
	}
	second, err := driver.configuredProvider()
	if err != nil {
		t.Fatal(err)
	}
	if first != second || builds.Load() != 1 {
		t.Fatalf("configuredProvider reuse = %v, builds = %d; want true, 1", first == second, builds.Load())
	}
}
