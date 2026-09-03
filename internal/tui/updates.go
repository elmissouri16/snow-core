package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/internal/app"
)

const (
	updateCheckTimeout   = 20 * time.Second
	updateInstallTimeout = 5 * time.Minute
)

type updateCheckReason uint8

const (
	updateCheckStartup updateCheckReason = iota + 1
	updateCheckManual
	updateCheckBeforeInstall
)

type updateCheckDoneMsg struct {
	generation uint64
	reason     updateCheckReason
	status     app.UpdateStatus
	err        error
}

type updateInstallDoneMsg struct {
	generation uint64
	manual     bool
	result     app.UpdateResult
	err        error
}

func (m *Model) startUpdateCheck(reason updateCheckReason) tea.Cmd {
	if m.app == nil || m.updateCheckRunning || m.updateInstallRunning {
		return nil
	}
	m.updateGeneration++
	generation := m.updateGeneration
	m.updateCheckReason = reason
	m.updateCheckRunning = true
	m.updateLastError = ""
	if reason != updateCheckStartup {
		m.settingsError = ""
		m.settingsStatus = "checking for updates…"
	}
	check := m.checkForUpdate
	if check == nil {
		check = m.app.CheckForUpdate
	}
	ctx := m.ctx
	return func() tea.Msg {
		checkCtx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
		defer cancel()
		status, err := check(checkCtx)
		return updateCheckDoneMsg{generation: generation, reason: reason, status: status, err: err}
	}
}

func (m *Model) handleUpdateCheckDone(msg updateCheckDoneMsg) tea.Cmd {
	if msg.generation != m.updateGeneration || !m.updateCheckRunning {
		return nil
	}
	m.updateCheckRunning = false
	if msg.err != nil {
		text := "update check failed: " + msg.err.Error()
		m.updateLastError = text
		m.updateLastStatus = ""
		if msg.reason != updateCheckStartup {
			m.settingsError = text
			m.settingsStatus = ""
		}
		return nil
	}

	m.updateStatus = msg.status
	m.updateChecked = true
	m.updateLastError = ""
	current := displayUpdateVersion(msg.status.CurrentVersion)
	latest := displayUpdateVersion(msg.status.LatestVersion)
	if !msg.status.Available {
		m.updateLastStatus = "Current " + current + " · latest " + latest + " · up to date"
		if msg.reason != updateCheckStartup {
			m.settingsStatus = m.updateLastStatus
			m.settingsError = ""
		}
		return nil
	}

	available := "Current " + current + " · latest " + latest + " available"
	m.updateLastStatus = available
	if msg.reason == updateCheckBeforeInstall {
		if !msg.status.Eligible {
			m.settingsStatus = ""
			m.settingsError = updateUnavailableMessage(msg.status)
			return nil
		}
		return m.startUpdateInstall(msg.status, true)
	}
	if msg.reason == updateCheckManual {
		m.settingsStatus = available
		m.settingsError = ""
		return nil
	}

	if m.app.Cfg.Updates.AutoUpdate && msg.status.Eligible {
		m.updateAutoInstallPending = true
		return m.maybeStartPendingAutoUpdate()
	}
	m.lastStatus = available + " · open /settings to update"
	return nil
}

func (m *Model) startUpdateInstall(status app.UpdateStatus, manual bool) tea.Cmd {
	if m.app == nil || m.updateCheckRunning || m.updateInstallRunning || !status.Available {
		return nil
	}
	if !status.Eligible {
		m.settingsError = updateUnavailableMessage(status)
		m.settingsStatus = ""
		return nil
	}
	m.updateGeneration++
	generation := m.updateGeneration
	m.updateInstallRunning = true
	m.updateAutoInstallPending = false
	m.updateLastError = ""
	m.settingsError = ""
	m.settingsStatus = "installing " + displayUpdateVersion(status.LatestVersion) + "…"
	install := m.installUpdate
	if install == nil {
		install = m.app.InstallUpdate
	}
	ctx := m.ctx
	return func() tea.Msg {
		installCtx, cancel := context.WithTimeout(ctx, updateInstallTimeout)
		defer cancel()
		result, err := install(installCtx, status)
		return updateInstallDoneMsg{generation: generation, manual: manual, result: result, err: err}
	}
}

func (m *Model) handleUpdateInstallDone(msg updateInstallDoneMsg) {
	if msg.generation != m.updateGeneration || !m.updateInstallRunning {
		return
	}
	m.updateInstallRunning = false
	if msg.err != nil {
		text := "update install failed: " + msg.err.Error()
		m.updateLastError = text
		m.updateLastStatus = ""
		m.settingsError = text
		m.settingsStatus = ""
		return
	}
	m.updateInstalledVersion = msg.result.InstalledVersion
	if m.updateInstalledVersion == "" {
		m.updateInstalledVersion = m.updateStatus.LatestVersion
	}
	m.updateStatus.Available = false
	m.updateLastError = ""
	m.updateLastStatus = "Snow " + displayUpdateVersion(m.updateInstalledVersion) + " installed; restart required"
	m.settingsError = ""
	m.settingsStatus = m.updateLastStatus
	m.lastStatus = m.updateLastStatus
	m.restartChoice = 0
	m.restartPromptPending = true
	if msg.manual {
		m.pickSettings = false
	}
}

