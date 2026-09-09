package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	internalplugin "github.com/elmissouri16/snow-core/internal/plugin"
	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func TestPluginDiagnosticsShownOnceAndSanitized(t *testing.T) {
	manager := internalplugin.NewManager(tools.NewRegistry())
	runtime := javascript.New(&javascript.Package{Manifest: javascript.Manifest{ID: "test", Name: "Test", Version: "1"}}, javascript.Options{})
	if err := manager.LoadJavaScript(runtime, "test"); err != nil {
		t.Fatal(err)
	}
	manager.RecordDiagnostic("test", "warning", "disabled\x1b[31m-marker")
	m := &Model{app: &app.App{PluginManager: manager}}
	if cmd := m.pollPluginDiagnostics(); cmd == nil {
		t.Fatal("missing follow-up poll")
	}
	count := len(m.lines)
	if count == 0 {
		t.Fatal("missing diagnostic line")
	}
	m.pollPluginDiagnostics()
	if len(m.lines) != count {
		t.Fatal("repeated diagnostic")
	}
	if strings.Contains(strings.Join(m.lines, ""), "\x1b[31m-marker") {
		t.Fatal("plugin supplied terminal escape survived")
	}
}

func TestPluginDiagnosticsSurviveStartupHydration(t *testing.T) {
	testHome(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"snow-plugin.json": `{"id":"test","name":"Test","version":"1","api_version":1,"entry":"main.js"}`,
		"main.js":          `snow.log("info","startup-plugin-marker")`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := app.New(t.Context(), app.Options{Provider: "fake", CWD: dir, NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPlugins: map[string]plugin.JavaScriptSpec{"test": {Path: dir}}})
	if err != nil {
		t.Fatal(err)
	}
	a.Cfg.Updates.CheckOnStartup = false
	m := newModel(t.Context(), app.Options{})
	t.Cleanup(func() { m.Close() })
	m.Update(doneMsg{app: a})
	if !strings.Contains(strings.Join(m.lines, "\n"), "startup-plugin-marker") {
		t.Fatal("startup hydration discarded the plugin diagnostic")
	}
}
