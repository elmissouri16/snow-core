package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/supervisor"
	"github.com/snow-core/snow/internal/trust"
	"github.com/snow-core/snow/pkg/protocol"
)

const (
	worktreeMaxActivityRows  = 128
	worktreeMaxActivityBytes = 32 * 1024
	worktreeMaxLiveBytes     = 64 * 1024
)

func (m *Model) worktreeSidebarVisible() bool {
	// Worktree management owns a clean dashboard frame instead of permanently
	// squeezing the conversation into a narrow split pane.
	return false
}

func (m *Model) frameOwnedOverlay() bool {
	return m.trustPending || m.subagentFleetOpen || m.pickProvider || m.pickChatGPTAuth || m.pickModel ||
		m.pickThinking || m.pickSettings || m.sandboxSetup || m.pickSession || m.pickTree || m.pickInfo ||
		m.pickPermissionMode || m.permPending || m.userInputPending || m.confirmGoalReplace || m.planPrompt ||
		m.loginMode || m.loginProfileMode || m.loginEndpointMode || m.compVisible || m.skillVisible ||
		m.mentionVisible || m.mentionLoading
}

func (m *Model) worktreeSidebarWidth() int {
	return min(36, max(24, m.managedFrameWidth()/4))
}

func (m *Model) primaryFrameWidth() int {
	width := m.managedFrameWidth()
	if m.worktreeSidebarVisible() {
		width -= m.worktreeSidebarWidth() + 1
	}
	return max(1, width)
}

func (m *Model) selectedWorkspace() *app.WorktreeWorkspace {
	if len(m.worktreeWorkspaces) == 0 {
		return nil
	}
	m.worktreeIndex = min(max(0, m.worktreeIndex), len(m.worktreeWorkspaces)-1)
	return &m.worktreeWorkspaces[m.worktreeIndex]
}

func (m *Model) workerForWorkspace(workspaceID string) (supervisor.WorkerState, bool) {
	var selected supervisor.WorkerState
	found := false
	for _, worker := range m.worktreeWorkers {
		if worker.WorkspaceID != workspaceID {
			continue
		}
		if !found || worktreeWorkerRank(worker) > worktreeWorkerRank(selected) ||
			(worktreeWorkerRank(worker) == worktreeWorkerRank(selected) && (worker.ProcessGeneration > selected.ProcessGeneration ||
				(worker.ProcessGeneration == selected.ProcessGeneration && worker.ID < selected.ID))) {
			selected = worker
			found = true
		}
	}
	return selected, found
}

func worktreeWorkerRank(worker supervisor.WorkerState) int {
	switch worker.ProcessStatus {
	case supervisor.ProcessReady:
		return 5
	case supervisor.ProcessStarting:
		return 4
	case supervisor.ProcessStopping:
		return 3
	case supervisor.ProcessCrashed:
		return 2
	default:
		return 1
	}
}

func (m *Model) selectedWorktreeWorker() (supervisor.WorkerState, bool) {
	workspace := m.selectedWorkspace()
	if workspace == nil || workspace.Current {
		return supervisor.WorkerState{}, false
	}
	return m.workerForWorkspace(workspace.ID)
}

func (m *Model) selectedRemoteWorkspace() (*app.WorktreeWorkspace, supervisor.WorkerState, bool) {
	workspace := m.selectedWorkspace()
	if workspace == nil || workspace.Current {
		return workspace, supervisor.WorkerState{}, false
	}
	worker, managed := m.workerForWorkspace(workspace.ID)
	return workspace, worker, managed
}

func (m *Model) startWorktreeInventory() tea.Cmd {
	if m.app == nil {
		return nil
	}
	m.worktreeGeneration++
	generation := m.worktreeGeneration
	attached := m.app
	m.worktreeLoading = true
	return func() tea.Msg {
		workspaces, err := attached.WorktreeWorkspaces(m.ctx)
		return worktreeInventoryMsg{generation: generation, workspaces: workspaces, err: err}
	}
}

func waitForWorktreeSupervisor(ctx context.Context, events <-chan supervisor.Event) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case event := <-events:
			return worktreeSupervisorMsg{event: event}
		case <-ctx.Done():
			return nil
		}
	}
}

func (m *Model) applyWorktreeInventory(msg worktreeInventoryMsg) {
	if msg.generation != m.worktreeGeneration {
		return
	}
	m.worktreeLoading = false
	if msg.err != nil {
		m.worktreeError = msg.err.Error()
		return
	}
	selectedID := ""
	if selected := m.selectedWorkspace(); selected != nil {
		selectedID = selected.ID
	}
	m.worktreeWorkspaces = append([]app.WorktreeWorkspace(nil), msg.workspaces...)
	if m.app != nil && m.app.Supervisor != nil {
		for _, state := range m.app.Supervisor.List() {
			current, ok := m.worktreeWorkers[state.ID]
			if !ok || state.ProcessGeneration >= current.ProcessGeneration {
				m.worktreeWorkers[state.ID] = state
			}
		}
	}
	m.worktreeIndex = 0
	for i := range m.worktreeWorkspaces {
		if m.worktreeWorkspaces[i].ID == selectedID {
			m.worktreeIndex = i
			break
		}
	}
	m.worktreeError = ""
}

