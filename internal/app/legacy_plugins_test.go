package app

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
)

func TestAppNeverStartsLegacyPluginDeclarations(t *testing.T) {
	for _, scope := range []string{"global", "trusted-project"} {
		t.Run(scope, func(t *testing.T) {
			home, cwd := t.TempDir(), t.TempDir()
			t.Setenv("SNOW_HOME", home)
			marker := filepath.Join(cwd, "plugin-started")
			configPath := filepath.Join(home, "config.json")
			if err := os.WriteFile(configPath, []byte(`{"default_project_trust":"allow"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			declarationPath := configPath
			if scope == "trusted-project" {
				declarationPath = filepath.Join(cwd, ".snow", "config.json")
				if err := os.MkdirAll(filepath.Dir(declarationPath), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			declaration, err := json.Marshal(map[string]any{
				"default_project_trust": "allow",
				"plugins": []any{map[string]any{
					"id": "legacy", "enabled": true,
					"command": []string{"sh", "-c", `printf started > "$1"`, "legacy", marker},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(declarationPath, declaration, 0o600); err != nil {
				t.Fatal(err)
			}
			a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: cwd, ConfigPath: configPath})
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("legacy command ran or marker could not be checked: %v", err)
			}
			if diagnostics := a.PluginManager.Diagnostics(); len(diagnostics) != 0 {
				t.Fatalf("legacy declarations reached plugin manager: %+v", diagnostics)
			}
		})
	}
}

func TestNoPluginsSkipsSuppliedGoPlugin(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	closed := 0
	a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, NoPlugins: true, Permission: "deny", CWD: t.TempDir(), GoPlugins: []publicplugin.Plugin{appCloseTrackingPlugin{closed: &closed}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if closed != 0 {
		t.Fatalf("disabled Go plugin was loaded: close count %d", closed)
	}
}
