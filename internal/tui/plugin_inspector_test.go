package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func pluginInspectorTestModel(t *testing.T, count int) *Model {
	t.Helper()
	m := pluginLayoutModel(t)
	m.width, m.height = 100, 30
	p := &pluginInspectorState{}
	for i := range count {
		id := fmt.Sprintf("plugin-%02d", i)
		view := protocol.PluginView{ID: id + ":main", PluginID: id, Title: "Workspace", Placement: "screen", Content: new(protocol.PluginNode{Type: "text", Text: "Plugin workspace"})}
		p.entries = append(p.entries, pluginInspectorEntry{
			status: protocol.PluginStatus{ID: id, Enabled: true, Loaded: true, CanToggle: true, Scope: "global", Path: "/very/long/project/path/" + id},
			info: protocol.PluginInfo{ID: id, Name: fmt.Sprintf("Extension %02d", i), Version: "1.0",
				Views:    []protocol.PluginView{view},
				Commands: []protocol.PluginCommand{{ID: id + ":run", Description: "Run this plugin"}},
				Settings: []protocol.PluginSetting{{Name: "focus"}},
			},
		})
		m.plugins.views = append(m.plugins.views, view)
	}
	m.plugins.inspector, m.plugins.screen = p, "snow:plugins"
	m.updatePluginInspectorView()
	m.layout()
	return m
}

func TestPluginInspectorSelectionOwnsActionsAndBackNavigation(t *testing.T) {
	m := pluginInspectorTestModel(t, 8)
	m.editor.SetValue("keep this draft")
	frame := stripANSI(m.renderPluginScreen())
	if !strings.Contains(frame, "8 plugins") || strings.Contains(frame, "/very/") || strings.Contains(frame, "Actions") {
		t.Fatalf("inventory contains document details:\n%s", frame)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !m.plugins.inspector.detail {
		t.Fatal("opening details executed an action")
	}
	actions := pluginActions(m.pluginScreenView().Content)
	if len(actions) != 3 || actions[0].Action != "disable:plugin-01" || actions[1].Action != "open:plugin-01:main" || actions[2].Action != "setting:plugin-01:focus" {
		t.Fatalf("selected plugin did not own its actions: %+v", actions)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.plugins.selected != 1 || m.plugins.scroll != 0 {
		t.Fatal("arrow keys scrolled details instead of selecting an action")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.plugins.screen != "plugin-01:main" {
		t.Fatal("did not open the selected plugin view")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.plugins.screen != "snow:plugins" || !m.plugins.inspector.detail || m.plugins.selected != 1 {
		t.Fatal("Escape did not return to plugin details")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.plugins.inspector.detail || m.plugins.inspector.index != 1 {
		t.Fatal("Escape lost inventory selection")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.plugins.screen != "" || m.editor.Value() != "keep this draft" || m.busy {
		t.Fatal("closing inspector changed composer or started a turn")
	}
}

func TestPluginInspectorFilteringAndEmptyResults(t *testing.T) {
	m := pluginInspectorTestModel(t, 30)
	p := m.plugins.inspector
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("EXTENSION 24")})
	if entry := p.selectedEntry(); entry == nil || entry.status.ID != "plugin-24" {
		t.Fatal("filter did not match display name case-insensitively")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.query != "EXTENSION 24" || !strings.Contains(stripANSI(m.renderPluginScreen()), "› plugin-24") {
		t.Fatal("back navigation lost filtered selection")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("missing")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || p.detail || !strings.Contains(stripANSI(m.renderPluginScreen()), "No matching plugins") {
		t.Fatal("empty result activated an old selection")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if p.index != 29 {
		t.Fatal("End did not select last plugin")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if p.index >= 29 {
		t.Fatal("Page Up did not move selection")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("界")})
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if p.query != "" {
		t.Fatal("backspace did not remove one Unicode character")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("a", 200))})
	if len([]rune(p.query)) != 120 || m.editor.Value() != "" {
		t.Fatal("filter is unbounded or leaked into composer")
	}
}

func TestPluginInspectorFramesKeepSelectionAndControls(t *testing.T) {
	m := pluginInspectorTestModel(t, 32)
	for _, inline := range []bool{false, true} {
		m.inlineTranscript = inline
		for _, size := range [][2]int{{140, 40}, {100, 30}, {60, 16}, {40, 12}, {20, 8}} {
			m.width, m.height = size[0], size[1]
			m.layout()
			for _, detail := range []bool{false, true} {
				m.plugins.inspector.detail = detail
				m.plugins.inspector.index = 31
				m.updatePluginInspectorView()
				card := m.renderPluginScreen()
				frame := stripANSI(card)
				if lipgloss.Width(card) > m.managedFrameWidth() || lipgloss.Height(card) > m.managedFrameHeight() {
					t.Fatalf("unbounded panel at %v (inline %v, detail %v)", size, inline, detail)
				}
				if !strings.Contains(frame, "Esc") || (!detail && !strings.Contains(frame, "› plugin-31")) || (detail && !strings.Contains(frame, "› ")) {
					t.Fatalf("selection or controls hidden at %v:\n%s", size, frame)
				}
				full := m.View()
				if lipgloss.Width(full) != m.managedFrameWidth() || lipgloss.Height(full) != m.managedFrameHeight() {
					t.Fatalf("overlay changed frame dimensions at %v", size)
				}
			}
		}
	}
}

func TestPluginInspectorLaunchControlsDiagnosticsAndDialogs(t *testing.T) {
	m := pluginInspectorTestModel(t, 1)
	p := m.plugins.inspector
	p.entries[0].status.CanToggle = false
	p.entries[0].status.Scope = "explicit"
	p.diagnostics = []protocol.ConfigDiagnostic{{Path: "plugin-00", Message: "observer failed"}}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, action := range pluginActions(m.pluginScreenView().Content) {
		if strings.HasPrefix(action.Action, "disable:") || strings.HasPrefix(action.Action, "enable:") {
			t.Fatal("launch-controlled plugin offered a toggle")
		}
	}
	if !strings.Contains(stripANSI(m.renderPluginScreen()), "Controlled by launch options") {
		t.Fatal("missing launch control explanation")
	}
	m.startUserInput(protocol.UserInputRequest{ID: "input", Questions: []protocol.UserInputQuestion{{ID: "answer", Question: "Configure extension"}}})
	if strings.Contains(stripANSI(m.View()), "Actions") {
		t.Fatal("inspector covered the user-input dialog")
	}
	m.clearUserInput()
	if !p.detail || !strings.Contains(stripANSI(m.View()), "Actions") {
		t.Fatal("dialog completion did not return to details")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(stripANSI(m.renderPluginScreen()), "observer failed") || len(pluginActions(m.pluginScreenView().Content)) != 0 {
		t.Fatal("diagnostics lost or inherited plugin actions")
	}
	p.entries, p.diagnostics, p.detail = nil, nil, false
	m.updatePluginInspectorView()
	if !strings.Contains(stripANSI(m.renderPluginScreen()), "No plugins registered") {
		t.Fatal("empty inventory missing guidance")
	}
}
