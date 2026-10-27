package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
)

// A runtime that started on the placeholder model (no catalog before login)
// must land on the configured model once discovery works again, instead of
// failing every prompt until a manual /model selection.
func TestRefreshRecoversPlaceholderModel(t *testing.T) {
	for _, tc := range []struct{ name, saved, want string }{
		{"configured model available", "saved-model", "saved-model"},
		{"configured model withdrawn", "withdrawn-model", "first-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("SNOW_HOME", home)
			var catalogReady atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/models") {
					t.Errorf("unexpected provider request: %s", r.URL.Path)
				}
				if !catalogReady.Load() {
					_, _ = w.Write([]byte(`{"models":[]}`))
					return
				}
				fmt.Fprint(w, `{"models":[{"slug":"first-model","visibility":"list"},{"slug":"saved-model","visibility":"list"}]}`)
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.DefaultProvider, cfg.DefaultModel, cfg.Thinking = "chatgpt", tc.saved, "off"
			configPath := filepath.Join(home, "config.json")
			if err := config.Save(configPath, cfg); err != nil {
				t.Fatal(err)
			}
			authPath := filepath.Join(home, "auth.json")
			store, err := auth.NewFileStore(authPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Put("chatgpt", auth.Credential{Provider: "chatgpt", Type: auth.CredentialOAuth, Access: "test-token", AccountID: "workspace"}); err != nil {
				t.Fatal(err)
			}
			a, err := New(t.Context(), Options{CWD: t.TempDir(), ConfigPath: configPath, AuthPath: authPath, BaseURL: server.URL, NoSession: true, Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.Close() })
			if a.Agent.Model().ID != placeholderModelID {
				t.Fatalf("startup model = %q, want placeholder", a.Agent.Model().ID)
			}
			catalogReady.Store(true)
			if err := a.RefreshProviderModels(t.Context(), "chatgpt"); err != nil {
				t.Fatalf("refresh: %v", err)
			}
			if a.Agent.Model().ID != tc.want || a.Model.ID != tc.want {
				t.Fatalf("recovered model agent=%q app=%q, want %q", a.Agent.Model().ID, a.Model.ID, tc.want)
			}
		})
	}
}
