package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *Model) openSubagentFleet(target string) tea.Cmd {
	m.closeProcessFleet()
	m.subagentFleetOpen = true
	m.subagentFleetLoading = true
	m.subagentFleetError = ""
	m.subagentFleetRequested = strings.TrimSpace(target)
	m.subagentFleetGeneration++
	generation := m.subagentFleetGeneration
	return m.fetchSubagentFleetCmd(generation, m.subagentFleetRequested)
}

func (m *Model) fetchSubagentFleetCmd(generation uint64, target string) tea.Cmd {
	appRuntime := m.app
	ctx := m.ctx
	return func() tea.Msg {
		list, err := appRuntime.ListSubagents(ctx, "")
		historicalCount := 0
		if err == nil && len(list.Agents) <= 1 {
			if messages, messageErr := appRuntime.Agent.Messages(); messageErr == nil {
				historicalCount = historicalChildMessageCount(messages)
			}
		}
		return subagentFleetListMsg{generation: generation, target: target, list: list, historicalCount: historicalCount, err: err}
	}
}

func (m *Model) refreshSubagentFleet() tea.Cmd {
	if !m.subagentFleetOpen || m.subagentFleetLoading {
		return nil
	}
	m.subagentFleetMD.clearCache()
	m.subagentFleetLoading = true
	m.subagentFleetError = ""
	// Invalidate an in-flight selected transcript immediately. The refreshed
	// authoritative list will launch a new detail generation for its selection.
	m.subagentFleetDetailGeneration++
	m.subagentFleetDetailLoading = true
	return m.fetchSubagentFleetCmd(m.subagentFleetGeneration, m.subagentFleetSelectedPath())
}

func (m *Model) loadSubagentFleetDetail() tea.Cmd {
	m.subagentFleetMD.clearCache()
	target := m.subagentFleetSelectedPath()
	if target == "" {
		m.subagentFleetDetailLoading = false
		m.subagentFleetMessages = nil
		return nil
	}
	m.subagentFleetDetailGeneration++
	generation := m.subagentFleetDetailGeneration
	m.subagentFleetDetailLoading = true
	m.subagentFleetDetailError = ""
	m.subagentFleetDetailOffset = 0
	// Follow the newest activity by default. PageUp/Home deliberately disable
	// follow mode; End restores it.
	m.subagentFleetDetailEnd = true
	appRuntime := m.app
	ctx := m.ctx
	return func() tea.Msg {
		state, err := appRuntime.Subagent(ctx, target)
		if err != nil {
			return subagentFleetDetailMsg{generation: generation, target: target, err: err}
		}
		var messages []protocol.Message
		var messageErr error
		if state.Agent.Path == protocol.RootAgentPath {
			messages, messageErr = appRuntime.Agent.Messages()
		} else {
			messages, messageErr = appRuntime.SubagentMessages(ctx, target)
		}
		if len(messages) > maxAgentInspectionMessages {
			messages = append([]protocol.Message(nil), messages[len(messages)-maxAgentInspectionMessages:]...)
		} else {
			messages = append([]protocol.Message(nil), messages...)
		}
		return subagentFleetDetailMsg{generation: generation, target: target, state: state, messages: messages, messageErr: messageErr}
	}
}

