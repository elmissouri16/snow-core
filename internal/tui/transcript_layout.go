package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const maxStreamingTranscriptSnapshotBytes = 512 << 10

// joinTranscriptContent avoids copying the complete stable transcript when no
// live tail exists. A live snapshot is still copied into a right-sized buffer
// so renderer scratch capacity cannot remain pinned by transcriptContent.
func joinTranscriptContent(base, live string) string {
	if live == "" {
		return base
	}
	var content strings.Builder
	content.Grow(len(base) + len(live) + 1)
	content.WriteString(base)
	if base != "" {
		content.WriteByte('\n')
	}
	content.WriteString(live)
	return content.String()
}

func boundedRenderedTail(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	start := len(value) - maxBytes
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	// Begin at a rendered line boundary so the tail cannot start in the middle
	// of a CSI/SGR escape. Explicit resets prevent styles active in the omitted
	// prefix from leaking into the retained viewport or following UI.
	if newline := strings.IndexByte(value[start:], '\n'); newline >= 0 {
		start += newline + 1
	} else if escape := strings.LastIndexByte(value[:start], '\x1b'); escape >= 0 {
		// If the byte cutoff landed inside the final ANSI sequence, advance past
		// its terminator. Generated transcript styling uses CSI sequences.
		end := escape + 1
		if end < len(value) && value[end] == '[' {
			end++
			for end < len(value) && (value[end] < 0x40 || value[end] > 0x7e) {
				end++
			}
			if end < len(value) {
				end++
			}
		} else if end < len(value) {
			end++
		}
		if end > start {
			start = end
		}
	}
	return "\x1b[0m" + value[start:] + "\x1b[0m"
}

func (m *Model) refreshTranscriptWithForce(force bool) {
	if m.batchingEvents {
		m.transcriptDirty = true
		return
	}
	width := m.transcript.Width
	// Selection coordinates belong to an immutable wrapped snapshot. Keep live
	// stream deltas off-screen until release so a response cannot cancel a drag
	// or move the text beneath the pointer.
	if m.transcriptSelection.pressActive && m.transcriptContent != "" {
		m.transcriptDirty = true
		return
	}
	// Freeze the current snapshot while the user reads away from the tail.
	// State and source buffers continue to update; reaching the snapshot bottom
	// or resizing performs one complete catch-up render.
	if !force && m.transcriptContent != "" && !m.transcript.AtBottom() && m.transcriptBaseWidth == width {
		m.transcriptDirty = true
		return
	}
	if m.transcriptBaseDirty || m.transcriptBaseWidth != width {
		stableLines := m.lines
		if m.inlineTranscript {
			stableLines = m.lines[m.inlineDisplayStart():]
		}
		if m.transcriptBaseWidth != width || !m.transcriptBaseAppend || m.transcriptBaseSynced > len(stableLines) {
			m.transcriptBase = ""
			m.transcriptBaseSynced = 0
		}
		if delta := stableLines[m.transcriptBaseSynced:]; len(delta) > 0 {
			wrapped := strings.Join(delta, "\n")
			if width > 0 {
				// Wrapping is line-local, so appending the newly stable suffix is
				// equivalent to reflowing the complete transcript at this width.
				wrapped = lipgloss.NewStyle().Width(width).Render(wrapped)
			}
			var base strings.Builder
			base.Grow(len(m.transcriptBase) + len(wrapped) + 1)
			base.WriteString(m.transcriptBase)
			if m.transcriptBase != "" {
				base.WriteByte('\n')
			}
			base.WriteString(wrapped)
			m.transcriptBase = base.String()
		}
		m.transcriptBaseSynced = len(stableLines)
		m.transcriptBaseWidth = width
		m.transcriptBaseDirty = false
		m.transcriptBaseAppend = false
	}
	live := m.liveText()
	if live != "" && width > 0 {
		live = lipgloss.NewStyle().Width(width).Render(live)
	}
	baseContent := m.transcriptBase
	if live != "" && !force && len(baseContent) > maxStreamingTranscriptSnapshotBytes {
		baseContent = styleFooter.Render("── older transcript hidden while streaming; scroll up to load ──") + "\n" + boundedRenderedTail(baseContent, maxStreamingTranscriptSnapshotBytes)
	}
	content := joinTranscriptContent(baseContent, live)
	if content == m.transcriptContent {
		m.transcriptDirty = false
		return
	}
	// Selection points refer to the current wrapped transcript snapshot. A live
	// stream boundary or width-dependent reflow can replace those rows, so clear
	// selection before publishing a different source rather than copying text
	// that no longer matches the highlighted cells.
	if content != m.transcriptContent {
		m.clearTranscriptSelection()
		// Selection rows are needed only while the user is interacting. Avoid a
		// second full transcript split on every streaming refresh.
		m.transcriptSelectionLines = nil
	}
	wasAtBottom := m.transcript.AtBottom()
	m.transcript.SetContent(content)
	m.transcriptViewRevision++
	m.transcriptViewCacheValid = false
	// Follow new output only when the user was already following the tail.
	// Preserve an intentional scroll position while a stream continues.
	if wasAtBottom {
		m.transcript.GotoBottom()
	}
	m.transcriptContent = content
	m.transcriptDirty = false
}

