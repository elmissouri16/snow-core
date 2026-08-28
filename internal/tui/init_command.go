package tui

import (
	_ "embed"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

//go:embed init_prompt.md
var initCommandPrompt string

func (m *Model) startInitCommand(displayLine string) (tea.Model, tea.Cmd) {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("init: wait for the current turn to finish"))
		return m, nil
	}
	if m.app.Agent.Mode() == protocol.ModePlan {
		m.pushLine(styleError.Render("init: switch to Default mode first"))
		return m, nil
	}
	displayLine = strings.TrimSpace(displayLine)
	if displayLine == "" {
		displayLine = "/init"
	}
	m.pushLine(styleUser.Render("› " + displayLine))
	return m, m.startPromptWithDisplay(initCommandPrompt, displayLine)
}
