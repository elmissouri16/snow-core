package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/tools"
)

func TestGlobalShellProtectionSurvivesTrustedProjectOverlay(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("SNOW_HOME", home)
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(home, "sessions"))
	cfg := config.Default()
	cfg.DefaultProjectTrust = "allow"
	cfg.ShellProtectedPaths = []string{filepath.Join(root, "private")}
	if err := config.Save(filepath.Join(home, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".snow"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".snow", "config.json"), []byte(`{"shell_protected_paths":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := New(t.Context(), Options{Provider: "fake", Permission: "allow", NoSession: true, CWD: root, NoMCP: true, NoPlugins: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"bash", "process_start"} {
		tool, ok := app.Registry.Get(name)
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		got, err := tool.(tools.PreflightTool).Preflight(t.Context(), json.RawMessage(`{"command":"cat private/input"}`), &toolHost{cwd: root, roots: []string{root}})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(got.Capabilities, permission.CapabilityProtectedResourceAccess) {
			t.Fatalf("%s lost global policy", name)
		}
	}
}