func (m *Model) applySubagentFleetList(msg subagentFleetListMsg) tea.Cmd {
	if !m.subagentFleetOpen || msg.generation != m.subagentFleetGeneration {
		return nil
	}
	m.subagentFleetLoading = false
	if msg.err != nil {
		m.subagentFleetError = msg.err.Error()
		return nil
	}
	selectedBeforeRefresh := m.subagentFleetSelectedPath()
	// An event may have advanced an agent while this asynchronous snapshot was
	// loading. Never replace a newer generation or a terminal state with stale
	// list state from the same execution generation.
	liveByThread := make(map[string]protocol.SubagentState, len(m.subagentFleetList.Agents))
	for _, state := range m.subagentFleetList.Agents {
		liveByThread[state.Agent.ThreadID] = state
	}
	for i, state := range msg.list.Agents {
		if live, ok := liveByThread[state.Agent.ThreadID]; ok && preferLiveFleetState(live, state) {
			msg.list.Agents[i] = live
		}
	}
	m.subagentFleetList = msg.list
	m.recountSubagentFleet()
	m.subagentFleetError = ""
	m.subagentFleetWarning = ""
	if msg.historicalCount > 0 {
		m.subagentFleetWarning = fmt.Sprintf("%d historical child completions remain, but this older session has no recoverable child transcript topology", msg.historicalCount)
	}
	if len(msg.list.Agents) == 0 {
		m.subagentFleetIndex = 0
		m.subagentFleetMessages = nil
		return nil
	}
	target := strings.TrimSpace(msg.target)
	if target == "" {
		target = selectedBeforeRefresh
	}
	m.subagentFleetIndex = 0
	matched := target == ""
	for i, state := range msg.list.Agents {
		if target == string(state.Agent.Path) || target == state.Agent.ThreadID {
			m.subagentFleetIndex = i
			matched = true
			break
		}
	}
	if !matched {
		m.subagentFleetError = "agent not found: " + target
		m.subagentFleetMessages = nil
		m.subagentFleetDetailLoading = false
		return nil
	}
	return m.loadSubagentFleetDetail()
}

func (m *Model) applySubagentFleetDetail(msg subagentFleetDetailMsg) {
	if !m.subagentFleetOpen || msg.generation != m.subagentFleetDetailGeneration || msg.target != m.subagentFleetSelectedPath() {
		return
	}
	m.subagentFleetDetailLoading = false
	if msg.err != nil {
		m.subagentFleetDetailError = msg.err.Error()
		return
	}
	if current := m.subagentFleetDetailState; current.Agent.ThreadID == msg.state.Agent.ThreadID && preferLiveFleetState(current, msg.state) {
		msg.state = current
	}
	m.subagentFleetDetailState = msg.state
	messages := msg.messages
	if len(messages) > maxAgentInspectionMessages {
		messages = messages[len(messages)-maxAgentInspectionMessages:]
	}
	m.subagentFleetMessages = append([]protocol.Message(nil), messages...)
	if msg.messageErr != nil {
		m.subagentFleetDetailError = msg.messageErr.Error()
	} else {
		m.subagentFleetDetailError = ""
	}
}

func preferLiveFleetState(live, snapshot protocol.SubagentState) bool {
	return live.Generation > snapshot.Generation ||
		(live.Generation == snapshot.Generation && live.Status.Terminal() && !snapshot.Status.Terminal())
}

func (m *Model) closeSubagentFleet() {
	m.subagentFleetOpen = false
	m.subagentFleetLoading = false
	m.subagentFleetDetailLoading = false
	m.subagentFleetGeneration++
	m.subagentFleetDetailGeneration++
	m.subagentFleetRequested = ""
	m.subagentFleetError = ""
	m.subagentFleetDetailError = ""
	m.subagentFleetWarning = ""
	m.subagentFleetMD.clearCache()
}

func (m *Model) subagentFleetSelectedPath() string {
	if m.subagentFleetIndex < 0 || m.subagentFleetIndex >= len(m.subagentFleetList.Agents) {
		return ""
	}
	return string(m.subagentFleetList.Agents[m.subagentFleetIndex].Agent.Path)
}

func (m *Model) handleSubagentFleetMouse(msg tea.MouseMsg) {
	event := tea.MouseEvent(msg)
	if event.Action != tea.MouseActionPress {
		return
	}
	delta := max(1, m.transcript.MouseWheelDelta)
	switch event.Button {
	case tea.MouseButtonWheelUp:
		m.scrollSubagentFleetDetail(-delta)
	case tea.MouseButtonWheelDown:
		m.scrollSubagentFleetDetail(delta)
	}
}

