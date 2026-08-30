package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func oppositeCollaborationMode(mode protocol.CollaborationMode) protocol.CollaborationMode {
	if mode == protocol.ModePlan {
		return protocol.ModeDefault
	}
	return protocol.ModePlan
}

func (m *Model) toggleCollaborationMode() tea.Cmd {
	if m.app == nil || m.app.Agent == nil {
		return nil
	}
	if m.modeSwitching {
		m.lastStatus = "mode switch in progress"
		return nil
	}
	current := m.app.Agent.Mode()
	base := current
	if m.pendingMode != nil {
		base = *m.pendingMode
	}
	target := oppositeCollaborationMode(base)
	if m.busy {
		if target == current {
			m.pendingMode = nil
			m.modeSwitchReady = false
			m.lastStatus = "mode switch canceled"
			return nil
		}
		m.pendingMode = new(target)
		m.modeSwitchReady = false
		m.lastStatus = "mode switches after current turn"
		return nil
	}
	m.pendingMode = new(target)
	return m.beginPendingModeSwitch()
}

func (m *Model) beginPendingModeSwitch() tea.Cmd {
	m.modeSwitchReady = false
	if m.pendingMode == nil || m.modeSwitching || m.app == nil || m.app.Agent == nil {
		return nil
	}
	target := *m.pendingMode
	if m.app.Agent.Mode() == target {
		m.pendingMode = nil
		return nil
	}
	m.modeSwitching = true
	m.lastStatus = "switching mode"
	agent := m.app.Agent
	return func() tea.Msg {
		return modeSwitchDoneMsg{target: target, err: agent.SetMode(target)}
	}
}

func (m *Model) finishModeSwitch(msg modeSwitchDoneMsg) {
	m.modeSwitching = false
	m.modeSwitchReady = false
	m.pendingMode = nil
	if msg.err != nil {
		m.lastStatus = "mode switch failed"
		if m.app != nil && m.app.Agent != nil && !m.app.Agent.IsRunning() {
			// A queued goal boundary can optimistically report continuation. If
			// mode persistence fails after stop/join, reconcile chrome with the
			// agent; the core restarts any still-eligible automatic goal.
			m.busy = false
			m.toolRunning = false
			m.runStartedAt = time.Time{}
		}
		m.pushLine(styleError.Render("mode: " + msg.err.Error()))
		return
	}
	if m.app == nil || m.app.Agent == nil {
		m.lastStatus = "mode switch failed"
		m.pushLine(styleError.Render("mode: app unavailable"))
		return
	}
	actual := m.app.Agent.Mode()
	if actual != msg.target {
		m.lastStatus = "mode switch failed"
		m.pushLine(styleError.Render(fmt.Sprintf("mode: persisted %s, wanted %s", actual, msg.target)))
		return
	}
	if actual == protocol.ModeDefault {
		// A plan completion and its queued boundary switch travel through
		// separate Bubble Tea message paths. Never leave the implementation
		// picker modal after the switch has made it inapplicable.
		m.planPrompt = false
	}
	if actual == protocol.ModePlan {
		// A goal turn can advertise continuation just before the queued mode
		// command stops the automatic worker. SetMode joins that worker, so no
		// later turn_done is guaranteed to clear the optimistic busy flag.
		m.busy = false
		m.toolRunning = false
		m.runStartedAt = time.Time{}
	}
	m.lastStatus = "mode " + string(actual)
}

func (m *Model) collaborationModeLabel() string {
	if m.app == nil || m.app.Agent == nil {
		return string(protocol.ModeDefault)
	}
	current := m.app.Agent.Mode()
	if m.pendingMode != nil && *m.pendingMode != current {
		return string(current) + "→" + string(*m.pendingMode)
	}
	return string(current)
}
