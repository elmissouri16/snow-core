package tui

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case inlineExitMsg:
		m.inlineExiting = true
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.clearTranscriptSelection()
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.refreshTranscriptForced()
	case tea.MouseMsg:
		if m.processFleetOpen {
			m.handleProcessFleetMouse(msg)
			return m, nil
		}
		if m.subagentFleetOpen {
			m.handleSubagentFleetMouse(msg)
			return m, nil
		}
		// Application-owned drag selection and viewport wheel scrolling share
		// the same cell-motion mouse stream.
		cmd := m.applyMouse(msg)
		return m, cmd
	case transcriptSelectionAutoScrollMsg:
		return m, m.handleTranscriptSelectionAutoScroll(uint64(msg))
	case transcriptSelectionCopiedMsg:
		if msg.err != nil {
			m.lastStatus = "copy failed: " + msg.err.Error()
			return m, nil
		}
		m.lastStatus = fmt.Sprintf("copied %d characters", msg.characters)
		if msg.sequence == "" {
			return m, nil
		}
		m.transcriptSelectionCopyID++
		id := m.transcriptSelectionCopyID
		m.transcriptSelectionClipboard = msg.sequence
		return m, tea.Tick(transcriptSelectionClipboardRenderGrace, func(time.Time) tea.Msg {
			return transcriptSelectionClipboardClearMsg(id)
		})
	case transcriptSelectionClipboardClearMsg:
		if uint64(msg) == m.transcriptSelectionCopyID {
			m.transcriptSelectionClipboard = ""
		}
		return m, nil
	case tea.KeyMsg:
		if handled, cmd := m.applyTranscriptSelectionContextMenuKey(msg); handled {
			return m, cmd
		}
		if handled, cmd := m.normalizeTerminalKey(msg); handled {
			m.layout()
			return m, cmd
		}
		// PageUp/PageDown/Home/End and explicit Ctrl+arrow bindings scroll the
		// transcript when not in a picker.
		if !m.loginMode && !m.loginProfileMode && !m.loginEndpointMode && !m.pickProvider && !m.pickChatGPTAuth && !m.pickModel && !m.permPending && !m.userInputPending && !m.subagentFleetOpen && !m.processFleetOpen && !m.pickPermissionMode && !m.pickSession && !m.pickTree && !m.pickInfo && !m.compVisible && !m.skillVisible && !m.mentionVisible {
			switch {
			case keyMatches(msg, m.keys.PageUp):
				m.refreshTranscriptForced()
				m.transcript.PageUp()
				return m, nil
			case keyMatches(msg, m.keys.PageDown):
				m.transcript.PageDown()
				m.catchUpTranscriptAtBottom()
				return m, nil
			case keyMatches(msg, m.keys.Top):
				m.refreshTranscriptForced()
				m.transcript.GotoTop()
				return m, nil
			case keyMatches(msg, m.keys.Bottom):
				m.transcript.GotoBottom()
				m.catchUpTranscriptAtBottom()
				return m, nil
			case keyMatches(msg, m.keys.LineUp):
				m.refreshTranscriptForced()
				m.transcript.ScrollUp(m.transcript.MouseWheelDelta)
				return m, nil
			case keyMatches(msg, m.keys.LineDown):
				m.transcript.ScrollDown(m.transcript.MouseWheelDelta)
				m.catchUpTranscriptAtBottom()
				return m, nil
			}
		}
		model, cmd := m.handleKey(msg)
		m.layout()
		return model, cmd
	case trustPromptMsg:
		m.trustPending = true
		m.trustPath = msg.path
		m.trustStore = msg.store
		m.trustChoice = 0
		m.trustError = ""
		m.trustSaving = false
		m.layout()
		return m, nil
	case trustDecisionMsg:
		m.releaseStartupApp(msg.app)
		m.trustSaving = false
		if msg.err != nil {
			if msg.app != nil {
				_ = msg.app.Close()
			}
			m.trustError = msg.err.Error()
			m.layout()
			return m, nil
		}
		m.trustPending = false
		m.trustPath = ""
		m.trustStore = nil
		m.trustError = ""
		return m.Update(doneMsg{app: msg.app})
	case doneMsg:
		m.releaseStartupApp(msg.app)
		if msg.err != nil {
			if msg.app != nil {
				_ = msg.app.Close()
			}
			m.lastErr = msg.err
			m.busy = false
			m.editor.Reset()
			m.editor.Placeholder = "Startup failed · ctrl+c to quit"
			m.editor.Blur()
			m.pushLine(styleError.Render("error: " + msg.err.Error()))
			m.layout()
			m.refreshTranscript()
			return m, nil
		}
		if msg.app != nil {
			m.app = msg.app
			m.loadAuxiliaryTUIConfig()
			if msg.app.Cfg.TUI.Theme != "" {
				if err := m.applyThemeSelection(msg.app.Cfg.TUI.Theme, false, false); err != nil {
					m.auxDiagnostics = append(m.auxDiagnostics, config.Diagnostic{Path: "tui.theme", Message: err.Error()})
					_ = m.applyThemeSelection("default", false, false)
				}
			}
			m.app.EnableUserInputReplies()
			m.modelList = uniquePickerModels(m.app.AllModels, m.app.ProviderID)
			m.asker.SetPublisher(m.app.Agent.Publish)
			m.app.Perm.SetAsker(m.asker)
			m.hydrateSession()
			if err := m.subscribe(); err != nil {
				m.lastErr = err
				m.pushLine(styleError.Render("goal restore: " + err.Error()))
			}
			for _, diagnostic := range append(append([]config.Diagnostic(nil), msg.app.Diagnostics...), m.auxDiagnostics...) {
				m.pushLine(styleFooter.Render("config warning: " + diagnostic.Path + ": " + diagnostic.Message))
			}
			// The sticky header and footer already expose provider, model, cwd,
			// and commands; do not duplicate startup chrome in the transcript.
			cmds = append(cmds, waitForEvent(m.events))
			if m.pickSessionOnStart {
				m.pickSessionOnStart = false
				_, pickerCmd := m.startSessionPick()
				cmds = append(cmds, pickerCmd)
			}
		}
		m.busy = false
		return m, tea.Batch(cmds...)
	case spinner.TickMsg:
		// Standard bubbles spinner pump: advance the frame and re-arm only while
		// there is something visible to animate. Streaming responsiveness does
		// not depend on these ticks — agent events arrive via waitForEvent.
		if !m.spinnerActive() {
			m.spinnerRunning = false
			return m, nil
		}
		m.spinnerRunning = true
		if msg.ID == 0 || msg.ID == m.spinner.ID() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		if msg.ID == 0 || msg.ID == m.thinkingSpinner.ID() {
			var cmd tea.Cmd
			m.thinkingSpinner, cmd = m.thinkingSpinner.Update(msg)
			cmds = append(cmds, cmd)
			if m.showThinkingPlaceholder() {
				m.refreshTranscript()
			}
		}
	case agentEventMsg:
		return m.Update(agentEventBatchMsg{events: []protocol.AgentEvent{msg.ev}})
	case contextUsageRefreshMsg:
		m.contextRefreshPending = false
		if msg.err == nil && msg.version == m.contextRefreshVersion {
			m.applyContextUsageSnapshot(msg.snapshot)
		}
		if cmd := m.scheduleContextUsageRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case contextReportMsg:
		if m.app == nil || m.app.Agent == nil || msg.epoch != m.app.Agent.RootEpoch() {
			break
		}
		if msg.err != nil {
			m.pushLine(styleError.Render("context report: " + msg.err.Error()))
		} else {
			m.pushLine(styleFooter.Render(formatContextReport(msg.report, m.contextTokens, m.contextEstimated)))
		}
	case agentEventBatchMsg:
		// Ingest a bounded, already-coalesced logical batch. Streaming deltas
		// schedule a render separately so input is never queued behind reflow.
		m.batchingEvents = true
		immediate := false
		for _, ev := range coalesceTUISnapshotEvents(msg.events, os.Getenv("SNOW_DEBUG") != "") {
			m.handleAgentEvent(ev)
			immediate = immediate || eventNeedsImmediateTranscript(ev.Type)
		}
		m.batchingEvents = false
		m.layout()
		if m.transcriptDirty {
			if immediate && m.transcript.AtBottom() {
				m.flushTranscriptImmediately()
			} else if cmd := m.scheduleTranscriptFlush(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(m.queueFallbacks) > 0 && !m.busy && m.app != nil {
			if m.app.Agent.IsRunning() {
				if cmd := m.waitForQueueSettlement(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			} else if cmd := m.startQueueFallback(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		} else if m.modeSwitchReady {
			if cmd := m.beginPendingModeSwitch(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if cmd := m.scheduleContextUsageRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Re-arm immediately; the mailbox self-signals while more batches wait.
		cmds = append(cmds, waitForEvent(m.events))
	case compactDoneMsg:
		if msg.generation != m.compactGeneration {
			return m, nil
		}
		// EvCompactionDone is the authoritative lifecycle transition. This
		// command result only reports the manual operation's summary/error; it
		// must not unlock a newer operation or an automatic goal continuation.
		if m.app != nil && m.app.Agent != nil && !m.app.Agent.IsRunning() {
			m.setRunIdle()
		}
		m.refreshContextUsageFromSession()
		m.layout()
		if msg.err != nil {
			m.lastErrorText = msg.err.Error()
			m.pushLine(styleError.Render("compact: " + msg.err.Error()))
		} else if msg.result.SummarizedMessages == 0 {
			m.pushLine(styleFooter.Render("compact: nothing to compact"))
		} else {
			status := fmt.Sprintf("compact: summarized %d messages, retained %d", msg.result.SummarizedMessages, msg.result.RetainedMessages)
			if msg.result.UsedFallback {
				status += " (local fallback)"
			}
			if m.goal != nil && m.goal.Status == protocol.GoalActive && m.app != nil && m.app.Agent != nil && !m.app.Agent.IsRunning() {
				if deferred, err := m.app.GoalContinuationDeferred(); err == nil && deferred {
					status += " · goal paused; /goal resume to continue"
				}
			}
			m.lastStatus = status
			m.pushLine(styleFooter.Render(status))
		}
		if m.pendingMode != nil {
			m.modeSwitchReady = true
			if cmd := m.beginPendingModeSwitch(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case promptDoneMsg:
		if msg.generation != m.runGeneration || msg.err == nil {
			return m, nil
		}
		if !msg.admitted {
			m.forgetNewestInputHistory(msg.historyText)
			if len(msg.attachments) > 0 {
				m.promptImages = append(msg.attachments, m.promptImages...)
			}
			if msg.text != "" {
				current := m.editor.Value()
				if !shouldCollapsePastedText(msg.text) && pastedTextAttachmentsReferenced(msg.text, msg.pastedTexts) {
					m.pastedTexts = append(msg.pastedTexts, m.pastedTexts...)
					if current == "" {
						m.editor.SetValue(msg.text)
					} else {
						m.editor.SetValue(msg.text + "\n" + current)
					}
					m.editor.CursorEnd()
				} else {
					restored := expandPastedTextAttachments(msg.text, msg.pastedTexts)
					if current != "" {
						restored += "\n" + m.expandedPastedText(current)
					}
					m.setComposerValueCollapsingLargeText(restored)
				}
			}
			m.layout()
		}
		// Only errors rejected before admission lack a terminal lifecycle event.
		// An admitted failed turn remains locked until its correlated turn_done or
		// aborted event reaches this reducer.
		if !msg.admitted && m.app != nil && m.app.Agent != nil && !m.app.Agent.IsRunning() && m.activeTurnID == "" {
			m.setRunIdle()
		}
		m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, TurnID: msg.turnID, Message: msg.err.Error()})
	case appendLineMsg:
		m.pushLine(msg.line)
	case oauthProgressMsg:
		if m.oauthLoading {
			m.oauthProgress = msg.progress
			return m, waitOAuthEvent(m.oauthEvents)
		}
	case oauthDoneMsg:
		if m.oauthLoading {
			m.oauthLoading = false
			if m.oauthCancel != nil {
				m.oauthCancel()
			}
			m.oauthCancel = nil
			m.oauthEvents = nil
			m.pickChatGPTAuth = false
			if msg.err != nil {
				m.pushLine(styleError.Render(msg.err.Error()))
			} else {
				m.pushLine(styleFooter.Render("chatgpt: " + chatgpt.FormatStatus(msg.status)))
			}
		}
	case logoutDoneMsg:
		if msg.err != nil {
			m.pushLine(styleError.Render("logout: " + msg.err.Error()))
		} else {
			m.pushLine(styleFooter.Render("logged out " + msg.provider))
		}
	case compatibleLoginDoneMsg:
		if msg.generation != m.compatibleLoginGeneration {
			break
		}
		m.compatibleLoginPending = false
		m.modelList = uniquePickerModels(m.app.AllModels, m.app.ProviderID)
		if msg.err != nil {
			m.pushLine(styleError.Render(msg.provider + " configured; model discovery failed: " + msg.err.Error()))
		} else {
			m.pushLine(styleFooter.Render(msg.provider + " configured for " + msg.endpoint + " · choose /model to switch"))
		}
	case inlineHistoryAckMsg:
		if msg.generation == m.inlinePrintGeneration && m.inlinePrintInFlight && msg.end == m.inlinePrintEnd {
			m.inlineCommitted = msg.end
			m.inlinePrintEnd = msg.end
			m.inlinePrintInFlight = false
			m.inlineEverCommitted = true
			m.transcriptBaseDirty = true
			m.transcriptDirty = true
			m.refreshTranscriptForced()
			m.layout()
		}
	case transcriptFlushMsg:
		if uint64(msg) == m.transcriptFlushSeq {
			m.transcriptFlushPending = false
			m.layout()
			m.refreshTranscript()
		}
	case modeSwitchDoneMsg:
		m.finishModeSwitch(msg)
	case clearThinkingFlashMsg:
		if uint64(msg) == m.thinkingFlashSeq {
			m.thinkingFlash = false
			m.layout()
		}
	case clearMetaEnterMsg:
		messages := m.expireTerminalInput(uint64(msg))
		if len(messages) == 0 && uint64(msg) == m.metaEnterSeq {
			m.metaEnterPending = false
		}
		m.replayTerminalMessages(messages, &cmds)
		m.layout()
	case mentionFilesMsg:
		if msg.generation != m.mentionGeneration || m.app == nil || msg.cwd != m.app.CWD() {
			return m, nil
		}
		m.mentionLoading = false
		if msg.err != nil {
			m.mentionFiles = nil
			m.mentionFilesCWD = msg.cwd
			m.mentionFilesLoaded = true
			m.pushLine(styleError.Render("file mentions: " + msg.err.Error()))
			m.refreshMentions()
			return m, nil
		}
		m.mentionFiles = append([]string(nil), msg.files...)
		m.mentionFilesCWD = msg.cwd
		m.mentionFilesLoaded = true
		m.refreshMentions()
		m.layout()
	case sessionListMsg:
		if msg.generation != m.pickerGeneration || !m.pickSession {
			return m, nil
		}
		m.sessionLoading = false
		if msg.err != nil {
			m.pickSession = false
			m.pushLine(styleError.Render("session list: " + msg.err.Error()))
			if m.startupResumeRequired {
				return m, m.quitCmd()
			}
			return m, nil
		}
		m.sessions = msg.sessions
		if len(m.sessions) == 0 {
			m.pickSession = false
			m.pushLine(styleFooter.Render(m.noSessionsResumeMessage()))
			if m.startupResumeRequired {
				return m, m.quitCmd()
			}
			return m, nil
		}
		m.sessionIndex = 0
		for i, info := range m.sessions {
			if m.app != nil && info.ID == currentSessionID(m.app) {
				m.sessionIndex = i
				break
			}
		}
		m.layout()
	case sessionRenameMsg:
		if msg.generation != m.pickerGeneration || !m.pickSession {
			return m, nil
		}
		m.sessionLoading = false
		if msg.err != nil {
			m.pushLine(styleError.Render("session rename: " + msg.err.Error()))
			return m, nil
		}
		if msg.index >= 0 && msg.index < len(m.sessions) {
			m.sessions[msg.index].Name = msg.title
		}
		m.lastStatus = "renamed session " + msg.title
		m.layout()
	case sessionDeleteMsg:
		if msg.generation != m.pickerGeneration || !m.pickSession {
			return m, nil
		}
		m.sessionLoading = false
		m.sessionDeleteInFlight = false
		var cleanupWarning *app.SessionDeleteCleanupError
		if msg.err != nil && !errors.As(msg.err, &cleanupWarning) {
			m.pushLine(styleError.Render("session delete: " + msg.err.Error()))
			return m, nil
		}
		if cleanupWarning != nil {
			m.pushLine(styleTool.Render(cleanupWarning.Error()))
		}
		for i := range m.sessions {
			if m.sessions[i].Path == msg.path {
				m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
				if m.sessionIndex >= len(m.sessions) {
					m.sessionIndex = max(0, len(m.sessions)-1)
				}
				break
			}
		}
		m.lastStatus = "deleted session " + msg.name
		if len(m.sessions) == 0 {
			m.pickSession = false
			if m.startupResumeRequired {
				return m, m.quitCmd()
			}
		}
		m.layout()
	case branchListMsg:
		if msg.generation != m.pickerGeneration || !m.pickTree {
			return m, nil
		}
		m.treeLoading = false
		if msg.err != nil {
			m.pickTree = false
			m.pushLine(styleError.Render("tree: " + msg.err.Error()))
			return m, nil
		}
		m.branches = orderBranches(msg.branches)
		if len(m.branches) == 0 {
			m.pickTree = false
			m.pushLine(styleFooter.Render("tree: no branches"))
			return m, nil
		}
		m.branchIndex = 0
		for i, branch := range m.branches {
			if branch.Active {
				m.branchIndex = i
				break
			}
		}
		m.layout()
	case modelListMsg:
		if msg.generation != m.pickerGeneration || !m.pickModel {
			return m, nil
		}
		m.modelLoading = false
		if msg.err != nil && len(msg.models) == 0 && len(m.modelList) == 0 {
			m.pickModel = false
			m.pushLine(styleError.Render("model list: " + msg.err.Error()))
			return m, nil
		}
		if msg.err != nil {
			m.lastStatus = "some provider catalogs could not be loaded"
		}
		if len(msg.models) > 0 {
			m.modelList = uniquePickerModels(msg.models, m.app.ProviderID)
		}
		m.modelQuery = ""
		m.modelSearchActive = false
		if len(m.modelList) == 0 {
			m.pickModel = false
			m.pushLine(styleError.Render("no models available"))
			return m, nil
		}
		m.modelIndex = 0
		for i, model := range m.modelList {
			if m.app != nil && model.Provider == m.app.Model.Provider && model.ID == m.app.Model.ID {
				m.modelIndex = i
				break
			}
		}
		m.layout()
	case subagentFleetListMsg:
		return m, m.applySubagentFleetList(msg)
	case subagentFleetDetailMsg:
		m.applySubagentFleetDetail(msg)
		return m, nil
	case processFleetListMsg:
		return m, m.applyProcessFleetList(msg)
	case processFleetLogsMsg:
		m.applyProcessFleetLogs(msg)
		return m, nil
	case processFleetTickMsg:
		if m.processFleetOpen && msg.generation == m.processFleetGeneration && msg.tick == m.processFleetTickGeneration {
			return m, m.refreshProcessFleet()
		}
		return m, nil
	case subagentInspectMsg:
		if msg.generation != m.pickerGeneration {
			return m, nil
		}
		m.lastStatus = "idle"
		if msg.err != nil {
			m.pushLine(styleError.Render(msg.err.Error()))
			return m, nil
		}
		m.pushLine(styleFooter.Render(renderSubagentInspection(msg.state, msg.messages, msg.messageErr, m.app.Cfg.Subagents.Durable, time.Now())))
	case subagentListMsg:
		if msg.generation != m.pickerGeneration || !m.pickInfo {
			return m, nil
		}
		m.infoLoading = false
		if msg.err != nil {
			m.closeInfoPicker()
			m.pushLine(styleError.Render("agents: " + msg.err.Error()))
			return m, nil
		}
		items, targets := subagentInfoItems(msg.list, m.app.Cfg.Subagents.Durable, time.Now())
		m.infoTitle = subagentInfoTitle(msg.list)
		m.infoItems = items
		m.infoAgentTargets = targets
		if len(items) == 0 {
			m.closeInfoPicker()
			m.pushLine(styleFooter.Render("agents: none"))
		}
		m.layout()
	case branchActionMsg:
		if msg.generation != m.pickerGeneration {
			return m, nil
		}
		if msg.err != nil {
			m.treeLoading = false
			m.forkLoading = false
			prefix := "tree: "
			if m.pickFork {
				prefix = "fork: "
			}
			m.pushLine(styleError.Render(prefix + msg.err.Error()))
			return m, nil
		}
		m.treeLoading = false
		m.forkLoading = false
		m.pickFork = false
		m.pickTree = false
		m.branches = nil
		m.subagentFleetActivity = make(map[string][]string)
		m.subagentFleetActivityKinds = make(map[string]protocol.AgentEventType)
		m.subagentFleetActivitySpace = make(map[string]bool)
		m.subagentFleetList = protocol.SubagentList{}
		m.subagentFleetMessages = nil
		m.subagentFleetDetailState = protocol.SubagentState{}
		m.closeSubagentFleet()
		if msg.action == "select" || msg.action == "fork" {
			m.fenceRootTurnProjection()
		}
		m.hydrateSession()
		if err := m.app.ReadyGoal(); err != nil {
			m.pushLine(styleError.Render("tree goal: " + err.Error()))
			return m, nil
		}
		switch msg.action {
		case "fork":
			m.lastStatus = "forked branch " + msg.branch.Name
		case "rename":
			m.lastStatus = "renamed branch " + msg.branch.Name
		case "delete":
			m.lastStatus = "deleted branch"
		default:
			m.lastStatus = "selected branch " + msg.branch.Name
		}
	case worktreeForkMsg:
		if msg.generation != m.pickerGeneration {
			return m, nil
		}
		m.forkLoading = false
		if msg.err != nil {
			m.pushLine(styleError.Render("fork worktree: " + msg.err.Error()))
			return m, nil
		}
		m.pickFork = false
		if msg.result.Worktree == nil {
			m.pushLine(styleError.Render("fork worktree: missing worktree result"))
			return m, nil
		}
		m.lastStatus = "created worktree " + msg.result.Worktree.Path
		m.pushLine(styleFooter.Render(fmt.Sprintf("worktree fork created\n  path: %s\n  branch: %s\n  session: %s\nRun `snow resume %q` in a new process. Trust is evaluated independently for the new project path.", msg.result.Worktree.Path, msg.result.Worktree.Branch, msg.result.SessionPath, msg.result.SessionPath)))
	case sessionStoreMsg:
		if msg.generation != m.sessionOpGeneration {
			if msg.store != nil {
				_ = msg.store.Close()
			}
			return m, nil
		}
		m.sessionOpLoading = false
		if msg.err != nil {
			m.pushLine(styleError.Render("session: " + msg.err.Error()))
			if m.startupResumeRequired {
				return m.startSessionPick()
			}
			return m, nil
		}
		if msg.store == nil {
			m.pushLine(styleError.Render("session: empty store result"))
			if m.startupResumeRequired {
				return m.startSessionPick()
			}
			return m, nil
		}
		if err := m.switchSession(msg.store); err != nil {
			_ = msg.store.Close()
			m.pushLine(styleError.Render("session: " + err.Error()))
			return m, nil
		}
		if msg.path == "new" {
			m.lastStatus = "new session"
		} else if msg.path == "fork" {
			m.lastStatus = "forked independent session " + shortSessionID(msg.store.ID())
		} else {
			m.lastStatus = "resumed " + shortSessionID(msg.store.ID())
		}
	case queueSettledMsg:
		if msg.epoch != m.queueEpoch {
			return m, nil
		}
		m.queueSettleWaiting = false
		if msg.err != nil || len(m.queueFallbacks) == 0 || m.busy || m.app == nil || m.app.Agent.IsRunning() {
			return m, nil
		}
		return m, m.startQueueFallback()
	case queueSubmitMsg:
		m.removeQueueAttempt(msg)
		if msg.epoch != m.queueEpoch {
			return m, nil
		}
		if msg.err != nil {
			m.pushLine(styleError.Render(string(msg.kind) + ": " + msg.err.Error()))
			return m, nil
		}
		if msg.fallback {
			// Wait until the preceding turn_done has been ingested before starting
			// the fallback prompt. Otherwise that late event can mark the new run
			// idle after it has already started.
			m.queueFallbacks = append(m.queueFallbacks, msg)
			if !m.busy {
				if m.app.Agent.IsRunning() {
					return m, m.waitForQueueSettlement()
				}
				return m, m.startQueueFallback()
			}
			m.lastStatus = "waiting for current turn to settle"
			return m, nil
		}
		if msg.accepted {
			fullText := queueMessageFullText(msg)
			m.rememberInputHistory(fullText)
			pending := false
			for _, item := range m.app.Agent.PendingInputs().Items {
				if item.ID == msg.itemID {
					pending = true
					m.queueOriginalText[item.ID] = fullText
					m.renderQueuedInput(item, msg.text)
					break
				}
			}
			if !pending {
				// Delivery may beat this acknowledgment. Still render the accepted
				// submission, but do not retain it as abort-restorable state.
				m.renderQueuedInput(protocol.QueuedInput{ID: msg.itemID, Kind: msg.kind, Text: msg.text}, msg.text)
				delete(m.queueRendered, msg.itemID)
			}
			if m.editor.Value() == msg.text {
				m.editor.Reset()
				m.pastedTexts = nil
				m.refreshInputCompletions()
			}
		}
		m.layout()
		return m, nil
	case textareaResultMsg:
		return m.applyTextareaResult(msg)
	case clipboardImageMsg:
		if msg.generation != 0 && msg.generation != m.imagePasteGeneration {
			return m, nil
		}
		if msg.err != nil {
			// A non-image clipboard is expected during ordinary text paste. Other
			// failures (timeout, oversize, malformed image) must remain visible.
			if errors.Is(msg.err, errClipboardHasNoImage) {
				return m, routeTextareaCmdGeneration(textareaTargetComposer, "", "", msg.generation, textarea.Paste)
			}
			m.lastErrorText = "paste image: " + msg.err.Error()
			m.pushLine(styleError.Render(m.lastErrorText))
			return m, nil
		}
		if len(m.promptImages) >= maxPromptImages {
			m.lastErrorText = fmt.Sprintf("paste: at most %d images per prompt", maxPromptImages)
			return m, nil
		}
		if promptImageBytes(m.promptImages)+len(msg.block.Data) > maxPromptImageTotalBytes {
			m.lastErrorText = fmt.Sprintf("paste: image attachments exceed %d MiB aggregate limit", maxPromptImageTotalBytes>>20)
			return m, nil
		}
		index := len(m.promptImages)
		m.promptImages = append(m.promptImages, msg.block)
		lineInfo := m.editor.LineInfo()
		m.editor.InsertString(imageAttachmentInsertion(
			m.editor.Value(), m.editor.Line(), lineInfo.StartColumn+lineInfo.ColumnOffset, index,
		))
		m.lastStatus = fmt.Sprintf("attached %s", imageAttachmentToken(index))
		if cmd := m.refreshInputCompletions(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.layout()
		return m, tea.Batch(cmds...)
	}

	if m.userInputPending && m.userInputEditing {
		previous := m.userInputEditor.Value()
		var cmd tea.Cmd
		m.userInputEditor, cmd = m.userInputEditor.Update(msg)
		if question := m.currentUserInputQuestion(); question != nil {
			m.userInputDrafts[question.ID] = m.userInputEditor.Value()
		}
		if m.userInputEditor.Value() != previous {
			m.layout()
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	} else {
		previousEditorValue := m.editor.Value()
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		if m.editor.Value() != previousEditorValue {
			// Paste and other non-key textarea messages bypass handleKey but must
			// still resize the composer and refresh input-driven overlays.
			m.prunePastedTextAttachments(m.editor.Value())
			if mentionCmd := m.refreshInputCompletions(); mentionCmd != nil {
				cmds = append(cmds, mentionCmd)
			}
			m.layout()
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	m.transcript, _ = m.transcript.Update(msg)
	return m, tea.Batch(cmds...)
}
