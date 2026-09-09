package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// pluginChrome shares the same row allocation between measurement and drawing.
// Core controls and overlays take precedence; hidden views remain in /plugins.
func (m *Model) pluginChrome() map[string]string {
	if m.plugins == nil || m.inlineModalOverlay() || m.inlineInputOverlay() {
		return nil
	}
	remaining := m.managedFrameHeight() - m.fixedChromeRows() - m.editor.Height() - m.runStatusHeight() - minTranscriptHeight
	if overlay := m.renderOverlays(); overlay != "" {
		remaining -= lipgloss.Height(overlay)
	}
	if remaining <= 0 {
		return nil
	}
	width := m.managedFrameWidth()
	result := make(map[string]string, 3)
	for _, placement := range []string{"footer", "above_input", "header"} {
		text := m.pluginPlacementContent(placement, max(1, width-2))
		if text == "" {
			continue
		}
		lines := strings.Split(text, "\n")
		lines = lines[:min(len(lines), remaining)]
		result[placement] = lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(lines, "\n"))
		remaining -= len(lines)
		if remaining == 0 {
			break
		}
	}
	return result
}