func (m *Model) applyWorktreeSupervisorEvent(event supervisor.Event) {
	if event.State != nil {
		current, ok := m.worktreeWorkers[event.WorkerID]
		if !ok || event.Generation >= current.ProcessGeneration {
			m.worktreeWorkers[event.WorkerID] = *event.State
			if event.State.UserInput != nil {
				m.prepareWorktreeInput(event.WorkerID, event.State.UserInput.ID)
			} else if m.worktreeInputWorker == event.WorkerID {
				m.resetWorktreeInput()
			}
			if event.State.TurnStatus == supervisor.TurnIdle {
				delete(m.worktreeLiveText, event.WorkerID)
				delete(m.worktreeLiveThinking, event.WorkerID)
			}
		}
	}
	if event.Agent != nil {
		current, ok := m.worktreeWorkers[event.WorkerID]
		if ok && event.Generation < current.ProcessGeneration {
			return
		}
		switch event.Agent.Type {
		case protocol.EvTextDelta:
			m.worktreeLiveText[event.WorkerID] = appendBoundedText(m.worktreeLiveText[event.WorkerID], event.Agent.Text, worktreeMaxLiveBytes)
		case protocol.EvThinkingDelta:
			m.worktreeLiveThinking[event.WorkerID] = appendBoundedText(m.worktreeLiveThinking[event.WorkerID], event.Agent.Text, worktreeMaxLiveBytes)
			m.appendWorktreeActivity(event.WorkerID, "thinking: "+event.Agent.Text)
		case protocol.EvToolStart:
			m.appendWorktreeActivity(event.WorkerID, "tool start: "+event.Agent.ToolName)
		case protocol.EvToolProgress:
			m.appendWorktreeActivity(event.WorkerID, "tool: "+event.Agent.Message)
		case protocol.EvToolEnd:
			status := "done"
			if event.Agent.IsError {
				status = "failed"
			}
			m.appendWorktreeActivity(event.WorkerID, fmt.Sprintf("tool %s: %s", event.Agent.ToolName, status))
		case protocol.EvUsage:
			if event.Agent.Usage != nil {
				m.appendWorktreeActivity(event.WorkerID, fmt.Sprintf("usage: %d tokens", event.Agent.Usage.Total))
			}
		case protocol.EvError:
			m.appendWorktreeActivity(event.WorkerID, "error: "+event.Agent.Message)
		case protocol.EvPermissionRequest, protocol.EvUserInputRequest:
			m.selectWorktreeWorker(event.WorkerID)
			m.worktreeSidebarRequested = true
			m.worktreeInspectorOpen = true
			m.worktreeSidebarFocus = true
		}
	}
}

func (m *Model) prepareWorktreeInput(worker supervisor.WorkerID, requestID string) {
	if m.worktreeInputWorker == worker && m.worktreeInputRequest == requestID {
		return
	}
	m.worktreeInputWorker = worker
	m.worktreeInputRequest = requestID
	m.worktreeInputIndex = 0
	m.worktreeInputOption = 0
	m.worktreeInputAnswers = make(map[string]string)
	m.worktreeInputEditor.Reset()
	m.worktreeInputEditor.Focus()
	m.editor.Blur()
}

func (m *Model) resetWorktreeInput() {
	m.worktreeInputWorker = ""
	m.worktreeInputRequest = ""
	m.worktreeInputIndex = 0
	m.worktreeInputOption = 0
	m.worktreeInputAnswers = make(map[string]string)
	m.worktreeInputEditor.Reset()
	m.worktreeInputEditor.Blur()
	if !m.worktreeSidebarFocus && !m.worktreeInspectorOpen {
		m.editor.Focus()
	}
}

func appendBoundedText(existing, delta string, limit int) string {
	value := existing + delta
	if len(value) > limit {
		value = value[len(value)-limit:]
		for len(value) > 0 && value[0]&0xc0 == 0x80 {
			value = value[1:]
		}
	}
	return value
}

func (m *Model) appendWorktreeActivity(id supervisor.WorkerID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	rows := append(m.worktreeActivity[id], text)
	for len(rows) > worktreeMaxActivityRows || joinedBytes(rows) > worktreeMaxActivityBytes {
		rows = rows[1:]
	}
	m.worktreeActivity[id] = rows
}

func joinedBytes(rows []string) int {
	total := 0
	for _, row := range rows {
		total += len(row) + 1
	}
	return total
}

func (m *Model) selectWorktreeWorker(id supervisor.WorkerID) {
	worker, ok := m.worktreeWorkers[id]
	if !ok {
		return
	}
	for i := range m.worktreeWorkspaces {
		if m.worktreeWorkspaces[i].ID == worker.WorkspaceID {
			m.worktreeIndex = i
			return
		}
	}
}

func (m *Model) toggleWorktreeSidebar() tea.Cmd {
	m.worktreeInspectorOpen = !m.worktreeInspectorOpen
	m.worktreeSidebarRequested = m.worktreeInspectorOpen
	m.worktreeSidebarFocus = m.worktreeInspectorOpen
	if m.worktreeInspectorOpen {
		m.editor.Blur()
		return m.startWorktreeInventory()
	}
	m.selectCurrentWorkspace()
	m.editor.Focus()
	return nil
}

func (m *Model) openWorktreeSupervisor() tea.Cmd {
	m.worktreeInspectorOpen = true
	m.worktreeSidebarRequested = true
	m.worktreeSidebarFocus = true
	m.editor.Blur()
	return m.startWorktreeInventory()
}

func (m *Model) selectCurrentWorkspace() {
	for i := range m.worktreeWorkspaces {
		if m.worktreeWorkspaces[i].Current {
			m.worktreeIndex = i
			return
		}
	}
	m.worktreeIndex = 0
}

func (m *Model) worktreeInteractionPending() bool {
	worker, ok := m.selectedWorktreeWorker()
	return ok && (worker.Permission != nil || worker.UserInput != nil)
}