// desiredComposerHeight grows with explicit and soft-wrapped input while
// keeping the idle composer comfortably usable. The textarea remains internally
// scrollable after reaching maxComposerHeight.
func (m *Model) desiredComposerHeight() int {
	text := m.editor.Value()
	if m.loginMode || m.loginProfileMode || m.loginEndpointMode || text == "" {
		return minComposerHeight
	}
	width := m.editor.Width()
	if width < 1 {
		width = max(1, m.width-4)
	}
	// The composer stops growing after maxComposerHeight. Avoid wrapping an
	// entire large paste on every edit once a bounded prefix already proves the
	// editor must be at that cap. Word wrapping can add breaks compared with
	// hard wrapping, but cannot reduce this lower-bound line count.
	if composerHardWrapReaches(text, width, maxComposerHeight) {
		return maxComposerHeight
	}
	wrapped := xansi.Wordwrap(text, width, "")
	wrapped = xansi.Hardwrap(wrapped, width, true)
	return min(maxComposerHeight, max(minComposerHeight, lipgloss.Height(wrapped)))
}

func composerHardWrapReaches(text string, width, target int) bool {
	if width < 1 || target <= 1 {
		return true
	}
	lines, columns := 1, 0
	for text != "" {
		if text[0] == '\n' {
			lines++
			columns = 0
			text = text[1:]
			if lines >= target {
				return true
			}
			continue
		}
		cluster, cellWidth := xansi.FirstGraphemeCluster(text, xansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		text = text[len(cluster):]
		if columns > 0 && columns+cellWidth > width {
			lines++
			columns = 0
			if lines >= target {
				return true
			}
		}
		columns += max(0, cellWidth)
	}
	return false
}

func (m *Model) currentTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *Model) showRunStatus() bool {
	return m.busy && !m.compacting && !m.permPending && !m.userInputPending && !m.runStartedAt.IsZero()
}

func (m *Model) runStatusHeight() int {
	if m.showRunStatus() {
		return 1
	}
	return 0
}

func formatRunElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	totalSeconds := int(elapsed / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func (m *Model) renderRunStatus() string {
	if !m.showRunStatus() {
		return ""
	}
	elapsed := formatRunElapsed(m.currentTime().Sub(m.runStartedAt))
	detail := elapsed
	if pending := len(m.pendingInputs.Items); pending > 0 {
		detail += fmt.Sprintf(" · %d queued", pending)
	}
	detail += " · esc to interrupt"
	return styleThinking.Render(m.spinner.View()+" ") +
		styleHeader.Render("Working") +
		styleHeaderDim.Render(" ("+detail+")")
}

func (m *Model) runStatusMouseBounds() (y, start, end int, ok bool) {
	if !m.showRunStatus() {
		return 0, 0, 0, false
	}
	y = 2 + m.transcript.Height // header + separator + transcript
	if overlay := m.renderOverlays(); overlay != "" {
		y += lipgloss.Height(overlay)
	}
	start = xansi.StringWidth(m.spinner.View() + " ")
	end = start + xansi.StringWidth("Working")
	return y, start, end, true
}

// handleRunStatusMouse turns the persistent Working label into a quick return
// to live output. This is available only in application mouse mode; native
// terminal mouse mode remains owned by the terminal emulator.
func (m *Model) handleRunStatusMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	if m.app == nil || !m.app.Cfg.TUI.Mouse {
		return false, nil
	}
	event := tea.MouseEvent(msg)
	if event.Action != tea.MouseActionPress || event.Button != tea.MouseButtonLeft {
		return false, nil
	}
	y, start, end, ok := m.runStatusMouseBounds()
	if !ok || event.Y != y || event.X < start || event.X >= end {
		return false, nil
	}
	m.clearTranscriptSelection()
	m.transcript.GotoBottom()
	m.catchUpTranscriptAtBottom()
	return true, nil
}

