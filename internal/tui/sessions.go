package tui

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *Model) switchSession(st session.Store) error {
	if err := m.app.SetSession(st); err != nil {
		return err
	}
	m.fenceRootTurnProjection()
	m.startupResumeRequired = false
	m.pickSession = false
	m.sessions = nil
	m.sessionIndex = 0
	// Child runtimes are scoped to the root session. The app manager detaches
	// terminal children during a successful switch; discard their old UI
	// snapshots before restored topology for the new session is delivered.
	m.subagentFleetActivity = make(map[string][]string)
	m.subagentFleetActivityKinds = make(map[string]protocol.AgentEventType)
	m.subagentFleetActivitySpace = make(map[string]bool)
	m.subagentFleetList = protocol.SubagentList{}
	m.subagentFleetMessages = nil
	m.subagentFleetDetailState = protocol.SubagentState{}
	m.closeSubagentFleet()
	m.processFleetList = nil
	m.closeProcessFleet()
	m.assistantBuf.Reset()
	m.thinkingBuf.Reset()
	m.planBuf.Reset()
	m.latestPlan = ""
	goalState, goalStateErr := m.app.GoalState()
	if goalStateErr != nil {
		m.goal = nil
		m.pushLine(styleError.Render("restore goal: " + goalStateErr.Error()))
	} else {
		m.goal = goalState
	}
	m.planPrompt = false
	m.pendingMode = nil
	m.modeSwitchReady = false
	m.modeSwitching = false
	m.setRunIdle()
	m.transcript.GotoBottom()
	m.transcript.SetContent("")
	m.transcriptContent = ""
	m.transcriptViewRevision++
	m.transcriptViewCacheValid = false
	m.transcriptSelectionLines = nil
	m.transcriptSelectionView = ""
	m.transcriptSelectionViewValid = false
	m.transcriptSelectionRendered = ""
	m.transcriptSelectionRenderedValid = false
	m.transcriptBase = ""
	m.transcriptBaseSynced = 0
	m.transcriptDropped = 0
	m.transcriptBytes = 0
	m.hydrateSession()
	if err := m.app.ReadyGoal(); err != nil {
		// SetSession has already committed this store across App, Agent, Goal,
		// permissions, and subagents. Readiness failures are diagnostics; returning
		// an error would make the caller close the now-active store.
		m.pushLine(styleError.Render("continue restored goal: " + err.Error()))
	}
	if m.goal != nil && (m.goal.Status == protocol.GoalPaused || m.goal.Status == protocol.GoalBlocked || m.goal.Status == protocol.GoalUsageLimited) {
		if m.goal.Status == protocol.GoalBlocked {
			m.pushLine(renderBlockedGoalTranscript(m.goal))
		}
		m.pushLine(styleFooter.Render(fmt.Sprintf("Resume %s goal? Use /goal resume to continue.", m.goal.Status)))
	}
	return nil
}

func (m *Model) inlineSessionKey() string {
	if m.app == nil || m.app.Session == nil {
		return ""
	}
	key := m.app.Session.ID()
	if branches, ok := m.app.Session.(session.BranchStore); ok {
		if list, err := branches.Branches(); err == nil {
			for _, branch := range list {
				if branch.Active {
					return key + "\x00" + branch.ID
				}
			}
		}
	}
	return key
}

func boundedInlineHydration(hydrated []string, common int, switched bool) []string {
	common = min(max(0, common), len(hydrated))
	rows := hydrated[common:]
	const hydrationSegmentLimit = 2000
	omitted := 0
	if len(rows) > hydrationSegmentLimit {
		omitted = len(rows) - hydrationSegmentLimit
		rows = rows[len(rows)-hydrationSegmentLimit:]
	}
	out := make([]string, 0, len(rows)+2)
	if switched {
		out = append(out, styleFooter.Render("── switched transcript ──"))
	}
	if omitted > 0 {
		out = append(out, styleFooter.Render(fmt.Sprintf("── %d older transcript segments omitted ──", omitted)))
	}
	return append(out, rows...)
}

