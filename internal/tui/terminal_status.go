package tui

import (
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const terminalProgressInterval = time.Second

type terminalActivity uint8

const (
	terminalIdle terminalActivity = iota
	terminalRunning
	terminalCompacting
	terminalPermission
	terminalInput
	terminalDone
	terminalFailed
	terminalAborted
)

var terminalActivityNames = [...]string{
	"Ready", "Running", "Compacting", "Waiting for approval", "Waiting for input", "Done", "Failed", "Stopped",
}

// Immutable values avoid allocating a progress bar on every streamed frame.
var terminalProgressBars = [...]tea.ProgressBar{
	{State: tea.ProgressBarNone},
	{State: tea.ProgressBarIndeterminate},
	{State: tea.ProgressBarIndeterminate},
	{State: tea.ProgressBarWarning},
	{State: tea.ProgressBarWarning},
	{State: tea.ProgressBarNone},
	{State: tea.ProgressBarError},
	{State: tea.ProgressBarNone},
}

type terminalAttention struct {
	permission   *protocol.PermissionRequest
	permissionID string
	inputID      string
	kind         terminalActivity
}

type terminalStatus struct {
	compaction     terminalCompactionSettlement
	unfocused      bool
	failed         bool
	failureAlerted bool
	failurePending bool
	outcome        terminalActivity
	attention      terminalAttention
	alert          string
	alertActivity  terminalActivity
	alertID        uint64
	alertScheduled bool
	heartbeat      bool
	generation     uint64
	titleCWD       string
	titleActivity  terminalActivity
	title          string
	view           tea.View
	viewReady      bool
	reuseView      bool
	settled        terminalSettlement
	hasSettlement  bool
}

type terminalSettlement struct {
	id         string
	epoch      uint64
	sequence   uint64
	generation uint64 // fallback for legacy events without a turn ID
}

type terminalProgressTick uint64
type terminalAlertMsg uint64
type terminalAlertOutput string
type terminalProgressOutput struct {
	sequence   string
	generation uint64
}

func (o terminalProgressOutput) String() string { return o.sequence }

func (m *Model) terminalActivity() terminalActivity {
	switch {
	case m.permPending:
		return terminalPermission
	case m.userInputPending || m.planPrompt || m.trustPending:
		return terminalInput
	case m.lastErr != nil || m.terminal.failed:
		return terminalFailed
	case m.compacting:
		return terminalCompacting
	case m.busy:
		return terminalRunning
	default:
		return m.terminal.outcome
	}
}

func (m *Model) applyTerminalStatus(view *tea.View) {
	if m.app == nil {
		return
	}
	activity := m.terminalActivity()
	if m.app.Cfg.TUI.TerminalTitle {
		cwd := m.app.CWD()
		if m.terminal.title == "" || cwd != m.terminal.titleCWD || activity != m.terminal.titleActivity {
			m.terminal.title = terminalWindowTitle(cwd, activity)
			m.terminal.titleCWD, m.terminal.titleActivity = cwd, activity
		}
		view.WindowTitle = m.terminal.title
	}
	if m.app.Cfg.TUI.TerminalProgress {
		view.ProgressBar = &terminalProgressBars[activity]
	}
}

func terminalWindowTitle(cwd string, activity terminalActivity) string {
	project := truncateRunes(sanitizeTerminalLine(filepath.Base(cwd)), 60)
	return "Snow · " + project + " · " + terminalActivityNames[activity]
}

func (m *Model) resetTerminalRun() {
	m.terminal.failed = false
	m.terminal.failureAlerted = false
	m.terminal.failurePending = false
	m.terminal.outcome = terminalIdle
	m.terminal.alert = ""
	m.terminal.alertID++
	m.terminal.alertScheduled = false
}

// Called only after the normal reducer has rejected stale root events. Child
// completions and goal continuation boundaries must not announce root success.
func (m *Model) observeTerminalEvent(ev protocol.AgentEvent) {
	if ev.Agent != nil || ev.Snapshot {
		return
	}
	switch ev.Type {
	case protocol.EvCompactionDone:
		if ev.Compaction == nil || !ev.Compaction.Automatic {
			m.fenceTerminalCompaction(ev.TurnID, ev.RootEpoch, ev.TurnSequence)
			m.settleTerminalCompaction(ev.TurnID, ev.RootEpoch, ev.IsError, m.abortNoticePending)
		}
	case protocol.EvError:
		m.terminal.failed = true
		m.terminal.failurePending = true
		if !m.busy {
			m.settleTerminalFailure()
		}
	case protocol.EvTurnDone:
		settlement := m.terminalEventSettlement(ev)
		if m.terminal.hasSettlement && m.terminal.settled == settlement {
			return
		}
		m.terminal.settled, m.terminal.hasSettlement = settlement, true
		// The core may publish turn_done after aborted to settle bookkeeping.
		// Cancellation is not a successful completion notification.
		if m.terminal.outcome == terminalAborted {
			return
		}
		if ev.GoalContinuing {
			m.resetTerminalRun()
			return
		}
		if m.terminal.failed {
			m.settleTerminalFailure()
		} else {
			m.terminal.outcome = terminalDone
			m.queueTerminalAlert("Snow finished")
		}
	case protocol.EvAborted:
		m.resetTerminalRun()
		m.terminal.settled, m.terminal.hasSettlement = m.terminalEventSettlement(ev), true
		m.terminal.outcome = terminalAborted
	}
}

// A goal worker may fail between turns, when no turn_done follows. Consume
// failure attention at its terminal goal boundary, even if focus or settings
// suppress delivery. Repeated snapshots must not announce it later.
func (m *Model) settleTerminalFailure() {
	if !m.terminal.failurePending || m.terminal.failureAlerted || m.abortNoticePending || m.terminal.outcome == terminalAborted {
		return
	}
	m.terminal.failureAlerted = true
	m.terminal.failurePending = false
	m.terminal.outcome = terminalFailed
	m.queueTerminalAlert("Snow encountered an error")
}

func (m *Model) terminalEventSettlement(ev protocol.AgentEvent) terminalSettlement {
	settlement := terminalSettlement{id: ev.TurnID, epoch: ev.RootEpoch, sequence: ev.TurnSequence}
	if ev.TurnID == "" {
		settlement.generation = m.runGeneration
	}
	return settlement
}

func (m *Model) trackTerminalFocus(msg tea.Msg) {
	switch msg.(type) {
	case tea.BlurMsg:
		m.terminal.unfocused = true
	case tea.FocusMsg, tea.KeyPressMsg, tea.PasteMsg, tea.MouseClickMsg:
		m.terminal.unfocused = false
		if !m.busy && !m.compacting {
			m.terminal.outcome = terminalIdle
			m.terminal.failed = false
		}
	}
}

func (m *Model) terminalAlertsAllowed() bool {
	if m.app == nil {
		return false
	}
	switch m.app.Cfg.TUI.Notifications {
	case "always":
		return true
	case "unfocused":
		return m.terminal.unfocused
	default:
		return false
	}
}

func (m *Model) queueTerminalAlert(text string) {
	if !m.terminalAlertsAllowed() {
		return
	}
	m.terminal.alertID++
	m.terminal.alert = text
	m.terminal.alertActivity = terminalIdle
	m.terminal.alertScheduled = false
}

func (m *Model) terminalNeedsHeartbeat() bool {
	return m.app != nil && m.app.Cfg.TUI.TerminalProgress &&
		(m.busy || m.compacting || m.permPending || m.userInputPending || m.planPrompt)
}

func (m *Model) syncTerminalStatus() tea.Cmd {
	attention := terminalAttention{}
	switch {
	case m.permPending:
		attention.kind, attention.permission = terminalPermission, m.permRequest
		if m.permRequest != nil && m.permRequest.ID != "" {
			attention.permissionID, attention.permission = m.permRequest.ID, nil
		}
	case m.userInputPending:
		attention.kind = terminalInput
		if m.userInputRequest != nil {
			attention.inputID = m.userInputRequest.ID
		}
	case m.planPrompt:
		attention.kind = terminalInput
	}
	if attention != m.terminal.attention {
		m.terminal.attention = attention
		if attention.kind != terminalIdle {
			m.queueTerminalAlert("Snow: " + terminalActivityNames[attention.kind])
			m.terminal.alertActivity = attention.kind
		}
	}
	var heartbeat, alert tea.Cmd
	if active := m.terminalNeedsHeartbeat(); active != m.terminal.heartbeat {
		m.terminal.generation++
		m.terminal.heartbeat = active
		if active {
			heartbeat = m.terminalProgressTick()
		}
	}
	if m.terminal.alert != "" && !m.terminal.alertScheduled {
		m.terminal.alertScheduled = true
		id := m.terminal.alertID
		alert = func() tea.Msg { return terminalAlertMsg(id) }
	}
	return tea.Batch(heartbeat, alert)
}

func (m *Model) terminalProgressTick() tea.Cmd {
	ctx, generation := m.ctx, m.terminal.generation
	return func() tea.Msg {
		timer := time.NewTimer(terminalProgressInterval)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return terminalProgressTick(generation)
		}
	}
}

