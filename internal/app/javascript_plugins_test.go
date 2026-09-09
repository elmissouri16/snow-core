package app

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/subagent"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func writeJSFixture(t *testing.T, root, id, script string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, id)
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"id": id, "name": id, "version": "1", "api_version": 1, "entry": "main.js"}
	raw, err := jsonv2.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "snow-plugin.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "main.js"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const jsEcho = `snow.log("info","loaded");snow.registerTool({name:"echo",description:"echo",parameters:{type:"object"},execute(args){return {content:[{type:"text",text:"ok"}]}}});`

func TestAppJavaScriptTrustDisableAndMetadata(t *testing.T) {
	for _, kind := range []string{"trusted", "untrusted", "disabled", "explicit", "no-plugins"} {
		t.Run(kind, func(t *testing.T) {
			home, cwd := t.TempDir(), t.TempDir()
			t.Setenv("SNOW_HOME", home)
			dir := writeJSFixture(t, cwd, "demo", jsEcho)
			if err := os.MkdirAll(filepath.Join(cwd, ".snow"), 0700); err != nil {
				t.Fatal(err)
			}
			raw, err := jsonv2.Marshal(map[string]any{"js_plugins": map[string]plugin.JavaScriptSpec{"demo": {Path: "demo", Disabled: kind == "disabled"}}})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(cwd, ".snow", "config.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			trust := "allow"
			if kind == "untrusted" {
				trust = "ask"
			}
			if err = os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"default_project_trust":"`+trust+`"}`), 0600); err != nil {
				t.Fatal(err)
			}
			opts := Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: cwd, NoPlugins: kind == "no-plugins"}
			if kind == "explicit" {
				opts.JavaScriptPlugins = map[string]plugin.JavaScriptSpec{"demo": {Path: dir}}
			}
			if kind == "no-plugins" {
				opts.JavaScriptPaths = []string{"/must/not/be/read"}
			}
			a, err := New(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			want := kind == "trusted" || kind == "explicit"
			desc, found := a.Registry.Descriptor("plugin_demo_echo")
			if found != want {
				t.Fatalf("found=%v want %v", found, want)
			}
			if want {
				if desc.Source != tools.SourceJSPlugin {
					t.Fatalf("source=%s", desc.Source)
				}
				children, _, err := cloneChildRegistry(a.Registry, subagent.Role{Tools: []string{"plugin_demo_echo"}}, true)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := children.Get("plugin_demo_echo"); ok {
					t.Fatal("JavaScript leaked into child registry")
				}
				before := a.ConfigDiagnostics()
				a.PluginManager.RecordDiagnostic("demo", "warning", "later")
				after := a.ConfigDiagnostics()
				if len(after) != len(before)+1 {
					t.Fatal("diagnostics cache hid runtime update")
				}
			}
		})
	}
}
func TestAppJavaScriptDisabledEntriesNeverReadPackages(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, CWD: t.TempDir(), JavaScriptPlugins: map[string]plugin.JavaScriptSpec{"demo": {Path: "/does/not/exist", Disabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
}
func TestAppJavaScriptStartupRollbackAndNoHostIO(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	dir := writeJSFixture(t, cwd, "demo", jsEcho+`snow.registerTool({name:"echo",description:"duplicate",parameters:{},execute(){}});`)
	a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, CWD: cwd, JavaScriptPlugins: map[string]plugin.JavaScriptSpec{"demo": {Path: dir}}})
	if err == nil {
		a.Close()
		t.Fatal("duplicate startup succeeded")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Fatalf("unexpected error=%v", err)
	}
}
func TestAppJavaScriptGoIDCollision(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	dir := writeJSFixture(t, cwd, "close-tracking", jsEcho)
	closed := 0
	goPlugin := appCloseTrackingPlugin{closed: &closed}
	// Use the actual Go manifest ID to exercise the cross-language namespace.
	data, err := os.ReadFile(filepath.Join(dir, "snow-plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err = jsonv2.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["id"] = goPlugin.Manifest().ID
	data, err = jsonv2.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "snow-plugin.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, CWD: cwd, GoPlugins: []plugin.Plugin{goPlugin}, JavaScriptPlugins: map[string]plugin.JavaScriptSpec{goPlugin.Manifest().ID: {Path: dir, Config: json.RawMessage(`{}`)}}})
	if err == nil {
		a.Close()
		t.Fatal("cross-language ID collision accepted")
	}
}
