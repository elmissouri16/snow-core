package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

func renderSubagentToolSummary(toolName, output string) (string, bool) {
	switch toolName {
	case "spawn_agent", "send_message", "followup_task", "interrupt_agent":
		return "", true
	case "wait_agent":
		var result protocol.WaitSubagentsResult
		if json.Unmarshal([]byte(output), &result) != nil {
			return "", false
		}
		status := formatSubagentCounts(result.Running, result.Queued, result.Terminal)
		switch {
		case result.TimedOut:
			status += " · timed out"
		case result.AllTerminal:
			status += " · all finished"
		default:
			status += " · activity received"
		}
		return styleHeaderDim.Render("  ↳ " + status), true
	case "list_agents":
		var result struct {
			Running         int `json:"running"`
			Queued          int `json:"queued"`
			Terminal        int `json:"terminal"`
			ConcurrentLimit int `json:"concurrent_limit"`
			AgentLimit      int `json:"agent_limit"`
		}
		if json.Unmarshal([]byte(output), &result) != nil {
			return "", false
		}
		status := formatSubagentCounts(result.Running, result.Queued, result.Terminal)
		status += fmt.Sprintf(" · capacity %d/%d · identities %d/%d", result.Running, result.ConcurrentLimit, result.Running+result.Queued+result.Terminal, result.AgentLimit)
		return styleHeaderDim.Render("  ↳ " + status), true
	default:
		return "", false
	}
}

func formatSubagentCounts(running, queued, terminal int) string {
	return fmt.Sprintf("agents: %d running · %d queued · %d finished", running, queued, terminal)
}

func toolHasDiffPreview(toolName, output string) bool {
	return (toolName == "edit" || toolName == "write") && looksLikeEditDiff(output)
}

func looksLikeEditDiff(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			return true
		}
	}
	return false
}

// renderEditDiff colors the line-oriented edit preview so additions and
// deletions are immediately visible without making the model-facing result
// noisy. Context stays muted, matching the surrounding transcript.
func renderEditDiff(output string, width int) string {
	// Keep the leading marker on context lines; only remove framing newlines.
	output = strings.Trim(output, "\n")
	if output == "" {
		return ""
	}
	output = sanitizeToolPreview(output, 8*1024)
	lines := strings.Split(output, "\n")
	if len(lines) > 80 {
		lines = append(lines[:80], "... [diff preview truncated]")
	}
	maxWidth := width - 2
	if maxWidth < 20 {
		maxWidth = 20
	}
	for i, line := range lines {
		line = truncateRunes(line, maxWidth)
		switch {
		case strings.HasPrefix(line, "-"):
			lines[i] = styleDiffDel.Render(line)
		case strings.HasPrefix(line, "+"):
			lines[i] = styleDiffAdd.Render(line)
		default:
			lines[i] = styleHeaderDim.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

// renderToolOutputPreview keeps tool cards useful without dumping a whole
// read/grep result into the transcript. The complete output remains available
// to the model through the session and to SDK/RPC subscribers.
func renderToolOutputPreview(output string, width int) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	output = sanitizeToolPreview(output, 2400)
	lines := strings.Split(output, "\n")
	maxLines := 6
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], "… [preview truncated]")
	}
	maxWidth := width - 8
	if maxWidth < 20 {
		maxWidth = 20
	}
	for i, line := range lines {
		lines[i] = "  │ " + truncateRunes(line, maxWidth)
	}
	return styleHeaderDim.Render(strings.Join(lines, "\n"))
}

func compactBashCommand(command string) string {
	command = strings.ReplaceAll(command, "\r\n", "\n")
	command = strings.ReplaceAll(command, "\r", "\n")
	command = sanitizeToolPreview(command, 2*1024)
	command = strings.ReplaceAll(command, "\n", " ↵ ")
	command = strings.ReplaceAll(command, "\t", " ")
	return strings.TrimSpace(command)
}

