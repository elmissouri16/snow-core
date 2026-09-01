package app

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRPCThemeCatalogAndSettingsUpdateAreCanonicalAndAtomic(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "themes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "themes", "ocean.yaml"), []byte("version: 1\nname: ocean\nextends: frost\ncolors:\n  accent: {dark: '#abcdef'}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.ConfigPath = filepath.Join(home, "config.json")
	if err := config.Save(a.ConfigPath, a.PersistedCfg); err != nil {
		t.Fatal(err)
	}
	catalog, err := a.RPCThemes()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Themes) != 5 || catalog.Themes[0].DisplayName != "Snow" || catalog.Themes[4].Scope != "global" {
		t.Fatalf("catalog = %+v", catalog)
	}
	ocean := "ocean"
	snapshot, err := a.UpdateRPCSettings(SettingsUpdate{Theme: &ocean})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Theme != ocean || a.Cfg.TUI.Theme != ocean {
		t.Fatalf("snapshot=%+v cfg=%q", snapshot, a.Cfg.TUI.Theme)
	}
	persisted, err := config.Load(a.ConfigPath)
	if err != nil || persisted.TUI.Theme != ocean {
		t.Fatalf("persisted=%q err=%v", persisted.TUI.Theme, err)
	}

	before := a.Cfg.TUI.Theme
	a.ConfigPath = home // a directory cannot be atomically replaced as a config file.
	frost := "frost"
	if _, err := a.UpdateRPCSettings(SettingsUpdate{Theme: &frost}); err == nil {
		t.Fatal("theme persistence failure was accepted")
	}
	if a.Cfg.TUI.Theme != before {
		t.Fatalf("failed update changed runtime theme to %q", a.Cfg.TUI.Theme)
	}
}

func TestRPCKeybindingsSerializeBindingListsAsArrays(t *testing.T) {
	view := rpcKeybindings(config.KeybindingLayers{Actions: []string{"submit"}})
	payload, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Actions []map[string]any `json:"actions"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(wire.Actions))
	}
	for _, field := range []string{"global", "project", "effective"} {
		if _, ok := wire.Actions[0][field].([]any); !ok {
			t.Fatalf("%s wire value = %#v, want JSON array", field, wire.Actions[0][field])
		}
	}
}

func TestRPCKeybindingsLayeredUpdateAndProjectTrust(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	a := newRuntimeControlsTestApp(t)
	a.ProjectInputRoot = project
	a.ProjectAllowed = true

	global, err := a.UpdateRPCKeybindings(protocol.RPCKeybindingsUpdateParams{
		Scope: "global", Bindings: map[string][]string{"models": {"alt+z"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	models := global.Actions[slices.IndexFunc(global.Actions, func(action protocol.RPCKeybindingAction) bool { return action.Name == "models" })]
	if models.Source != "global" || !slices.Equal(models.Effective, []string{"alt+z"}) {
		t.Fatalf("global models = %+v", models)
	}
	projectView, err := a.UpdateRPCKeybindings(protocol.RPCKeybindingsUpdateParams{
		Scope: "project", Bindings: map[string][]string{"models": {"ctrl+m"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	models = projectView.Actions[slices.IndexFunc(projectView.Actions, func(action protocol.RPCKeybindingAction) bool { return action.Name == "models" })]
	if models.Source != "project" || !slices.Equal(models.Effective, []string{"ctrl+m"}) {
		t.Fatalf("project models = %+v", models)
	}
	if _, err := a.UpdateRPCKeybindings(protocol.RPCKeybindingsUpdateParams{
		Scope: "global", Bindings: map[string][]string{"submit": {"ctrl+t"}},
	}); err == nil {
		t.Fatal("collision accepted")
	}
	if _, err := a.UpdateRPCKeybindings(protocol.RPCKeybindingsUpdateParams{
		Scope: "global", Bindings: map[string][]string{"abort": {"alt+x"}},
	}); err != nil {
		t.Fatal(err)
	}
	view, err := a.RPCKeybindings()
	if err != nil {
		t.Fatal(err)
	}
	abort := view.Actions[slices.IndexFunc(view.Actions, func(action protocol.RPCKeybindingAction) bool { return action.Name == "abort" })]
	for _, emergency := range []string{"alt+x", "ctrl+c", "esc"} {
		if !slices.Contains(abort.Effective, emergency) {
			t.Fatalf("abort = %v, missing %q", abort.Effective, emergency)
		}
	}

	a.ProjectAllowed = false
	if _, err := a.UpdateRPCKeybindings(protocol.RPCKeybindingsUpdateParams{Scope: "project", Reset: []string{"models"}}); err == nil {
		t.Fatal("untrusted project update accepted")
	}
}