func (m *Model) managedFrameHeight() int {
	// The normal-screen renderer still owns one terminal-height live frame. Its
	// transcript viewport absorbs unused rows so the composer/footer remain
	// anchored at the bottom while finalized rows print above into native
	// scrollback. A short fixed frame would leave the composer stranded near the
	// last response and can expose stale separator rows when its geometry changes.
	return m.height
}

func (m *Model) managedFrameWidth() int {
	// Leave the terminal's final cell unused. Writing through the right margin
	// can trigger physical autowrap before Bubble Tea's logical newline, causing
	// stale rows or apparent flicker when frame geometry changes.
	return max(1, m.width-1)
}

// inlineModalOverlay reports pickers that can temporarily own the entire fixed
// inline frame. Replacing the transcript/composer tail keeps the renderer at a
// constant height (so native scrollback is untouched) while giving modal lists
// enough rows to show more than their selected item.
func (m *Model) inlineModalOverlay() bool {
	return m.inlineTranscript && (m.pickSession || m.pickTree || m.pickInfo ||
		m.pickPermissionMode || m.permPending || m.userInputPending ||
		m.confirmGoalReplace || m.planPrompt)
}

// Inline completion lists still need the composer for live filtering. They own
// the rest of the same fixed frame while visible, hiding transcript/footer rows
// instead of growing the normal-screen renderer.
func (m *Model) inlineInputOverlay() bool {
	return m.inlineTranscript && (m.compVisible || m.skillVisible || m.mentionVisible || m.mentionLoading)
}

// availableOverlayHeight is the maximum picker/palette area that leaves one
// transcript row visible inside the fixed managed frame.
func (m *Model) availableOverlayHeight() int {
	if m.inlineModalOverlay() {
		return min(m.managedFrameHeight(), inlineOverlayMaxHeight)
	}
	if m.inlineInputOverlay() {
		return max(1, min(inlineOverlayMaxHeight, m.managedFrameHeight()-1-m.editor.Height())) // separator + composer
	}
	return max(0, m.managedFrameHeight()-m.fixedChromeRows()-m.editor.Height()-m.runStatusHeight()-minTranscriptHeight)
}

// chromeHeight returns the exact rows outside the transcript viewport.
func (m *Model) fixedChromeRows() int {
	if m.inlineTranscript {
		return inlineFixedChromeHeight
	}
	return fixedChromeHeight
}

func (m *Model) chromeHeight() int {
	overlayHeight := 0
	if overlay := m.renderOverlays(); overlay != "" {
		overlayHeight = lipgloss.Height(overlay)
	}
	return m.fixedChromeRows() + m.editor.Height() + m.runStatusHeight() + overlayHeight
}

func (m *Model) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	// Dynamic chrome (especially the one-line run status) changes viewport
	// height at prompt start/end. Preserve tail-following across that resize;
	// otherwise shrinking a bottomed viewport makes AtBottom false and freezes
	// reasoning/tool events until the run-status row disappears at turn_done.
	wasAtBottom := m.transcript.AtBottom()
	frameWidth := m.managedFrameWidth()
	m.editor.SetWidth(max(1, frameWidth-4))
	m.userInputEditor.SetWidth(max(1, frameWidth-6))
	frameHeight := m.managedFrameHeight()
	maxEditorHeight := max(minComposerHeight, frameHeight-m.fixedChromeRows()-m.runStatusHeight()-minTranscriptHeight)
	editorH := min(m.desiredComposerHeight(), min(maxComposerHeight, maxEditorHeight))
	m.editor.SetHeight(editorH)
	bodyH := max(minTranscriptHeight, frameHeight-m.chromeHeight())
	if m.transcript.Width != frameWidth || m.transcript.Height != bodyH {
		m.transcriptDirty = true
	}
	m.transcript.Width = frameWidth
	m.transcript.Height = bodyH
	if wasAtBottom {
		m.transcript.GotoBottom()
	}
}

func (m *Model) quitCmd() tea.Cmd {
	if m.inlineTranscript {
		m.inlineExiting = true
	}
	return tea.Quit
}