func (m *Model) renderBashSummary(command, duration, message string, isError bool) string {
	symbol := "✓"
	symbolStyle := styleDiffAdd
	if isError {
		symbol = "✕"
		symbolStyle = styleError
	}

	command = compactBashCommand(command)
	if command == "" {
		command = "bash"
	}
	durationTail := ""
	if duration != "" {
		durationTail = " · " + duration
	}
	errorTail := ""
	if isError && message != "" {
		message = compactBashCommand(message)
		if message != "" {
			errorTail = " · " + message
		}
	}

	width := m.transcript.Width
	if width <= 0 {
		width = m.width
	}
	if width > 0 {
		// Keep the status and elapsed time visible. Error text receives a bounded
		// share so the model-issued command remains identifiable on the same row.
		if errorTail != "" {
			messageBudget := max(12, width/3)
			message = truncateRunes(message, messageBudget)
			errorTail = " · " + message
		}
		available := width - lipgloss.Width(symbol+" "+durationTail+errorTail) - 1
		if available <= 0 {
			available = 1
		}
		command = truncateRunes(command, available)
	}

	line := symbolStyle.Render(symbol) + " " + styleTool.Render(command)
	if durationTail != "" {
		line += styleHeaderDim.Render(durationTail)
	}
	if errorTail != "" {
		line += styleError.Render(errorTail)
	}
	return line
}

// sanitizeToolPreview removes terminal controls before tool output is rendered
// in the TUI. Tool output is untrusted repository/process data.
func sanitizeToolPreview(value string, maxBytes int) string {
	var b strings.Builder
	for _, r := range value {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			b.WriteRune(r)
		}
		if b.Len() >= maxBytes {
			break
		}
	}
	return b.String()
}

func (m *Model) startMCPInfo() (tea.Model, tea.Cmd) {
	statuses := m.app.MCPStatuses
	if m.app.MCPManager != nil {
		statuses = m.app.MCPManager.Statuses()
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	items := make([]statusInfoItem, 0, len(statuses))
	for _, status := range statuses {
		state := status.State
		if state == "" {
			state = "failed"
			if status.Connected {
				state = "connected"
			}
		}
		if status.Message == "disabled" || strings.HasPrefix(status.Message, "disabled by") {
			state = "disabled"
		}
		label := fmt.Sprintf("%s  ·  %s  ·  %s", status.ID, state, status.Transport)
		if status.ProtocolVersion != "" {
			label += "  ·  " + status.ProtocolVersion
		}
		detail := strings.TrimSpace(status.ServerName + " " + status.ServerVersion)
		if status.ToolCount > 0 {
			detail += fmt.Sprintf(" · %d tools", status.ToolCount)
		}
		if len(status.Capabilities) > 0 {
			detail += " · " + strings.Join(status.Capabilities, ", ")
		}
		if status.Message != "" {
			detail += " · " + status.Message
		}
		items = append(items, statusInfoItem{Label: label, Detail: strings.Trim(detail, " ·")})
	}
	return m.startInfoPicker("MCP servers", items)
}

func (m *Model) startSkillsInfo() (tea.Model, tea.Cmd) {
	var items []statusInfoItem
	if m.app.Skills != nil {
		for _, skill := range m.app.Skills.Inventory() {
			state := "enabled"
			if !skill.Enabled {
				state = "disabled"
			}
			label := fmt.Sprintf("%s  ·  %s  ·  %s/%s", skill.Name, state, skill.Scope, skill.Source)
			detail := skill.Description + " · " + skill.Location
			if skill.DisabledBy != "" {
				detail += " · " + skill.DisabledBy
			}
			items = append(items, statusInfoItem{Label: label, Detail: detail})
		}
	}
	return m.startInfoPicker("Agent Skills", items)
}

func (m *Model) startInfoPicker(title string, items []statusInfoItem) (tea.Model, tea.Cmd) {
	if len(items) == 0 {
		m.pushLine(styleFooter.Render(strings.ToLower(title) + ": none configured or discovered"))
		return m, nil
	}
	m.pickInfo, m.infoTitle, m.infoItems, m.infoIndex = true, title, items, 0
	m.infoLoading = false
	m.compVisible = false
	return m, nil
}

func (m *Model) handleInfoPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if m.infoLoading {
		if msg.Type == tea.KeyEsc {
			m.closeInfoPicker()
			m.pickerGeneration++
		}
		return m, nil
	}
	count := len(m.infoItems)
	if count == 0 {
		m.closeInfoPicker()
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.infoIndex = (m.infoIndex - 1 + count) % count
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.infoIndex = (m.infoIndex + 1) % count
	case tea.KeyPgUp:
		m.infoIndex -= m.infoPickerVisibleItems()
		if m.infoIndex < 0 {
			m.infoIndex = 0
		}
	case tea.KeyPgDown:
		m.infoIndex += m.infoPickerVisibleItems()
		if m.infoIndex >= count {
			m.infoIndex = count - 1
		}
	case tea.KeyHome:
		m.infoIndex = 0
	case tea.KeyEnd:
		m.infoIndex = count - 1
	case tea.KeyEsc:
		m.closeInfoPicker()
	case tea.KeyEnter:
		if strings.HasPrefix(m.infoTitle, "Agents") && m.infoIndex < len(m.infoAgentTargets) {
			target := m.infoAgentTargets[m.infoIndex]
			m.closeInfoPicker()
			return m, m.inspectAgent(target)
		} else {
			m.closeInfoPicker()
		}
	}
	return m, nil
}

