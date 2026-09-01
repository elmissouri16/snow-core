package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) startInitCommand(displayLine string) (tea.Model, tea.Cmd) {
	if m.busy {
		m.pushLine(styleError.Render("init: wait for the current turn to finish"))
		return m, nil
	}
	prompt, err := m.app.PrepareProjectInit()
	if err != nil {
		m.pushLine(styleError.Render(err.Error()))
		return m, nil
	}
	displayLine = strings.TrimSpace(displayLine)
	if displayLine == "" {
		displayLine = "/init"
	}
	m.pushLine(styleUser.Render("› " + displayLine))
	return m, m.startPromptWithDisplay(prompt, displayLine)
}
