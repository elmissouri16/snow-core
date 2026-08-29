package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type settingsCardRow struct {
	text      string
	available bool
}

func (m *Model) settingsModalVisible() bool {
	return m.pickSettings
}

func (m *Model) overlaySettingsModal(frame string) string {
	return m.overlayCenteredModal(frame, m.renderSettings())
}

func (m *Model) renderSettings() string {
	if !m.settingsModalVisible() || m.app == nil {
		return ""
	}
	geometry := m.pickerCardGeometry()
	rows := m.settingsCardRows()
	selected := clampPickerIndex(m.settingsIndex, len(rows))
	headerStatus := fmt.Sprintf("%d of %d", selected+1, len(rows))
	header := renderPickerCardHeader("Settings", headerStatus, geometry.innerWidth)
	messageText := truncateDisplayText(" Changes save immediately", geometry.innerWidth)
	message := styleHeaderDim.Render(messageText)
	if m.settingsError != "" {
		messageText = truncateDisplayText(" "+sanitizeTerminalLine(m.settingsError), geometry.innerWidth)
		message = styleError.Render(messageText)
	} else if m.settingsStatus != "" {
		messageText = truncateDisplayText(" "+sanitizeTerminalLine(m.settingsStatus), geometry.innerWidth)
		message = styleFooter.Render(messageText)
	}
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	bodyHeight := max(1, geometry.innerHeight-4)
	body := lipgloss.NewStyle().
		Width(geometry.innerWidth).
		Height(bodyHeight).
		MaxWidth(geometry.innerWidth).
		MaxHeight(bodyHeight).
		Render(renderSettingsCardRows(rows, selected, geometry.innerWidth, bodyHeight))
	footerText := " ↑/↓ navigate · ←/→ change · Enter select · Esc close "
	footer := styleFooter.Render(truncateDisplayText(footerText, geometry.innerWidth))
	content := lipgloss.JoinVertical(lipgloss.Left, header, message, separator, body, footer)
	return renderPickerCard(content, geometry)
}

func (m *Model) settingsCardRows() []settingsCardRow {
	model := m.app.Agent.Model()
	chatGPT := m.chatGPTSettingsEnabled()
	reasoning := "Reasoning summary  " + string(m.app.Agent.ReasoningSummary()) + settingsAvailabilityNote(chatGPT)
	verbosity := "Text verbosity  " + string(m.app.Agent.TextVerbosity()) + settingsAvailabilityNote(chatGPT)
	concurrency := fmt.Sprintf("Concurrent subagents  %d (restart to apply)", m.app.Cfg.Subagents.MaxConcurrentThreads)
	return []settingsCardRow{
		{text: "Model  " + model.Provider + "/" + model.ID, available: true},
		{text: "Theme  " + themeDisplayName(m.themeName), available: true},
		{text: "Thinking effort  " + string(m.app.Agent.Thinking()), available: true},
		{text: reasoning, available: chatGPT},
		{text: verbosity, available: chatGPT},
		{text: "Session permission  " + string(m.app.Perm.Mode()), available: true},
		{text: "Subagents  " + onOff(m.app.Cfg.Subagents.Enabled) + " (restart to apply)", available: true},
		{text: concurrency, available: true},
		{text: "Agent Skills  " + onOff(!m.app.Cfg.Skills.Disabled) + " (restart to apply)", available: true},
		{text: "Debug diagnostics  " + onOff(m.app.DebugStatus().Enabled) + " (captures sensitive content)", available: true},
		{text: "Keybindings  configure shortcuts", available: true},
	}
}

func settingsAvailabilityNote(available bool) string {
	if available {
		return ""
	}
	return "  (ChatGPT only)"
}

func renderSettingsCardRows(rows []settingsCardRow, selected, width, height int) string {
	if len(rows) == 0 || width <= 0 || height <= 0 {
		return ""
	}
	start, end := settingsCardWindow(selected, len(rows), height)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		prefix := "  "
		style := styleCompletion
		if i == selected {
			prefix = "› "
			style = styleCompletionSelected
		} else if !rows[i].available {
			style = styleHeaderDim
		}
		line := truncateDisplayText(prefix+sanitizeTerminalLine(rows[i].text), width)
		lines = append(lines, style.Render(line))
	}
	return strings.Join(lines, "\n")
}

func settingsCardWindow(selected, total, height int) (start, end int) {
	if total <= 0 || height <= 0 {
		return 0, 0
	}
	selected = clampPickerIndex(selected, total)
	visible := min(total, max(1, height))
	start = selected - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}
