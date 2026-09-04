package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/app"
)

func (m *Model) openProcessFleet(target string) tea.Cmd {
	m.closeSubagentFleet()
	m.processFleetOpen = true
	m.processFleetLoading = true
	m.processFleetError = ""
	m.processFleetRequested = strings.TrimSpace(target)
	m.processFleetGeneration++
	m.processFleetList = nil
	m.processFleetIndex = 0
	m.resetProcessFleetOutput()
	return m.fetchProcessFleetCmd(m.processFleetGeneration, m.processFleetRequested)
}

func (m *Model) fetchProcessFleetCmd(generation uint64, target string) tea.Cmd {
	appRuntime := m.app
	ctx := m.ctx
	return func() tea.Msg {
		list, err := appRuntime.ListManagedProcesses(ctx)
		return processFleetListMsg{generation: generation, target: target, list: list, err: err}
	}
}

func (m *Model) scheduleProcessFleetRefresh() tea.Cmd {
	generation := m.processFleetGeneration
	m.processFleetTickGeneration++
	tick := m.processFleetTickGeneration
	return tea.Tick(processFleetRefreshInterval, func(time.Time) tea.Msg {
		return processFleetTickMsg{generation: generation, tick: tick}
	})
}

func (m *Model) refreshProcessFleet() tea.Cmd {
	if !m.processFleetOpen || m.processFleetLoading {
		return nil
	}
	m.processFleetLoading = true
	return m.fetchProcessFleetCmd(m.processFleetGeneration, m.processFleetSelectedID())
}

func (m *Model) applyProcessFleetList(msg processFleetListMsg) tea.Cmd {
	if !m.processFleetOpen || msg.generation != m.processFleetGeneration {
		return nil
	}
	m.processFleetLoading = false
	if msg.err != nil {
		m.processFleetError = msg.err.Error()
		return m.scheduleProcessFleetRefresh()
	}
	selectedBefore := m.processFleetSelectedID()
	m.processFleetList = slices.Clone(msg.list)
	m.processFleetError = ""
	if len(m.processFleetList) == 0 {
		m.processFleetIndex = 0
		m.resetProcessFleetOutput()
		return m.scheduleProcessFleetRefresh()
	}
	// Keyboard selection may have changed while this snapshot was in flight.
	// Preserve the live selection; the request target is only needed for the
	// initial explicit /processes ID_OR_NAME preselection.
	target := selectedBefore
	if target == "" {
		target = strings.TrimSpace(msg.target)
	}
	m.processFleetIndex = 0
	matched := target == ""
	for i, state := range m.processFleetList {
		if target == state.ProcessID || target == state.Name {
			m.processFleetIndex = i
			matched = true
			break
		}
	}
	if !matched {
		m.processFleetError = "process not found: " + target
		m.resetProcessFleetOutput()
		return m.scheduleProcessFleetRefresh()
	}
	if selectedBefore != m.processFleetSelectedID() || m.processFleetOutputID != m.processFleetSelectedID() {
		m.resetProcessFleetOutput()
	}
	return tea.Batch(m.loadProcessFleetLogs(), m.scheduleProcessFleetRefresh())
}

func (m *Model) loadProcessFleetLogs() tea.Cmd {
	target := m.processFleetSelectedID()
	if target == "" || m.processFleetLogLoading {
		return nil
	}
	m.processFleetLogGeneration++
	generation := m.processFleetLogGeneration
	m.processFleetLogLoading = true
	m.processFleetOutputID = target
	var cursor *int64
	if m.processFleetCursorSet {
		value := m.processFleetCursor
		cursor = &value
	}
	appRuntime := m.app
	ctx := m.ctx
	return func() tea.Msg {
		var combined app.ManagedProcessLogs
		for chunk := range processFleetLogBatchChunks {
			logs, err := appRuntime.ManagedProcessLogs(ctx, target, cursor, processFleetLogReadBytes)
			if err != nil {
				return processFleetLogsMsg{generation: generation, target: target, err: err}
			}
			if chunk == 0 {
				combined = logs
			} else {
				combined.Status = logs.Status
				combined.Output += logs.Output
				combined.NextCursor = logs.NextCursor
				combined.Omitted += logs.Omitted
				combined.EOF = logs.EOF
			}
			if logs.EOF || logs.NextCursor == cursorValue(cursor) || logs.Output == "" {
				break
			}
			next := logs.NextCursor
			cursor = &next
		}
		return processFleetLogsMsg{generation: generation, target: target, logs: combined}
	}
}

func cursorValue(cursor *int64) int64 {
	if cursor == nil {
		return 0
	}
	return *cursor
}

