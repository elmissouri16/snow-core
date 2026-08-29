package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// renderOverlays returns the picker/palette area between the transcript and
// composer. Its height is bounded so no overlay can push the frame into the
// terminal's scrollback buffer.
func (m *Model) renderOverlays() string {
	// Blocking host requests are exclusive overlays and mirror keyboard
	// precedence. Do not let an unrelated picker hide a request that is holding
	// a root or child agent.
	if m.permPending {
		return m.limitOverlay(m.renderPermissionPicker())
	}
	if m.userInputPending {
		return m.limitOverlay(m.renderUserInput())
	}
	var overlays []string
	if m.confirmGoalReplace {
		overlays = append(overlays, styleHeader.Render("Replace unfinished goal?")+"\n"+styleCompletionSelected.Render("› Enter to replace")+"\n"+styleCompletion.Render("  Esc to cancel"))
	} else if m.planPrompt {
		overlays = append(overlays, m.renderPlanImplementationPrompt())
	} else if m.planNudgeVisible() {
		overlays = append(overlays, styleHeaderDim.Render("Tip: use /plan to explore and produce a decision-complete plan"))
	}
	if m.compVisible {
		matches := m.compMatches
		selected := m.compIndex
		if limit := m.availableOverlayHeight(); len(matches) > limit {
			start := selected - limit/2
			if start < 0 {
				start = 0
			}
			if start+limit > len(matches) {
				start = len(matches) - limit
			}
			matches = matches[start : start+limit]
			selected -= start
		}
		overlays = append(overlays, renderCompletions(matches, selected, m.width))
	}
	if m.skillVisible {
		if r := m.renderSkillCompletionPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if !m.skillVisible && (m.mentionVisible || m.mentionLoading) {
		if r := m.renderMentionPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickFork {
		if r := m.renderForkPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickSession {
		if r := m.renderSessionPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickTree {
		if r := m.renderTreePicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickInfo {
		if r := m.renderInfoPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.compacting {
		overlays = append(overlays, m.renderCompactionProgress())
	}
	if m.pickPermissionMode {
		if r := m.renderPermissionModePicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if len(overlays) == 0 {
		return ""
	}
	return m.limitOverlay(strings.Join(overlays, "\n"))
}

func (m *Model) limitOverlay(overlay string) string {
	maxHeight := m.availableOverlayHeight()
	if maxHeight <= 0 || overlay == "" {
		return ""
	}
	lines := strings.Split(overlay, "\n")
	if len(lines) > maxHeight {
		lines = lines[:maxHeight]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) planNudgeScope() string {
	if m.app == nil || m.app.Session == nil {
		return ""
	}
	branchID := "main"
	// Branches returns rich picker metadata and SQLite computes it by walking
	// and decoding every branch history. Rendering the composer only needs the
	// already-cached active identity; never put a history scan on View/layout.
	if active, ok := m.app.Session.(session.ActiveBranchStore); ok {
		if id := strings.TrimSpace(active.ActiveBranchID()); id != "" {
			branchID = id
		}
	}
	return currentSessionID(m.app) + ":" + branchID
}

func (m *Model) planNudgeVisible() bool {
	if m.app == nil || m.busy || m.app.Agent.Mode() != protocol.ModeDefault || strings.HasPrefix(strings.TrimSpace(m.editor.Value()), "/") {
		return false
	}
	containsPlan := false
	for _, word := range strings.FieldsFunc(strings.ToLower(m.editor.Value()), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') }) {
		if word == "plan" {
			containsPlan = true
			break
		}
	}
	return containsPlan && !m.nudgeDismissed[m.planNudgeScope()]
}

func (m *Model) renderPlanImplementationPrompt() string {
	items := []string{"Yes, implement this plan", "Yes, clear context and implement", "No, stay in Plan mode"}
	var b strings.Builder
	b.WriteString(styleHeader.Render("Implement this plan?") + "\n")
	for i, item := range items {
		prefix, style := "  ", styleCompletion
		if i == m.planPromptChoice {
			prefix, style = "› ", styleCompletionSelected
		}
		b.WriteString(style.Render(prefix + item))
		if i < len(items)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m *Model) handlePlanImplementationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		m.planPromptChoice = (m.planPromptChoice + 2) % 3
	case tea.KeyDown, tea.KeyTab:
		m.planPromptChoice = (m.planPromptChoice + 1) % 3
	case tea.KeyEsc:
		m.planPrompt = false
	case tea.KeyEnter:
		choice := m.planPromptChoice
		m.planPrompt = false
		if choice == 2 {
			return m, nil
		}
		message := "Implement the plan."
		planText := m.latestPlan
		if choice == 1 {
			var st session.Store
			var err error
			if currentSessionPath(m.app) == "" {
				st = session.NewMemoryStore(session.Options{CWD: m.app.CWD()})
			} else {
				st, err = session.NewFileIndex(session.DefaultSessionsRoot()).Create(m.app.CWD())
			}
			if err != nil {
				m.pushLine(styleError.Render("new session: " + err.Error()))
				return m, nil
			}
			if err := m.switchSession(st); err != nil {
				_ = st.Close()
				m.pushLine(styleError.Render("new session: " + err.Error()))
				return m, nil
			}
			message = "A previous agent produced the plan below. Implement it in a fresh context, re-read files as needed, and carry the work through implementation and verification.\n\n" + planText
		}
		m.pushLine(styleUser.Render("› " + message))
		return m, m.startPromptWithMode(message, protocol.ModeDefault)
	}
	return m, nil
}

func (m *Model) currentHeaderStatus() string {
	status := "starting…"
	if m.lastErr != nil {
		return "error"
	}
	if m.app == nil {
		return status
	}
	status = "idle"
	if m.busy {
		if m.showRunStatus() {
			status = ""
		} else {
			status = m.spinner.View() + " working"
		}
	}
	if m.compacting {
		status = m.spinner.View() + " compacting"
	}
	if m.sessionOpLoading {
		status = "session…"
	}
	if m.pickSettings || m.settingsReturnToPanel {
		status = "settings"
	}
	if m.pickKeybindings {
		status = "keybindings"
	}
	if m.pickHelp {
		status = "help"
	}
	if m.modelModalVisible() {
		status = "models"
	} else if m.pickThinking {
		status = "thinking"
	}
	if m.loginModalVisible() {
		status = "login"
		if m.logoutPending {
			status = "logout…"
		} else if m.providerLogout {
			status = "logout"
		} else if m.oauthLoading || m.compatibleLoginPending {
			status = "login…"
		}
	}
	if m.pickInfo {
		status = "inspect"
	}
	// Blocking host requests own both the visible overlay and the status label,
	// even when an ordinary modal remains suspended underneath.
	if m.permPending {
		status = "permission"
	}
	if m.userInputPending {
		status = "input needed"
	}
	return status
}

// View implements tea.Model as one full-window frame: sticky header, scrollable
// transcript viewport, overlays/run status, composer, and footer.
func (m *Model) View() string {
	if m.inlineTranscript && m.inlineExiting {
		return ""
	}
	if m.width <= 0 || m.height <= 0 {
		return "loading snow…"
	}
	clipboardSequence := m.transcriptSelectionClipboard
	if m.height < minFullFrameHeight+m.runStatusHeight() || m.width < 4 {
		return fitFrame(styleBrand.Render(" snow ")+styleHeaderDim.Render("terminal too small"), m.width, m.height)
	}
	if m.trustPending {
		return m.renderTrustPrompt()
	}
	// The fleet inspector owns the frame, except when a blocking host request
	// must preempt it. Its renderer consumes only bounded in-memory snapshots.
	if m.processFleetOpen && !m.permPending && !m.userInputPending {
		return clipboardSequence + fitFrame(m.renderProcessFleetModal(), m.managedFrameWidth(), m.managedFrameHeight())
	}
	if m.subagentFleetOpen && !m.permPending && !m.userInputPending {
		return clipboardSequence + fitFrame(m.renderSubagentFleetModal(), m.managedFrameWidth(), m.managedFrameHeight())
	}

	status := m.currentHeaderStatus()
	header := m.renderHeader(status)
	frameWidth := m.managedFrameWidth()
	sep := styleSep.Render(strings.Repeat("─", frameWidth))
	overlay := m.renderOverlays()
	if m.inlineModalOverlay() && overlay != "" {
		// Modal pickers replace the live tail but remain bottom-anchored inside the
		// same terminal-height frame, so closing one restores the composer without
		// moving terminal-owned history.
		return clipboardSequence + fitFrameBottom(overlay, frameWidth, m.managedFrameHeight())
	}
	if m.inlineInputOverlay() && overlay != "" {
		frame := lipgloss.JoinVertical(lipgloss.Left, overlay, sep, m.renderEditor())
		return clipboardSequence + fitFrameBottom(frame, frameWidth, m.managedFrameHeight())
	}
	runStatus := m.renderRunStatus()

	editorView := m.renderEditor()
	footer := styleFooter.Render(" starting snow…")
	if m.lastErr != nil {
		footer = styleError.Render(" startup failed") + styleFooter.Render(" · ctrl+c to quit")
	} else if m.app != nil {
		footer = m.renderFooter()
	}

	parts := make([]string, 0, 8)
	// Keep the active provider/model/mode visible in both render modes. Inline
	// session headers also remain in native scrollback as historical boundaries,
	// but the current selection must not disappear above the visible window.
	parts = append(parts, header, sep, m.renderTranscriptView())
	if overlay != "" {
		parts = append(parts, overlay)
	}
	if runStatus != "" {
		parts = append(parts, runStatus)
	}
	parts = append(parts, sep, editorView, footer)
	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if m.inlineTranscript {
		// Keep a constant logical row count. Growing a normal-screen Bubble Tea
		// frame at the terminal bottom scrolls old chrome into native history;
		// only tea.Println transcript commits are allowed to move scrollback.
		frame = m.fitManagedFrame(frame, frameWidth, m.managedFrameHeight())
	} else {
		frame = m.fitManagedFrame(frame, frameWidth, m.height)
		if !m.permPending && !m.userInputPending {
			frame = overlayTranscriptSelectionContextMenu(frame, m.transcriptSelectionMenu)
		}
	}
	if m.loginModalVisible() && !m.permPending && !m.userInputPending {
		frame = m.overlayLoginModal(frame)
	} else if m.modelModalVisible() && !m.permPending && !m.userInputPending {
		frame = m.overlayModelModal(frame)
	} else if m.thinkingModalVisible() && !m.permPending && !m.userInputPending {
		frame = m.overlayThinkingModal(frame)
	} else if m.keybindingsModalVisible() && !m.permPending && !m.userInputPending {
		frame = m.overlayKeybindingsModal(frame)
	} else if m.settingsModalVisible() && !m.permPending && !m.userInputPending {
		frame = m.overlaySettingsModal(frame)
	} else if m.helpModalVisible() && !m.permPending && !m.userInputPending {
		frame = m.overlayHelpModal(frame)
	}
	return clipboardSequence + frame
}

func (m *Model) renderTrustPrompt() string {
	width := max(20, m.width-8)
	choice := func(index int, label string) string {
		prefix := "  "
		style := styleHeaderDim
		if m.trustChoice == index {
			prefix = "› "
			style = styleHeader
		}
		return prefix + style.Render(label)
	}
	status := "Choose with arrows/Tab · Enter confirms · Esc continues untrusted"
	if m.trustSaving {
		status = m.spinner.View() + " saving trust decision…"
	}
	body := []string{
		styleBrand.Render(" snow ") + styleHeader.Render("Project trust"),
		"",
		styleHeaderDim.Render("Project:"),
		lipgloss.NewStyle().Width(width).Render(m.trustPath),
		"",
		"Snow always reads AGENTS.md as project context. Trust additionally permits",
		"project config, plugins, MCP declarations, and project skills to load.",
		styleError.Render("Trust is not a sandbox; loaded code runs with your OS privileges."),
		"",
		choice(0, "Continue untrusted"),
		choice(1, "Trust project"),
		"",
		styleFooter.Render(status),
		styleFooter.Render("Ctrl+C/Ctrl+D exits without recording a decision."),
	}
	if m.trustError != "" {
		body = append(body, styleError.Render("trust: "+m.trustError))
	}
	return fitFrame(lipgloss.NewStyle().Padding(1, 3).Render(strings.Join(body, "\n")), m.width, m.height)
}

const maxManagedFrameCacheBytes = 256 << 10

func (m *Model) fitManagedFrame(frame string, width, height int) string {
	if m.managedFrameCacheValid && m.managedFrameCacheWidth == width &&
		m.managedFrameCacheHeight == height && m.managedFrameCacheInput == frame {
		return m.managedFrameCacheOutput
	}
	if len(frame) > maxManagedFrameCacheBytes {
		m.clearManagedFrameCache()
		return fitFrame(frame, width, height)
	}
	output := fitFrame(frame, width, height)
	// Retain at most one bounded input/output pair. Extremely large PTYs still
	// render correctly but cannot turn the stable-frame optimization into an
	// unbounded steady-state heap cost.
	if len(output) > maxManagedFrameCacheBytes-len(frame) {
		m.clearManagedFrameCache()
		return output
	}
	m.managedFrameCacheInput = frame
	m.managedFrameCacheOutput = output
	m.managedFrameCacheWidth = width
	m.managedFrameCacheHeight = height
	m.managedFrameCacheValid = true
	return output
}

func (m *Model) clearManagedFrameCache() {
	m.managedFrameCacheInput = ""
	m.managedFrameCacheOutput = ""
	m.managedFrameCacheWidth = 0
	m.managedFrameCacheHeight = 0
	m.managedFrameCacheValid = false
}

func fitFrame(frame string, width, height int) string {
	width = max(1, width)
	height = max(1, height)
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		Render(frame)
}

func fitFrameBottom(frame string, width, height int) string {
	width = max(1, width)
	height = max(1, height)
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		AlignVertical(lipgloss.Bottom).
		Render(frame)
}

type headerRender struct {
	view                       string
	modelStart, modelEnd       int
	thinkingStart, thinkingEnd int
	modeStart, modeEnd         int
}

// renderHeader is the sticky top bar: brand · provider/model · thinking · runtime · cwd · status.
func (m *Model) renderHeader(status string) string {
	return m.renderHeaderLayout(status).view
}

// renderHeaderLayout owns both the rendered selector and its mouse hit bounds,
// so truncation can never leave an approximate or stale clickable region.
func (m *Model) renderHeaderLayout(status string) headerRender {
	w := m.managedFrameWidth()
	fullBrand := styleBrand.Render(" ❄ snow ")
	brandWidth := min(w, lipgloss.Width(fullBrand))
	brand := xansi.Truncate(fullBrand, brandWidth, "")
	brandWidth = lipgloss.Width(brand)
	midText := "booting"
	selectorText := ""
	thinkingText := ""
	modeText := ""
	selectorEnabled := false
	if m.lastErr != nil {
		midText = "startup failed"
	} else if m.app != nil && m.app.Agent != nil {
		model := m.app.Agent.Model()
		goalText := ""
		if m.goal != nil {
			goalText = fmt.Sprintf("  ·  goal:%s %s", m.goal.Status, formatGoalTokenUsage(m.goal))
		}
		selectorEnabled = m.app.Cfg.TUI.Mouse
		selectorText = m.app.ProviderID + "/" + model.ID
		if selectorEnabled {
			selectorText += " ▾"
		}
		midText = selectorText
		modeText = "mode:" + m.collaborationModeLabel()
		if w >= 80 {
			thinkingText = "thinking:" + string(m.app.Agent.Thinking())
			midText += "  ·  " + thinkingText +
				"  ·  " + modeText + goalText +
				"  ·  " + shortPath(m.app.CWD(), max(12, w/3))
		} else if w >= 48 {
			midText += "  ·  " + modeText + "  ·  " + shortPath(m.app.CWD(), max(10, w/4))
		}
	}

	remaining := max(0, w-brandWidth)
	fullRight := styleHeaderDim.Render(status + " ")
	// Preserve four cells for the selector before adding the right-aligned
	// status. Extremely narrow frames therefore omit status before model state.
	rightWidth := min(lipgloss.Width(fullRight), max(0, remaining-4))
	right := xansi.Truncate(fullRight, rightWidth, "")
	rightWidth = lipgloss.Width(right)
	midArea := max(0, remaining-rightWidth)
	maxMid := midArea
	if rightWidth > 0 && maxMid > 0 {
		maxMid-- // retain one separating cell before status
	}

	midStyle := styleHeaderDim
	if m.thinkingFlash {
		midStyle = styleBrand
	}
	mid := ""
	modelStart, modelEnd := 0, 0
	thinkingStart, thinkingEnd := 0, 0
	modeStart, modeEnd := 0, 0
	if selectorEnabled && selectorText != "" {
		visibleSelector := truncateDisplayText(selectorText, maxMid)
		metadata := strings.TrimPrefix(midText, selectorText)
		metadataWidth := max(0, maxMid-xansi.StringWidth(visibleSelector))
		visibleMetadata := truncateDisplayText(metadata, metadataWidth)
		styledSelector := styleCompletionSelected.Render(visibleSelector)
		if visibleSelector != "" {
			modelStart = min(w, brandWidth)
			modelEnd = min(w, modelStart+xansi.StringWidth(styledSelector))
			if modelEnd <= modelStart {
				modelStart, modelEnd = 0, 0
			}
		}
		styledMetadata, thinkingBounds, modeBounds := renderHeaderMetadataControls(
			visibleMetadata, modelEnd, thinkingText, modeText, m.thinkingFlash,
		)
		thinkingStart, thinkingEnd = thinkingBounds[0], thinkingBounds[1]
		modeStart, modeEnd = modeBounds[0], modeBounds[1]
		mid = styledSelector + styledMetadata
	} else {
		mid = midStyle.Render(truncateDisplayText(midText, maxMid))
	}
	pad := max(0, midArea-lipgloss.Width(mid))
	return headerRender{
		view:          brand + mid + strings.Repeat(" ", pad) + right,
		modelStart:    modelStart,
		modelEnd:      modelEnd,
		thinkingStart: thinkingStart,
		thinkingEnd:   thinkingEnd,
		modeStart:     modeStart,
		modeEnd:       modeEnd,
	}
}

func renderHeaderMetadataControls(metadata string, base int, thinkingText, modeText string, thinkingFlash bool) (string, [2]int, [2]int) {
	type control struct {
		name       string
		start, end int
	}
	controls := make([]control, 0, 2)
	for name, text := range map[string]string{"thinking": thinkingText, "mode": modeText} {
		if text == "" {
			continue
		}
		if start := strings.Index(metadata, text); start >= 0 {
			controls = append(controls, control{name: name, start: start, end: start + len(text)})
		}
	}
	if len(controls) == 0 {
		style := styleHeaderDim
		if thinkingFlash {
			style = styleBrand
		}
		return style.Render(metadata), [2]int{}, [2]int{}
	}
	sort.Slice(controls, func(i, j int) bool { return controls[i].start < controls[j].start })
	baseStyle := styleHeaderDim
	if thinkingFlash {
		baseStyle = styleBrand
	}
	var view strings.Builder
	cursor := 0
	thinkingBounds, modeBounds := [2]int{}, [2]int{}
	for _, item := range controls {
		view.WriteString(baseStyle.Render(metadata[cursor:item.start]))
		style := styleCompletionSelected
		if item.name == "thinking" && thinkingFlash {
			style = styleBrand
		}
		view.WriteString(style.Render(metadata[item.start:item.end]))
		bounds := [2]int{
			base + xansi.StringWidth(metadata[:item.start]),
			base + xansi.StringWidth(metadata[:item.end]),
		}
		if item.name == "thinking" {
			thinkingBounds = bounds
		} else {
			modeBounds = bounds
		}
		cursor = item.end
	}
	view.WriteString(baseStyle.Render(metadata[cursor:]))
	return view.String(), thinkingBounds, modeBounds
}

// renderEditor draws a composer that grows from three to six rows.
func (m *Model) renderEditor() string {
	var input string
	if m.loginModalVisible() {
		input = stylePrompt.Render("› ")
	} else {
		editor := m.editor
		selectAll := m.composerSelectAll && editor.Value() != ""
		if selectAll {
			// Reverse video mirrors native terminal selection without imposing a
			// permanent background color on Snow's transparent composer.
			editor.FocusedStyle.Text = editor.FocusedStyle.Text.Reverse(true)
			editor.BlurredStyle.Text = editor.BlurredStyle.Text.Reverse(true)
			editor.Cursor.Blur()
		}
		editorView := editor.View()
		if !selectAll {
			for i := range m.promptImages {
				token := imageAttachmentToken(i)
				editorView = strings.ReplaceAll(editorView, token, stylePrompt.Render(token))
			}
			for _, attachment := range m.pastedTexts {
				editorView = strings.ReplaceAll(editorView, attachment.token, stylePrompt.Render(attachment.token))
			}
		}
		input = stylePrompt.Render("› ") + editorView
	}
	height := max(minComposerHeight, m.editor.Height())
	width := m.managedFrameWidth()
	return styleComposer.
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		PaddingLeft(1).
		Render(input)
}

func (m *Model) permissionStatus() string {
	if m.app == nil || m.app.Perm == nil {
		return "permission: unavailable"
	}
	return "permission: " + string(m.app.Perm.Mode())
}

// renderFooter is the sticky bottom status bar.
func (m *Model) permissionStatusStyle() lipgloss.Style {
	if m.app == nil || m.app.Perm == nil {
		return styleFooter
	}
	switch m.app.Perm.Mode() {
	case permission.ModeAllow:
		return lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	case permission.ModeDeny:
		return lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	case permission.ModeAsk:
		return lipgloss.NewStyle().Foreground(colorOk).Bold(true)
	default:
		return styleFooter
	}
}

func (m *Model) renderFooter() string {
	permissionText := m.permissionStatus()
	// Reserve a fixed field so the permission label does not jump when
	// ask/allow/deny changes (the labels have different lengths).
	permissionWidth := lipgloss.Width("permission: unavailable")
	permissionField := lipgloss.PlaceHorizontal(permissionWidth, lipgloss.Left, permissionText)
	available := max(8, m.managedFrameWidth()-2)
	mode := string(protocol.ModeDefault)
	if m.app != nil && m.app.Agent != nil {
		mode = m.collaborationModeLabel()
	}
	goalText := ""
	if m.goal != nil {
		goalText = fmt.Sprintf(" · goal:%s %s", m.goal.Status, formatGoalTokenUsage(m.goal))
	}
	contextUsage := m.renderContextUsage()
	cachedInput := m.renderCachedInputShare()
	runStatsText := fmt.Sprintf("turns:%d · steps:%d", m.turnCount, m.stepCount)
	runtimeText := "mode:" + mode
	if m.inlineTranscript && m.app != nil && m.app.Agent != nil && available >= 72 {
		model := m.app.Agent.Model()
		runtimeText = model.Provider + "/" + model.ID + " · " + runtimeText + " · thinking:" + string(m.app.Agent.Thinking())
	}
	rightPrefix := "· " + runtimeText + goalText + " · " + runStatsText + " · "
	if cachedInput != "" {
		rightPrefix += cachedInput + " · "
	}
	right := rightPrefix + contextUsage
	// Add width-aware help only when it fits beside the persistent context
	// indicator. Narrow terminals keep the footer quiet and leave shortcuts in
	// /help rather than forcing the usage counter off-screen.
	m.help.Width = available
	helpText := m.help.ShortHelpView(m.keys.ShortHelp())
	maxRight := available - lipgloss.Width(" "+permissionField)
	if lipgloss.Width(helpText)+lipgloss.Width(" · ")+lipgloss.Width(right) <= maxRight {
		rightPrefix = helpText + " · " + rightPrefix
		right = rightPrefix + contextUsage
	}
	// Keep the whole footer inside the terminal: shrink the usage side before
	// the fixed permission field when the terminal is narrow.
	if maxRight < 4 {
		maxRight = 4
	}
	if lipgloss.Width(right) > maxRight {
		compactRightPrefix := ""
		if m.inlineTranscript && m.app != nil && m.app.Agent != nil && maxRight >= 18 {
			model := m.app.Agent.Model()
			runtimeText = model.ID + " · " + mode + "/" + string(m.app.Agent.Thinking())
			compactRightPrefix = "· " + runtimeText + " · "
			rightPrefix = compactRightPrefix + runStatsText + " · "
			if cachedInput != "" {
				rightPrefix += cachedInput + " · "
			}
			right = rightPrefix + contextUsage
		}
		if lipgloss.Width(right) > maxRight && runStatsText != "" {
			runStatsText = ""
			if compactRightPrefix != "" {
				rightPrefix = compactRightPrefix
			} else {
				rightPrefix = "· " + runtimeText + goalText + " · "
			}
			if cachedInput != "" {
				rightPrefix += cachedInput + " · "
			}
			right = rightPrefix + contextUsage
		}
		if lipgloss.Width(right) > maxRight && cachedInput != "" {
			cachedInput = ""
			if compactRightPrefix != "" {
				rightPrefix = compactRightPrefix
			} else {
				rightPrefix = "· " + runtimeText + goalText + " · "
			}
			right = rightPrefix + contextUsage
		}
		if lipgloss.Width(right) > maxRight {
			rightPrefix = "· "
			contextUsage = truncateRunes(contextUsage, maxRight-2)
			right = rightPrefix + contextUsage
		}
	}
	line := m.permissionStatusStyle().Render(permissionField)
	pad := available - lipgloss.Width(" "+permissionField) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	runtimeStyle := styleFooter
	if m.thinkingFlash {
		runtimeStyle = styleBrand
	}
	styledRight := runtimeStyle.Render(rightPrefix) + m.contextUsageStyle().Render(contextUsage)
	line += strings.Repeat(" ", pad) + styledRight
	return styleFooter.Render(" ") + line
}

func contextUsageBand(current, total int) string {
	if total <= 0 || current < 0 {
		return "unknown"
	}
	ratio := float64(current) / float64(total)
	switch {
	case ratio >= 0.9:
		return "critical"
	case ratio >= 0.7:
		return "warning"
	case ratio >= 0.5:
		return "notice"
	default:
		return "healthy"
	}
}

func (m *Model) contextUsageStyle() lipgloss.Style {
	total := 0
	if m.app != nil {
		total = m.app.Model.ContextWindow
	}
	switch contextUsageBand(m.contextTokens, total) {
	case "healthy":
		return lipgloss.NewStyle().Foreground(colorOk).Bold(true)
	case "notice":
		return lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	case "warning":
		return lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	case "critical":
		return lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	default:
		return styleFooter
	}
}

// renderCachedInputShare reports the share of the latest request's input
// tokens that the provider says were read from its prompt cache. It is token
// coverage, not a request-level cache-hit frequency.
func (m *Model) renderCachedInputShare() string {
	usage := m.lastRequestUsage
	if usage == nil || (!usage.CacheReadKnown && usage.CacheRead <= 0) || usage.Input <= 0 {
		return ""
	}
	// Invalid provider accounting must not be normalized into a plausible
	// percentage. Omit the metric instead of masking it as 0% or 100%.
	if usage.CacheRead < 0 || usage.CacheRead > usage.Input {
		return ""
	}
	percent := 100 * float64(usage.CacheRead) / float64(usage.Input)
	return fmt.Sprintf("cached:%.1f%%", percent)
}

func (m *Model) renderContextUsage() string {
	current := formatTokenCount(int64(m.contextTokens))
	if m.contextEstimated && m.contextTokens > 0 {
		current = "~" + current
	}
	total := "?"
	if m.app != nil && m.app.Model.ContextWindow > 0 {
		total = formatTokenCount(int64(m.app.Model.ContextWindow))
	}
	return fmt.Sprintf("context: %s/%s", current, total)
}

func (m *Model) renderCompactionProgress() string {
	status := strings.TrimSpace(m.compactStatus)
	if status == "" {
		status = "compacting context"
	}
	return m.spinner.View() + " " + styleHeaderDim.Render(status+"…")
}

func formatGoalTokenUsage(goal *protocol.ThreadGoal) string {
	if goal == nil {
		return "0 tks"
	}
	usage := formatTokenCount(goal.TokensUsed)
	if goal.TokenBudget != nil {
		usage += "/" + formatTokenCount(*goal.TokenBudget)
	}
	usage += " tks"
	if len(goal.EstimatedCosts) == 0 {
		return usage
	}
	costs := append([]protocol.Cost(nil), goal.EstimatedCosts...)
	sort.Slice(costs, func(i, j int) bool { return costs[i].Currency < costs[j].Currency })
	formatted := make([]string, 0, len(costs))
	for _, cost := range costs {
		formatted = append(formatted, formatEstimatedCost(cost))
	}
	return usage + " · est. " + strings.Join(formatted, " + ")
}

func formatEstimatedCost(cost protocol.Cost) string {
	currency := strings.ToUpper(strings.TrimSpace(cost.Currency))
	prefix := currency + " "
	if currency == "USD" {
		prefix = "$"
	}
	if cost.Total > 0 && cost.Total < 0.0001 {
		return "<" + prefix + "0.0001"
	}
	precision := 2
	if cost.Total < 1 {
		precision = 4
	}
	value := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.*f", precision, cost.Total), "0"), ".")
	if !strings.Contains(value, ".") {
		value += ".00"
	}
	return prefix + value
}

func formatTokenCount(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		value := strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0")
		return value + "k"
	}
	if n < 1_000_000_000 {
		value := strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0")
		return value + "m"
	}
	value := strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000_000), ".0")
	return value + "b"
}

// shortPath collapses the home prefix to ~ and truncates the middle of long paths.
func shortPath(p string, maxLen int) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		p = "~" + p[len(home):]
	}
	return truncateRunes(p, maxLen)
}

// truncateRunes trims s to at most n display runes, adding an ellipsis when cut.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
