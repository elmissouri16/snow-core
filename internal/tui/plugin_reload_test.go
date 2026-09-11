package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestPluginReloadSurfaceRefreshesAliasesAndInspector(t *testing.T) {
	testHome(t)
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"snow-plugin.json": `{"id":"demo","name":"Demo","version":"1","api_version":2,"entry":"main.js","capabilities":["commands"]}`, "main.js": `snow.registerCommand({name:"old",alias:"oldalias",description:"old",run(){return "old";}});`} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	opts := app.Options{CWD: path, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{path}}
	a, err := app.New(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(t.Context(), opts)
	m.app, m.width, m.height = a, 100, 30
	m.pluginInspector()
	m.plugins.generation = a.PluginGeneration()
	m.refreshPluginCatalog()
	if err := os.WriteFile(filepath.Join(path, "main.js"), []byte(`snow.registerCommand({name:"new",alias:"newalias",description:"new",run(){return "new";}});`), 0600); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.runCommand("/plugins reload demo")
	if cmd == nil {
		t.Fatal("reload command not routed")
	}
	done := cmd().(pluginReloadDone)
	if done.err != nil || !done.result.Applied {
		t.Fatalf("reload %+v", done)
	}
	// Refresh also works when the generation event arrives before the receipt.
	m.syncPluginGeneration()
	m.finishPluginReload(done)
	if m.plugins.screen != "snow:plugins" {
		t.Fatal("inspector lost on reload")
	}
	if len(m.plugins.commands) != 1 || m.plugins.commands[0].Name != "new" {
		t.Fatalf("commands=%+v", m.plugins.commands)
	}
	found := false
	for _, spec := range m.pluginSpecs() {
		if spec.name == "/oldalias" {
			t.Fatal("stale completion alias")
		}
		found = found || spec.name == "/newalias"
	}
	if !found {
		t.Fatal("new completion alias missing")
	}
	if m.busy || m.plugins.managementPending {
		t.Fatal("reload left surface busy")
	}
}
