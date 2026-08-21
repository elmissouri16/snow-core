package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *Model) applyTextareaResult(result textareaResultMsg) (tea.Model, tea.Cmd) {
	if result.msg == nil || result.target == textareaTargetComposer && result.pasteGeneration != 0 && result.pasteGeneration != m.imagePasteGeneration {
		return m, nil
	}
	switch result.target {
	case textareaTargetUserInput:
		question := m.currentUserInputQuestion()
		if !m.userInputPending || !m.userInputEditing || m.userInputRequest == nil || question == nil ||
			m.userInputRequest.ID != result.requestID || question.ID != result.questionID {
			return m, nil
		}
		var cmd tea.Cmd
		m.userInputEditor, cmd = m.userInputEditor.Update(result.msg)
		m.userInputDrafts[question.ID] = m.userInputEditor.Value()
		if m.userInputEditor.Err != nil {
			m.userInputError = "paste: " + m.userInputEditor.Err.Error()
		}
		m.layout()
		return m, cmd
	default:
		previous := m.editor.Value()
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(result.msg)
		if m.editor.Err != nil {
			m.lastErrorText = "paste: " + m.editor.Err.Error()
			m.pushLine(styleError.Render(m.lastErrorText))
		}
		if m.editor.Value() != previous {
			m.resetInputHistoryNavigation()
			if mentionCmd := m.refreshInputCompletions(); mentionCmd != nil {
				cmd = tea.Batch(cmd, mentionCmd)
			}
			m.layout()
		}
		return m, cmd
	}
}

func (m *Model) handleSubagentEvent(ev protocol.AgentEvent) {
	m.recordSubagentFleetEvent(ev)
	switch ev.Type {
	case protocol.EvSubagentStarted:
		m.pushLine(styleTool.Render(fmt.Sprintf("• agent %s started (%s)", ev.Agent.Path, ev.Agent.Role)))
	case protocol.EvSubagentStatus:
		if ev.Subagent != nil && ev.Subagent.Status.TerminalOutcome() {
			m.pushLine(styleTool.Render(fmt.Sprintf("• agent %s %s", ev.Agent.Path, ev.Subagent.Status)))
		}
	}
}

func (m *Model) renderQueuedInput(item protocol.QueuedInput, text string) {
	if m.queueRendered[item.ID] {
		return
	}
	label := "queued steer"
	if item.Kind == protocol.QueuedInputFollowUp {
		label = "queued follow-up"
	}
	m.pushLine(styleTool.Render("↳ " + label + ": " + sanitizeToolPreview(text, 300)))
	m.queueRendered[item.ID] = true
}

func (m *Model) setRunIdle() {
	m.busy = false
	m.activeTurnID = ""
	m.toolRunning = false
	m.activeToolCallID = ""
	m.activeBashCommand = ""
	m.compacting = false
	m.compactStatus = ""
	m.runStartedAt = time.Time{}
	m.cancelRun = nil
}

func (m *Model) fenceRootTurnProjection() {
	m.setRunIdle()
	m.rootTurnSequence = 0
	m.rootEventEpoch = 0
	if m.app != nil && m.app.Agent != nil {
		m.rootEventEpoch = m.app.Agent.RootEpoch()
	}
	m.rootTurnFence = true
}

func (m *Model) adoptTurn(ev protocol.AgentEvent) {
	if ev.Agent != nil || ev.TurnID == "" {
		return
	}
	if ev.TurnID != m.activeTurnID {
		m.turnUsageSeen = false
	}
	if ev.TurnSequence > m.rootTurnSequence {
		m.rootTurnSequence = ev.TurnSequence
	}
	m.activeTurnID = ev.TurnID
	m.busy = true
	if m.runStartedAt.IsZero() {
		m.runStartedAt = m.currentTime()
	}
}

func (m *Model) staleRootEvent(ev protocol.AgentEvent) bool {
	if ev.Agent != nil {
		return false
	}
	if ev.RootEpoch != 0 {
		if m.rootEventEpoch != 0 && ev.RootEpoch < m.rootEventEpoch {
			return true
		}
	} else if m.rootTurnFence {
		return true
	}
	if ev.TurnID == "" {
		return false
	}
	// Finish the turn currently projected by the UI before adopting a newer
	// core identity. Ordered terminal events may still be queued when the next
	// operation has already been admitted.
	if m.activeTurnID != "" && ev.TurnID == m.activeTurnID {
		return false
	}
	if ev.TurnSequence != 0 {
		// Admission sequence preserves every queued intermediate turn even when
		// core has already completed or admitted successors. The high-water mark
		// rejects older replays within the accepted root epoch.
		return ev.TurnSequence < m.rootTurnSequence
	}
	return m.activeTurnID != "" && ev.TurnID != m.activeTurnID
}