func (m *Model) scrollSubagentFleetDetail(delta int) {
	page := m.subagentFleetDetailPageSize()
	maxOffset := max(0, m.subagentFleetDetailLineCount()-page)
	current := min(max(0, m.subagentFleetDetailOffset), maxOffset)
	if m.subagentFleetDetailEnd {
		current = maxOffset
	}
	next := min(max(0, current+delta), maxOffset)
	m.subagentFleetDetailOffset = next
	m.subagentFleetDetailEnd = next >= maxOffset
}

func (m *Model) handleSubagentFleetKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	count := len(m.subagentFleetList.Agents)
	switch msg.Type {
	case tea.KeyEsc:
		m.closeSubagentFleet()
		return m, nil
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'q':
				m.closeSubagentFleet()
				return m, nil
			case 'r':
				return m, m.refreshSubagentFleet()
			case 'j':
				msg.Type = tea.KeyDown
			case 'k':
				msg.Type = tea.KeyUp
			}
		}
	}
	if count == 0 {
		return m, nil
	}
	previous := m.subagentFleetIndex
	switch msg.Type {
	case tea.KeyUp:
		m.subagentFleetIndex = (m.subagentFleetIndex - 1 + count) % count
	case tea.KeyDown:
		m.subagentFleetIndex = (m.subagentFleetIndex + 1) % count
	case tea.KeyPgUp:
		m.scrollSubagentFleetDetail(-m.subagentFleetDetailPageSize())
	case tea.KeyPgDown:
		m.scrollSubagentFleetDetail(m.subagentFleetDetailPageSize())
	case tea.KeyHome:
		m.subagentFleetDetailOffset = 0
		m.subagentFleetDetailEnd = false
	case tea.KeyEnd:
		m.subagentFleetDetailOffset = max(0, m.subagentFleetDetailLineCount()-m.subagentFleetDetailPageSize())
		m.subagentFleetDetailEnd = true
	}
	if m.subagentFleetIndex != previous {
		return m, m.loadSubagentFleetDetail()
	}
	return m, nil
}

func (m *Model) subagentFleetDetailPageSize() int {
	return m.subagentFleetLayout().detailHeight
}

func (m *Model) subagentFleetDetailLineCount() int {
	return len(m.subagentFleetDetailLines(m.subagentFleetLayout().detailWidth))
}

func (m *Model) recordSubagentFleetEvent(ev protocol.AgentEvent) {
	if ev.Agent == nil {
		return
	}
	threadID := ev.Agent.ThreadID
	line := formatSubagentFleetEvent(ev)
	if line != "" {
		lines := m.subagentFleetActivity[threadID]
		// Providers often deliver one token per delta. Coalesce adjacent text or
		// thinking fragments so the inspector shows readable log entries instead
		// of spending its bounded row budget on individual tokens.
		if (ev.Type == protocol.EvTextDelta || ev.Type == protocol.EvThinkingDelta) &&
			len(lines) > 0 && m.subagentFleetActivityKinds[threadID] == ev.Type {
			fragment := compactAgentText(ev.Text, 800)
			if (m.subagentFleetActivitySpace[threadID] || startsWithSpace(ev.Text)) && fragment != "" {
				fragment = " " + fragment
			}
			lines[len(lines)-1] = boundedUTF8Tail(lines[len(lines)-1]+fragment, 4096)
		} else {
			lines = append(lines, line)
		}
		if len(lines) > maxFleetActivityLines {
			lines = append([]string(nil), lines[len(lines)-maxFleetActivityLines:]...)
		}
		for fleetActivityBytes(lines) > maxFleetActivityBytes && len(lines) > 1 {
			lines = lines[1:]
		}
		m.subagentFleetActivity[threadID] = lines
		m.subagentFleetActivityKinds[threadID] = ev.Type
		m.subagentFleetActivitySpace[threadID] = strings.HasSuffix(ev.Text, " ") || strings.HasSuffix(ev.Text, "\n") || strings.HasSuffix(ev.Text, "\t")
	}
	found := false
	for i := range m.subagentFleetList.Agents {
		state := &m.subagentFleetList.Agents[i]
		if state.Agent.ThreadID != threadID {
			continue
		}
		found = true
		if ev.Subagent != nil && ev.Subagent.Generation >= state.Generation {
			*state = *ev.Subagent.Clone()
		}
		if ev.Usage != nil {
			state.Usage = ev.Usage.Clone()
		}
		if m.subagentFleetSelectedPath() == string(state.Agent.Path) {
			m.subagentFleetDetailState = *state
		}
		break
	}
	if !found && ev.Type == protocol.EvSubagentStarted {
		state := protocol.SubagentState{Agent: *ev.Agent.Clone(), Status: protocol.AgentRunning}
		if ev.Subagent != nil {
			state = *ev.Subagent.Clone()
		}
		m.subagentFleetList.Agents = append(m.subagentFleetList.Agents, state)
	}
	m.recountSubagentFleet()
}

