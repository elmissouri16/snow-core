package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/snow-core/snow/internal/auth"
)

func TestNoSessionStillUsesConfiguredAuthStore(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{{"slug": "m", "display_name": "M", "visibility": "list", "priority": 1, "context_window": 1000}}})
	}))
	defer server.Close()
	authPath := filepath.Join(t.TempDir(), "auth.json")
	store, _ := auth.NewFileStore(authPath)
	_ = store.Put("chatgpt", auth.Credential{Type: auth.CredentialOAuth, Access: "access", Refresh: "refresh", AccountID: "acct"})
	a, err := New(context.Background(), Options{Provider: "chatgpt", NoSession: true, CWD: t.TempDir(), AuthPath: authPath, BaseURL: server.URL, Permission: "deny"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	cred, ok := a.Auth.Get("chatgpt")
	if !ok || cred.Access != "access" {
		t.Fatalf("credential not loaded: %+v ok=%v", cred, ok)
	}
	resolved, err := a.Provider.Resolve(context.Background(), cred)
	if err != nil || resolved.Access != "access" {
		t.Fatalf("resolve=%+v err=%v", resolved, err)
	}
}
