package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func (m *Model) helpModalVisible() bool {
	return m.pickHelp
}

func (m *Model) startHelp() (tea.Model, tea.Cmd) {
	m.closeTranscriptSelectionContextMenu()
	m.pickHelp = true
	m.helpOffset = 0
	m.compVisible = false
	return m, nil
}

func (m *Model) closeHelp() {
	m.pickHelp = false
	m.helpOffset = 0
}

func (m *Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	limit := m.helpOffsetLimit()
	m.helpOffset = min(max(0, m.helpOffset), limit)
	switch msg.Type {
	case tea.KeyUp:
		m.helpOffset = max(0, m.helpOffset-1)
	case tea.KeyDown:
		m.helpOffset = min(limit, m.helpOffset+1)
	case tea.KeyPgUp:
		m.helpOffset = max(0, m.helpOffset-m.helpBodyHeight())
	case tea.KeyPgDown:
		m.helpOffset = min(limit, m.helpOffset+m.helpBodyHeight())
	case tea.KeyHome:
		m.helpOffset = 0
	case tea.KeyEnd:
		m.helpOffset = limit
	case tea.KeyEnter, tea.KeyEsc:
		m.closeHelp()
	}
	return m, nil
}

func (m *Model) overlayHelpModal(frame string) string {
	return m.overlayCenteredModal(frame, m.renderHelp())
}

func (m *Model) helpBodyHeight() int {
	return max(1, m.pickerCardGeometry().innerHeight-3)
}

func (m *Model) helpOffsetLimit() int {
	geometry := m.pickerCardGeometry()
	return max(0, len(m.helpRows(geometry.innerWidth))-m.helpBodyHeight())
}

func (m *Model) helpLines() []string {
	mouseHelp := "Mouse: native terminal selection · F6 enables wheel + app drag-copy"
	if m.app != nil && m.app.Cfg.TUI.Mouse {
		mouseHelp = "Mouse: wheel + app drag-copy · F6 restores native terminal selection"
	}
	content := formatCommandListWithKeys(m.keys) +
		"\n\nBehavior\n" +
		"  While working, submit queues steer; follow-up uses its configured binding\n" +
		"  Header: app mouse clicks open models/thinking or toggle mode\n" +
		"  Working: app mouse click jumps to live output\n" +
		"  " + mouseHelp
	return strings.Split(content, "\n")
}

func (m *Model) helpRows(width int) []string {
	width = max(1, width)
	rows := make([]string, 0, len(m.helpLines()))
	for _, line := range m.helpLines() {
		line = sanitizeTerminalLine(line)
		if line == "" {
			rows = append(rows, "")
			continue
		}
		wrapped := xansi.Wordwrap(line, width, "")
		wrapped = xansi.Hardwrap(wrapped, width, true)
		rows = append(rows, strings.Split(wrapped, "\n")...)
	}
	return rows
}

func (m *Model) renderHelp() string {
	if !m.helpModalVisible() {
		return ""
	}
	geometry := m.pickerCardGeometry()
	lines := m.helpRows(geometry.innerWidth)
	bodyHeight := m.helpBodyHeight()
	start := min(max(0, m.helpOffset), max(0, len(lines)-bodyHeight))
	end := min(len(lines), start+bodyHeight)
	status := fmt.Sprintf("%d–%d of %d", start+1, end, len(lines))
	header := renderPickerCardHeader("Help", status, geometry.innerWidth)
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	rendered := make([]string, 0, end-start)
	for _, line := range lines[start:end] {
		line = truncateDisplayText(sanitizeTerminalLine(line), geometry.innerWidth)
		switch line {
		case "Commands", "Composer", "Shortcuts", "Behavior":
			rendered = append(rendered, styleHeaderDim.Render(line))
		default:
			rendered = append(rendered, styleCompletion.Render(line))
		}
	}
	body := lipgloss.NewStyle().
		Width(geometry.innerWidth).
		Height(bodyHeight).
		MaxWidth(geometry.innerWidth).
		MaxHeight(bodyHeight).
		Render(strings.Join(rendered, "\n"))
	footerText := " ↑/↓ scroll · PgUp/PgDn page · Enter/Esc close "
	footer := styleFooter.Render(truncateDisplayText(footerText, geometry.innerWidth))
	content := lipgloss.JoinVertical(lipgloss.Left, header, separator, body, footer)
	return renderPickerCard(content, geometry)
}