func (m *Model) handleAgentEvent(ev protocol.AgentEvent) {
	// Child streams never reuse root scalar buffers or trigger root session
	// hydration. Bubble Tea's Update goroutine alone mutates this map.
	if ev.Agent != nil {
		m.handleSubagentEvent(ev)
		if ev.Type != protocol.EvPermissionRequest && ev.Type != protocol.EvUserInputRequest {
			return
		}
	}
	// Ignore every delayed event from an older turn, not just its terminal
	// boundary. Command results and mailbox delivery use separate Bubble Tea
	// paths, so a newer turn can be admitted before an old batch is reduced.
	if m.staleRootEvent(ev) {
		return
	}
	if ev.Agent == nil && ev.RootEpoch > m.rootEventEpoch {
		m.rootEventEpoch = ev.RootEpoch
	}
	// Root stream events deliberately have no Agent attribution. Give the fleet
	// inspector an inspector-only root identity after stale-event rejection,
	// without changing the public event or allowing it into child handling.
	if ev.Agent == nil && m.app != nil && m.app.Agent != nil && m.subagentFleetOpen {
		root := protocol.AgentRef{ThreadID: "root", Path: protocol.RootAgentPath, Role: "root", Depth: 0}
		for _, state := range m.subagentFleetList.Agents {
			if state.Agent.Path == protocol.RootAgentPath {
				root = state.Agent
				break
			}
		}
		fleetEvent := ev.Clone()
		fleetEvent.Agent = root.Clone()
		m.recordSubagentFleetEvent(fleetEvent)
	}
	// Session updates describe persistence, not active provider work. In
	// particular, a delayed update after a terminal compaction event must not
	// resurrect the completed turn and restart the idle spinner.
	if ev.Type != protocol.EvTurnDone && ev.Type != protocol.EvAborted && ev.Type != protocol.EvSessionUpdated {
		m.adoptTurn(ev)
	}
	switch ev.Type {
	case protocol.EvCompactionStarted:
		m.busy = true
		m.compacting = true
		m.compactStatus = strings.TrimSpace(ev.Message)
		if m.compactStatus == "" {
			m.compactStatus = "compacting context"
		}
	case protocol.EvCompactionDone:
		m.compacting = false
		m.compactStatus = ""
		m.activeTurnID = ""
		// Automatic compaction is one phase of an admitted ordinary or goal turn.
		// Manual compaction settles only when the core confirms no admitted
		// operation is still running; never unlock based solely on event ordering.
		if ev.Compaction == nil || !ev.Compaction.Automatic {
			if m.app == nil || m.app.Agent == nil || !m.app.Agent.IsRunning() {
				m.setRunIdle()
			}
		}
		m.refreshContextUsageFromSession()
	case protocol.EvSessionUpdated:
		// Assistant/tool persistence happens before turn_done. Rehydrating or
		// rescanning the full SQLite branch here both duplicates live transcript
		// state and blocks Bubble Tea's input goroutine on every tool cycle.
		// EvUsage already carries authoritative current-context usage while busy.
		// Turn-attributed updates are represented by the live event stream even if
		// an optimistic abort has already unlocked the composer; retain idle
		// hydration only for externally initiated, unattributed session mutations.
		if ev.TurnID == "" && !m.busy && m.assistantBuf.Len() == 0 && m.thinkingBuf.Len() == 0 && m.planBuf.Len() == 0 {
			m.hydrateSession()
		}
	case protocol.EvTextDelta:
		m.finalizeThinking()
		m.finalizePlan()
		m.assistantBuf.WriteString(ev.Text)
		m.refreshTranscript()
	case protocol.EvPlanStarted:
		m.finishAssistant()
		m.planBuf.Reset()
		m.currentPlanID = ""
		if ev.Plan != nil {
			m.currentPlanID = ev.Plan.ID
		}
		m.sawPlanThisTurn = true
	case protocol.EvPlanDelta:
		m.planBuf.WriteString(ev.Text)
		m.refreshTranscript()
	case protocol.EvPlanCompleted:
		if ev.Plan != nil && strings.TrimSpace(ev.Plan.Text) != "" {
			m.planBuf.Reset()
			m.planBuf.WriteString(ev.Plan.Text)
			m.completedPlanThisTurn = true
			m.finalizePlan()
		}
	case protocol.EvPlanUpdate:
		m.finishAssistant()
		if ev.PlanUpdate != nil {
			m.pushLine(m.renderPlanUpdate(*ev.PlanUpdate))
		}
	case protocol.EvThreadGoalUpdated:
		if ev.GoalContinuing {
			if ev.TurnID != "" && ev.TurnID != m.activeTurnID {
				m.lastErrorText = ""
			}
			m.adoptTurn(ev)
		}
		if ev.ThreadGoal != nil {
			if ev.ThreadGoal.Cleared {
				m.goal = nil
			} else {
				m.goal = ev.ThreadGoal.Goal.Clone()
			}
			// Goal workers can stop at a safe boundary (pause, clear, blocked
			// auto-compaction) without another turn terminal event. A terminal goal
			// snapshot is therefore also a lifecycle boundary once core is idle.
			if (m.goal == nil || m.goal.Status != protocol.GoalActive) &&
				(m.app == nil || m.app.Agent == nil || !m.app.Agent.IsRunning()) {
				m.setRunIdle()
			}
		}
		m.refreshTranscript()
	case protocol.EvModeChanged:
		if ev.Mode != nil {
			m.lastStatus = "mode " + string(ev.Mode.Mode)
		}
	case protocol.EvThinkingDelta:
		m.thinkingBuf.WriteString(ev.Text)
		m.refreshTranscript()
	case protocol.EvToolStart:
		m.toolRunning = true
		m.activeToolCallID = ev.ToolCallID
		m.activeBashCommand = ""
		if ev.ToolName == "bash" {
			m.activeBashCommand = compactBashCommand(ev.Message)
		}
		m.finishAssistant()
		for _, row := range toolStartTranscriptRows(ev.ToolName, ev.Message) {
			m.pushLine(row)
		}
		m.busy = true
	case protocol.EvToolProgress:
		m.busy = true
		message := strings.TrimSpace(ev.Message)
		if ev.ToolProgress != nil && message == "" {
			message = strings.TrimSpace(ev.ToolProgress.Message)
		}
		if row := toolProgressTranscriptRow(ev.ToolName, message); row != "" {
			m.pushLine(row)
		}
	case protocol.EvToolEnd:
		if ev.ToolName == "ask_user" && m.userInputPending {
			m.clearUserInput()
		}
		m.toolRunning = false
		for _, row := range m.toolEndTranscriptRows(ev.ToolName, m.activeBashCommand, ev.ToolDurationMS, ev.Message, ev.ToolOutput, ev.IsError) {
			m.pushLine(row)
		}
		if m.activeToolCallID == "" || ev.ToolCallID == "" || m.activeToolCallID == ev.ToolCallID {
			m.activeToolCallID = ""
			m.activeBashCommand = ""
		}
		// Keep the composer locked until turn_done. Tool calls are serial but
		// their end/start events can be separated by scheduling, so unlocking
		// here permits a second Prompt to race the current agent turn.
		m.busy = true
	case protocol.EvUsage:
		if ev.Usage != nil {
			m.lastUsage = ev.Usage.Clone()
			m.lastRequestUsage = ev.Usage.Clone()
			m.contextTokens = contextTokensFromUsage(*ev.Usage)
			m.contextEstimated = false
			m.turnUsageSeen = true
			m.contextRefreshNeeded = false
			m.contextRefreshVersion++
			// Per-request token accounting remains available in debug mode;
			// the compact aggregate stays in the sticky footer.
			if os.Getenv("SNOW_DEBUG") != "" {
				m.finishAssistant()
				m.pushLine(styleFooter.Render(fmt.Sprintf("tokens %d in · %d out · %d cached",
					ev.Usage.Input, ev.Usage.Output, ev.Usage.CacheRead)))
			}
		}
	case protocol.EvQueueUpdated:
		previous := make(map[string]bool, len(m.pendingInputs.Items))
		for _, item := range m.pendingInputs.Items {
			previous[item.ID] = true
		}
		if ev.Queue == nil {
			m.pendingInputs = protocol.InputQueue{}
		} else {
			m.pendingInputs = *ev.Queue.Clone()
		}
		current := make(map[string]bool, len(m.pendingInputs.Items))
		for _, item := range m.pendingInputs.Items {
			current[item.ID] = true
			if previous[item.ID] {
				continue
			}
			if original := m.queueOriginalText[item.ID]; original != "" {
				m.renderQueuedInput(item, original)
			} else if !m.hasQueueAttempt(item) {
				// Inputs admitted by another programmatic surface have no compact
				// TUI draft to recover, so render their model-visible text directly.
				m.renderQueuedInput(item, item.Text)
			}
		}
		for id := range m.queueOriginalText {
			if !current[id] {
				delete(m.queueOriginalText, id)
				delete(m.queueRendered, id)
			}
		}
		m.layout()
	case protocol.EvPermissionRequest:
		if ev.Permission != nil {
			if ev.Agent == nil {
				m.busy = true
			}
			req := ev.Permission.Request
			m.permPending = true
			m.permRequest = &req
			m.permAgent = ev.Agent.Clone()
			m.permChoice = 0
			m.layout()
			m.finishAssistant()
			label := "🔐 permission request: " + req.Tool
			if ev.Agent != nil {
				label += " · " + string(ev.Agent.Path)
			}
			m.pushLine(styleTool.Render(label))
		}
	case protocol.EvUserInputRequest:
		if ev.UserInput != nil {
			m.startUserInput(*ev.UserInput)
		}
	case protocol.EvError:
		// Errors are diagnostics, not lifecycle boundaries. Correlated
		// turn_done/aborted events settle admitted work; promptDoneMsg handles the
		// only no-turn case (optimistic admission/preflight failure).
		m.sawPlanThisTurn = false
		m.completedPlanThisTurn = false
		m.planPrompt = false
		message := strings.TrimSpace(ev.Message)
		for _, prefix := range []string{"agent: provider stream: ", "agent: provider chat: ", "agent: provider resolve: ", "agent: "} {
			message = strings.TrimPrefix(message, prefix)
		}
		if message != "" && message != m.lastErrorText {
			m.lastErrorText = message
			m.finishAssistant()
			m.pushLine(styleError.Render("✖ " + message))
		}
	case protocol.EvTurnDone:
		if !m.turnUsageSeen {
			m.contextRefreshVersion++
			m.contextRefreshNeeded = true
		}
		m.turnUsageSeen = false
		m.clearUserInput()
		m.toolRunning = false
		if ev.Usage != nil {
			m.lastUsage = ev.Usage.Clone()
			if os.Getenv("SNOW_DEBUG") != "" {
				line := fmt.Sprintf("turn usage %d input · %d output · %d cached · %d total",
					ev.Usage.Input, ev.Usage.Output, ev.Usage.CacheRead, ev.Usage.Total)
				if ev.Usage.Cost != nil {
					line += fmt.Sprintf(" · %s %.6f", ev.Usage.Cost.Currency, ev.Usage.Cost.Total)
				}
				m.pushLine(styleFooter.Render(line))
			}
		}
		m.busy = ev.GoalContinuing
		m.activeTurnID = ""
		m.lastErrorText = ""
		if !ev.GoalContinuing {
			m.setRunIdle()
			if m.app != nil && m.app.Agent != nil {
				recovered := m.app.Agent.ClearPendingInputs()
				if len(recovered.Items) > 0 {
					m.pendingInputs = protocol.InputQueue{}
					m.restoreAbortedInputs(recovered, nil, m.editor.Value())
					m.pushLine(styleFooter.Render(fmt.Sprintf("restored %d undelivered queued input(s)", len(recovered.Items))))
				}
			}
		} else if m.runStartedAt.IsZero() {
			m.runStartedAt = m.currentTime()
		}
		m.finishAssistant()
		m.finalizePlan()
		// Do not open the implementation picker when the user already queued a
		// switch out of Plan mode for this boundary. The mode command runs on a
		// separate Bubble Tea path, so opening it here would leave a stale modal
		// in front of the now-Default composer.
		leavingPlan := m.pendingMode != nil && *m.pendingMode == protocol.ModeDefault
		if m.completedPlanThisTurn && strings.TrimSpace(m.latestPlan) != "" && m.app != nil && m.app.Agent.Mode() == protocol.ModePlan && !leavingPlan {
			m.planPrompt = true
			m.planPromptChoice = 0
		}
		m.sawPlanThisTurn = false
		m.completedPlanThisTurn = false
		if m.pendingMode != nil && !m.modeSwitching {
			m.modeSwitchReady = true
		}
		m.refreshTranscript()
	case protocol.EvAborted:
		if !m.turnUsageSeen {
			m.contextRefreshVersion++
			m.contextRefreshNeeded = true
		}
		m.turnUsageSeen = false
		m.sawPlanThisTurn = false
		m.completedPlanThisTurn = false
		m.planPrompt = false
		m.clearUserInput()
		m.toolRunning = false
		m.permPending = false
		m.permRequest = nil
		m.permAgent = nil
		m.setRunIdle()
		m.finishAssistant()
		if !m.abortNoticePending {
			m.pushLine(styleError.Render("aborted"))
		}
		m.abortNoticePending = false
		m.refreshTranscript()
	case protocol.EvModelChanged:
		if ev.Model != nil {
			m.app.Model = *ev.Model
			if ev.Model.Provider != "" {
				m.app.ProviderID = ev.Model.Provider
			}
		}
	}
}

