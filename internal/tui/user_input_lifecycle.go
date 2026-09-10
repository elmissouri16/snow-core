package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type userInputSettledMsg struct {
	app     *app.App
	request *protocol.UserInputRequest
}

func (m *Model) pluginInputPending() bool {
	return m.userInputPending && m.userInputRequest != nil && strings.HasPrefix(m.userInputRequest.ID, "plugin-")
}

func (m *Model) clearTurnUserInput() {
	// Plugin commands have an independent lifetime. Their broker settlement,
	// rather than a coincident root turn boundary, dismisses their input.
	if !m.pluginInputPending() {
		m.clearUserInput()
	}
}

func (m *Model) waitUserInputSettlement() tea.Cmd {
	if m.app == nil || !m.userInputPending || m.userInputRequest == nil || m.userInputWaitRequest == m.userInputRequest {
		return nil
	}
	active, request := m.app, m.userInputRequest
	m.userInputWaitRequest = request
	done := active.UserInputDone(request.ID)
	ctx := m.ctx
	return func() tea.Msg {
		select {
		case <-done:
		case <-ctx.Done():
		}
		return userInputSettledMsg{app: active, request: request}
	}
}
