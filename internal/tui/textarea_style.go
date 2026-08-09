package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

// normalizeTextareaStyles removes Bubbles' default cursor-line and end-of-
// buffer backgrounds. Snow draws the composer container itself; those nested
// defaults can leak as bright vertical bars at the left/right edges when a
// light theme is selected on a dark terminal.
func normalizeTextareaStyles(editor *textarea.Model) {
	focused, blurred := textarea.DefaultStyles()
	for _, style := range []*textarea.Style{&focused, &blurred} {
		style.Base = lipgloss.NewStyle()
		style.CursorLine = lipgloss.NewStyle()
		style.EndOfBuffer = lipgloss.NewStyle()
		style.Prompt = lipgloss.NewStyle()
	}
	focused.Placeholder = lipgloss.NewStyle().Foreground(colorMuted)
	blurred.Placeholder = lipgloss.NewStyle().Foreground(colorMuted)
	focused.Text = lipgloss.NewStyle().Foreground(colorSoft)
	blurred.Text = lipgloss.NewStyle().Foreground(colorSoft)
	editor.FocusedStyle = focused
	editor.BlurredStyle = blurred
}