func (m *Model) handleWorktreeKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.frameOwnedOverlay() {
		return false, nil
	}
	workspace, worker, hasWorker := m.selectedRemoteWorkspace()
	if hasWorker && worker.Permission != nil {
		if keyMatches(msg, m.keys.Abort) {
			return true, m.rejectWorktreePermission(worker)
		}
		switch strings.ToLower(msg.String()) {
		case "a", "enter":
			return true, m.replyWorktreePermission(worker, permission.DecisionAllow)
		case "s":
			return true, m.replyWorktreePermission(worker, permission.DecisionAllowSession)
		case "d", "esc":
			return true, m.rejectWorktreePermission(worker)
		default:
			return true, nil
		}
	}
	if hasWorker && worker.UserInput != nil {
		return true, m.handleWorktreeInputKey(msg, worker)
	}
	if m.worktreeStartConfirm {
		return true, m.handleWorktreeStartConfirmKey(msg)
	}
	ownsFocus := m.worktreeSidebarFocus || m.worktreeInspectorOpen
	if ownsFocus {
		switch {
		case keyMatches(msg, m.keys.Close):
			m.worktreeInspectorOpen = false
			m.worktreeSidebarRequested = false
			m.worktreeSidebarFocus = false
			m.selectCurrentWorkspace()
			m.editor.Focus()
			return true, nil
		case keyMatches(msg, m.keys.PickerUp):
			if len(m.worktreeWorkspaces) > 0 {
				m.worktreeIndex = (m.worktreeIndex - 1 + len(m.worktreeWorkspaces)) % len(m.worktreeWorkspaces)
				m.resetWorktreeDetail()
			}
			return true, nil
		case keyMatches(msg, m.keys.PickerDown):
			if len(m.worktreeWorkspaces) > 0 {
				m.worktreeIndex = (m.worktreeIndex + 1) % len(m.worktreeWorkspaces)
				m.resetWorktreeDetail()
			}
			return true, nil
		case keyMatches(msg, m.keys.PageUp):
			m.worktreeDetailOffset += max(1, m.transcript.Height-1)
			return true, nil
		case keyMatches(msg, m.keys.PageDown):
			m.worktreeDetailOffset = max(0, m.worktreeDetailOffset-max(1, m.transcript.Height-1))
			return true, nil
		case keyMatches(msg, m.keys.Top):
			m.worktreeDetailOffset = int(^uint(0) >> 1)
			return true, nil
		case keyMatches(msg, m.keys.Bottom):
			m.worktreeDetailOffset = 0
			return true, nil
		case keyMatches(msg, m.keys.LineUp):
			m.worktreeDetailOffset += max(1, m.transcript.MouseWheelDelta)
			return true, nil
		case keyMatches(msg, m.keys.LineDown):
			m.worktreeDetailOffset = max(0, m.worktreeDetailOffset-max(1, m.transcript.MouseWheelDelta))
			return true, nil
		case keyMatches(msg, m.keys.Accept):
			m.worktreeInspectorOpen = false
			m.worktreeSidebarRequested = false
			m.worktreeSidebarFocus = false
			m.editor.Focus()
			return true, nil
		}
		switch strings.ToLower(msg.String()) {
		case "left", "h":
			m.worktreeTab = (m.worktreeTab + 3) % 4
			return true, m.maybeLoadWorktreeDiff()
		case "right", "l", "tab":
			m.worktreeTab = (m.worktreeTab + 1) % 4
			return true, m.maybeLoadWorktreeDiff()
		case "n":
			_, cmd := m.startForkPick()
			return true, cmd
		case "s":
			return true, m.startSelectedWorktreePreflight()
		case "r":
			return true, m.startWorktreeInventory()
		case "x":
			if hasWorker && worker.ProcessStatus == supervisor.ProcessReady && worker.TurnStatus != supervisor.TurnIdle {
				return true, m.abortWorktreeWorker(worker)
			}
			return true, nil
		case "d":
			m.worktreeTab = 3
			return true, m.maybeLoadWorktreeDiff()
		}
		return true, nil
	}

	// When a remote row is selected, the normal composer targets that worker.
	if workspace != nil && !workspace.Current {
		if m.handleWorktreeDetailScrollKey(msg) {
			return true, nil
		}
		trimmed := strings.TrimSpace(m.editor.Value())
		if keyMatches(msg, m.keys.FollowUp) && hasWorker && worker.ProcessStatus == supervisor.ProcessReady && worker.TurnStatus != supervisor.TurnIdle && trimmed != "" {
			m.editor.Reset()
			return true, m.worktreeCommand(worker, "follow_up", trimmed)
		}
		if keyMatches(msg, m.keys.Submit) && trimmed != "" {
			if !hasWorker || worker.ProcessStatus != supervisor.ProcessReady {
				m.worktreeStatus = "worker is not attached; press Ctrl+B then s to start it"
				return true, nil
			}
			m.editor.Reset()
			if worker.TurnStatus == supervisor.TurnIdle {
				return true, m.worktreeCommand(worker, "prompt", trimmed)
			}
			return true, m.worktreeCommand(worker, "steer", trimmed)
		}
		if keyMatches(msg, m.keys.Abort) && hasWorker && worker.ProcessStatus == supervisor.ProcessReady && worker.TurnStatus != supervisor.TurnIdle {
			return true, m.abortWorktreeWorker(worker)
		}
	}
	return false, nil
}

func (m *Model) handleWorktreeDetailScrollKey(msg tea.KeyMsg) bool {
	switch {
	case keyMatches(msg, m.keys.PageUp):
		m.worktreeDetailOffset += max(1, m.transcript.Height-1)
	case keyMatches(msg, m.keys.PageDown):
		m.worktreeDetailOffset = max(0, m.worktreeDetailOffset-max(1, m.transcript.Height-1))
	case keyMatches(msg, m.keys.Top):
		m.worktreeDetailOffset = int(^uint(0) >> 1)
	case keyMatches(msg, m.keys.Bottom):
		m.worktreeDetailOffset = 0
	case keyMatches(msg, m.keys.LineUp):
		m.worktreeDetailOffset += max(1, m.transcript.MouseWheelDelta)
	case keyMatches(msg, m.keys.LineDown):
		m.worktreeDetailOffset = max(0, m.worktreeDetailOffset-max(1, m.transcript.MouseWheelDelta))
	default:
		return false
	}
	return true
}