// finalizeThinking promotes a completed reasoning block to a permanent
// transcript line. It runs when the model starts emitting answer text or a
// tool call (thinking always precedes those), and again at turn end for
// thinking-only turns.
func (m *Model) appendTranscriptLine(line string) {
	if !m.inlineTranscript && len(line) > maxTranscriptBytes-256 {
		line = styleFooter.Render("── transcript entry truncated ──") + "\n" + boundedUTF8Tail(xansi.Strip(line), maxTranscriptBytes-512)
	}
	m.transcriptBaseAppend = true
	m.lines = append(m.lines, line)
	if m.inlineTranscript {
		return
	}
	m.transcriptBytes += len(line)
	if (len(m.lines) > maxTranscriptEntries || m.transcriptBytes > maxTranscriptBytes) && len(m.lines) > 1 {
		removeAt := 0
		if m.transcriptDropped > 0 {
			removeAt = 1 // preserve the existing omission marker
		}
		// Create bounded headroom in one slice move instead of shifting up to
		// 2,000 entries for every subsequent streamed line.
		targetEntries := max(1, maxTranscriptEntries-64)
		targetBytes := max(1, maxTranscriptBytes-(64<<10))
		removeCount, removedBytes := 0, 0
		for removeAt+removeCount < len(m.lines)-1 && (len(m.lines)-removeCount > targetEntries || m.transcriptBytes-removedBytes > targetBytes) {
			removedBytes += len(m.lines[removeAt+removeCount])
			removeCount++
		}
		if removeCount > 0 {
			m.transcriptBytes -= removedBytes
			copy(m.lines[removeAt:], m.lines[removeAt+removeCount:])
			for i := len(m.lines) - removeCount; i < len(m.lines); i++ {
				m.lines[i] = ""
			}
			m.lines = m.lines[:len(m.lines)-removeCount]
			m.transcriptDropped += removeCount
			m.transcriptBaseAppend = false
		}
	}
	if m.transcriptDropped == 0 {
		return
	}
	hasMarker := len(m.lines) > 0 && strings.Contains(m.lines[0], "older transcript entries omitted")
	if !hasMarker {
		// Reserve one bounded slot for the marker on the first trim.
		for (len(m.lines) >= maxTranscriptEntries || m.transcriptBytes+256 > maxTranscriptBytes) && len(m.lines) > 1 {
			m.transcriptBytes -= len(m.lines[0])
			copy(m.lines, m.lines[1:])
			m.lines = m.lines[:len(m.lines)-1]
			m.transcriptDropped++
			m.transcriptBaseAppend = false
		}
	}
	marker := styleFooter.Render(fmt.Sprintf("── %d older transcript entries omitted ──", m.transcriptDropped))
	if hasMarker {
		m.transcriptBytes += len(marker) - len(m.lines[0])
		m.lines[0] = marker
		return
	}
	m.lines = append(m.lines, "")
	copy(m.lines[1:], m.lines[:len(m.lines)-1])
	m.lines[0] = marker
	m.transcriptBytes += len(marker)
	m.transcriptBaseSynced = 0
}