func (m *Model) recountSubagentFleet() {
	m.subagentFleetList.Running, m.subagentFleetList.Queued, m.subagentFleetList.Terminal = 0, 0, 0
	for _, state := range m.subagentFleetList.Agents {
		if state.Agent.Path == protocol.RootAgentPath {
			continue
		}
		switch {
		case state.Status.Terminal():
			m.subagentFleetList.Terminal++
		case state.Status == protocol.AgentRunning:
			m.subagentFleetList.Running++
		default:
			m.subagentFleetList.Queued++
		}
	}
}

func formatSubagentFleetEvent(ev protocol.AgentEvent) string {
	stamp := time.Now().Format("15:04:05")
	switch ev.Type {
	case protocol.EvTextDelta:
		return stamp + "  response  " + compactAgentText(ev.Text, 800)
	case protocol.EvThinkingDelta:
		return stamp + "  thinking  " + compactAgentText(ev.Text, 800)
	case protocol.EvToolStart:
		return stamp + "  tool ▶  " + ev.ToolName
	case protocol.EvToolProgress:
		text := ev.Message
		if ev.ToolProgress != nil && ev.ToolProgress.Message != "" {
			text = ev.ToolProgress.Message
		}
		return stamp + "  tool …  " + ev.ToolName + "  " + compactAgentText(text, 600)
	case protocol.EvToolEnd:
		mark := "✓"
		if ev.IsError {
			mark = "✗"
		}
		return fmt.Sprintf("%s  tool %s  %s  %s", stamp, mark, ev.ToolName, compactAgentText(ev.ToolOutput, 800))
	case protocol.EvError:
		return stamp + "  error  " + compactAgentText(ev.Message, 800)
	case protocol.EvAborted:
		return stamp + "  interrupted"
	case protocol.EvSubagentStarted:
		return stamp + "  started"
	case protocol.EvSubagentStatus:
		if ev.Subagent != nil {
			return stamp + "  status  " + string(ev.Subagent.Status)
		}
	}
	return ""
}

func startsWithSpace(value string) bool {
	for _, r := range value {
		return r == ' ' || r == '\n' || r == '\t' || r == '\r'
	}
	return false
}

func boundedUTF8Tail(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	value = value[len(value)-maxBytes:]
	for len(value) > 0 && !utf8.RuneStart(value[0]) {
		value = value[1:]
	}
	return value
}

func fleetActivityBytes(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line)
	}
	return total
}

func (m *Model) subagentFleetLayout() subagentFleetLayout {
	width := max(20, m.managedFrameWidth()-4)
	height := max(8, m.managedFrameHeight()-4)
	layout := subagentFleetLayout{
		innerWidth:  max(16, width-2),
		innerHeight: max(4, height-2),
	}
	// The header and footer are each constrained to one rendered row.
	layout.bodyHeight = max(1, layout.innerHeight-2)
	layout.wide = layout.innerWidth >= fleetWideMinWidth
	if layout.wide {
		layout.listWidth = max(30, layout.innerWidth*38/100)
		layout.detailWidth = max(20, layout.innerWidth-layout.listWidth-1)
		layout.listHeight = layout.bodyHeight
		layout.detailHeight = layout.bodyHeight
	} else {
		layout.listWidth = layout.innerWidth
		layout.detailWidth = layout.innerWidth
		layout.listHeight = max(4, min(len(m.subagentFleetList.Agents)*2, layout.bodyHeight/2))
		layout.detailHeight = max(1, layout.bodyHeight-layout.listHeight-1)
	}
	return layout
}