func (m *Model) resetWorktreeDetail() {
	m.worktreeTab = 0
	m.worktreeDetailOffset = 0
	m.worktreeDiff = ""
	m.worktreeStatus = ""
	m.worktreeError = ""
}

func (m *Model) startSelectedWorktreePreflight() tea.Cmd {
	workspace := m.selectedWorkspace()
	if m.app == nil || workspace == nil || workspace.Current {
		m.worktreeStatus = "select a linked worktree first"
		return nil
	}
	if len(workspace.Sessions) == 0 {
		m.worktreeStatus = "no exact-CWD Snow session; create one with n"
		return nil
	}
	m.worktreeControlGeneration++
	generation := m.worktreeControlGeneration
	workspaceID := workspace.ID
	path := workspace.Path
	m.worktreeStatus = "checking destination trust and sandbox…"
	return func() tea.Msg {
		preflight, err := m.app.PreflightWorktreeWorker(path)
		return worktreePreflightMsg{generation: generation, workspace: workspaceID, preflight: preflight, err: err}
	}
}

func (m *Model) applyWorktreePreflight(msg worktreePreflightMsg) {
	if msg.generation != m.worktreeControlGeneration {
		return
	}
	workspace := m.selectedWorkspace()
	if workspace == nil || workspace.ID != msg.workspace {
		return
	}
	if msg.err != nil {
		m.worktreeError = msg.err.Error()
		m.worktreeStatus = ""
		return
	}
	m.worktreePreflight = &msg.preflight
	m.worktreeStartConfirm = true
	m.worktreeStartSession = min(m.worktreeStartSession, max(0, len(workspace.Sessions)-1))
	m.worktreeTrustDestination = false
	m.worktreeStatus = ""
}

func (m *Model) handleWorktreeStartConfirmKey(msg tea.KeyMsg) tea.Cmd {
	workspace := m.selectedWorkspace()
	if workspace == nil || len(workspace.Sessions) == 0 {
		m.worktreeStartConfirm = false
		return nil
	}
	switch {
	case keyMatches(msg, m.keys.Close):
		m.worktreeStartConfirm = false
		m.worktreePreflight = nil
		return nil
	case keyMatches(msg, m.keys.PickerUp), msg.String() == "left":
		m.worktreeStartSession = (m.worktreeStartSession - 1 + len(workspace.Sessions)) % len(workspace.Sessions)
		return nil
	case keyMatches(msg, m.keys.PickerDown), msg.String() == "right":
		m.worktreeStartSession = (m.worktreeStartSession + 1) % len(workspace.Sessions)
		return nil
	}
	if strings.EqualFold(msg.String(), "t") && m.worktreePreflight != nil && !m.worktreePreflight.ProjectInputs {
		m.worktreeTrustDestination = !m.worktreeTrustDestination
		return nil
	}
	if keyMatches(msg, m.keys.Accept) {
		if m.worktreePreflight != nil && m.worktreePreflight.RequireSandbox && m.worktreePreflight.Shell != "vm" {
			m.worktreeError = "sandbox is required but this exact worktree has no active association"
			return nil
		}
		selected := workspace.Sessions[m.worktreeStartSession]
		workspaceCopy := *workspace
		trustDestination := m.worktreeTrustDestination
		generation := m.worktreeControlGeneration
		m.worktreeStartConfirm = false
		m.worktreeStatus = "starting managed RPC worker…"
		return func() tea.Msg {
			if trustDestination {
				if err := m.app.SetWorktreeTrust(workspaceCopy.Path, trust.LevelAllow); err != nil {
					return worktreeControlMsg{generation: generation, workspace: workspaceCopy.ID, action: "start", err: err}
				}
			}
			_, err := m.app.StartWorktreeWorker(m.ctx, workspaceCopy, selected)
			return worktreeControlMsg{generation: generation, workspace: workspaceCopy.ID, action: "start", err: err}
		}
	}
	return nil
}

func (m *Model) applyWorktreeControl(msg worktreeControlMsg) {
	if msg.generation != m.worktreeControlGeneration {
		return
	}
	workspace := m.selectedWorkspace()
	if workspace == nil || workspace.ID != msg.workspace {
		return
	}
	if msg.err != nil {
		m.worktreeError = msg.err.Error()
		m.worktreeStatus = ""
		return
	}
	m.worktreeError = ""
	m.worktreeStatus = msg.action + " complete"
	if msg.action == "input" {
		m.resetWorktreeInput()
	}
}

func (m *Model) worktreeCommand(worker supervisor.WorkerState, action, text string) tea.Cmd {
	m.worktreeControlGeneration++
	generation := m.worktreeControlGeneration
	workspace := worker.WorkspaceID
	return func() tea.Msg {
		var err error
		switch action {
		case "prompt":
			err = m.app.Supervisor.Prompt(m.ctx, worker.ID, text)
		case "steer":
			err = m.app.Supervisor.Steer(m.ctx, worker.ID, text)
		case "follow_up":
			err = m.app.Supervisor.FollowUp(m.ctx, worker.ID, text)
		}
		return worktreeControlMsg{generation: generation, workspace: workspace, action: action, err: err}
	}
}

func (m *Model) abortWorktreeWorker(worker supervisor.WorkerState) tea.Cmd {
	m.worktreeControlGeneration++
	generation := m.worktreeControlGeneration
	return func() tea.Msg {
		err := m.app.Supervisor.Abort(m.ctx, worker.ID)
		return worktreeControlMsg{generation: generation, workspace: worker.WorkspaceID, action: "interrupt", err: err}
	}
}

func (m *Model) replyWorktreePermission(worker supervisor.WorkerState, decision permission.Decision) tea.Cmd {
	requestID := worker.Permission.ID
	m.worktreeControlGeneration++
	generation := m.worktreeControlGeneration
	return func() tea.Msg {
		err := m.app.Supervisor.ReplyPermission(m.ctx, worker.ID, requestID, decision)
		return worktreeControlMsg{generation: generation, workspace: worker.WorkspaceID, action: "permission", err: err}
	}
}