func (m *Model) finalizeThinking() {
	if m.thinkingBuf.Len() == 0 {
		return
	}
	m.appendTranscriptLine(m.renderThinkingBody(m.thinkingBuf.String()))
	m.thinkingMD.clearCache()
	m.transcriptBaseDirty = true
	m.thinkingBuf.Reset()
	m.refreshTranscript()
}

func (m *Model) finishAssistant() {
	m.finalizeThinking()
	m.finalizeAssistant()
	m.finalizePlan()
}

// finalizeAssistant promotes the current answer segment to the durable
// transcript. Tool, permission, error, and abort events call this before
// appending their own lines so the visible transcript remains chronological.
func (m *Model) finalizeAssistant() {
	if m.assistantBuf.Len() > 0 {
		m.appendTranscriptLine(m.renderAssistantBody(m.assistantBuf.String()))
		m.md.clearCache()
		m.transcriptBaseDirty = true
		m.assistantBuf.Reset()
		m.refreshTranscript()
	}
}

// renderAssistantBody renders the assistant response without a role label.
// The user prompt already has the blue prompt marker; the response should
// read as a clean continuation, like the pi transcript.
func (m *Model) renderAssistantBody(text string) string {
	width := m.transcript.Width - 4
	body := strings.TrimSpace(text)
	if m.md != nil && looksLikeMarkdown(body) {
		body = strings.TrimSpace(m.md.render(body, width))
	}
	return styleAssistant.Render(body)
}