func (m *Model) hydrateSession() {
	defer m.clearFinalizedMarkdownCaches()
	m.clearTranscriptSelection()
	if m.app == nil || m.app.Agent == nil {
		m.inputHistory = nil
		m.resetInputHistoryNavigation()
		m.lines = nil
		m.transcriptBase = ""
		m.transcriptBaseSynced = 0
		m.transcriptDropped = 0
		m.transcriptBytes = 0
		m.inlineCommitted = 0
		m.inlinePrintEnd = 0
		m.inlinePrintInFlight = false
		m.inlinePrintGeneration++
		m.inlineHeaderPending = false
		m.transcriptBaseDirty = true
		m.transcriptDirty = true
		m.refreshTranscript()
		return
	}
	messages, err := m.app.Agent.Messages()
	if err != nil {
		m.pushLine(styleError.Render("session read: " + err.Error()))
		return
	}
	m.hydrateInputHistory(messages)
	renderMessages := messages
	durableIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		durableIDs = append(durableIDs, message.ID)
	}
	if entries, ok := m.app.Session.(session.BranchEntryStore); ok {
		if branch, branchErr := entries.BranchEntries(); branchErr == nil {
			renderMessages = make([]protocol.Message, 0, len(messages))
			durableIDs = make([]string, 0, len(branch))
			for _, entry := range branch {
				durableIDs = append(durableIDs, entry.ID)
				switch {
				case entry.Type == session.EntryMessage && entry.Message != nil:
					renderMessages = append(renderMessages, entry.Message.Clone())
				case entry.Type == session.EntryMeta && entry.Key == session.MetaToolTranscript:
					var transcript protocol.ToolTranscript
					if json.Unmarshal([]byte(entry.Value), &transcript) == nil && transcript.ToolName != "" {
						renderMessages = append(renderMessages, protocol.Message{
							ID:          entry.ID,
							Role:        protocol.RoleTool,
							ToolName:    transcript.ToolName,
							IsError:     transcript.IsError,
							ToolDisplay: transcript.Display.Clone(),
						})
					}
				}
			}
		}
	}
	key := m.inlineSessionKey()
	if m.inlineTranscript && key != "" && key == m.inlineHistoryKey {
		// Live events already committed this branch's new rows. Track durable
		// identity for a future branch switch without replaying history now.
		m.inlineDurableMessageIDs = durableIDs
		m.refreshContextUsage(messages)
		return
	}
	hadPrintedHistory := m.inlineTranscript && m.inlineEverCommitted
	m.inlineCommitted = 0
	m.inlinePrintEnd = 0
	m.inlinePrintInFlight = false
	m.inlinePrintGeneration++
	m.latestPlan = ""
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshContextUsage(messages)
	hydrated := make([]string, 0, min(len(messages), maxTranscriptEntries))
	hydratedIDs := make([]string, 0, min(len(messages), maxTranscriptEntries))
	appendHydrated := func(id, row string) {
		hydrated = append(hydrated, row)
		hydratedIDs = append(hydratedIDs, id)
	}
	toolCalls := make(map[string]protocol.ContentBlock)
	for _, msg := range renderMessages {
		switch msg.Role {
		case protocol.RoleUser:
			text := sessionMessageText(msg)
			images := sessionMessageImageCount(msg)
			if text != "" || images > 0 {
				if text == "" {
					text = fmt.Sprintf("[%d image(s)]", images)
				} else if images > 0 {
					text += fmt.Sprintf(" [%d image(s)]", images)
				}
				appendHydrated(msg.ID, styleUser.Render("› "+text))
			}
		case protocol.RoleAssistant:
			if thinking := sessionMessageThinking(msg); thinking != "" {
				appendHydrated(msg.ID, m.renderThinkingBody(thinking))
			}
			for _, block := range msg.Content {
				switch block.Type {
				case protocol.BlockText:
					if strings.TrimSpace(block.Text) != "" {
						appendHydrated(msg.ID, m.renderAssistantBody(block.Text))
					}
				case protocol.BlockPlan:
					if strings.TrimSpace(block.Text) != "" {
						m.latestPlan = block.Text
						appendHydrated(msg.ID, m.renderPlanBody(block.Text))
					}
				case protocol.BlockToolCall:
					if block.ToolCallID != "" {
						toolCalls[block.ToolCallID] = block
					}
				}
			}
			switch msg.StopReason {
			case protocol.StopAborted:
				appendHydrated(msg.ID, styleError.Render("aborted"))
			case protocol.StopError:
				if message := strings.TrimSpace(msg.Error); message != "" {
					for _, prefix := range []string{"agent: provider stream: ", "agent: provider chat: ", "agent: provider resolve: ", "agent: "} {
						message = strings.TrimPrefix(message, prefix)
					}
					appendHydrated(msg.ID, styleError.Render("✖ "+message))
				}
			}
		case protocol.RoleTool:
			call, _ := toolCalls[msg.ToolCallID]
			for _, row := range m.hydratedToolTranscriptRows(msg, call) {
				appendHydrated(msg.ID, row)
			}
		}
	}
	hydrationOmitted := 0
	if !m.inlineTranscript && len(hydrated) > maxTranscriptEntries {
		hydrationOmitted = len(hydrated) - (maxTranscriptEntries - 1)
		hydrated = hydrated[hydrationOmitted:]
		hydratedIDs = hydratedIDs[hydrationOmitted:]
	}
	m.lines = nil
	m.transcriptBase = ""
	m.transcriptBaseSynced = 0
	m.transcriptDropped = 0
	m.transcriptBytes = 0
	if m.inlineTranscript {
		m.lines = hydrated
		commonRows := 0
		if hadPrintedHistory {
			commonMessages := 0
			limit := min(len(m.inlineDurableMessageIDs), len(durableIDs))
			for commonMessages < limit && m.inlineDurableMessageIDs[commonMessages] == durableIDs[commonMessages] {
				commonMessages++
			}
			shared := make(map[string]struct{}, commonMessages)
			for _, id := range durableIDs[:commonMessages] {
				shared[id] = struct{}{}
			}
			for commonRows < len(hydratedIDs) {
				if _, ok := shared[hydratedIDs[commonRows]]; !ok {
					break
				}
				commonRows++
			}
		}
		m.inlineHistoryKey = key
		m.inlineDurableMessageIDs = durableIDs
		m.inlineHeaderPending = true
		m.lines = boundedInlineHydration(hydrated, commonRows, hadPrintedHistory)
	} else {
		if hydrationOmitted > 0 {
			m.transcriptDropped = hydrationOmitted
			marker := styleFooter.Render(fmt.Sprintf("── %d older transcript entries omitted ──", hydrationOmitted))
			m.lines = append(m.lines, marker)
			m.transcriptBytes += len(marker)
		}
		for _, row := range hydrated {
			m.appendTranscriptLine(row)
		}
	}
	m.refreshTranscript()
}

