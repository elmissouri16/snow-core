package app

import (
	"context"
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func writePluginConfig(t *testing.T, path string, specs map[string]plugin.JavaScriptSpec) {
	t.Helper()
	raw, err := jsonv2.Marshal(map[string]any{"js_plugins": specs, "default_project_trust": "allow"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func pluginStatusByID(t *testing.T, a *App, id string) protocol.PluginStatus {
	t.Helper()
	statuses, err := a.PluginStatuses()
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if status.ID == id {
			return status
		}
	}
	t.Fatalf("missing %s in %+v", id, statuses)
	return protocol.PluginStatus{}
}

func TestPluginManagementPersistsIndividualStateUntilRestart(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	t.Setenv("SNOW_HOME", home)
	command := `snow.registerCommand({name:"run",description:"run",run(){return "still active";}});`
	first := writeV2Fixture(t, cwd, "first", command, []string{"commands"}, nil)
	second := writeV2Fixture(t, cwd, "second", command, []string{"commands"}, nil)
	writePluginConfig(t, filepath.Join(home, "config.json"), map[string]plugin.JavaScriptSpec{"first": {Path: first}, "second": {Path: second, Disabled: true}})
	opts := Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: cwd}
	a, err := New(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	before := pluginStatusByID(t, a, "first")
	if !before.Enabled || !before.Loaded || !before.CanToggle || before.RestartRequired {
		t.Fatalf("before=%+v", before)
	}
	after, err := a.SetPluginEnabled(t.Context(), "first", false)
	if err != nil || after.Enabled || !after.Loaded || !after.RestartRequired {
		t.Fatalf("after=%+v err=%v", after, err)
	}
	if _, err := a.RunPluginCommand(t.Context(), "first:run", ""); err != nil {
		t.Fatalf("saving a toggle disrupted the running catalog: %v", err)
	}
	if status := pluginStatusByID(t, a, "second"); status.Enabled || status.Loaded || status.RestartRequired {
		t.Fatalf("unrelated plugin changed: %+v", status)
	}
	after, err = a.SetPluginEnabled(t.Context(), "second", true)
	if err != nil || !after.Enabled || after.Loaded || !after.RestartRequired {
		t.Fatalf("enable=%+v err=%v", after, err)
	}
	if _, err := a.RunPluginCommand(t.Context(), "second:run", ""); err == nil {
		t.Fatal("enabled plugin ran before restart")
	}
	// Undoing a pending change immediately clears the restart indication.
	if status, err := a.SetPluginEnabled(t.Context(), "first", true); err != nil || status.RestartRequired {
		t.Fatalf("undo=%+v err=%v", status, err)
	}
	if _, err := a.SetPluginEnabled(t.Context(), "first", false); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	for _, id := range []string{"first", "second"} {
		status := pluginStatusByID(t, restarted, id)
		if status.Enabled != (id == "second") || status.Loaded != status.Enabled || status.RestartRequired {
			t.Fatalf("restart=%+v", status)
		}
	}
}

func TestPluginManagementHonorsEffectiveScope(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "trusted"}[trusted], func(t *testing.T) {
			home, cwd := t.TempDir(), t.TempDir()
			t.Setenv("SNOW_HOME", home)
			global := filepath.Join(home, "config.json")
			writePluginConfig(t, global, map[string]plugin.JavaScriptSpec{"demo": {Path: "missing", Disabled: true}})
			if !trusted {
				if _, err := config.Update(global, func(c *config.Config) error { c.DefaultProjectTrust = "deny"; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			project := filepath.Join(cwd, ".snow", "config.json")
			path := writeJSFixture(t, cwd, "demo", jsEcho)
			writePluginConfig(t, project, map[string]plugin.JavaScriptSpec{"demo": {Path: "demo", Disabled: true}, "project-only": {Path: "missing", Disabled: true}})
			a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, CWD: cwd})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			status := pluginStatusByID(t, a, "demo")
			wantScope := "global"
			if trusted {
				wantScope = "project"
			}
			if status.Scope != wantScope || trusted && status.Path != path {
				t.Fatalf("status=%+v", status)
			}
			_, err = a.SetPluginEnabled(t.Context(), "demo", true)
			if trusted && err != nil || !trusted && err == nil {
				t.Fatalf("enable err=%v trusted=%v", err, trusted)
			}
			cfg, err := config.Load(global)
			if err != nil || !cfg.JavaScriptPlugins["demo"].Disabled {
				t.Fatalf("global shadow modified: %+v %v", cfg.JavaScriptPlugins, err)
			}
			if !trusted {
				if _, err := a.SetPluginEnabled(t.Context(), "project-only", false); err == nil {
					t.Fatal("untrusted project entry was exposed")
				}
			}
		})
	}
}

func TestPluginManagementListingNeverReadsDisabledPackages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	writePluginConfig(t, filepath.Join(home, "config.json"), map[string]plugin.JavaScriptSpec{"broken": {Path: "/does/not/exist", Disabled: true}, "tombstone": {Disabled: true}})
	a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, id := range []string{"broken", "tombstone"} {
		if status := pluginStatusByID(t, a, id); status.Enabled || status.Loaded {
			t.Fatalf("status=%+v", status)
		}
		if _, err := a.SetPluginEnabled(t.Context(), id, true); err == nil {
			t.Fatalf("enabled unreadable %s", id)
		}
		if status, err := a.SetPluginEnabled(t.Context(), id, false); err != nil || status.Enabled {
			t.Fatalf("idempotent disable=%+v %v", status, err)
		}
	}
	if _, err := a.SetPluginEnabled(t.Context(), "missing", false); err == nil {
		t.Fatal("unknown id accepted")
	}
}