func (m *Model) renderSubagentFleetModal() string {
	layout := m.subagentFleetLayout()
	header := m.renderSubagentFleetHeader(layout.innerWidth)
	footer := styleFooter.Render(" ↑/↓ or j/k select · PgUp/PgDn detail · Home/End · r refresh · Esc close ")
	var body string
	if layout.wide {
		left := lipgloss.NewStyle().Width(layout.listWidth).Height(layout.listHeight).MaxHeight(layout.listHeight).Render(m.renderSubagentFleetList(layout.listWidth, layout.listHeight))
		right := lipgloss.NewStyle().Width(layout.detailWidth).Height(layout.detailHeight).MaxHeight(layout.detailHeight).Render(m.renderSubagentFleetDetail(layout.detailWidth, layout.detailHeight))
		divider := styleSep.Render(strings.Repeat("│\n", max(0, layout.bodyHeight-1)) + "│")
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
	} else {
		list := lipgloss.NewStyle().Width(layout.listWidth).Height(layout.listHeight).MaxHeight(layout.listHeight).Render(m.renderSubagentFleetList(layout.listWidth, layout.listHeight))
		sep := styleSep.Render(strings.Repeat("─", layout.innerWidth))
		detail := lipgloss.NewStyle().Width(layout.detailWidth).Height(layout.detailHeight).MaxHeight(layout.detailHeight).Render(m.renderSubagentFleetDetail(layout.detailWidth, layout.detailHeight))
		body = lipgloss.JoinVertical(lipgloss.Left, list, sep, detail)
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Width(layout.innerWidth).Height(layout.innerHeight).Render(content)
}

func (m *Model) renderSubagentFleetHeader(width int) string {
	title := styleHeader.Render(" Subagent fleet inspector ")
	status := fmt.Sprintf("%d running · %d queued · %d finished · capacity %d/%d",
		m.subagentFleetList.Running, m.subagentFleetList.Queued, m.subagentFleetList.Terminal,
		m.subagentFleetList.Running, m.subagentFleetList.ConcurrentLimit)
	if m.subagentFleetLoading {
		status = "refreshing… · " + status
	}
	return title + styleHeaderDim.Render(truncateRunes(status, max(0, width-lipgloss.Width(title))))
}

func (m *Model) renderSubagentFleetList(width, height int) string {
	if m.subagentFleetError != "" {
		return styleError.Render("agents: " + m.subagentFleetError)
	}
	if len(m.subagentFleetList.Agents) == 0 {
		if m.subagentFleetLoading {
			return styleHeaderDim.Render("loading agents…")
		}
		if m.subagentFleetWarning != "" {
			return styleHeaderDim.Render(ansi.Wordwrap(m.subagentFleetWarning, max(1, width), ""))
		}
		return styleHeaderDim.Render("No subagents")
	}
	const rowsPerAgent = 2
	visibleAgents := max(1, height/rowsPerAgent)
	start := m.subagentFleetIndex - visibleAgents/2
	if start < 0 {
		start = 0
	}
	if start+visibleAgents > len(m.subagentFleetList.Agents) {
		start = max(0, len(m.subagentFleetList.Agents)-visibleAgents)
	}
	var rows []string
	for i := start; i < len(m.subagentFleetList.Agents) && len(rows)+rowsPerAgent <= height; i++ {
		state := m.subagentFleetList.Agents[i]
		prefix := "  "
		rowStyle := styleCompletion
		if i == m.subagentFleetIndex {
			prefix = "› "
			rowStyle = styleCompletionSelected
		}
		model := state.Model
		if model == "" {
			model = state.Provider
		}
		// Identity gets its own row so neither a long path nor model ID hides the
		// other. The selected style spans both rows to keep them visually paired.
		identity := fleetStatusGlyph(state.Status) + " " + string(state.Agent.Path)
		metadata := strings.Join(nonEmptyStrings([]string{string(state.Status), model, string(state.Agent.Role)}), " · ")
		rows = append(rows,
			rowStyle.Render(prefix+truncateRunes(identity, max(1, width-2))),
			rowStyle.Render("  "+truncateRunes(metadata, max(1, width-2))),
		)
	}
	return strings.Join(rows, "\n")
}

func fleetStatusGlyph(status protocol.AgentStatus) string {
	switch status {
	case protocol.AgentRunning:
		return "●"
	case protocol.AgentCompleted:
		return "✓"
	case protocol.AgentErrored:
		return "✗"
	case protocol.AgentInterrupted, protocol.AgentShutdown:
		return "■"
	default:
		return "○"
	}
}

func (m *Model) renderSubagentFleetDetail(width, height int) string {
	wrapped := m.subagentFleetDetailLines(width)
	if len(wrapped) == 0 {
		return ""
	}
	maxOffset := max(0, len(wrapped)-height)
	offset := min(max(0, m.subagentFleetDetailOffset), maxOffset)
	if m.subagentFleetDetailEnd {
		offset = maxOffset
	}
	end := min(len(wrapped), offset+height)
	return strings.Join(wrapped[offset:end], "\n")
}

func (m *Model) subagentFleetDetailLines(width int) []string {
	if len(m.subagentFleetList.Agents) == 0 {
		return nil
	}
	state := m.subagentFleetList.Agents[m.subagentFleetIndex]
	if m.subagentFleetDetailState.Agent.ThreadID == state.Agent.ThreadID {
		state = m.subagentFleetDetailState
	}
	lines := []string{
		styleHeader.Render(fmt.Sprintf(" %s %s", fleetStatusGlyph(state.Status), state.Agent.Path)) + styleHeaderDim.Render("  "+string(state.Status)),
		styleHeaderDim.Render(strings.Join(nonEmptyStrings([]string{string(state.Agent.Role), agentModelLabel(state), string(state.Thinking), fmt.Sprintf("generation %d", state.Generation)}), " · ")),
	}
	if elapsed := agentElapsed(state, m.currentTime()); elapsed > 0 {
		lines = append(lines, styleHeaderDim.Render("elapsed "+elapsed.Round(time.Second).String()))
	}
	if state.Usage != nil {
		lines = append(lines, styleHeaderDim.Render(fmt.Sprintf("usage %d total · %d in · %d out", state.Usage.Total, state.Usage.Input, state.Usage.Output)))
	}
	if state.Error != "" {
		lines = append(lines, styleError.Render("error: "+compactAgentText(state.Error, 1200)))
	} else if state.Result != "" {
		lines = append(lines, styleAssistant.Render(" Result"))
		result := m.renderSubagentFleetMarkdown(state.Result, max(10, width-2))
		for _, line := range strings.Split(result, "\n") {
			lines = append(lines, "  "+line)
		}
	}
	if m.subagentFleetDetailLoading {
		lines = append(lines, styleHeaderDim.Render("loading transcript…"))
	} else if m.subagentFleetDetailError != "" {
		lines = append(lines, styleError.Render("transcript: "+m.subagentFleetDetailError))
	} else {
		lines = append(lines, styleHeader.Render(fmt.Sprintf(" Conversation · %d recent messages", len(m.subagentFleetMessages))))
		for _, message := range m.subagentFleetMessages {
			lines = append(lines, m.fleetMessageLines(message, width)...)
		}
	}
	activity := m.subagentFleetActivity[state.Agent.ThreadID]
	if len(activity) > 0 {
		lines = append(lines, styleHeader.Render(" Live activity"))
		lines = append(lines, activity...)
	} else {
		lines = append(lines, styleHeader.Render(" Live activity"), styleHeaderDim.Render("No live events observed in this process."))
	}
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Wordwrap(line, max(1, width), ""), "\n")...)
	}
	return wrapped
}