func (m *Model) handleModelShortcut(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !keyMatches(msg, m.keys.Models) {
		return false, nil
	}
	if m.busy || (m.app != nil && m.app.Agent != nil && m.app.Agent.IsRunning()) {
		m.lastStatus = "model: wait for the current turn to finish"
		return true, nil
	}
	_, cmd := m.startModelPick()
	return true, cmd
}

func (m *Model) handleFleetShortcut(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Agents):
		if m.subagentFleetOpen {
			return true, nil
		}
		if m.app.Subagents == nil {
			m.lastStatus = "subagents are disabled (enable them in /settings or start with --subagents)"
			return true, nil
		}
		return true, m.openSubagentFleet("")
	case keyMatches(msg, m.keys.Processes):
		if m.processFleetOpen {
			return true, nil
		}
		return true, m.openProcessFleet("")
	default:
		return false, nil
	}
}

// handleKey processes key presses, the command palette, and login capture.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.trustPending {
		// Once persistence starts, keep ownership of the async result so a late
		// app cannot be constructed after Bubble Tea has already exited. Before
		// selection, Ctrl+C/Ctrl+D still quit without recording a decision.
		if m.trustSaving {
			return m, nil
		}
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlD {
			return m, m.quitCmd()
		}
		switch msg.Type {
		case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
			m.trustChoice = (m.trustChoice + 1) % 2
			m.trustError = ""
		case tea.KeyDown, tea.KeyRight, tea.KeyTab:
			m.trustChoice = (m.trustChoice + 1) % 2
			m.trustError = ""
		case tea.KeyEsc:
			m.trustChoice = 0
			m.trustSaving = true
			m.trustError = ""
			return m, m.saveTrustCmd(trust.LevelDeny)
		case tea.KeyEnter:
			level := trust.LevelDeny
			if m.trustChoice == 1 {
				level = trust.LevelAllow
			}
			m.trustSaving = true
			m.trustError = ""
			return m, m.saveTrustCmd(level)
		}
		return m, nil
	}

	// Startup can fail before the app exists. Keep emergency exit keys live
	// while booting and on the terminal error screen so the alt-screen can
	// always be restored cleanly.
	if m.app == nil {
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, m.quitCmd()
		case tea.KeyCtrlD:
			if m.editor.Value() == "" {
				return m, m.quitCmd()
			}
		}
		return m, nil
	}

	// F6 toggles application mouse reporting without restarting. Native terminal
	// selection/context menus require reporting to be disabled; application mode
	// adds wheel scrolling, transcript drag-copy, and edge auto-scroll.
	if msg.Type == tea.KeyF6 {
		if m.app.Cfg.TUI.Mouse {
			m.clearTranscriptSelection()
			m.catchUpTranscriptAfterSelection()
			m.app.Cfg.TUI.Mouse = false
			m.lastStatus = "native selection + context menu · keyboard viewport scrolling"
			return m, tea.DisableMouse
		}
		m.app.Cfg.TUI.Mouse = true
		m.lastStatus = "app mouse · wheel scroll + drag copy enabled"
		return m, tea.EnableMouseCellMotion
	}

	// Emergency Ctrl+C is resolved before any configurable action so a custom
	// submit/accept binding can never shadow terminal recovery.
	if msg.Type == tea.KeyCtrlC {
		if m.busy {
			m.composerSelectAll = false
			m.requestAbort()
			return m, nil
		}
		if m.composerSelectAll && m.editor.Value() != "" && !m.composerCoveredByModal() {
			return m, m.copyTranscriptSelectionCmd(m.editor.Value())
		}
		return m, m.quitCmd()
	}

	// Session creation/opening runs asynchronously in production. Keep that
	// transition modal so no prompt or command can be admitted against the old
	// (or startup placeholder) store before the new store is installed.
	if m.sessionOpLoading {
		m.lastStatus = "opening session…"
		return m, nil
	}

	// Host interaction requests preempt ordinary pickers. They may arrive from
	// an independent child while another overlay is open; keeping them first
	// guarantees the visible blocking request also owns the keyboard.
	if m.permPending {
		return m.handlePermissionPick(msg)
	}
	if m.userInputPending {
		return m.handleUserInputKey(msg)
	}
	if m.restartPromptVisible() {
		return m.handleRestartPromptKey(msg)
	}

	if m.processFleetOpen || m.subagentFleetOpen {
		if handled, cmd := m.handleFleetShortcut(msg); handled {
			return m, cmd
		}
	}
	if m.processFleetOpen {
		return m.handleProcessFleetKey(msg)
	}
	if m.subagentFleetOpen {
		return m.handleSubagentFleetKey(msg)
	}

	if m.compatibleLoginPending || m.logoutPending {
		return m, nil
	}

	if m.confirmGoalReplace {
		msg = normalizePickerKeyWithMap(msg, m.keys)
		switch msg.Type {
		case tea.KeyEsc:
			m.confirmGoalReplace = false
			return m, nil
		case tea.KeyEnter:
			objective, budget := m.pendingGoalObjective, m.pendingGoalBudget
			m.confirmGoalReplace = false
			g, err := m.app.CreateGoal(objective, budget, true)
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = g
				m.busy = true
				m.runStartedAt = m.currentTime()
			}
			return m, nil
		}
		return m, nil
	}
	if m.planPrompt {
		return m.handlePlanImplementationKey(normalizePickerKeyWithMap(msg, m.keys))
	}

	// Bubble Tea recognizes Option+Return when the terminal sends ESC+CR in
	// one read. Some macOS terminals split those bytes into two events. Join
	// that split form back into Alt+Enter before normal Enter submission.
	if m.metaEnterPending {
		m.metaEnterPending = false
		if msg.Type == tea.KeyEnter && !m.busy {
			msg.Alt = true
		}
	}

	// --- OpenAI-compatible profile-name capture mode ---
	if m.loginProfileMode {
		if keyMatches(msg, m.keys.Close) {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		} else if keyMatches(msg, m.keys.Accept) {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		} else if keyMatches(msg, m.keys.Paste) {
			msg = tea.KeyMsg{Type: tea.KeyCtrlV}
		}
		return m.handleLoginProfileKey(msg)
	}

	// --- OpenAI-compatible endpoint capture mode ---
	if m.loginEndpointMode {
		if keyMatches(msg, m.keys.Close) {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		} else if keyMatches(msg, m.keys.Accept) {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		} else if keyMatches(msg, m.keys.Paste) {
			msg = tea.KeyMsg{Type: tea.KeyCtrlV}
		}
		return m.handleLoginEndpointKey(msg)
	}

	// --- Masked login capture mode ---
	if m.loginMode {
		if keyMatches(msg, m.keys.Close) {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		} else if keyMatches(msg, m.keys.Accept) {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		} else if keyMatches(msg, m.keys.Paste) {
			msg = tea.KeyMsg{Type: tea.KeyCtrlV}
		}
		return m.handleLoginKey(msg)
	}

	// --- Provider picker (for /login) ---
	if m.pickProvider {
		return m.handleProviderPick(msg)
	}

	// --- ChatGPT auth-source picker ---
	if m.pickChatGPTAuth {
		return m.handleChatGPTAuthPick(msg)
	}

	// --- Model picker (for /model) ---
	if m.pickModel {
		return m.handleModelPick(msg)
	}

	// --- Interactive keybinding editor ---
	if m.pickKeybindings {
		return m.handleKeybindingsKey(msg)
	}

	// --- Unified settings panel ---
	if m.pickSettings {
		return m.handleSettingsKey(msg)
	}

	// --- Centered help viewer ---
	if m.pickHelp {
		return m.handleHelpKey(msg)
	}

	// --- Thinking picker (for /thinking) ---
	if m.pickThinking {
		return m.handleThinkingPick(msg)
	}

	// --- Fork destination picker ---
	if m.pickFork {
		return m.handleForkPick(msg)
	}

	// --- Session picker ---
	if m.pickSession {
		return m.handleSessionPick(msg)
	}

	// --- Branch tree picker ---
	if m.pickTree {
		return m.handleTreePick(msg)
	}

	// --- Read-only MCP/skills status picker ---
	if m.pickInfo {
		return m.handleInfoPick(msg)
	}

	// --- Permission mode picker (/permissions) ---
	if m.pickPermissionMode {
		return m.handlePermissionModePick(msg)
	}

	// Escape interrupts an active agent run. Modal Escape behavior above keeps
	// its existing meaning (cancel picker/login or deny a permission request).
	if msg.Type == tea.KeyEsc && m.busy && !m.compacting && !m.runStartedAt.IsZero() {
		m.requestAbort()
		return m, nil
	}

	// --- Command palette: navigation keys are consumed while open ---
	if m.compVisible {
		msg = normalizePickerKeyWithMap(msg, m.keys)
		switch msg.Type {
		case tea.KeyUp:
			if len(m.compMatches) > 0 {
				m.compIndex = (m.compIndex - 1 + len(m.compMatches)) % len(m.compMatches)
			}
			return m, nil
		case tea.KeyDown:
			if len(m.compMatches) > 0 {
				m.compIndex = (m.compIndex + 1) % len(m.compMatches)
			}
			return m, nil
		case tea.KeyTab:
			if len(m.compMatches) > 0 {
				return m.insertCompletion(m.compMatches[m.compIndex])
			}
			return m, nil
		case tea.KeyShiftTab:
			if len(m.compMatches) > 0 {
				m.compIndex = (m.compIndex - 1 + len(m.compMatches)) % len(m.compMatches)
			}
			return m, nil
		case tea.KeyEsc:
			m.compVisible = false
			return m, nil
		case tea.KeyEnter:
			if len(m.compMatches) == 0 {
				m.compVisible = false
				return m, nil
			}
			return m.pickCompletion(m.compMatches[m.compIndex])
		}
	}

	// --- Agent Skill picker: Enter/Tab complete the current $skill token. ---
	if m.skillVisible {
		msg = normalizePickerKeyWithMap(msg, m.keys)
		switch msg.Type {
		case tea.KeyUp, tea.KeyShiftTab:
			if len(m.skillMatches) > 0 {
				m.skillIndex = (m.skillIndex - 1 + len(m.skillMatches)) % len(m.skillMatches)
			}
			return m, nil
		case tea.KeyDown:
			if len(m.skillMatches) > 0 {
				m.skillIndex = (m.skillIndex + 1) % len(m.skillMatches)
			}
			return m, nil
		case tea.KeyTab, tea.KeyEnter:
			if len(m.skillMatches) > 0 {
				return m.insertSkillCompletion(m.skillMatches[m.skillIndex].Name)
			}
		case tea.KeyEsc:
			m.skillVisible = false
			return m, nil
		}
	}

	// --- File mention picker: Enter/Tab insert a path, never submit the prompt ---
	if m.mentionVisible {
		msg = normalizePickerKeyWithMap(msg, m.keys)
		switch msg.Type {
		case tea.KeyUp, tea.KeyShiftTab:
			if len(m.mentionMatches) > 0 {
				m.mentionIndex = (m.mentionIndex - 1 + len(m.mentionMatches)) % len(m.mentionMatches)
			}
			return m, nil
		case tea.KeyDown:
			if len(m.mentionMatches) > 0 {
				m.mentionIndex = (m.mentionIndex + 1) % len(m.mentionMatches)
			}
			return m, nil
		case tea.KeyTab, tea.KeyEnter:
			if len(m.mentionMatches) > 0 {
				return m.insertMention(m.mentionMatches[m.mentionIndex])
			}
		case tea.KeyEsc:
			m.mentionVisible = false
			return m, nil
		}
	}

	// Top-level shortcuts open model/fleet inspectors, cycle thinking effort, or
	// cycle collaboration mode. Every modal/completion path above retains its
	// existing navigation semantics.
	if handled, cmd := m.handleModelShortcut(msg); handled {
		return m, cmd
	}
	if handled, cmd := m.handleFleetShortcut(msg); handled {
		return m, cmd
	}
	if keyMatches(msg, m.keys.Thinking) {
		return m.cycleThinkingEffort()
	}
	if keyMatches(msg, m.keys.Mode) {
		return m, m.toggleCollaborationMode()
	}

	if handled, cmd := m.handleComposerSelectionKey(msg); handled {
		return m, cmd
	}

	if !m.busy && len(m.pastedTexts) > 0 &&
		strings.TrimSpace(stripImageAttachmentTokens(stripPastedTextAttachmentTokens(m.editor.Value(), m.pastedTexts), len(m.promptImages))) == "" &&
		(msg.Type == tea.KeyBackspace || msg.Type == tea.KeyEsc) {
		m.removeLastPastedTextAttachment()
		m.refreshInputCompletions()
		m.layout()
		return m, nil
	}

	if !m.busy && len(m.promptImages) > 0 &&
		strings.TrimSpace(stripImageAttachmentTokens(m.editor.Value(), len(m.promptImages))) == "" &&
		(msg.Type == tea.KeyBackspace || msg.Type == tea.KeyEsc) {
		index := len(m.promptImages) - 1
		m.editor.SetValue(removeImageAttachmentToken(m.editor.Value(), index))
		m.editor.CursorEnd()
		m.promptImages = m.promptImages[:index]
		m.lastStatus = "removed " + imageAttachmentToken(index)
		m.refreshInputCompletions()
		m.layout()
		return m, nil
	}

	// Preserve a standalone Escape briefly as a possible macOS Option/Meta
	// prefix. Modal Escape and active-run interruption have already returned
	// above, so this applies only to the idle composer. Replayed terminal input
	// has already passed through the fragment timeout and must not be held again.
	if msg.Type == tea.KeyEsc && !m.busy && !m.replayingInput {
		m.metaEnterSeq++
		seq := m.metaEnterSeq
		m.metaEnterPending = true
		return m, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
			return clearMetaEnterMsg(seq)
		})
	}

	// --- Normal editing / sending ---
	if handled, cmd := m.navigateInputHistory(msg); handled {
		return m, cmd
	}
	submitKey := keyMatches(msg, m.keys.Submit)
	followUpKey := keyMatches(msg, m.keys.FollowUp)
	abortKey := keyMatches(msg, m.keys.Abort)
	quitKey := keyMatches(msg, m.keys.Quit)
	// Ordinary typing, navigation, and deletion do not need submission-time
	// image stripping, whitespace scans, or queue/goal checks. Keeping this hot
	// path short is especially important while Backspace repeats over a paste.
	if !submitKey && !followUpKey && !abortKey && !quitKey {
		return m.updateComposerEditor(msg)
	}

	displayText := m.editor.Value()
	text := stripImageAttachmentTokens(m.expandedPastedText(displayText), len(m.promptImages))
	if submitKey && m.modeSwitching {
		m.lastStatus = "waiting for mode switch"
		return m, nil
	}
	trimmed := strings.TrimSpace(text)
	if submitKey && m.compatibleLoginPending && m.app != nil && m.app.ProviderID == openaicompat.ProviderID && trimmed != "" && !strings.HasPrefix(trimmed, "/") {
		m.lastStatus = "waiting for openai-compatible model discovery"
		return m, nil
	}
	goalControl := m.busy && (strings.HasPrefix(trimmed, "/goal pause") || strings.HasPrefix(trimmed, "/goal clear") || strings.HasPrefix(trimmed, "/goal edit"))
	initControl := m.busy && (trimmed == "/init" || strings.HasPrefix(trimmed, "/init "))
	keybindingsControl := m.busy && (trimmed == "/keybindings" || strings.HasPrefix(trimmed, "/keybindings "))
	processInspectorControl := m.busy && (trimmed == "/processes" || strings.HasPrefix(trimmed, "/processes "))
	busyControl := goalControl || initControl || keybindingsControl || processInspectorControl
	if abortKey && m.busy {
		m.requestAbort()
		return m, nil
	}
	if (followUpKey || submitKey && m.busy && !busyControl) && len(m.promptImages) > 0 {
		m.lastErrorText = "image attachments cannot be queued; wait for the current turn"
		m.lastStatus = m.lastErrorText
		return m, nil
	}
	if followUpKey && m.busy && trimmed != "" && !strings.HasPrefix(trimmed, "/") {
		return m, m.submitQueuedInput(displayText, text, protocol.QueuedInputFollowUp)
	}
	if submitKey && m.busy && !busyControl && trimmed != "" && !strings.HasPrefix(trimmed, "/") {
		return m, m.submitQueuedInput(displayText, text, protocol.QueuedInputSteer)
	}
	if submitKey && (!m.busy || busyControl) {
		if strings.HasPrefix(text, "/") && len(m.promptImages) == 0 {
			displayCommand := strings.TrimSpace(stripImageAttachmentTokens(displayText, len(m.promptImages)))
			return m.runCommandWithDisplay(trimmed, displayCommand)
		}
		if trimmed != "" || len(m.promptImages) > 0 {
			display := displayText
			if strings.TrimSpace(display) == "" {
				display = fmt.Sprintf("[%d image(s)]", len(m.promptImages))
			}
			m.pushLine(styleUser.Render("› " + display))
			m.imagePasteGeneration++
			m.editor.Reset()
			return m, m.startPrompt(displayText)
		}
	}

	if quitKey && !m.busy && (msg.String() != "ctrl+d" || displayText == "") {
		return m, m.quitCmd()
	}

	return m.updateComposerEditor(msg)
}