func (m *Model) closeInfoPicker() {
	m.pickInfo = false
	m.infoLoading = false
	m.infoTitle = ""
	m.infoItems = nil
	m.infoIndex = 0
	m.infoAgentTargets = nil
}

func (m *Model) inspectAgent(target string) tea.Cmd {
	if m.asyncIO {
		m.pickerGeneration++
		generation := m.pickerGeneration
		m.lastStatus = "inspecting"
		return func() tea.Msg {
			state, err := m.app.Subagent(m.ctx, target)
			if err != nil {
				return subagentInspectMsg{generation: generation, err: err}
			}
			var messages []protocol.Message
			var messageErr error
			if state.Agent.Path == protocol.RootAgentPath {
				messages, messageErr = m.app.Agent.Messages()
			} else {
				messages, messageErr = m.app.SubagentMessages(m.ctx, target)
			}
			return subagentInspectMsg{generation: generation, state: state, messages: messages, messageErr: messageErr}
		}
	}
	state, err := m.app.Subagent(m.ctx, target)
	if err != nil {
		m.pushLine(styleError.Render(err.Error()))
		return nil
	}
	var messages []protocol.Message
	var messageErr error
	if state.Agent.Path == protocol.RootAgentPath {
		messages, messageErr = m.app.Agent.Messages()
	} else {
		messages, messageErr = m.app.SubagentMessages(m.ctx, target)
	}
	m.pushLine(styleFooter.Render(renderSubagentInspection(state, messages, messageErr, m.app.Cfg.Subagents.Durable, time.Now())))
	return nil
}

func (m *Model) infoPickerVisibleItems() int {
	visible := m.height - 14
	if m.inlineModalOverlay() {
		visible = m.availableOverlayHeight() - 3 // title, selected detail, hint
	}
	if visible < 1 {
		visible = 1
	}
	if visible > len(m.infoItems) {
		visible = len(m.infoItems)
	}
	return visible
}

func (m *Model) infoWindow() (start, end int) {
	visible := m.infoPickerVisibleItems()
	if len(m.infoItems) <= visible {
		return 0, len(m.infoItems)
	}
	start = m.infoIndex - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(m.infoItems) {
		start = len(m.infoItems) - visible
	}
	return start, start + visible
}