func (m *Model) applyProcessFleetLogs(msg processFleetLogsMsg) {
	if !m.processFleetOpen || msg.generation != m.processFleetLogGeneration || msg.target != m.processFleetSelectedID() {
		return
	}
	m.processFleetLogLoading = false
	if msg.err != nil {
		m.processFleetLogError = msg.err.Error()
		return
	}
	m.processFleetLogError = ""
	changed := false
	if msg.logs.Omitted > 0 {
		notice := fmt.Sprintf("[... %d older output bytes omitted ...]\n", msg.logs.Omitted)
		m.processFleetOutput += notice
		changed = true
	}
	if msg.logs.Output != "" {
		m.processFleetOutput += sanitizeProcessOutput(msg.logs.Output)
		changed = true
	}
	if len(m.processFleetOutput) > processFleetOutputLimit {
		const notice = "[... older panel output omitted ...]\n"
		m.processFleetOutput = notice + boundedUTF8Tail(m.processFleetOutput, processFleetOutputLimit-len(notice))
		changed = true
	}
	if changed {
		m.invalidateProcessFleetOutput()
	}
	m.processFleetCursor = msg.logs.NextCursor
	m.processFleetCursorSet = true
	m.processFleetEOF = msg.logs.EOF
}

func (m *Model) invalidateProcessFleetOutput() {
	m.processFleetOutputVersion++
	m.processFleetWrappedOutput = nil
	m.processFleetWrappedWidth = 0
}

func (m *Model) resetProcessFleetOutput() {
	m.processFleetLogGeneration++
	m.processFleetLogLoading = false
	m.processFleetOutputID = ""
	m.processFleetOutput = ""
	m.invalidateProcessFleetOutput()
	m.processFleetCursor = 0
	m.processFleetCursorSet = false
	m.processFleetEOF = false
	m.processFleetLogError = ""
	m.processFleetDetailOffset = 0
	m.processFleetDetailEnd = true
}

func (m *Model) closeProcessFleet() {
	m.processFleetOpen = false
	m.processFleetLoading = false
	m.processFleetGeneration++
	m.processFleetRequested = ""
	m.processFleetError = ""
	m.resetProcessFleetOutput()
}

func (m *Model) processFleetSelectedID() string {
	if m.processFleetIndex < 0 || m.processFleetIndex >= len(m.processFleetList) {
		return ""
	}
	return m.processFleetList[m.processFleetIndex].ProcessID
}

func (m *Model) handleProcessFleetMouse(msg tea.MouseMsg) {
	event := tea.MouseEvent(msg)
	if event.Action != tea.MouseActionPress {
		return
	}
	delta := max(1, m.transcript.MouseWheelDelta)
	switch event.Button {
	case tea.MouseButtonWheelUp:
		m.scrollProcessFleetDetail(-delta)
	case tea.MouseButtonWheelDown:
		m.scrollProcessFleetDetail(delta)
	}
}

func (m *Model) handleProcessFleetKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	count := len(m.processFleetList)
	switch msg.Type {
	case tea.KeyEsc:
		m.closeProcessFleet()
		return m, nil
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'q':
				m.closeProcessFleet()
				return m, nil
			case 'r':
				return m, m.refreshProcessFleet()
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
	previous := m.processFleetIndex
	switch msg.Type {
	case tea.KeyUp:
		m.processFleetIndex = (m.processFleetIndex - 1 + count) % count
	case tea.KeyDown:
		m.processFleetIndex = (m.processFleetIndex + 1) % count
	case tea.KeyPgUp:
		m.scrollProcessFleetDetail(-m.processFleetDetailPageSize())
	case tea.KeyPgDown:
		m.scrollProcessFleetDetail(m.processFleetDetailPageSize())
	case tea.KeyHome:
		m.processFleetDetailOffset = 0
		m.processFleetDetailEnd = false
	case tea.KeyEnd:
		m.processFleetDetailOffset = max(0, m.processFleetDetailLineCount()-m.processFleetDetailPageSize())
		m.processFleetDetailEnd = true
	}
	if m.processFleetIndex != previous {
		m.resetProcessFleetOutput()
		return m, m.loadProcessFleetLogs()
	}
	return m, nil
}

func (m *Model) scrollProcessFleetDetail(delta int) {
	page := m.processFleetDetailPageSize()
	maxOffset := max(0, m.processFleetDetailLineCount()-page)
	current := min(max(0, m.processFleetDetailOffset), maxOffset)
	if m.processFleetDetailEnd {
		current = maxOffset
	}
	next := min(max(0, current+delta), maxOffset)
	m.processFleetDetailOffset = next
	m.processFleetDetailEnd = next >= maxOffset
}

func (m *Model) processFleetDetailPageSize() int {
	return m.processFleetLayout().detailHeight
}

func (m *Model) processFleetDetailLineCount() int {
	return len(m.processFleetDetailLines(m.processFleetLayout().detailWidth))
}

