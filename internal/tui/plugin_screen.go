package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type pluginScreenLayout struct {
	geometry                 pickerCardGeometry
	lines                    []string
	actions                  []protocol.PluginNode
	bodyHeight, actionHeight int
	selected, offset, limit  int
}

func (m *Model) pluginScreenLayout(view protocol.PluginView) pluginScreenLayout {
	layout := pluginScreenLayout{geometry: m.pickerCardGeometry(), actions: pluginActions(view.Content)}
	innerHeight := layout.geometry.innerHeight
	// Reserve the title, separators, and controls before allocating either pane.
	if len(layout.actions) > 0 {
		layout.actionHeight = min(len(layout.actions), 5, max(1, (innerHeight-5)/2))
		layout.selected = max(0, m.plugins.selected) % len(layout.actions)
	}
	body := m.renderPluginViewContent(view, max(1, layout.geometry.innerWidth-2), true)
	layout.lines = strings.Split(body, "\n")
	chromeHeight := 3 + layout.actionHeight
	if layout.actionHeight > 0 {
		chromeHeight++
	}
	// Short documents get a compact card; long documents retain the picker's
	// maximum dimensions and scroll within it.
	layout.geometry.innerHeight = min(innerHeight, max(10, len(layout.lines)+chromeHeight))
	layout.geometry.outerHeight = layout.geometry.innerHeight + 2
	layout.bodyHeight = max(1, layout.geometry.innerHeight-chromeHeight)
	layout.limit = max(0, len(layout.lines)-layout.bodyHeight)
	layout.offset = min(max(0, m.plugins.scroll), layout.limit)
	return layout
}

// renderPluginScreen shares the native picker frame. Actions live outside the
// scrolling document, so keyboard focus always refers to a visible control.
func (m *Model) renderPluginScreen() string {
	view := m.pluginScreenView()
	if view == nil {
		return ""
	}
	if view.ID == "snow:plugins" && m.plugins.inspector != nil && !m.plugins.inspector.detail {
		return m.renderPluginInspectorList()
	}
	layout := m.pluginScreenLayout(*view)
	width := layout.geometry.innerWidth
	end := min(len(layout.lines), layout.offset+layout.bodyHeight)
	status := ""
	if layout.limit > 0 {
		status = fmt.Sprintf("%d–%d of %d", layout.offset+1, end, len(layout.lines))
	}
	if view.ID == "snow:plugins" && m.plugins.inspector != nil {
		status = "Details"
	}
	parts := []string{
		renderPickerCardHeader(view.Title, status, width),
		styleSep.Render(strings.Repeat("─", width)),
		fitFrame(lipgloss.NewStyle().Padding(0, 1).Render(strings.Join(layout.lines[layout.offset:end], "\n")), width, layout.bodyHeight),
	}
	if layout.actionHeight > 0 {
		label := fmt.Sprintf(" Actions · %d/%d ", layout.selected+1, len(layout.actions))
		label = truncateDisplayText(label, width)
		parts = append(parts, styleHeaderDim.Render(label)+styleSep.Render(strings.Repeat("─", max(0, width-lipgloss.Width(label)))))
		start := min(max(0, layout.selected-layout.actionHeight/2), len(layout.actions)-layout.actionHeight)
		for i := start; i < start+layout.actionHeight; i++ {
			action := layout.actions[i]
			prefix, style := "  ", styleCompletion
			if action.Tone == "error" {
				style = styleError
			}
			if i == layout.selected {
				prefix, style = "› ", styleCompletionSelected
			}
			text := truncateDisplayText(prefix+sanitizeTerminalLine(action.Text), max(1, width-2))
			parts = append(parts, " "+style.Width(max(1, width-2)).Render(text)+" ")
		}
	}
	hint := " ↑/↓ scroll · PgUp/PgDn · Esc close "
	if len(layout.actions) > 0 {
		hint = " Tab/Shift+Tab actions · Enter run · ↑/↓ scroll · Esc close "
		if width < 58 {
			hint = " Tab actions · Enter · ↑↓ scroll · Esc "
		}
		if width < 36 {
			hint = " Tab ↵ ↑↓ Esc "
		}
	}
	if view.ID == "snow:plugins" && m.plugins.inspector != nil {
		hint = " ↑↓ actions · Enter select · PgUp/Dn details · Esc back "
		if width < 58 {
			hint = " ↑↓ ↵ · Pg scroll · Esc back "
		}
		if width < 30 {
			hint = " ↑↓ ↵ Pg Esc back "
		}
		if len(layout.actions) == 0 {
			hint = " ↑↓ scroll · Esc back "
		}
	}
	parts = append(parts, styleFooter.Render(truncateDisplayText(hint, width)))
	return renderPickerCard(fitFrame(strings.Join(parts, "\n"), width, layout.geometry.innerHeight), layout.geometry)
}

// Keep the declarative tree intact for sidebar/header rendering and dispatch;
// only the screen's presentation moves actionable buttons into its action pane.
func pluginScreenContent(node protocol.PluginNode) (protocol.PluginNode, bool) {
	if node.Type == "button" && node.Action != "" {
		return protocol.PluginNode{}, false
	}
	if len(node.Children) == 0 {
		return node, true
	}
	children := make([]protocol.PluginNode, 0, len(node.Children))
	for _, child := range node.Children {
		if content, keep := pluginScreenContent(child); keep {
			children = append(children, content)
		}
	}
	node.Children = children
	return node, len(children) > 0 || node.Text != ""
}