func (m *Model) renderInfoPicker() string {
	if !m.pickInfo {
		return ""
	}
	if m.infoLoading {
		return styleHeaderDim.Render(m.infoTitle + "\n  loading…")
	}
	if len(m.infoItems) == 0 {
		return ""
	}
	start, end := m.infoWindow()
	width := max(1, m.width-2)
	var b strings.Builder
	b.WriteString(styleHeaderDim.Render(truncateRunes(fmt.Sprintf("%s (%d)", m.infoTitle, len(m.infoItems)), width)) + "\n")
	for i := start; i < end; i++ {
		line := truncateRunes(m.infoItems[i].Label, max(8, m.width-4))
		if i == m.infoIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	b.WriteString(styleHeaderDim.Render(truncateRunes("  "+m.infoItems[m.infoIndex].Detail, width)) + "\n")
	b.WriteString(styleFooter.Render(truncateRunes("(↑/↓ inspect · Enter/Esc close)", width)))
	return b.String()
}

func (m *Model) sessionPickerBodyMin() int {
	// Below this size the session selector takes priority over transcript
	// history so the whole picker still fits in the terminal.
	if m.height < 14 {
		return 1
	}
	return 3
}

func (m *Model) sessionPickerMaxRows() int {
	if m.inlineModalOverlay() {
		return max(3, m.availableOverlayHeight())
	}
	rows := m.height - 8 - m.sessionPickerBodyMin()
	if rows < 3 {
		return 3
	}
	return rows
}

func (m *Model) sessionPickerVisibleItems() int {
	total := len(m.sessions)
	if total == 0 {
		return 0
	}
	// Keep rows for the title and hint. Reserve two more rows for scroll
	// markers when the terminal is tall enough to show them.
	visible := m.sessionPickerMaxRows() - 2
	if m.sessionPickerMaxRows() >= 5 && total > visible {
		visible -= 2
	}
	if visible < 1 {
		visible = 1
	}
	if visible > total {
		visible = total
	}
	return visible
}

func (m *Model) sessionWindow() (start, end int) {
	total := len(m.sessions)
	visible := m.sessionPickerVisibleItems()
	if total == 0 || total <= visible {
		return 0, total
	}
	start = m.sessionIndex - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func (m *Model) sessionPickerRows() int {
	if !m.pickSession {
		return 0
	}
	if m.sessionLoading {
		return 2
	}
	start, end := m.sessionWindow()
	rows := 2 + end - start // title + entries + hint
	if m.sessionPickerMaxRows() >= 5 {
		if start > 0 {
			rows++
		}
		if end < len(m.sessions) {
			rows++
		}
	}
	return rows
}

func (m *Model) renderSessionPicker() string {
	if !m.pickSession {
		return ""
	}
	if m.sessionLoading {
		status := "loading sessions…"
		if m.sessionDeleteInFlight {
			status = "deleting session and subagent histories…"
		}
		return styleHeaderDim.Render("sessions\n  " + status)
	}
	start, end := m.sessionWindow()
	var b strings.Builder
	pickerWidth := max(1, m.width-2)
	title := truncateRunes(fmt.Sprintf("sessions (%d)", len(m.sessions)), pickerWidth)
	b.WriteString(styleHeaderDim.Render(title) + "\n")
	showMarkers := m.sessionPickerMaxRows() >= 5
	if showMarkers && start > 0 {
		b.WriteString(styleHeaderDim.Render(truncateRunes("  ↑ more sessions", pickerWidth)) + "\n")
	}
	for i := start; i < end; i++ {
		line := formatSessionPickerInfo(m.sessions[i], currentSessionID(m.app))
		line = truncateRunes(line, max(8, m.width-4))
		if i == m.sessionIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	if showMarkers && end < len(m.sessions) {
		b.WriteString(styleHeaderDim.Render(truncateRunes("  ↓ more sessions", pickerWidth)) + "\n")
	}
	hint := "(↑/↓ choose · PgUp/PgDn scroll · Enter resume · r rename · d delete · Esc cancel)"
	if m.sessionRenaming {
		hint = "Rename session: " + m.sessionRenameInput + "_"
	}
	if m.sessionDeleting && m.sessionIndex >= 0 && m.sessionIndex < len(m.sessions) {
		name := m.sessions[m.sessionIndex].Name
		if name == "" {
			name = shortSessionID(m.sessions[m.sessionIndex].ID)
		}
		hint = "Permanently delete " + strconv.Quote(name) + " and its subagent histories? Enter confirm · Esc cancel"
	}
	b.WriteString(styleFooter.Render(truncateRunes(hint, pickerWidth)))
	return strings.TrimSuffix(b.String(), "\n")
}

func formatSessionPickerInfo(info session.SessionInfo, activeID string) string {
	label := shortSessionID(info.ID)
	if info.Name != "" {
		label = info.Name + "  ·  " + label
	}
	label += fmt.Sprintf("  ·  %d messages", info.Messages)
	if info.ID == activeID {
		label += "  ✓ active"
	}
	return label
}

func (m *Model) startTreePick() (tea.Model, tea.Cmd) {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("tree: wait for the current turn to finish"))
		return m, nil
	}
	if m.asyncIO {
		m.pickTree = true
		m.treeLoading = true
		m.branches = nil
		m.pickerGeneration++
		generation := m.pickerGeneration
		return m, func() tea.Msg {
			branches, err := m.app.Agent.Branches()
			return branchListMsg{generation: generation, branches: branches, err: err}
		}
	}
	branches, err := m.app.Agent.Branches()
	if err != nil {
		m.pushLine(styleError.Render("tree: " + err.Error()))
		return m, nil
	}
	if len(branches) == 0 {
		m.pushLine(styleFooter.Render("tree: no branches"))
		return m, nil
	}
	m.branches = orderBranches(branches)
	m.branchIndex = 0
	for i, branch := range m.branches {
		if branch.Active {
			m.branchIndex = i
			break
		}
	}
	m.pickTree = true
	m.compVisible = false
	return m, nil
}

func (m *Model) handleTreePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.treeLoading {
		if msg.Type == tea.KeyEsc {
			m.pickTree = false
			m.treeLoading = false
			m.pickerGeneration++
		}
		return m, nil
	}
	count := len(m.branches)
	if count == 0 {
		m.pickTree = false
		return m, nil
	}
	if m.branchAction != "" {
		if m.branchAction == "delete" {
			if keyMatches(msg, m.keys.Close) || (msg.Type == tea.KeyRunes && strings.EqualFold(string(msg.Runes), "n")) {
				m.branchAction = ""
				return m, nil
			}
			if keyMatches(msg, m.keys.Confirm) {
				return m.executeTreeAction()
			}
			return m, nil
		}
		switch {
		case keyMatches(msg, m.keys.Close):
			m.branchAction, m.branchInput = "", ""
		case keyMatches(msg, m.keys.Accept):
			return m.executeTreeAction()
		case msg.Type == tea.KeyBackspace:
			r := []rune(m.branchInput)
			if len(r) > 0 {
				m.branchInput = string(r[:len(r)-1])
			}
		case msg.Type == tea.KeyRunes:
			if len([]rune(m.branchInput))+len(msg.Runes) <= 64 {
				m.branchInput += string(msg.Runes)
			}
		}
		return m, nil
	}
	action := pickerKeyActionWithMap(msg, m.keys)
	if next, handled := movePicker(m.branchIndex, count, action, m.treePickerVisibleItems()); handled && action != pickerAccept && action != pickerClose {
		m.branchIndex = next
		return m, nil
	}
	if action == pickerClose {
		m.pickTree = false
		m.branches = nil
		return m, nil
	}
	if action == pickerAccept {
		branch := m.branches[m.branchIndex]
		if m.asyncIO {
			m.treeLoading = true
			m.pickerGeneration++
			gen := m.pickerGeneration
			return m, func() tea.Msg {
				return branchActionMsg{generation: gen, branch: branch, action: "select", err: m.app.SelectBranch(branch.ID)}
			}
		}
		if err := m.app.SelectBranch(branch.ID); err != nil {
			m.pushLine(styleError.Render("tree: " + err.Error()))
			return m, nil
		}
		m.pickTree = false
		m.branches = nil
		m.fenceRootTurnProjection()
		m.hydrateSession()
		m.lastStatus = "selected branch " + branch.Name
		return m, nil
	}
	switch {
	case keyMatches(msg, m.keys.BranchFork):
		m.branchAction = "fork"
		m.branchInput = ""
	case keyMatches(msg, m.keys.BranchRename):
		m.branchAction = "rename"
		m.branchInput = m.branches[m.branchIndex].Name
	case keyMatches(msg, m.keys.BranchDelete):
		m.branchAction = "delete"
	}
	return m, nil
}