func (m *Model) maybeStartPendingAutoUpdate() tea.Cmd {
	if !m.updateAutoInstallPending || m.app == nil || m.updateCheckRunning || m.updateInstallRunning || m.busy ||
		m.app.Agent == nil || m.app.Agent.IsRunning() {
		return nil
	}
	return m.startUpdateInstall(m.updateStatus, false)
}

func (m *Model) anyUpdateBlockingModalVisible() bool {
	return m.trustPending || m.permPending || m.userInputPending || m.loginModalVisible() || m.modelModalVisible() ||
		m.thinkingModalVisible() || m.keybindingsModalVisible() || m.settingsModalVisible() || m.helpModalVisible() ||
		m.processFleetOpen || m.subagentFleetOpen || m.confirmGoalReplace || m.planPrompt || m.pickFork ||
		m.pickSession || m.pickTree || m.pickInfo || m.pickPermissionMode || m.sessionOpLoading
}

func (m *Model) restartPromptVisible() bool {
	return m.restartPromptPending && !m.busy && m.app != nil && m.app.Agent != nil && !m.app.Agent.IsRunning() &&
		!m.anyUpdateBlockingModalVisible()
}

func (m *Model) handleRestartPromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.restartChoice = (m.restartChoice + 1) % 2
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.restartChoice = (m.restartChoice + 1) % 2
	case tea.KeyEsc:
		m.dismissRestartPrompt()
	case tea.KeyEnter:
		if m.restartChoice == 1 {
			m.dismissRestartPrompt()
			return m, nil
		}
		m.restartPromptPending = false
		m.restartRequested = true
		return m, m.quitCmd()
	}
	return m, nil
}

func (m *Model) dismissRestartPrompt() {
	m.restartPromptPending = false
	m.restartChoice = 0
	m.lastStatus = "Snow " + displayUpdateVersion(m.updateInstalledVersion) + " installed; restart Snow to use it"
}

func (m *Model) renderRestartPrompt() string {
	if !m.restartPromptVisible() {
		return ""
	}
	geometry := m.pickerCardGeometry()
	header := renderPickerCardHeader("Update installed", "restart required", geometry.innerWidth)
	version := displayUpdateVersion(m.updateInstalledVersion)
	message := "Snow " + version + " is installed. This process is still running the previous version."
	if currentSessionPath(m.app) == "" {
		message += " Restarting begins a fresh ephemeral session."
	}
	message = lipgloss.NewStyle().Width(geometry.innerWidth).Render(message)
	choices := []string{"Restart now", "Later"}
	var rows strings.Builder
	for i, choice := range choices {
		prefix, style := "  ", styleCompletion
		if i == m.restartChoice {
			prefix, style = "› ", styleCompletionSelected
		}
		rows.WriteString(style.Render(prefix + choice))
		if i+1 < len(choices) {
			rows.WriteByte('\n')
		}
	}
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	footer := styleFooter.Render(truncateDisplayText(" ↑/↓ choose · Enter confirm · Esc later ", geometry.innerWidth))
	content := lipgloss.JoinVertical(lipgloss.Left, header, message, separator, rows.String(), footer)
	return renderPickerCard(content, geometry)
}

func (m *Model) overlayRestartPrompt(frame string) string {
	return m.overlayCenteredModal(frame, m.renderRestartPrompt())
}

func (m *Model) updateActionText() string {
	current := m.currentUpdateVersion()
	latest := displayUpdateVersion(m.updateStatus.LatestVersion)
	switch {
	case m.updateInstallRunning:
		return "Update now  installing " + latest + "…"
	case m.updateCheckRunning && m.updateCheckReason == updateCheckBeforeInstall:
		return "Update now  checking from " + current + "…"
	case m.updateInstalledVersion != "":
		return "Update installed  " + displayUpdateVersion(m.updateInstalledVersion) + " · restart required"
	case !m.updateChecked:
		return "Update now  current " + current + " · check first"
	case !m.updateStatus.Available:
		return "Update now  " + current + " is latest"
	case !m.updateStatus.Eligible:
		return "Update now  " + current + " → " + latest + " · unavailable"
	default:
		return "Update now  " + current + " → " + latest
	}
}

func (m *Model) currentUpdateVersion() string {
	if m.updateStatus.CurrentVersion != "" {
		return displayUpdateVersion(m.updateStatus.CurrentVersion)
	}
	if m.app != nil {
		return displayUpdateVersion(m.app.BuildVersion)
	}
	return "unknown"
}

func (m *Model) updateActionAvailable() bool {
	return !m.updateCheckRunning && !m.updateInstallRunning && m.updateInstalledVersion == ""
}

func updateUnavailableMessage(status app.UpdateStatus) string {
	if reason := strings.TrimSpace(status.Reason); reason != "" {
		return "self-update unavailable: " + reason
	}
	return "self-update unavailable for this build or executable"
}

func displayUpdateVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "unknown"
	}
	if strings.HasPrefix(version, "v") || version[0] < '0' || version[0] > '9' {
		return version
	}
	return fmt.Sprintf("v%s", version)
}
