package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func renderPluginNode(node protocol.PluginNode, width int) string {
	width = max(1, width)
	text := sanitizeTerminalText(node.Text)
	var children []string
	childWidth := width
	if node.Type == "row" {
		childWidth = max(1, (width-max(0, len(node.Children)-1))/max(1, len(node.Children)))
	}
	for _, child := range node.Children {
		children = append(children, renderPluginNode(child, childWidth))
	}
	switch node.Type {
	case "row":
		text = lipgloss.JoinHorizontal(lipgloss.Top, children...)
	case "column":
		text = strings.Join(children, "\n")
	case "list":
		for i := range children {
			children[i] = "• " + children[i]
		}
		text = strings.Join(children, "\n")
	case "markdown":
		text = newMarkdownRenderer().render(text, width)
	case "table":
		var rows []string
		if len(node.Columns) > 0 {
			rows = append(rows, strings.Join(node.Columns, " | "))
		}
		for _, row := range node.Rows {
			rows = append(rows, strings.Join(row, " | "))
		}
		text = sanitizeTerminalText(strings.Join(rows, "\n"))
	case "progress":
		value := max(0, min(1, node.Value))
		cells := max(1, min(20, width-8))
		filled := int(float64(cells) * value)
		text = fmt.Sprintf("%s [%s%s] %.0f%%", text, strings.Repeat("━", filled), strings.Repeat("─", cells-filled), value*100)
	case "button":
		text = "[ " + text + " ]"
	case "input", "select":
		text = text + ": " + sanitizeTerminalText(node.Input)
	case "checkbox":
		checked := "☐ "
		if node.Value != 0 {
			checked = "☑ "
		}
		text = checked + text
	}
	text = xansi.Hardwrap(xansi.Wordwrap(text, width, ""), width, true)
	style := styleAssistant
	switch node.Tone {
	case "accent":
		style = styleUser
	case "muted":
		style = styleFooter
	case "warning":
		style = styleTool
	case "error":
		style = styleError
	case "success":
		style = lipgloss.NewStyle().Foreground(colorOk)
	}
	return style.Render(text)
}
func (m *Model) applyPluginTheme(theme protocol.PluginTheme) error {
	color := func(name string) config.AdaptiveColor {
		c := theme.Colors[name]
		return config.AdaptiveColor{Light: c.Light, Dark: c.Dark}
	}
	err := applyCustomTUITheme(config.ThemeFile{Version: 1, Name: theme.ID, Colors: config.ThemeColors{Accent: color("accent"), Muted: color("muted"), Foreground: color("foreground"), Warning: color("warning"), Error: color("error"), Success: color("success"), Separator: color("separator")}})
	if err == nil {
		m.refreshThemeStyles()
		m.rerenderThemedTranscript()
	}
	return err
}