func (m *Model) rejectWorktreePermission(worker supervisor.WorkerState) tea.Cmd {
	requestID := worker.Permission.ID
	m.worktreeControlGeneration++
	generation := m.worktreeControlGeneration
	return func() tea.Msg {
		err := m.app.Supervisor.RejectPermission(m.ctx, worker.ID, requestID)
		return worktreeControlMsg{generation: generation, workspace: worker.WorkspaceID, action: "permission", err: err}
	}
}

func (m *Model) handleWorktreeInputKey(msg tea.KeyMsg, worker supervisor.WorkerState) tea.Cmd {
	request := worker.UserInput
	if request == nil || len(request.Questions) == 0 {
		return nil
	}
	m.worktreeInputIndex = min(max(0, m.worktreeInputIndex), len(request.Questions)-1)
	question := request.Questions[m.worktreeInputIndex]
	if keyMatches(msg, m.keys.Close) || keyMatches(msg, m.keys.Abort) {
		m.worktreeControlGeneration++
		generation := m.worktreeControlGeneration
		return func() tea.Msg {
			err := m.app.Supervisor.RejectUserInput(m.ctx, worker.ID, request.ID)
			return worktreeControlMsg{generation: generation, workspace: worker.WorkspaceID, action: "input", err: err}
		}
	}
	if len(question.Options) > 0 {
		switch {
		case keyMatches(msg, m.keys.PickerUp):
			m.worktreeInputOption = (m.worktreeInputOption - 1 + len(question.Options)) % len(question.Options)
			return nil
		case keyMatches(msg, m.keys.PickerDown):
			m.worktreeInputOption = (m.worktreeInputOption + 1) % len(question.Options)
			return nil
		}
	}
	if keyMatches(msg, m.keys.Accept) {
		answer := strings.TrimSpace(m.worktreeInputEditor.Value())
		if len(question.Options) > 0 {
			m.worktreeInputOption = min(m.worktreeInputOption, len(question.Options)-1)
			answer = question.Options[m.worktreeInputOption].Label
		}
		if answer == "" {
			m.worktreeStatus = "answer is required"
			return nil
		}
		m.worktreeInputAnswers[question.ID] = answer
		m.worktreeInputEditor.Reset()
		m.worktreeInputOption = 0
		if m.worktreeInputIndex+1 < len(request.Questions) {
			m.worktreeInputIndex++
			return nil
		}
		answers := make([]protocol.UserInputAnswer, 0, len(request.Questions))
		for _, item := range request.Questions {
			answers = append(answers, protocol.UserInputAnswer{QuestionID: item.ID, Answer: m.worktreeInputAnswers[item.ID]})
		}
		response := protocol.UserInputResponse{RequestID: request.ID, Answers: answers}
		m.worktreeControlGeneration++
		generation := m.worktreeControlGeneration
		return func() tea.Msg {
			err := m.app.Supervisor.ReplyUserInput(m.ctx, worker.ID, response)
			return worktreeControlMsg{generation: generation, workspace: worker.WorkspaceID, action: "input", err: err}
		}
	}
	if len(question.Options) == 0 {
		var cmd tea.Cmd
		m.worktreeInputEditor, cmd = m.worktreeInputEditor.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) maybeLoadWorktreeDiff() tea.Cmd {
	if m.worktreeTab != 3 || m.app == nil {
		return nil
	}
	workspace := m.selectedWorkspace()
	if workspace == nil || workspace.Current {
		return nil
	}
	m.worktreeControlGeneration++
	generation := m.worktreeControlGeneration
	workspaceID := workspace.ID
	path := workspace.Path
	m.worktreeDiff = "loading Git status…"
	return func() tea.Msg {
		text, err := m.app.WorktreeDiffSummary(m.ctx, path)
		return worktreeDiffMsg{generation: generation, workspace: workspaceID, text: text, err: err}
	}
}

func (m *Model) applyWorktreeDiff(msg worktreeDiffMsg) {
	if msg.generation != m.worktreeControlGeneration {
		return
	}
	workspace := m.selectedWorkspace()
	if workspace == nil || workspace.ID != msg.workspace {
		return
	}
	if msg.err != nil {
		m.worktreeDiff = ""
		m.worktreeError = msg.err.Error()
		return
	}
	m.worktreeDiff = msg.text
}

func (m *Model) handleWorktreeSidebarMouse(msg tea.MouseMsg, modal bool) bool {
	event := tea.MouseEvent(msg)
	listWidth := m.worktreeSidebarWidth()
	if modal && m.managedFrameWidth() >= 70 {
		listWidth = min(40, max(26, m.managedFrameWidth()/4))
	}
	if !modal && event.X >= listWidth+1 {
		return false
	}
	if modal && m.managedFrameWidth() >= 70 && event.X >= listWidth+1 {
		return false
	}
	if modal && m.managedFrameWidth() < 70 {
		bodyHeight := max(1, m.managedFrameHeight()-2)
		listHeight := min(max(6, len(m.worktreeWorkspaces)*2+3), max(6, bodyHeight/3))
		if event.Y >= listHeight+1 {
			return false
		}
	}
	if event.Button == tea.MouseButtonWheelUp && len(m.worktreeWorkspaces) > 0 {
		m.worktreeIndex = (m.worktreeIndex - 1 + len(m.worktreeWorkspaces)) % len(m.worktreeWorkspaces)
		m.resetWorktreeDetail()
		return true
	}
	if event.Button == tea.MouseButtonWheelDown && len(m.worktreeWorkspaces) > 0 {
		m.worktreeIndex = (m.worktreeIndex + 1) % len(m.worktreeWorkspaces)
		m.resetWorktreeDetail()
		return true
	}
	if event.Action == tea.MouseActionPress && event.Button == tea.MouseButtonLeft {
		rowStart := 1
		if modal {
			rowStart = 3
		}
		if event.Y < rowStart {
			return true
		}
		index := (event.Y - rowStart) / 2
		if index >= 0 && index < len(m.worktreeWorkspaces) {
			m.worktreeIndex = index
			m.worktreeSidebarFocus = true
			m.editor.Blur()
			m.resetWorktreeDetail()
		}
		return true
	}
	return true
}

func (m *Model) handleWorktreeDetailMouse(msg tea.MouseMsg) {
	event := tea.MouseEvent(msg)
	delta := max(1, m.transcript.MouseWheelDelta)
	switch event.Button {
	case tea.MouseButtonWheelUp:
		m.worktreeDetailOffset += delta
	case tea.MouseButtonWheelDown:
		m.worktreeDetailOffset = max(0, m.worktreeDetailOffset-delta)
	}
}

func (m *Model) renderWorktreeSidebar(width, height int) string {
	width = max(16, width)
	height = max(1, height)
	rowWidth := max(1, width-2)
	lines := []string{styleHeaderDim.Render(fmt.Sprintf(" Agents  %d", len(m.worktreeWorkspaces))), ""}
	if m.worktreeLoading {
		lines = append(lines, styleHeaderDim.Render("  Loading worktrees…"))
	}
	if len(m.worktreeWorkspaces) == 0 && !m.worktreeLoading {
		lines = append(lines, styleHeaderDim.Render("  No linked worktrees"), "", styleFooter.Render("  Press r to refresh"))
	}
	selectedStyle := styleCompletionSelected.Copy().Background(styleComposer.GetBackground()).Width(rowWidth)
	selectedMeta := styleHeader.Copy().Background(styleComposer.GetBackground()).Width(rowWidth)
	for index, workspace := range m.worktreeWorkspaces {
		status, glyph := m.worktreeDisplayStatus(workspace)
		name := workspace.Name
		if workspace.Current {
			name = "current"
		}
		branch := workspace.Branch
		if branch == "" {
			branch = "detached"
		}
		marker := "  "
		if index == m.worktreeIndex {
			marker = "▶ "
		}
		row := xansi.Truncate(marker+glyph+" "+name, rowWidth, "…")
		meta := xansi.Truncate("    "+status+" · "+branch, rowWidth, "…")
		if index == m.worktreeIndex {
			lines = append(lines, selectedStyle.Render(row), selectedMeta.Render(meta))
		} else {
			lines = append(lines, styleAssistant.Render(row), styleHeaderDim.Render(meta))
		}
	}
	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(width).Height(height).MaxWidth(width).MaxHeight(height).Render(content)
}

func (m *Model) worktreeDisplayStatus(workspace app.WorktreeWorkspace) (string, string) {
	if workspace.Current {
		if m.busy {
			return "current · working", "▶"
		}
		return "current · idle", "●"
	}
	worker, ok := m.workerForWorkspace(workspace.ID)
	if !ok {
		return "not attached", "○"
	}
	if worker.OutcomeUnknown || worker.TurnStatus == supervisor.TurnOutcomeUnknown {
		return "managed · outcome unknown", "×"
	}
	if worker.ProcessStatus == supervisor.ProcessCrashed {
		return "managed · crashed", "×"
	}
	if worker.ProcessStatus == supervisor.ProcessStarting {
		return "managed · starting", "…"
	}
	if worker.ProcessStatus == supervisor.ProcessStopped {
		return "not attached", "○"
	}
	switch worker.TurnStatus {
	case supervisor.TurnPermission:
		return "managed · permission", "!"
	case supervisor.TurnInputNeeded:
		return "managed · input needed", "!"
	case supervisor.TurnAborting:
		return "managed · aborting", "…"
	case supervisor.TurnWorking:
		return "managed · working", "▶"
	default:
		return "managed · idle", "●"
	}
}

func (m *Model) renderSelectedWorktreeDetail(width, height int) string {
	workspace := m.selectedWorkspace()
	if workspace == nil {
		return styleHeaderDim.Render("No worktree selected")
	}
	if m.worktreeStartConfirm {
		return m.renderWorktreeStartConfirmation(width, height)
	}
	worker, managed := m.workerForWorkspace(workspace.ID)
	tabs := []string{"Chat", "Activity", "Workspace", "Diff"}
	var tabLine []string
	for i, tab := range tabs {
		if i == m.worktreeTab {
			tabLine = append(tabLine, styleHeader.Render("["+tab+"]"))
		} else {
			tabLine = append(tabLine, styleHeaderDim.Render(" "+tab+" "))
		}
	}
	status, _ := m.worktreeDisplayStatus(*workspace)
	title := workspace.Name + " · " + workspace.Branch
	if workspace.Current {
		title = "Current workspace · " + workspace.Branch
	}
	lines := []string{
		styleHeader.Render(xansi.Truncate(title, width, "…")),
		strings.Join(tabLine, " "), styleHeaderDim.Render(status), "",
	}
	switch m.worktreeTab {
	case 0:
		if workspace.Current {
			model := ""
			if m.app != nil {
				providerID, activeModel, _ := m.app.ActiveModelsSnapshot()
				model = providerID + "/" + activeModel.ID
			}
			lines = append(lines,
				styleHeader.Render("Root conversation"),
				styleHeaderDim.Render("This is the active Snow workspace."),
				"",
				"Runtime: "+model,
				"State: "+status,
				"",
				styleFooter.Render("Press Enter to return to chat."),
			)
		} else {
			lines = append(lines, m.worktreeChatLines(worker, managed, width)...)
		}
	case 1:
		if workspace.Current {
			lines = append(lines, styleHeaderDim.Render("Root activity remains visible in the main conversation."))
		} else if managed {
			for _, row := range m.worktreeActivity[worker.ID] {
				lines = append(lines, styleHeaderDim.Render(xansi.Wordwrap(row, width, "")))
			}
		}
		if len(lines) == 4 {
			lines = append(lines, styleHeaderDim.Render("No live activity yet."))
		}
	case 2:
		lines = append(lines,
			"Path: "+workspace.Path,
			"Branch: "+workspace.Branch,
			"HEAD: "+workspace.Head,
			fmt.Sprintf("Git dirty: %t", workspace.Dirty),
			fmt.Sprintf("Sessions: %d", workspace.SessionCount),
		)
		if managed {
			lines = append(lines, "Session: "+worker.SessionPath, "Runtime: "+worker.Provider+"/"+worker.Model, "Permission: ask")
		}
		if m.worktreePreflight != nil && m.worktreePreflight.Path == workspace.Path {
			lines = append(lines, "Trust: "+string(m.worktreePreflight.TrustLevel), "Shell: "+m.worktreePreflight.Shell)
		} else {
			lines = append(lines, "Trust/shell: resolved before launch")
		}
		if workspace.GitError != "" {
			lines = append(lines, styleError.Render("Git: "+workspace.GitError))
		}
	case 3:
		if m.worktreeDiff == "" {
			lines = append(lines, styleHeaderDim.Render("Press d to load bounded Git status and diff stat."))
		} else {
			lines = append(lines, xansi.Wordwrap(m.worktreeDiff, width, ""))
		}
	}
	if m.worktreeStatus != "" {
		lines = append(lines, "", styleFooter.Render(m.worktreeStatus))
	}
	if m.worktreeError != "" {
		lines = append(lines, "", styleError.Render(m.worktreeError))
	}
	return renderBoundedLines(lines, width, height, m.worktreeDetailOffset)
}

func (m *Model) worktreeChatLines(worker supervisor.WorkerState, managed bool, width int) []string {
	if !managed {
		workspace := m.selectedWorkspace()
		lines := []string{styleHeaderDim.Render("This worktree is not attached to this supervisor.")}
		if workspace != nil && len(workspace.Sessions) > 0 {
			lines = append(lines, "", "Snow sessions:")
			for _, item := range workspace.Sessions {
				name := item.Name
				if name == "" {
					name = item.ID
				}
				lines = append(lines, fmt.Sprintf("  %s · %d messages", name, item.Messages))
			}
			lines = append(lines, "", styleFooter.Render("Press s to review and start a managed worker."))
		} else {
			lines = append(lines, "", styleFooter.Render("Press n to create a worktree/session."))
		}
		return lines
	}
	var lines []string
	for _, message := range worker.Messages {
		lines = append(lines, m.fleetMessageLines(message, width)...)
	}
	if thinking := strings.TrimSpace(m.worktreeLiveThinking[worker.ID]); thinking != "" {
		lines = append(lines, styleThinking.Render("thinking"), xansi.Wordwrap(thinking, width, ""))
	}
	if text := strings.TrimSpace(m.worktreeLiveText[worker.ID]); text != "" {
		lines = append(lines, styleAssistant.Render("assistant"), xansi.Wordwrap(text, width, ""))
	}
	if len(lines) == 0 {
		lines = append(lines, styleHeaderDim.Render("Managed worker is ready. Type a task and press Enter."))
	}
	return lines
}

func renderBoundedLines(lines []string, width, height, offset int) string {
	var expanded []string
	for _, line := range lines {
		wrapped := xansi.Wordwrap(line, max(1, width), "")
		expanded = append(expanded, strings.Split(wrapped, "\n")...)
	}
	if len(expanded) > height {
		start := max(0, len(expanded)-height-offset)
		end := min(len(expanded), start+height)
		expanded = expanded[start:end]
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Height(height).MaxHeight(height).Render(strings.Join(expanded, "\n"))
}

func (m *Model) renderWorktreeStartConfirmation(width, height int) string {
	workspace := m.selectedWorkspace()
	if workspace == nil || len(workspace.Sessions) == 0 || m.worktreePreflight == nil {
		return styleError.Render("worktree launch preflight unavailable")
	}
	selected := workspace.Sessions[min(m.worktreeStartSession, len(workspace.Sessions)-1)]
	trustText := "trusted project input"
	if !m.worktreePreflight.ProjectInputs {
		trustText = "continue untrusted (" + string(m.worktreePreflight.TrustLevel) + ")"
		if m.worktreeTrustDestination {
			trustText = "trust exact destination"
		}
	}
	lines := []string{
		styleHeader.Render("Start managed worktree worker?"), "",
		"Path: " + m.worktreePreflight.Path,
		"Branch: " + workspace.Branch,
		"Session: " + selected.Name + " (" + selected.ID + ")",
		"Runtime: " + m.worktreePreflight.Provider + "/" + m.worktreePreflight.Model,
		"Thinking: " + string(m.worktreePreflight.Thinking),
		"Permission: ask",
		"Project input: " + trustText,
		"Shell: " + m.worktreePreflight.Shell,
		"",
		styleError.Render("The worker runs with your OS privileges; worktrees are not sandboxes."),
		styleFooter.Render("↑/↓ choose session · t toggle trust · Enter launch · Esc cancel"),
	}
	if m.worktreePreflight.RequireSandbox && m.worktreePreflight.Shell != "vm" {
		lines = append(lines, styleError.Render("Launch blocked: global require-sandbox is active."))
	}
	if m.worktreeError != "" {
		lines = append(lines, styleError.Render(m.worktreeError))
	}
	return renderBoundedLines(lines, width, height, 0)
}

func (m *Model) renderWorktreeInteraction() string {
	workspace, worker, managed := m.selectedRemoteWorkspace()
	if !managed || workspace == nil {
		return ""
	}
	width := max(32, m.managedFrameWidth()-8)
	var lines []string
	if worker.Permission != nil {
		request := worker.Permission
		lines = []string{
			styleBrand.Render(" snow ") + styleHeader.Render("Worker permission"), "",
			"Worker: " + workspace.Name + " · " + workspace.Branch,
			"Path: " + workspace.Path,
			"Tool: " + request.Tool,
			"Risk: " + request.Risk,
			"Arguments: " + string(request.Args),
		}
		if len(request.Paths) > 0 {
			lines = append(lines, "Affected paths: "+strings.Join(request.Paths, ", "))
		}
		lines = append(lines, "", styleFooter.Render("a/Enter allow once · s allow for worker session · d/Esc deny"))
	} else if worker.UserInput != nil && len(worker.UserInput.Questions) > 0 {
		request := worker.UserInput
		index := min(m.worktreeInputIndex, len(request.Questions)-1)
		question := request.Questions[index]
		lines = []string{
			styleBrand.Render(" snow ") + styleHeader.Render("Worker input needed"), "",
			"Worker: " + workspace.Name + " · " + workspace.Branch,
			"Path: " + workspace.Path, "",
			styleHeader.Render(question.Header), question.Question, "",
		}
		if len(question.Options) > 0 {
			for optionIndex, option := range question.Options {
				prefix := "  "
				if optionIndex == m.worktreeInputOption {
					prefix = "› "
				}
				lines = append(lines, prefix+option.Label+" — "+option.Description)
			}
		} else {
			lines = append(lines, m.worktreeInputEditor.View())
		}
		lines = append(lines, "", styleFooter.Render(fmt.Sprintf("Question %d/%d · Enter answer · Esc reject", index+1, len(request.Questions))))
	}
	if m.worktreeError != "" {
		lines = append(lines, styleError.Render(m.worktreeError))
	}
	return fitFrame(lipgloss.NewStyle().Padding(1, 3).Width(width).Render(strings.Join(lines, "\n")), m.managedFrameWidth(), m.managedFrameHeight())
}

func (m *Model) renderWorktreeInspectorModal() string {
	width, height := m.managedFrameWidth(), m.managedFrameHeight()
	if width < 1 || height < 1 {
		return ""
	}
	top := m.renderWorktreeDashboardHeader(width)
	footer := m.renderWorktreeDashboardFooter(width)
	bodyHeight := max(1, height-lipgloss.Height(top)-lipgloss.Height(footer))
	var body string
	if width < 70 {
		listHeight := min(max(6, len(m.worktreeWorkspaces)*2+3), max(6, bodyHeight/3))
		list := m.renderWorktreeSidebar(width, listHeight)
		detailHeight := max(1, bodyHeight-listHeight-1)
		detail := m.renderSelectedWorktreeDetail(width, detailHeight)
		body = lipgloss.JoinVertical(lipgloss.Left, list, styleSep.Render(strings.Repeat("─", width)), detail)
	} else {
		listWidth := min(40, max(26, width/4))
		detailWidth := max(1, width-listWidth-1)
		list := m.renderWorktreeSidebar(listWidth, bodyHeight)
		detail := m.renderSelectedWorktreeDetail(detailWidth, bodyHeight)
		divider := lipgloss.NewStyle().Foreground(styleSep.GetForeground()).Width(1).Height(bodyHeight).Render(strings.Repeat("│\n", max(1, bodyHeight-1)) + "│")
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, divider, detail)
	}
	return fitFrame(lipgloss.JoinVertical(lipgloss.Left, top, body, footer), width, height)
}

func (m *Model) renderWorktreeDashboardHeader(width int) string {
	managed, total := m.worktreeAggregateUsage()
	left := " ● Worktree agents"
	right := fmt.Sprintf("%d linked · %d managed", len(m.worktreeWorkspaces), managed)
	if total > 0 {
		right += fmt.Sprintf(" · %d tokens", total)
	}
	if m.worktreeLoading {
		right = "loading…"
	}
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right)-1)
	line := xansi.Truncate(left+strings.Repeat(" ", gap)+right+" ", width, "…")
	return styleHeader.Copy().Reverse(true).Width(width).Render(line)
}