func (m *Model) executeTreeAction() (tea.Model, tea.Cmd) {
	selected := m.branches[m.branchIndex]
	action, input := m.branchAction, strings.TrimSpace(m.branchInput)
	m.branchAction, m.branchInput = "", ""
	m.treeLoading = m.asyncIO
	m.pickerGeneration++
	gen := m.pickerGeneration
	run := func() branchActionMsg {
		switch action {
		case "fork":
			branch, err := m.app.ForkBranchWithOptions(protocol.BranchForkOptions{SourceBranchID: selected.ID, FromEntryID: selected.TipID, Name: input})
			return branchActionMsg{generation: gen, branch: branch, action: action, err: err}
		case "rename":
			branch, err := m.app.RenameBranch(selected.ID, input)
			return branchActionMsg{generation: gen, branch: branch, action: action, err: err}
		case "delete":
			err := m.app.DeleteBranch(selected.ID)
			return branchActionMsg{generation: gen, action: action, err: err}
		default:
			return branchActionMsg{generation: gen, err: errors.New("unknown branch action")}
		}
	}
	if m.asyncIO {
		return m, func() tea.Msg { return run() }
	}
	return m, func() tea.Msg { return run() }
}

func orderBranches(branches []protocol.SessionBranch) []protocol.SessionBranch {
	byParent := map[string][]protocol.SessionBranch{}
	for _, branch := range branches {
		byParent[branch.ParentID] = append(byParent[branch.ParentID], branch)
	}
	for parent := range byParent {
		sort.SliceStable(byParent[parent], func(i, j int) bool {
			if byParent[parent][i].CreatedAt == byParent[parent][j].CreatedAt {
				return byParent[parent][i].ID < byParent[parent][j].ID
			}
			return byParent[parent][i].CreatedAt < byParent[parent][j].CreatedAt
		})
	}
	var out []protocol.SessionBranch
	seen := map[string]bool{}
	var visit func(string)
	visit = func(parent string) {
		for _, branch := range byParent[parent] {
			if seen[branch.ID] {
				continue
			}
			seen[branch.ID] = true
			out = append(out, branch)
			visit(branch.ID)
		}
	}
	visit("")
	for _, branch := range branches {
		if !seen[branch.ID] {
			out = append(out, branch)
		}
	}
	return out
}
