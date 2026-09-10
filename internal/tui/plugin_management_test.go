package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func TestPluginInspectorTogglesDisabledOnlyRegistration(t *testing.T) {
	testHome(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"snow-plugin.json": `{"id":"demo","name":"Demo","version":"1","api_version":2,"entry":"main.js"}`,
		"main.js":          `throw new Error("must not execute during a toggle");`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.UpdateJavaScriptPlugins(configPath, true, func(specs map[string]plugin.JavaScriptSpec) error {
		specs["demo"] = plugin.JavaScriptSpec{Path: dir, Disabled: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	opts := app.Options{CWD: t.TempDir(), ConfigPath: configPath, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true}
	m := newModel(t.Context(), opts)
	a, err := app.New(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m.app, m.width, m.height = a, 100, 30
	if cmd := m.attachPlugins(); cmd != nil || m.plugins != nil {
		t.Fatal("disabled package attached UI")
	}
	m.pluginInspector()
	if m.pluginScreenView() == nil {
		t.Fatal("disabled-only inspector unavailable")
	}
	m.handlePluginKey(tea.KeyPressMsg{Code: tea.KeyEnter}) // Open the selected plugin.
	actions := pluginActions(m.pluginScreenView().Content)
	if len(actions) != 1 || actions[0].Action != "enable:demo" {
		t.Fatalf("actions=%+v", actions)
	}
	m.editor.SetValue("keep my draft")
	handled, cmd := m.handlePluginKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatal("enable button did not run")
	}
	if _, repeated := m.handlePluginKey(tea.KeyPressMsg{Code: tea.KeyEnter}); repeated != nil {
		t.Fatal("repeated Enter started another save")
	}
	if !strings.Contains(m.renderPluginScreen(), "Saving") {
		t.Fatal("pending save not visible")
	}
	m.finishPluginToggle(cmd().(pluginToggleDone))
	if m.editor.Value() != "keep my draft" || m.busy {
		t.Fatal("toggle changed composer or started a turn")
	}
	actions = pluginActions(m.pluginScreenView().Content)
	if len(actions) != 1 || actions[0].Action != "disable:demo" {
		t.Fatalf("updated actions=%+v", actions)
	}
	for _, size := range [][2]int{{20, 8}, {40, 18}, {100, 30}} {
		m.width, m.height = size[0], size[1]
		frame := m.renderPluginScreen()
		if lipgloss.Width(frame) > m.width || lipgloss.Height(frame) > m.height {
			t.Fatalf("frame exceeds %v", size)
		}
	}
	if !strings.Contains(m.renderPluginScreen(), "restart required") {
		t.Fatal("inspector hid pending restart")
	}
	_, cmd = m.runCommand("/plugins disable demo")
	if cmd == nil {
		t.Fatal("slash toggle unavailable")
	}
	m.plugins.screen = "" // Do not reopen an inspector dismissed during saving.
	m.finishPluginToggle(cmd().(pluginToggleDone))
	if m.plugins.screen != "" || strings.Contains(m.lastStatus, "restart") {
		t.Fatalf("undo left stale UI: screen=%s status=%s", m.plugins.screen, m.lastStatus)
	}
	m.pluginInspector()
	m.handlePluginKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.finishPluginToggle(pluginToggleDone{app: a, err: errors.New("package is missing")})
	if !strings.Contains(m.renderPluginScreen(), "package is missing") {
		t.Fatal("toggle failure hidden behind inspector")
	}
}