func TestPluginManagementNoPluginsAndExplicitOptions(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	t.Setenv("SNOW_HOME", home)
	path := writeJSFixture(t, cwd, "demo", `throw new Error("must not execute");`)
	writePluginConfig(t, filepath.Join(home, "config.json"), map[string]plugin.JavaScriptSpec{"demo": {Path: path}})
	a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, NoPlugins: true, JavaScriptPaths: []string{"/must/not/read"}, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if status := pluginStatusByID(t, a, "demo"); !status.Enabled || status.Loaded || !status.RestartRequired {
		t.Fatalf("no-plugins=%+v", status)
	}
	if _, err := a.SetPluginEnabled(t.Context(), "demo", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetPluginEnabled(t.Context(), "demo", true); err != nil {
		t.Fatalf("validation executed JavaScript: %v", err)
	}
	explicit, err := New(t.Context(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, CWD: cwd, JavaScriptPlugins: map[string]plugin.JavaScriptSpec{"demo": {Disabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	defer explicit.Close()
	if status := pluginStatusByID(t, explicit, "demo"); status.CanToggle || status.Enabled || status.Scope != "explicit" {
		t.Fatalf("explicit=%+v", status)
	}
	if _, err := explicit.SetPluginEnabled(t.Context(), "demo", true); err == nil || !strings.Contains(err.Error(), "launch options") {
		t.Fatalf("explicit err=%v", err)
	}
}

func TestPluginManagementConcurrentUpdatesAndCancellation(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	t.Setenv("SNOW_HOME", home)
	specs := map[string]plugin.JavaScriptSpec{}
	for _, id := range []string{"first", "second"} {
		specs[id] = plugin.JavaScriptSpec{Path: writeJSFixture(t, cwd, id, jsEcho)}
	}
	writePluginConfig(t, filepath.Join(home, "config.json"), specs)
	a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.SetPluginEnabled(ctx, "first", false); err == nil {
		t.Fatal("canceled update accepted")
	}
	if !pluginStatusByID(t, a, "first").Enabled {
		t.Fatal("canceled write changed state")
	}
	var wg sync.WaitGroup
	for id := range specs {
		wg.Go(func() {
			if _, err := a.SetPluginEnabled(t.Context(), id, false); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for id := range specs {
		if pluginStatusByID(t, a, id).Enabled {
			t.Fatalf("lost update for %s", id)
		}
	}
}