func (m *Model) hydratedToolTranscriptRows(msg protocol.Message, call protocol.ContentBlock) []string {
	display := msg.ToolDisplay
	startMessage := ""
	output := ""
	durationMS := int64(0)
	if display != nil {
		if display.Started {
			startMessage = display.StartMessage
		}
		output = display.Output
		durationMS = display.DurationMS
	} else {
		// Older sessions predate durable tool-card metadata. Reconstruct the
		// closest safe equivalent from the assistant call and tool result.
		if call.Type == protocol.BlockToolCall && legacyToolWasDispatched(msg) {
			startMessage = persistedToolStartMessage(msg.ToolName, call.Arguments)
		}
		output = legacyToolDisplayOutput(msg)
	}

	message := ""
	if msg.IsError {
		message = output
	}
	return m.toolEndTranscriptRows(msg.ToolName, startMessage, durationMS, message, output, msg.IsError)
}

func persistedToolStartMessage(name string, arguments json.RawMessage) string {
	switch name {
	case "bash":
		var input struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(arguments, &input) == nil && input.Command != "" {
			return input.Command
		}
	case "edit", "write":
		var input struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(arguments, &input) == nil && input.Path != "" {
			return input.Path
		}
	}
	return "running"
}

func legacyToolWasDispatched(msg protocol.Message) bool {
	if !msg.IsError {
		return true
	}
	text := sessionMessageText(msg)
	for _, prefix := range []string{
		"Permission denied:",
		"Error: tool arguments are not valid JSON:",
		"Error: unknown tool ",
		"Error: tool call cancelled:",
		"Error: tool call skipped ",
	} {
		if strings.HasPrefix(text, prefix) {
			return false
		}
	}
	return !strings.Contains(text, " is unavailable in ")
}

func legacyToolDisplayOutput(msg protocol.Message) string {
	switch msg.ToolName {
	case "get_goal", "create_goal", "update_goal":
		return "(private goal state updated)"
	default:
		return sessionMessageText(msg)
	}
}