// renderSubagentFleetMarkdown treats model-authored text as Markdown without
// guessing from a small marker list. Markdown such as emphasis and ordered
// lists otherwise rendered inconsistently while headings and bullets worked.
func (m *Model) renderSubagentFleetMarkdown(text string, width int) string {
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
	}
	text = truncateRunes(text, maxAgentMessagePreviewRunes)
	if m.subagentFleetMD != nil {
		return strings.TrimSpace(m.subagentFleetMD.render(text, width))
	}
	return strings.TrimSpace(ansi.Wordwrap(text, width, ""))
}

func (m *Model) fleetMessageLines(message protocol.Message, width int) []string {
	label, labelStyle := fleetMessageLabel(message)
	prefix := labelStyle.Render(" " + label + " ")
	contentWidth := max(10, width-2)
	var sections []string
	if text := sessionMessageText(message); text != "" {
		if message.Role == protocol.RoleAssistant || message.Role == protocol.RoleAgent {
			text = m.renderSubagentFleetMarkdown(text, contentWidth)
		} else {
			if !utf8.ValidString(text) {
				text = strings.ToValidUTF8(text, "�")
			}
			text = truncateRunes(text, maxAgentMessagePreviewRunes)
			text = strings.TrimSpace(ansi.Wordwrap(text, contentWidth, ""))
		}
		if text != "" {
			sections = append(sections, text)
		}
	}
	for _, block := range message.Content {
		if block.Type != protocol.BlockToolCall {
			continue
		}
		sections = append(sections, fleetToolCallBlock(block, contentWidth))
	}
	if len(sections) == 0 && message.Error != "" {
		sections = append(sections, ansi.Wordwrap(message.Error, contentWidth, ""))
	}
	if len(sections) == 0 {
		sections = append(sections, styleHeaderDim.Render("(no text content)"))
	}
	lines := []string{prefix}
	for _, section := range sections {
		for _, line := range strings.Split(section, "\n") {
			lines = append(lines, "  "+line)
		}
	}
	return append(lines, "")
}

