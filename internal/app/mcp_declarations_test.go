package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	internalmcp "github.com/elmissouri16/snow-core/internal/mcp"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/trust"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
)

func TestMergeMCPDeclarationsPreservesWinningScope(t *testing.T) {
	global := map[string]publicmcp.ServerSpec{"shared": {Command: "global"}, "global-only": {Command: "global-only"}}
	project := map[string]publicmcp.ServerSpec{"shared": {Command: "project"}, "project-only": {Command: "project-only"}}
	explicit := []publicmcp.ServerSpec{{ID: "shared", Command: "explicit"}}
	declarations := mergeMCPDeclarations(global, project, explicit, "/canonical/project")
	got := make(map[string]string, len(declarations))
	for _, declaration := range declarations {
		got[declaration.Spec.ID] = declaration.Scope
		if declaration.ProjectIdentity != "/canonical/project" {
			t.Fatalf("project identity = %q", declaration.ProjectIdentity)
		}
	}
	if got["shared"] != "explicit" || got["global-only"] != "global" || got["project-only"] != "project" {
		t.Fatalf("winning scopes = %+v", got)
	}
}

func TestAppStrictNoBootstrapNeverUsesTransportForUnusableCache(t *testing.T) {
	for _, mode := range []string{"missing", "expired", "corrupt", "mismatch"} {
		t.Run(mode, func(t *testing.T) {
			home, project := t.TempDir(), t.TempDir()
			t.Setenv("SNOW_HOME", home)
			var requests atomic.Int32
			server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "strict-app", Version: "1"}, nil)
			server.AddTool(&sdkmcp.Tool{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return &sdkmcp.CallToolResult{}, nil
			})
			handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, &sdkmcp.StreamableHTTPOptions{Stateless: true})
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				handler.ServeHTTP(w, r)
			}))
			defer httpServer.Close()

			seedSpec := publicmcp.ServerSpec{ID: "strict", URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy}
			declaration := internalmcp.Declaration{Spec: seedSpec, Scope: "project", ProjectIdentity: project}
			manager := internalmcp.NewManager(tools.NewRegistry(), internalmcp.Options{CWD: project, Roots: []string{project}, CacheRoot: home})
			manager.Initialize(context.Background(), []internalmcp.Declaration{declaration})
			if statuses := manager.Statuses(); len(statuses) != 1 || !statuses[0].Cached {
				t.Fatalf("seed statuses = %+v", statuses)
			}
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			cachePath := filepath.Join(home, "cache", "mcp-v1.json")
			switch mode {
			case "missing":
				if err := os.Remove(cachePath); err != nil {
					t.Fatal(err)
				}
			case "expired":
				data, err := os.ReadFile(cachePath)
				if err != nil {
					t.Fatal(err)
				}
				var file map[string]any
				if err := json.Unmarshal(data, &file); err != nil {
					t.Fatal(err)
				}
				for _, raw := range file["servers"].(map[string]any) {
					raw.(map[string]any)["cached_at"] = time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
				}
				data, err = json.Marshal(file)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cachePath, data, 0o600); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(cachePath, []byte(`{"version":`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "mismatch":
				seedSpec.URL += "/different"
			}

			seedSpec.CacheBootstrap = publicmcp.CacheBootstrapExplicit
			configData, err := json.Marshal(map[string]any{"mcp_servers": map[string]publicmcp.ServerSpec{"strict": seedSpec}})
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(home, "config.json")
			if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(project, ".snow"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, ".snow", "config.json"), configData, 0o600); err != nil {
				t.Fatal(err)
			}
			opts := Options{
				Provider: "fake", ConfigPath: configPath, CWD: project, NoSession: true,
				NoPlugins: true, NoSkills: true, Permission: "deny",
			}
			preflight, err := InspectProjectTrust(opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := preflight.Store.Set(project, trust.LevelAllow); err != nil {
				t.Fatal(err)
			}
			requests.Store(0)
			a, err := New(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if requests.Load() != 0 {
				t.Fatalf("strict %s startup made %d transport requests", mode, requests.Load())
			}
		})
	}
}
