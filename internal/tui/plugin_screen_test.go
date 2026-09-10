package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func pluginScreenTestModel(t *testing.T, width, height int) *Model {
	t.Helper()
	m := pluginLayoutModel(t)
	m.width, m.height = width, height
	m.plugins.views = []protocol.PluginView{{ID: "notes:main", PluginID: "notes", Title: "Workspace Notes", Placement: "screen", Content: new(protocol.PluginNode{
		Type: "column", Children: []protocol.PluginNode{
			{Type: "text", Text: "Project · 1 saved note", Tone: "muted"},
			{Type: "text", Text: "1. Keep the existing panel style."},
			{Type: "button", Text: "Add a note", Action: "add"},
			{Type: "button", Text: "Insert into composer", Action: "insert"},
			{Type: "button", Text: "Clear notes", Action: "clear", Tone: "error"},
		},
	})}}
	m.layout()
	return m
}

func TestPluginScreenUsesCenteredCard(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, size := range [][2]int{{140, 40}, {100, 30}, {60, 16}, {40, 12}, {20, 8}} {
			t.Run(fmt.Sprintf("%dx%d_inline_%v", size[0], size[1], inline), func(t *testing.T) {
				m := pluginScreenTestModel(t, size[0], size[1])
				m.inlineTranscript = inline
				m.editor.SetValue("existing draft")
				m.layout()
				beforeHeight := m.transcript.Height()
				m.plugins.screen = "notes:main"
				m.layout()
				card := m.renderPluginScreen()
				width, height := lipgloss.Width(card), lipgloss.Height(card)
				if width > pickerCardMaxWidth || height > pickerCardMaxHeight || width > m.managedFrameWidth() || height > m.managedFrameHeight() {
					t.Fatalf("unbounded panel %dx%d", width, height)
				}
				frame := m.viewContent()
				if lipgloss.Width(frame) != m.managedFrameWidth() || lipgloss.Height(frame) != m.managedFrameHeight() {
					t.Fatalf("frame changed geometry: %dx%d", lipgloss.Width(frame), lipgloss.Height(frame))
				}
				x, y := (m.managedFrameWidth()-width)/2, (m.managedFrameHeight()-height)/2
				lines := strings.Split(stripANSI(frame), "\n")
				if []rune(lines[y])[x] != '╭' {
					t.Fatalf("missing centered panel at %d,%d:\n%s", x, y, stripANSI(frame))
				}
				m.handlePluginKey(tea.KeyPressMsg{Code: tea.KeyEscape})
				if m.plugins.screen != "" || m.editor.Value() != "existing draft" || m.transcript.Height() != beforeHeight {
					t.Fatal("closing panel changed draft or transcript geometry")
				}
			})
		}
	}
}

func TestPluginScreenKeepsFocusedActionsVisible(t *testing.T) {
	m := pluginScreenTestModel(t, 60, 16)
	node := m.plugins.views[0].Content
	node.Children[1].Text = strings.Repeat("A long saved note wraps across many rows. ", 80)
	for i := range 20 {
		node.Children = append(node.Children, protocol.PluginNode{Type: "button", Text: fmt.Sprintf("Action %02d", i), Action: "run"})
	}
	m.plugins.screen = "notes:main"
	actions := pluginActions(node)
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyTab}, {Code: tea.KeyTab, Mod: tea.ModShift}} {
		for range len(actions) + 1 {
			m.handlePluginKey(key)
			frame := stripANSI(m.renderPluginScreen())
			selected := actions[m.plugins.selected].Text
			if !strings.Contains(frame, "› "+selected) {
				t.Fatalf("focused action %q is offscreen:\n%s", selected, frame)
			}
			if strings.Contains(frame, "Selected:") || strings.Contains(frame, "[ "+selected+" ]") {
				t.Fatal("screen still uses plain-text action controls")
			}
		}
	}
	for range 500 {
		m.handlePluginKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	end := m.plugins.scroll
	m.handlePluginKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.plugins.scroll != end-1 {
		t.Fatal("scrolling up from the end did not move immediately")
	}
}

func TestPluginScreenYieldsToBlockingDialogs(t *testing.T) {
	m := pluginScreenTestModel(t, 100, 30)
	m.plugins.screen = "notes:main"
	m.startUserInput(protocol.UserInputRequest{ID: "plugin-input", ToolCallID: "plugin-input", Questions: []protocol.UserInputQuestion{{ID: "note", Question: "Save a workspace note"}}})
	if strings.Contains(stripANSI(m.viewContent()), "Workspace Notes") {
		t.Fatal("plugin screen covered the input dialog")
	}
	m.clearUserInput()
	if !strings.Contains(stripANSI(m.viewContent()), "Workspace Notes") || m.busy {
		t.Fatal("panel did not resume after the dialog")
	}
	m.permPending = true
	if strings.Contains(stripANSI(m.viewContent()), "Workspace Notes") {
		t.Fatal("plugin screen covered the permission dialog")
	}
}

func TestPluginScreenOwnsBackdropInput(t *testing.T) {
	m := pluginScreenTestModel(t, 100, 30)
	m.plugins.screen = "notes:main"
	header := m.renderHeaderLayout(m.currentHeaderStatus())
	m.dispatchMouse(tea.MouseClickMsg{X: header.modelStart, Y: 0, Button: tea.MouseLeft})
	if m.pickModel {
		t.Fatal("click passed through the panel backdrop")
	}
	m.inlineTranscript = true
	m.compVisible, m.compMatches = true, []string{"/help"}
	m.layout()
	if !strings.Contains(stripANSI(m.viewContent()), "Workspace Notes") {
		t.Fatal("inline completion displaced the plugin panel")
	}
}

func TestPluginScreenDoesNotAlterOtherPlacements(t *testing.T) {
	m := pluginScreenTestModel(t, 100, 30)
	view := m.plugins.views[0]
	before := m.renderPluginView(view, 60)
	_ = m.renderPluginViewContent(view, 60, true)
	after := m.renderPluginView(view, 60)
	if before != after || !strings.Contains(stripANSI(after), "[ Add a note ]") || len(pluginActions(view.Content)) != 3 {
		t.Fatal("screen rendering changed the shared view tree or cache")
	}
	view.Content = new(protocol.PluginNode{Type: "text", Text: strings.Repeat("界", 200) + "\x1b[2J"})
	m.plugins.views[0] = view
	clear(m.plugins.cache)
	m.plugins.screen = view.ID
	for _, size := range [][2]int{{100, 30}, {40, 12}, {20, 8}, {140, 40}} {
		m.width, m.height = size[0], size[1]
		m.layout()
		m.handlePluginKey(tea.KeyPressMsg{Code: tea.KeyEnd})
		card := m.renderPluginScreen()
		if strings.Contains(card, "\x1b[2J") || lipgloss.Width(card) > m.managedFrameWidth() || lipgloss.Height(card) > m.managedFrameHeight() {
			t.Fatal("passive content escaped panel bounds or sanitization")
		}
	}
}