func sessionMessageText(msg protocol.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Type == protocol.BlockText {
			b.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func sessionMessageImageCount(msg protocol.Message) int {
	count := 0
	for _, block := range msg.Content {
		if block.Type == protocol.BlockImage {
			count++
		}
	}
	return count
}

func sessionMessageThinking(msg protocol.Message) string {
	var parts []string
	for _, block := range msg.Content {
		if block.Type == protocol.BlockThinking && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, strings.TrimSpace(block.Text))
		}
	}
	return strings.Join(parts, "\n\n")
}

func (m *Model) refreshContextUsageFromSession() {
	if m.app == nil || m.app.Agent == nil {
		m.applyContextUsageSnapshot(contextUsageSnapshot{})
		return
	}
	projected, err := m.app.Agent.ContextMessages()
	if err != nil {
		return
	}
	compacted := len(projected) > 0 && projected[0].Role == protocol.RoleCustom
	m.refreshProjectedContextUsage(projected, compacted)
}

func (m *Model) scheduleContextUsageRefresh() tea.Cmd {
	if !m.contextRefreshNeeded || m.contextRefreshPending || m.app == nil || m.app.Agent == nil {
		return nil
	}
	m.contextRefreshNeeded = false
	m.contextRefreshPending = true
	version := m.contextRefreshVersion
	a := m.app.Agent
	return func() tea.Msg {
		projected, err := a.ContextMessages()
		compacted := len(projected) > 0 && projected[0].Role == protocol.RoleCustom
		return contextUsageRefreshMsg{version: version, snapshot: projectedContextUsage(projected, compacted), err: err}
	}
}

func (m *Model) refreshContextUsage(messages []protocol.Message) {
	projected := messages
	compacted := false
	if contextStore, ok := m.app.Session.(session.ContextStore); ok {
		if contextMessages, err := contextStore.ContextMessages(); err == nil {
			projected = contextMessages
			compacted = len(contextMessages) != len(messages) ||
				(len(contextMessages) > 0 && contextMessages[0].Role == protocol.RoleCustom)
		}
	}
	m.refreshProjectedContextUsage(projected, compacted)
}

func (m *Model) refreshProjectedContextUsage(projected []protocol.Message, compacted bool) {
	m.applyContextUsageSnapshot(projectedContextUsage(projected, compacted))
}

func (m *Model) applyContextUsageSnapshot(snapshot contextUsageSnapshot) {
	m.lastUsage = snapshot.usage.Clone()
	m.lastRequestUsage = snapshot.usage.Clone()
	m.contextTokens = snapshot.tokens
	m.contextEstimated = snapshot.estimated
	m.contextRefreshVersion++
}

func projectedContextUsage(projected []protocol.Message, compacted bool) contextUsageSnapshot {
	if compacted {
		tokens := estimateContextTokens(projected)
		return contextUsageSnapshot{tokens: tokens, estimated: tokens > 0}
	}
	for i := len(projected) - 1; i >= 0; i-- {
		usage := projected[i].Usage
		if usage == nil {
			continue
		}
		snapshot := contextUsageSnapshot{usage: usage.Clone(), tokens: contextTokensFromUsage(*usage)}
		if i+1 < len(projected) {
			snapshot.tokens += estimateContextTokens(projected[i+1:])
			snapshot.estimated = true
		}
		return snapshot
	}
	tokens := estimateContextTokens(projected)
	return contextUsageSnapshot{tokens: tokens, estimated: tokens > 0}
}

func contextTokensFromUsage(usage protocol.Usage) int {
	if usage.Total > 0 {
		return usage.Total
	}
	return usage.Input + usage.Output
}

func estimateContextTokens(messages []protocol.Message) int {
	chars := 0
	for _, message := range messages {
		chars += len(message.Role) + 8
		for _, block := range message.Content {
			chars += len(block.Type) + len(block.Text) + len(block.Name) +
				len(block.ToolCallID) + len(block.Arguments) + 8
		}
	}
	if chars == 0 {
		return 0
	}
	return (chars + 3) / 4
}