func (m *Model) processFleetLayout() processFleetLayout {
	width := max(20, m.managedFrameWidth()-4)
	height := max(8, m.managedFrameHeight()-4)
	layout := processFleetLayout{innerWidth: max(16, width-2), innerHeight: max(4, height-2)}
	layout.bodyHeight = max(1, layout.innerHeight-2)
	layout.wide = layout.innerWidth >= fleetWideMinWidth
	if layout.wide {
		layout.listWidth = max(30, layout.innerWidth*36/100)
		layout.detailWidth = max(20, layout.innerWidth-layout.listWidth-1)
		layout.listHeight = layout.bodyHeight
		layout.detailHeight = layout.bodyHeight
	} else {
		layout.listWidth = layout.innerWidth
		layout.detailWidth = layout.innerWidth
		layout.listHeight = max(4, min(len(m.processFleetList)*2, layout.bodyHeight/2))
		layout.detailHeight = max(1, layout.bodyHeight-layout.listHeight-1)
	}
	return layout
}

func (m *Model) renderProcessFleetModal() string {
	layout := m.processFleetLayout()
	header := m.renderProcessFleetHeader(layout.innerWidth)
	footer := styleFooter.Render(" ↑/↓ or j/k select · PgUp/PgDn output · Home/End · r refresh · " + m.keys.Agents.Help().Key + " agents · Esc close ")
	var body string
	if layout.wide {
		left := lipgloss.NewStyle().Width(layout.listWidth).Height(layout.listHeight).MaxHeight(layout.listHeight).Render(m.renderProcessFleetList(layout.listWidth, layout.listHeight))
		right := lipgloss.NewStyle().Width(layout.detailWidth).Height(layout.detailHeight).MaxHeight(layout.detailHeight).Render(m.renderProcessFleetDetail(layout.detailWidth, layout.detailHeight))
		divider := styleSep.Render(strings.Repeat("│\n", max(0, layout.bodyHeight-1)) + "│")
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
	} else {
		list := lipgloss.NewStyle().Width(layout.listWidth).Height(layout.listHeight).MaxHeight(layout.listHeight).Render(m.renderProcessFleetList(layout.listWidth, layout.listHeight))
		sep := styleSep.Render(strings.Repeat("─", layout.innerWidth))
		detail := lipgloss.NewStyle().Width(layout.detailWidth).Height(layout.detailHeight).MaxHeight(layout.detailHeight).Render(m.renderProcessFleetDetail(layout.detailWidth, layout.detailHeight))
		body = lipgloss.JoinVertical(lipgloss.Left, list, sep, detail)
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Width(layout.innerWidth).Height(layout.innerHeight).Render(content)
}

func (m *Model) renderProcessFleetHeader(width int) string {
	running, finished := 0, 0
	for _, state := range m.processFleetList {
		if state.Status == "running" {
			running++
		} else {
			finished++
		}
	}
	title := styleHeader.Render(" Process fleet inspector ")
	status := fmt.Sprintf("%d running · %d finished · auto-refresh", running, finished)
	if m.processFleetLoading {
		status = "refreshing… · " + status
	}
	return title + styleHeaderDim.Render(truncateRunes(status, max(0, width-lipgloss.Width(title))))
}

func (m *Model) renderProcessFleetList(width, height int) string {
	if m.processFleetError != "" {
		return styleError.Render("processes: " + m.processFleetError)
	}
	if len(m.processFleetList) == 0 {
		if m.processFleetLoading {
			return styleHeaderDim.Render("loading processes…")
		}
		return styleHeaderDim.Render("No managed processes in this session")
	}
	const rowsPerProcess = 2
	visible := max(1, height/rowsPerProcess)
	start := max(m.processFleetIndex-visible/2, 0)
	if start+visible > len(m.processFleetList) {
		start = max(0, len(m.processFleetList)-visible)
	}
	rows := make([]string, 0, visible*rowsPerProcess)
	for i := start; i < len(m.processFleetList) && len(rows)+rowsPerProcess <= height; i++ {
		state := m.processFleetList[i]
		prefix := "  "
		rowStyle := styleCompletion
		if i == m.processFleetIndex {
			prefix = "› "
			rowStyle = styleCompletionSelected
		}
		identity := processStateGlyph(state) + " " + state.Name
		metadata := strings.Join(nonEmptyStrings([]string{state.Status, processReadyLabel(state), processExitLabel(state), shortProcessID(state.ProcessID)}), " · ")
		rows = append(rows,
			rowStyle.Render(prefix+truncateRunes(identity, max(1, width-2))),
			rowStyle.Render("  "+truncateRunes(metadata, max(1, width-2))),
		)
	}
	return strings.Join(rows, "\n")
}

func processStateGlyph(state app.ManagedProcessState) string {
	if state.Status == "exited" && state.ExitCode != nil && *state.ExitCode != 0 {
		return "✗"
	}
	switch state.Status {
	case "running":
		return "●"
	case "exited":
		return "✓"
	case "failed":
		return "✗"
	case "stopped":
		return "■"
	default:
		return "○"
	}
}

func processReadyLabel(state app.ManagedProcessState) string {
	if state.Status == "running" && state.Ready {
		return "ready"
	}
	return ""
}

func processExitLabel(state app.ManagedProcessState) string {
	if state.ExitCode == nil {
		return ""
	}
	return fmt.Sprintf("exit %d", *state.ExitCode)
}

func shortProcessID(id string) string {
	if len(id) <= 13 {
		return id
	}
	return "proc_…" + id[len(id)-8:]
}

func (m *Model) renderProcessFleetDetail(width, height int) string {
	lines := m.processFleetDetailLines(width)
	if len(lines) == 0 {
		return ""
	}
	maxOffset := max(0, len(lines)-height)
	offset := min(max(0, m.processFleetDetailOffset), maxOffset)
	if m.processFleetDetailEnd {
		offset = maxOffset
	}
	return strings.Join(lines[offset:min(len(lines), offset+height)], "\n")
}

func (m *Model) processFleetDetailLines(width int) []string {
	if len(m.processFleetList) == 0 || m.processFleetIndex < 0 || m.processFleetIndex >= len(m.processFleetList) {
		return nil
	}
	state := m.processFleetList[m.processFleetIndex]
	lines := []string{
		styleHeader.Render(fmt.Sprintf(" %s %s", processStateGlyph(state), state.Name)) + styleHeaderDim.Render("  "+state.Status),
		styleHeaderDim.Render(state.ProcessID),
	}
	if state.StartedAt > 0 {
		started := time.UnixMilli(state.StartedAt)
		lines = append(lines, styleHeaderDim.Render("started "+started.Format("15:04:05")+" · elapsed "+processElapsed(state, m.currentTime())))
	}
	if state.Ready {
		lines = append(lines, styleDiffAdd.Render("ready"))
	}
	if state.ExitCode != nil {
		lines = append(lines, styleHeaderDim.Render(fmt.Sprintf("exit code %d", *state.ExitCode)))
	}
	if state.Signal != "" {
		lines = append(lines, styleHeaderDim.Render("signal "+state.Signal))
	}
	if state.Reason != "" && state.Reason != "natural" {
		lines = append(lines, styleHeaderDim.Render("reason "+state.Reason))
	}
	streamState := "live"
	if m.processFleetEOF {
		streamState = "complete"
	}
	lines = append(lines, styleHeader.Render(" Combined stdout / stderr")+styleHeaderDim.Render(" · "+streamState))
	if m.processFleetLogError != "" {
		lines = append(lines, styleError.Render("logs: "+m.processFleetLogError))
	} else if m.processFleetOutput == "" {
		if m.processFleetLogLoading {
			lines = append(lines, styleHeaderDim.Render("loading output…"))
		} else {
			lines = append(lines, styleHeaderDim.Render("No output captured yet."))
		}
	} else {
		lines = append(lines, m.processOutputLines(width)...)
	}
	return lines
}

func processElapsed(state app.ManagedProcessState, now time.Time) string {
	start := time.UnixMilli(state.StartedAt)
	end := now
	if state.FinishedAt > 0 {
		end = time.UnixMilli(state.FinishedAt)
	}
	if end.Before(start) {
		return "0s"
	}
	return end.Sub(start).Round(time.Second).String()
}

func (m *Model) processOutputLines(width int) []string {
	width = max(1, width)
	if m.processFleetWrappedOutput != nil && m.processFleetWrappedVersion == m.processFleetOutputVersion && m.processFleetWrappedWidth == width {
		return m.processFleetWrappedOutput
	}
	output := sanitizeProcessOutput(m.processFleetOutput)
	lines := make([]string, 0, strings.Count(output, "\n")+1)
	for line := range strings.SplitSeq(output, "\n") {
		wrapped := ansi.Wordwrap(line, width, "")
		lines = append(lines, strings.Split(wrapped, "\n")...)
	}
	m.processFleetWrappedVersion = m.processFleetOutputVersion
	m.processFleetWrappedWidth = width
	m.processFleetWrappedOutput = lines
	return m.processFleetWrappedOutput
}

func sanitizeProcessOutput(output string) string {
	output = strings.ReplaceAll(strings.ToValidUTF8(output, "�"), "\r\n", "\n")
	var safe strings.Builder
	safe.Grow(len(output))
	for _, r := range output {
		switch {
		case r == '\n':
			safe.WriteRune(r)
		case r == '\r':
			safe.WriteRune('\n')
		case r == '\t':
			safe.WriteString("    ")
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			fmt.Fprintf(&safe, "\\x%02x", r)
		default:
			safe.WriteRune(r)
		}
	}
	return safe.String()
}