func (m *Model) finalizePlan() {
	if m.planBuf.Len() == 0 {
		return
	}
	text := m.planBuf.String()
	m.latestPlan = text
	m.appendTranscriptLine(m.renderPlanBody(text))
	m.md.clearCache()
	m.transcriptBaseDirty = true
	m.planBuf.Reset()
	m.currentPlanID = ""
	m.refreshTranscript()
}

func (m *Model) renderPlanBody(text string) string {
	body := strings.TrimSpace(text)
	width := m.transcript.Width - 4
	if m.md != nil {
		body = strings.TrimSpace(m.md.render(body, width))
	}
	return styleHeader.Render("Plan") + "\n" + body
}

func (m *Model) renderPlanUpdate(update protocol.PlanUpdate) string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("Plan update"))
	if strings.TrimSpace(update.Explanation) != "" {
		b.WriteString("\n" + styleHeaderDim.Render(strings.TrimSpace(update.Explanation)))
	}
	for _, step := range update.Plan {
		mark := "○"
		if step.Status == protocol.PlanStepCompleted {
			mark = "✓"
		} else if step.Status == protocol.PlanStepInProgress {
			mark = "→"
		}
		b.WriteString("\n" + mark + " " + step.Step)
	}
	return b.String()
}

func (m *Model) renderThinkingBody(text string) string {
	body := strings.TrimSpace(text)
	if body == "" {
		return ""
	}
	label := "think: "
	width := max(10, m.transcript.Width-lipgloss.Width(label)-4)
	if m.thinkingMD != nil {
		body = strings.TrimSpace(m.thinkingMD.render(body, width))
	} else {
		body = styleThinking.Render(body)
	}
	lines := strings.Split(body, "\n")
	if len(lines) == 1 {
		return styleThinking.Render(label) + lines[0]
	}
	indent := strings.Repeat(" ", lipgloss.Width(label))
	return styleThinking.Render(label) + lines[0] + "\n" + indent + strings.Join(lines[1:], "\n"+indent)
}

