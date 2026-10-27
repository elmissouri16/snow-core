package app

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestStartupWithoutCatalogPreservesSavedThinking(t *testing.T) {
	for _, scenario := range []string{"logged out", "empty catalog", "failed catalog"} {
		for _, projectSelection := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/project=%t", scenario, projectSelection), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("SNOW_HOME", home)
				project := t.TempDir()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !strings.HasSuffix(r.URL.Path, "/models") {
						t.Errorf("unexpected provider request: %s", r.URL.Path)
					}
					if scenario == "failed catalog" {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					_, _ = w.Write([]byte(`{"models":[]}`))
				}))
				defer server.Close()
				cfg := config.Default()
				cfg.DefaultProvider = "chatgpt"
				cfg.DefaultModel = "saved-model"
				cfg.Thinking = "high"
				if projectSelection {
					var err error
					cfg, err = config.WithProjectSelection(cfg, project, config.ProjectSelection{
						Provider: "chatgpt", Model: "saved-model", Thinking: "high",
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				configPath := filepath.Join(home, "config.json")
				if err := config.Save(configPath, cfg); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				authPath := filepath.Join(home, "auth.json")
				if scenario != "logged out" {
					store, err := auth.NewFileStore(authPath)
					if err != nil {
						t.Fatal(err)
					}
					if err := store.Put("chatgpt", auth.Credential{Provider: "chatgpt", Type: auth.CredentialOAuth, Access: "test-token", AccountID: "workspace"}); err != nil {
						t.Fatal(err)
					}
				}
				opts := Options{CWD: project, ConfigPath: configPath, AuthPath: authPath,
					BaseURL: server.URL, NoSession: true, Permission: "deny",
					NoPlugins: true, NoMCP: true, NoSkills: true}
				a, err := New(t.Context(), opts)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = a.Close() })
				wantModel := "default"
				if scenario == "logged out" {
					wantModel = "saved-model"
				}
				if len(a.Models) != 0 || a.Agent.Model().ID != wantModel {
					t.Fatalf("model=%q catalog=%+v, want %q without a catalog", a.Agent.Model().ID, a.Models, wantModel)
				}
				if a.Agent.Thinking() != protocol.ThinkingOff || a.Cfg.Thinking != "off" || a.PersistedCfg.Thinking != "high" {
					t.Fatalf("thinking: agent=%q runtime=%q persisted=%q", a.Agent.Thinking(), a.Cfg.Thinking, a.PersistedCfg.Thinking)
				}
				found := false
				for _, diagnostic := range a.Diagnostics {
					if strings.Contains(diagnostic.Message, "using thinking off for this run") && strings.Contains(diagnostic.Message, "/model") {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing recovery diagnostic: %+v", a.Diagnostics)
				}
				opts.Thinking = "high"
				if explicit, err := New(t.Context(), opts); err == nil {
					_ = explicit.Close()
					t.Fatal("explicit unsupported thinking accepted")
				} else if !strings.Contains(err.Error(), "does not advertise thinking level") {
					t.Fatalf("explicit thinking error: %v", err)
				}
				after, err := os.ReadFile(configPath)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("saved configuration changed: %v", err)
				}
			})
		}
	}
}
