package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPluginViewGeometryAndKeyboard(t *testing.T) {
	testHome(t)
	for _, width := range []int{40, 120} {
		m := newModel(t.Context(), app.Options{})
		m.width = width
		m.height = 24
		node := protocol.PluginNode{Type: "column", Children: []protocol.PluginNode{{Type: "text", Text: strings.Repeat("long text ", 30)}, {Type: "button", Text: "Refresh", Action: "refresh"}}}
		m.plugins = &pluginUIState{views: []protocol.PluginView{{ID: "test:panel", PluginID: "test", Name: "panel", Title: "Panel", Placement: "sidebar", Content: &node}}, cache: map[pluginRenderKey]string{}, running: map[string]bool{}}
		m.layout()
		if width == 40 && m.pluginSidebarWidth() != 0 {
			t.Fatal("sidebar crowding narrow terminal")
		}
		if width == 120 && m.pluginSidebarWidth() == 0 {
			t.Fatal("missing wide sidebar")
		}
		m.plugins.screen = "test:panel"
		frame := m.renderPluginScreen()
		if lipgloss.Width(frame) > width || lipgloss.Height(frame) > 24 {
			t.Fatalf("geometry %dx%d", lipgloss.Width(frame), lipgloss.Height(frame))
		}
		handled, _ := m.handlePluginKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		if !handled || m.plugins.screen != "" {
			t.Fatal("escape did not restore transcript")
		}
	}
}
func TestPluginShortcutsCannotCaptureCoreKeys(t *testing.T) {
	if _, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"plugin:demo:run": {"ctrl+c"}}); err == nil {
		t.Fatal("plugin captured emergency key")
	}
	if _, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"plugin:demo:run": {"alt+m"}}); err == nil {
		t.Fatal("plugin shadowed core model shortcut")
	}
	if _, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"plugin:demo:run": {"alt+r"}}); err != nil {
		t.Fatal(err)
	}
}

func TestPluginDialogDoesNotChangeAgentBusyState(t *testing.T) {
	testHome(t)
	for _, busy := range []bool{false, true} {
		m := newModel(t.Context(), app.Options{})
		m.width, m.height, m.busy = 100, 30, busy
		m.startUserInput(protocol.UserInputRequest{ID: "plugin-dialog", ToolCallID: "plugin-dialog", Questions: []protocol.UserInputQuestion{{ID: "answer", Question: "Choose a theme"}}})
		if !m.userInputPending || m.busy != busy {
			t.Fatalf("dialog changed turn state: pending=%v busy=%v want=%v", m.userInputPending, m.busy, busy)
		}
		m.clearUserInput()
		if m.userInputPending || m.busy != busy {
			t.Fatalf("dialog left stale turn state: pending=%v busy=%v want=%v", m.userInputPending, m.busy, busy)
		}
	}
}

func BenchmarkPluginViewCached(b *testing.B) {
	node := protocol.PluginNode{Type: "column", Children: []protocol.PluginNode{{Type: "text", Text: "Workspace"}, {Type: "progress", Value: 0.5}}}
	view := protocol.PluginView{ID: "dashboard:status", Content: &node}
	m := &Model{plugins: &pluginUIState{cache: map[pluginRenderKey]string{}}}
	m.renderPluginView(view, 32)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.renderPluginView(view, 32)
	}
}