// The filter runs on Bubble Tea's event goroutine, immediately before RawMsg
// output. Derive the sequence here so an idle transition, new turn, focus change,
// or settings change cannot be overtaken by stale asynchronous commands.
func terminalStatusFilter(model tea.Model, msg tea.Msg) tea.Msg {
	m, ok := model.(*Model)
	if !ok {
		return msg
	}
	switch msg := msg.(type) {
	case terminalProgressTick:
		if uint64(msg) != m.terminal.generation || !m.terminal.heartbeat || !m.terminalNeedsHeartbeat() {
			return nil
		}
		sequence := ansi.SetIndeterminateProgressBar
		switch m.terminalActivity() {
		case terminalPermission, terminalInput:
			sequence = ansi.SetWarningProgressBar(0)
		case terminalFailed:
			sequence = ansi.SetErrorProgressBar(0)
		}
		return tea.RawMsg{Msg: terminalProgressOutput{sequence: sequence, generation: uint64(msg)}}
	case terminalAlertMsg:
		if uint64(msg) != m.terminal.alertID || m.terminal.alert == "" {
			return nil
		}
		text := m.terminal.alert
		m.terminal.alert, m.terminal.alertScheduled = "", false
		if !m.terminalAlertsAllowed() || (m.terminal.alertActivity != terminalIdle && m.terminalActivity() != m.terminal.alertActivity) {
			return nil
		}
		// Generic text only: no prompts, paths, tool arguments, or error details
		// enter desktop notification history. BEL requests terminal attention.
		return tea.RawMsg{Msg: terminalAlertOutput("\a" + ansi.Notify(text))}
	}
	return msg
}

// Raw output is serialized by Bubble Tea. Reuse the last complete view for
// these transport-only messages instead of rebuilding the transcript/composer.
// Every ordinary Update invalidates this shortcut, including resize and focus.
func (m *Model) updateTerminalOutput(msg tea.Msg) (bool, tea.Cmd) {
	raw, ok := msg.(tea.RawMsg)
	if !ok {
		return false, nil
	}
	switch output := raw.Msg.(type) {
	case terminalProgressOutput:
		m.terminal.reuseView = true
		if output.generation == m.terminal.generation && m.terminal.heartbeat {
			return true, m.terminalProgressTick()
		}
		return true, nil
	case terminalAlertOutput:
		m.terminal.reuseView = true
		return true, nil
	default:
		return false, nil
	}
}