func fleetMessageLabel(message protocol.Message) (string, lipgloss.Style) {
	label := string(message.Role)
	style := styleCompletion
	switch message.Role {
	case protocol.RoleAssistant:
		style = styleAssistant
	case protocol.RoleTool:
		label = "tool"
		style = styleTool
	case protocol.RoleUser:
		style = styleUser
	case protocol.RoleAgent:
		style = styleHeader
	}
	if message.ToolName != "" {
		label += " · " + message.ToolName
	}
	if message.StopReason != "" {
		label += " · " + string(message.StopReason)
	}
	if message.IsError {
		label += " · error"
	}
	return label, style
}

func fleetToolCallBlock(block protocol.ContentBlock, width int) string {
	args := strings.TrimSpace(string(block.Arguments))
	if args != "" {
		var formatted any
		if json.Unmarshal(block.Arguments, &formatted) == nil {
			if encoded, err := json.MarshalIndent(formatted, "", "  "); err == nil {
				args = string(encoded)
			}
		}
	}
	label := styleTool.Render("call · " + block.Name)
	if args == "" {
		return label
	}
	args = sanitizeToolPreview(args, maxAgentMessagePreviewRunes)
	var lines []string
	for _, line := range strings.Split(args, "\n") {
		wrapped := strings.Split(ansi.Wordwrap(line, max(1, width-2), ""), "\n")
		for _, part := range wrapped {
			lines = append(lines, styleHeaderDim.Render("│ "+part))
		}
	}
	return label + "\n" + strings.Join(lines, "\n")
}