func formatContextReport(report agent.ContextReport, currentTokens int, currentEstimated bool) string {
	rawInput := report.EstimatedInputTokens
	calibrated := currentTokens > 0 || (report.Usage != nil && report.Usage.Input > 0)
	if currentTokens <= 0 {
		switch {
		case report.Usage != nil && report.Usage.Input > 0 && report.Usage.Total > 0:
			currentTokens = report.Usage.Total
		case report.Usage != nil && report.Usage.Input > 0:
			currentTokens = report.Usage.Input + report.Usage.Output
		default:
			currentTokens = rawInput
			currentEstimated = currentTokens > 0
		}
	}

	inputTarget := currentTokens
	generatedTokens := 0
	if report.LatestRequest && report.Usage != nil && report.Usage.Input > 0 {
		inputTarget = report.Usage.Input
		if currentTokens < inputTarget {
			currentTokens = inputTarget
		}
		generatedTokens = currentTokens - inputTarget
	}
	categories := calibrateContextCategories(report.Categories, rawInput, inputTarget)

	scope := "stored context preflight"
	if report.LatestRequest {
		scope = "latest provider request + generated content"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Context report · %s\n", scope)
	marker := ""
	if currentEstimated {
		marker = "~"
	}
	fmt.Fprintf(&b, "  Current context         %s%s", marker, formatTokenCount(int64(currentTokens)))
	if report.ContextWindow > 0 {
		percent := 100 * float64(currentTokens) / float64(report.ContextWindow)
		fmt.Fprintf(&b, " / %s (%.1f%%)", formatTokenCount(int64(report.ContextWindow)), percent)
	}
	b.WriteByte('\n')
	if report.LatestRequest && report.Usage != nil && report.Usage.Input > 0 {
		fmt.Fprintf(&b, "  Latest provider input   %s\n", formatTokenCount(int64(report.Usage.Input)))
	}
	fmt.Fprintf(&b, "  Snapshot                %d messages · %d tools\n", report.MessageCount, report.ToolCount)
	if report.FixedContextBudgetTokens > 0 {
		status := ""
		if report.FixedContextOverBudget {
			status = " · over budget"
		}
		fmt.Fprintf(&b, "  Fixed context           ~%s / %s%s\n", formatTokenCount(int64(report.FixedContextTokens)), formatTokenCount(int64(report.FixedContextBudgetTokens)), status)
	}

	if calibrated {
		b.WriteString("\nEstimated composition (provider-calibrated)\n")
	} else {
		b.WriteString("\nEstimated composition (raw local estimate)\n")
	}
	for _, category := range categories {
		percent := 0.0
		if currentTokens > 0 {
			percent = 100 * float64(category.tokens) / float64(currentTokens)
		}
		name := category.name
		if category.name == "Images" && category.items > 0 {
			name = fmt.Sprintf("Images (%d)", category.items)
		}
		fmt.Fprintf(&b, "  %-22s ~%8s  %5.1f%%\n", name, formatTokenCount(int64(category.tokens)), percent)
	}
	if generatedTokens > 0 {
		percent := 100 * float64(generatedTokens) / float64(currentTokens)
		fmt.Fprintf(&b, "  %-22s %9s  %5.1f%%\n", "Generated since input", formatTokenCount(int64(generatedTokens)), percent)
	}
	fmt.Fprintf(&b, "  %-22s %9s\n", "Context total", marker+formatTokenCount(int64(currentTokens)))
	if rawInput > 0 && rawInput != inputTarget {
		fmt.Fprintf(&b, "  %-22s ~%8s  (before calibration)\n", "Raw local estimate", formatTokenCount(int64(rawInput)))
	}
	if report.Usage != nil && (report.Usage.Output > 0 || report.Usage.Reasoning > 0) {
		fmt.Fprintf(&b, "\nLatest generation usage  %s output", formatTokenCount(int64(report.Usage.Output)))
		if report.Usage.Reasoning > 0 {
			fmt.Fprintf(&b, " · %s reasoning", formatTokenCount(int64(report.Usage.Reasoning)))
		}
		b.WriteByte('\n')
	}
	if calibrated {
		b.WriteString("\nCategory shares are byte-based estimates calibrated to current/provider totals; opaque and multimodal attribution remains approximate.")
	} else {
		b.WriteString("\nCategory shares use UTF-8 bytes/4 for text and a bounded dimension-based image estimate until provider context usage is available; opaque and multimodal attribution remains approximate.")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func calibrateContextCategories(categories []agent.ContextCategory, rawTotal, target int) []contextDisplayCategory {
	out := make([]contextDisplayCategory, 0, len(categories))
	if rawTotal <= 0 || target <= 0 {
		for _, category := range categories {
			out = append(out, contextDisplayCategory{name: category.Name, tokens: category.EstimatedTokens, items: category.Items})
		}
		return out
	}

	sum := 0
	largest := -1
	for _, category := range categories {
		tokens := int(float64(category.EstimatedTokens)*float64(target)/float64(rawTotal) + 0.5)
		out = append(out, contextDisplayCategory{name: category.Name, tokens: tokens, items: category.Items})
		sum += tokens
		if largest < 0 || tokens > out[largest].tokens {
			largest = len(out) - 1
		}
	}
	if largest >= 0 {
		out[largest].tokens += target - sum
	}
	return out
}

func shortSessionID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func toolTranscriptLabel(toolName, message string) string {
	label := toolName
	if detail := strings.TrimSpace(message); detail != "" && detail != "running" {
		label += " " + sanitizeToolPreview(detail, 500)
	}
	return label
}

// toolStartTranscriptRows returns the temporary rows shown while a tool runs.
// Completion replaces these rows, and call IDs remain correlation metadata.
func toolStartTranscriptRows(toolName, message string) []string {
	label := toolTranscriptLabel(toolName, message)
	switch toolName {
	case "spawn_agent", "bash":
		// Subagent lifecycle events provide the useful spawn row. Bash is kept to
		// one completion summary rather than a start plus routine progress rows.
		return nil
	case "edit":
		return []string{styleTool.Render(label)}
	default:
		return []string{styleTool.Render("▶ " + label)}
	}
}

func toolProgressTranscriptRow(toolName, message string) string {
	message = strings.TrimSpace(message)
	if message == "" || toolName == "bash" {
		return ""
	}
	return styleHeaderDim.Render("  ↳ " + sanitizeToolPreview(message, 500))
}

func (m *Model) toolEndTranscriptRows(toolName, startMessage string, durationMS int64, message, output string, isError bool) []string {
	label := toolTranscriptLabel(toolName, startMessage)
	duration := ""
	if durationMS > 0 {
		duration = fmt.Sprintf("%dms", durationMS)
		if toolName != "bash" {
			label += "  (" + duration + ")"
		}
	}
	rows := make([]string, 0, 2)
	if isError {
		message = strings.TrimSpace(message)
		if message == "" {
			message = "tool failed"
		}
		if toolName == "bash" {
			rows = append(rows, m.renderBashSummary(startMessage, duration, message, true))
		} else {
			rows = append(rows, styleError.Render("✖ "+label+": "+sanitizeToolPreview(message, 700)))
		}
		return rows
	}
	if toolName == "bash" {
		rows = append(rows, m.renderBashSummary(startMessage, duration, "", false))
	} else if toolName != "spawn_agent" {
		rows = append(rows, styleTool.Render("✔ "+label))
	}
	if preview := renderToolOutput(toolName, output, m.width); preview != "" {
		rows = append(rows, preview)
	}
	return rows
}

func renderToolOutput(toolName, output string, width int) string {
	if summary, handled := renderSkillToolSummary(toolName, output); handled {
		return summary
	}
	if summary, handled := renderSubagentToolSummary(toolName, output); handled {
		return summary
	}
	if toolHasDiffPreview(toolName, output) {
		return renderEditDiff(output, width)
	}
	return renderToolOutputPreview(output, width)
}

func renderSkillToolSummary(toolName, output string) (string, bool) {
	if toolName != "activate_skill" {
		return "", false
	}
	label := "skill instructions loaded"
	decoder := xml.NewDecoder(strings.NewReader(output))
	if token, err := decoder.Token(); err == nil {
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "skill_content" {
			for _, attr := range start.Attr {
				if attr.Name.Local == "name" && strings.TrimSpace(attr.Value) != "" {
					label += ": " + sanitizeToolPreview(strings.TrimSpace(attr.Value), 100)
					break
				}
			}
		}
	}
	// Skill bodies are XML-escaped to keep their model-facing delimiter safe.
	// The generic output preview would expose entities such as &#xA; and &lt;
	// instead of useful transcript information, so show only activation status.
	return styleHeaderDim.Render("  ↳ " + label), true
}