func looksLikeMarkdown(text string) bool {
	for _, marker := range []string{"# ", "## ", "- ", "* ", "```", "`", "[", "> "} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func (m *Model) pushLine(s string) {
	m.appendTranscriptLine(s)
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	if m.batchingEvents {
		m.refreshTranscript()
		return
	}
	m.flushTranscriptImmediately()
}

// liveText renders the streaming (unfinished) tail: the in-progress thinking
// block and/or the current assistant answer segment. Visible lifecycle events
// finalize this tail before appending their own durable transcript lines.
func (m *Model) liveText() string {
	var b strings.Builder
	if m.thinkingBuf.Len() > 0 {
		// Streaming deltas stay cheap; finalized thinking is rendered as Markdown
		// once in finalizeThinking, matching the assistant streaming path.
		b.WriteString(styleThinking.Render(m.thinkingBuf.String()))
	} else if m.showThinkingPlaceholder() {
		b.WriteString(styleThinking.Render(m.thinkingSpinner.View() + " thinking…"))
	}
	if m.assistantBuf.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		// Keep streaming text cheap. Markdown is rendered once when finalized.
		b.WriteString(styleAssistant.Render(m.assistantBuf.String()))
	}
	if m.planBuf.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(styleHeader.Render("Plan") + "\n" + styleAssistant.Render(m.planBuf.String()))
	}
	return b.String()
}

func (m *Model) showThinkingPlaceholder() bool {
	return m.busy && !m.toolRunning && !m.compacting && !m.permPending && !m.userInputPending &&
		m.lastErrorText == "" && m.thinkingBuf.Len() == 0 && m.assistantBuf.Len() == 0
}

func (m *Model) refreshTranscript() {
	m.refreshTranscriptWithForce(false)
}

func (m *Model) refreshTranscriptForced() {
	m.refreshTranscriptWithForce(true)
}