func (m *Model) renderWorktreeDashboardFooter(width int) string {
	keys := " ↑↓ Navigate   Enter Chat   ←/→ Tabs   s Start   n New   r Refresh   d Diff   x Interrupt   Esc Close "
	if width < 90 {
		keys = " ↑↓ Navigate  Enter Chat  s Start  r Refresh  Esc Close "
	}
	return styleFooter.Copy().Reverse(true).Width(width).Render(xansi.Truncate(keys, width, "…"))
}

func (m *Model) composeWorktreeSidebar(primary string) string {
	if !m.worktreeSidebarVisible() {
		return primary
	}
	height := m.managedFrameHeight()
	sidebarWidth := m.worktreeSidebarWidth()
	sidebar := m.renderWorktreeSidebar(sidebarWidth, height)
	divider := lipgloss.NewStyle().Foreground(styleSep.GetForeground()).Width(1).Height(height).Render(strings.Repeat("│\n", max(1, height-1)) + "│")
	return fitFrame(lipgloss.JoinHorizontal(lipgloss.Top, sidebar, divider, primary), m.managedFrameWidth(), height)
}

func (m *Model) worktreeAggregateUsage() (workers int, total int) {
	for _, worker := range m.worktreeWorkers {
		if worker.ProcessStatus == supervisor.ProcessReady || worker.ProcessStatus == supervisor.ProcessStarting {
			workers++
		}
		if worker.Usage != nil {
			total += worker.Usage.Total
		}
	}
	return workers, total
}

func sortedWorkerIDs(workers map[supervisor.WorkerID]supervisor.WorkerState) []supervisor.WorkerID {
	ids := make([]supervisor.WorkerID, 0, len(workers))
	for id := range workers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
