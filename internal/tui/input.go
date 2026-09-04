package tui

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// handleComposerSelectionKey implements select-all for the ordinary composer.
// Bubbles' textarea does not expose a selection model, so Snow tracks the
// whole-draft selection explicitly and applies normal replacement semantics to
// the next text edit. Modal textareas are handled before this path.
func (m *Model) handleComposerSelectionKey(msg tea.KeyMsg) (handled bool, cmd tea.Cmd) {
	if msg.Type == tea.KeyCtrlA {
		// Selection belongs to one surface at a time. In app-mouse mode an old
		// transcript drag selection can otherwise remain highlighted beside the
		// newly selected draft, making Select All appear to cover both surfaces.
		m.clearTranscriptSelection()
		m.catchUpTranscriptAfterSelection()
		m.composerSelectAll = m.editor.Value() != ""
		return true, nil
	}
	if !m.composerSelectAll {
		return false, nil
	}
	m.composerSelectAll = false
	deleteSelection := key.Matches(msg, m.editor.KeyMap.DeleteCharacterBackward) ||
		key.Matches(msg, m.editor.KeyMap.DeleteCharacterForward) ||
		key.Matches(msg, m.editor.KeyMap.DeleteAfterCursor) ||
		key.Matches(msg, m.editor.KeyMap.DeleteBeforeCursor) ||
		key.Matches(msg, m.editor.KeyMap.DeleteWordBackward) ||
		key.Matches(msg, m.editor.KeyMap.DeleteWordForward)
	replaceSelection := msg.Type == tea.KeyRunes || keyMatches(msg, m.keys.Paste) ||
		(keyMatches(msg, m.keys.Newline) && !(m.busy && keyMatches(msg, m.keys.FollowUp)))
	if !deleteSelection && !replaceSelection {
		return false, nil
	}

	m.editor.Reset()
	m.promptImages = nil
	m.pastedTexts = nil
	m.resetInputHistoryNavigation()
	if deleteSelection {
		return true, m.refreshInputCompletions()
	}
	return false, nil
}

func (m *Model) composerCoveredByModal() bool {
	return m.loginModalVisible() || m.pickModel || m.pickThinking ||
		m.pickSettings || m.pickKeybindings || m.pickHelp || m.pickFork || m.pickSession || m.pickTree ||
		m.pickInfo || m.pickPermissionMode || m.permPending || m.userInputPending ||
		m.confirmGoalReplace || m.planPrompt || m.processFleetOpen || m.subagentFleetOpen ||
		m.sessionOpLoading || m.restartPromptVisible() || m.updateOfferVisible() || m.updateInstallProgressVisible()
}

func (m *Model) updateComposerEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Forward to the editor, then refresh the palette from the new text. Keep
	// the returned command: textarea uses it to read the clipboard for paste.
	if keyMatches(msg, m.keys.Paste) {
		msg = tea.KeyMsg{Type: tea.KeyCtrlV}
	}
	deleteBackward := key.Matches(msg, m.editor.KeyMap.DeleteCharacterBackward)
	deleteForward := key.Matches(msg, m.editor.KeyMap.DeleteCharacterForward)
	if (deleteBackward || deleteForward) && m.removePastedTextAtCursor(deleteBackward) {
		m.resetInputHistoryNavigation()
		return m, m.refreshInputCompletions()
	}
	if m.collapseComposerPaste(msg) {
		m.resetInputHistoryNavigation()
		return m, m.refreshInputCompletions()
	}
	textMayChange := composerEditorKeyMayChange(msg, m.editor.KeyMap)
	previous := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if m.editor.Value() != previous {
		m.resetInputHistoryNavigation()
		m.prunePastedTextAttachments(m.editor.Value())
	}
	if msg.Type == tea.KeyEsc {
		m.compVisible = false
		return m, nil
	}
	var mentionCmd tea.Cmd
	if textMayChange {
		mentionCmd = m.refreshInputCompletionsFor(m.editor.Value())
	}
	if msg.Type == tea.KeyCtrlV {
		m.editor.Err = nil
		m.imagePasteGeneration++
		generation := m.imagePasteGeneration
		if m.pasteCmdOverride != nil {
			cmd = m.pasteCmdOverride
			return m, tea.Batch(routeTextareaCmdGeneration(textareaTargetComposer, "", "", generation, cmd), mentionCmd)
		}
		imageCmd := m.imagePasteCmdOverride
		if imageCmd == nil {
			imageCmd = func() tea.Msg {
				block, err := readClipboardImageFunc()
				return clipboardImageMsg{generation: generation, block: block, err: err}
			}
		} else {
			override := imageCmd
			imageCmd = func() tea.Msg {
				msg := override()
				if result, ok := msg.(clipboardImageMsg); ok {
					result.generation = generation
					return result
				}
				return msg
			}
		}
		return m, tea.Batch(imageCmd, mentionCmd)
	}
	return m, mentionCmd
}

func composerEditorKeyMayChange(msg tea.KeyMsg, keyMap textarea.KeyMap) bool {
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		return true
	}
	return key.Matches(msg, keyMap.DeleteAfterCursor) ||
		key.Matches(msg, keyMap.DeleteBeforeCursor) ||
		key.Matches(msg, keyMap.DeleteCharacterBackward) ||
		key.Matches(msg, keyMap.DeleteCharacterForward) ||
		key.Matches(msg, keyMap.DeleteWordBackward) ||
		key.Matches(msg, keyMap.DeleteWordForward) ||
		key.Matches(msg, keyMap.InsertNewline) ||
		key.Matches(msg, keyMap.UppercaseWordForward) ||
		key.Matches(msg, keyMap.LowercaseWordForward) ||
		key.Matches(msg, keyMap.CapitalizeWordForward) ||
		key.Matches(msg, keyMap.TransposeCharacterBackward)
}

// pickCompletion selects a palette entry: commands needing args are inserted
// into the editor for completion; argument-free commands run immediately.
func (m *Model) insertCompletion(name string) (tea.Model, tea.Cmd) {
	m.resetInputHistoryNavigation()
	suffix := ""
	if spec, ok := commandByExact(name); ok && spec.needsArgs() {
		suffix = " "
	}
	m.editor.SetValue(name + suffix)
	m.editor.CursorEnd()
	m.compVisible = false
	m.refreshPalette()
	return m, nil
}

func (m *Model) pickCompletion(name string) (tea.Model, tea.Cmd) {
	m.compVisible = false
	if spec, ok := commandByExact(name); ok && spec.needsArgs() {
		m.resetInputHistoryNavigation()
		m.editor.SetValue(name + " ")
		m.editor.CursorEnd()
		m.refreshPalette()
		return m, nil
	}
	return m.runCommand(name)
}

// refreshPalette recomputes completion candidates from the editor's first
// token, opening or closing the palette accordingly.
func (m *Model) refreshPalette() {
	m.refreshPaletteFor(m.editor.Value())
}

func (m *Model) refreshPaletteFor(text string) {
	if isCommandPrefix(text) {
		// Keep the complete match set navigable. renderOverlays applies a
		// selection-following viewport, so truncating here would make commands
		// beyond the first visible page unreachable with the arrow keys.
		m.compMatches = completeCommand(text[1:])
		m.compVisible = true
		if m.compIndex >= len(m.compMatches) {
			m.compIndex = 0
		}
	} else {
		m.compVisible = false
		m.compMatches = nil
		m.compIndex = 0
	}
}

// refreshInputCompletions keeps slash commands, $skill references, and @ file
// references mutually exclusive while the editor changes.
func (m *Model) refreshInputCompletions() tea.Cmd {
	return m.refreshInputCompletionsFor(m.editor.Value())
}

func (m *Model) refreshInputCompletionsFor(text string) tea.Cmd {
	m.refreshPaletteFor(text)
	m.refreshSkillCompletionsFor(text)
	if m.skillVisible {
		m.mentionVisible = false
		return nil
	}
	return m.refreshMentionsFor(text)
}

// refreshMentions never walks the repository from Bubble Tea's Update loop.
// The first @ query schedules a bounded discovery command; subsequent edits
// only match the cached list. Generation checks in mentionFilesMsg prevent a
// slow result from reopening a picker for an obsolete editor state.
func (m *Model) refreshMentions() tea.Cmd {
	return m.refreshMentionsFor(m.editor.Value())
}

func (m *Model) refreshMentionsFor(text string) tea.Cmd {
	m.mentionVisible = false
	m.mentionMatches = nil
	m.mentionIndex = 0
	if m.app == nil {
		return nil
	}
	query, _, ok := mentionQuery(text)
	if !ok {
		return nil
	}
	cwd := m.app.CWD()
	if !m.mentionFilesLoaded || m.mentionFilesCWD != cwd {
		if m.mentionLoading {
			return nil
		}
		m.mentionLoading = true
		m.mentionGeneration++
		generation := m.mentionGeneration
		return func() tea.Msg {
			return mentionFilesMsg{
				cwd: cwd, generation: generation,
				files: discoverMentionFiles(cwd),
			}
		}
	}
	m.mentionMatches = matchMentionFiles(m.mentionFiles, query)
	m.mentionVisible = len(m.mentionMatches) > 0
	return nil
}

func (m *Model) insertMention(path string) (tea.Model, tea.Cmd) {
	m.resetInputHistoryNavigation()
	text := m.editor.Value()
	_, start, ok := mentionQuery(text)
	if !ok {
		return m, nil
	}
	m.editor.SetValue(replaceMentionToken(text, start, path))
	m.editor.CursorEnd()
	m.mentionVisible = false
	m.mentionMatches = nil
	m.refreshInputCompletions()
	return m, nil
}

func (m *Model) handleLoginProfileKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if !m.restorePreviousLoginStep() {
			m.cancelLoginFlow()
		}
		return m, nil
	case tea.KeyEnter:
		profileDraft := sanitizeTerminalLine(m.editor.Value())
		profileID := strings.TrimSpace(profileDraft)
		if profileID == "" {
			profileID = openaicompat.ProviderID
		}
		if err := config.ValidateProviderProfileID(profileID); err != nil {
			m.loginError = err.Error()
			return m, nil
		}
		if configured, exists := m.app.PersistedCfg.Providers[profileID]; exists && !config.IsOpenAICompatibleProfile(profileID, configured) {
			m.loginError = "provider name " + profileID + " is already used by another provider type"
			return m, nil
		}
		m.loginError = ""
		m.loginProfileMode = false
		m.rememberLoginStep(loginNavigationProfile, openaicompat.ProviderID, profileDraft)
		m.beginCompatibleEndpointCapture(profileID)
		return m, nil
	}
	previous := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if value := sanitizeTerminalLine(m.editor.Value()); value != m.editor.Value() {
		m.editor.SetValue(value)
		m.editor.CursorEnd()
	}
	if m.editor.Value() != previous {
		m.loginError = ""
	}
	if msg.Type == tea.KeyCtrlV {
		m.editor.Err = nil
		if m.pasteCmdOverride != nil {
			cmd = m.pasteCmdOverride
		}
		return m, routeTextareaCmdGeneration(textareaTargetLoginProfile, "", "", m.loginFieldGeneration, cmd)
	}
	return m, cmd
}

func (m *Model) handleLoginEndpointKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if !m.restorePreviousLoginStep() {
			m.cancelLoginFlow()
		}
		return m, nil
	case tea.KeyEnter:
		endpoint := strings.TrimSpace(sanitizeTerminalLine(m.editor.Value()))
		compatible, err := openaicompat.New(openaicompat.Config{BaseURL: endpoint})
		if err != nil || !compatible.Configured() {
			if err == nil {
				err = errors.New("endpoint is required")
			}
			m.loginError = "invalid endpoint: " + err.Error()
			return m, nil
		}
		m.loginError = ""
		provider := m.loginProvider
		m.loginEndpoint = endpoint
		m.loginEndpointMode = false
		m.editor.Reset()
		m.editor.Placeholder = "Type a message…"
		m.rememberLoginStep(loginNavigationEndpoint, provider, endpoint)
		m.beginKeyCapture(provider)
		return m, nil
	}
	previous := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if value := sanitizeTerminalLine(m.editor.Value()); value != m.editor.Value() {
		m.editor.SetValue(value)
		m.editor.CursorEnd()
	}
	if m.editor.Value() != previous {
		m.loginError = ""
	}
	if msg.Type == tea.KeyCtrlV {
		m.editor.Err = nil
		if m.pasteCmdOverride != nil {
			cmd = m.pasteCmdOverride
		}
		return m, routeTextareaCmdGeneration(textareaTargetLoginEndpoint, "", "", m.loginFieldGeneration, cmd)
	}
	return m, cmd
}

// handleLoginKey captures a masked API key.
func (m *Model) handleLoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if !m.restorePreviousLoginStep() {
			m.cancelLoginFlow()
		}
		return m, nil
	case tea.KeyEnter:
		secret := m.secretBuf.String()
		provider := m.loginProvider
		if m.loginEndpoint != "" {
			m.loginMode = false
			m.loginError = ""
			m.clearLoginNavigation()
			m.secretBuf.Reset()
			m.editor.Reset()
			return m.finishCompatibleLogin(secret)
		}
		if strings.TrimSpace(secret) == "" {
			if !m.providerAuthOptional(provider) {
				m.loginError = "API key is required"
				return m, nil
			}
			m.loginMode = false
			m.loginError = ""
			m.loginProvider = ""
			m.clearLoginNavigation()
			m.secretBuf.Reset()
			m.editor.Reset()
			if credential, ok := m.app.Auth.Get(provider); ok && credential.Valid() {
				m.pushLine(styleFooter.Render("kept stored API key for " + provider))
			} else if status, err := m.app.AuthStatus(m.ctx, provider); err == nil && status.Configured() {
				m.pushLine(styleFooter.Render(provider + ": no stored key; explicit or environment credential remains active"))
			} else {
				m.pushLine(styleFooter.Render(provider + ": using anonymous/keyless access"))
			}
			return m, nil
		}
		if _, err := m.app.Login(m.ctx, provider, auth.LoginRequest{Method: "api_key"}, fixedAuthInteraction{value: secret}); err != nil {
			m.loginError = err.Error()
			return m, nil
		}
		m.loginMode = false
		m.loginError = ""
		m.loginProvider = ""
		m.clearLoginNavigation()
		m.secretBuf.Reset()
		m.editor.Reset()
		m.pushLine(styleFooter.Render("stored API key for " + provider + " (0600)"))
		return m, nil
	case tea.KeyBackspace:
		runes := []rune(m.secretBuf.String())
		if len(runes) > 0 {
			m.secretBuf.Reset()
			m.secretBuf.WriteString(string(runes[:len(runes)-1]))
			m.loginError = ""
		}
		return m, nil
	case tea.KeyCtrlC:
		m.cancelLoginFlow()
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.secretBuf.WriteString(string(msg.Runes))
		m.loginError = ""
	} else if msg.Type == tea.KeySpace {
		m.secretBuf.WriteString(" ")
		m.loginError = ""
	}
	return m, nil
}

func (m *Model) finishCompatibleLogin(secret string) (tea.Model, tea.Cmd) {
	profileID := m.loginProvider
	endpoint := strings.TrimSpace(m.loginEndpoint)
	m.loginEndpoint = ""
	m.loginProvider = ""
	if endpoint == "" {
		m.pushLine(styleError.Render("login: openai-compatible endpoint is required"))
		return m, nil
	}

	oldPersisted := m.app.PersistedCfg
	authStore := m.app.AuthService.Store()
	credentialChanged := strings.TrimSpace(secret) != ""
	var previousProviderConfig config.ProviderConfig
	var hadPreviousProvider bool
	var writtenProviderConfig config.ProviderConfig
	candidate, err := m.persistConfig(func(latest *config.Config) error {
		if latest.Providers == nil {
			latest.Providers = map[string]config.ProviderConfig{}
		}
		previousProviderConfig, hadPreviousProvider = latest.Providers[profileID]
		writtenProviderConfig = previousProviderConfig
		writtenProviderConfig.BaseURL = endpoint
		if profileID != openaicompat.ProviderID {
			writtenProviderConfig.Type = config.ProviderTypeOpenAICompatible
		}
		latest.Providers[profileID] = writtenProviderConfig
		return nil
	})
	if err != nil {
		m.pushLine(styleError.Render("login: persist endpoint: " + err.Error()))
		return m, nil
	}

	m.app.PersistedCfg = candidate
	oldRuntimeProviderConfig, hadOldRuntimeProvider := m.app.Cfg.Providers[profileID]
	if m.app.Cfg.Providers == nil {
		m.app.Cfg.Providers = map[string]config.ProviderConfig{}
	}
	m.app.Cfg.Providers[profileID] = candidate.Providers[profileID]
	if err := m.app.ConfigureOpenAICompatibleProfile(profileID, endpoint); err != nil {
		rolledBack := oldPersisted
		updated, rollbackErr := m.persistConfig(func(latest *config.Config) error {
			current, exists := latest.Providers[profileID]
			if !exists || current != writtenProviderConfig {
				return nil // a concurrent writer now owns this profile
			}
			if hadPreviousProvider {
				latest.Providers[profileID] = previousProviderConfig
			} else {
				delete(latest.Providers, profileID)
			}
			return nil
		})
		if rollbackErr == nil {
			rolledBack = updated
		}
		m.app.PersistedCfg = rolledBack
		if hadOldRuntimeProvider {
			m.app.Cfg.Providers[profileID] = oldRuntimeProviderConfig
		} else {
			delete(m.app.Cfg.Providers, profileID)
		}
		m.pushLine(styleError.Render("login: " + errors.Join(err, rollbackErr).Error()))
		return m, nil
	}

	if credentialChanged {
		if err := authStore.Put(profileID, auth.Credential{Type: auth.CredentialAPIKey, Key: secret}); err != nil {
			m.pushLine(styleError.Render("login: endpoint saved but credential persistence failed: " + err.Error()))
			return m, nil
		}
	}

	m.compatibleLoginGeneration++
	generation := m.compatibleLoginGeneration
	m.compatibleLoginPending = true
	m.compatibleLoginProvider = profileID
	app := m.app
	ctx := m.ctx
	return m, func() tea.Msg {
		return compatibleLoginDoneMsg{generation: generation, provider: profileID, err: app.RefreshProviderModels(ctx, profileID)}
	}
}

func (m *Model) expandedPrompt(text string) string {
	if m.app == nil {
		return text
	}
	return expandMentionPrompt(text, m.app.CWD(), m.mentionFiles)
}

func (m *Model) submitQueuedInput(text, fullText string, kind protocol.QueuedInputKind) tea.Cmd {
	expanded := m.expandedPrompt(fullText)
	epoch := m.queueEpoch
	m.queueAttempts = append(m.queueAttempts, queuedTUIAttempt{kind: kind, text: text, fullText: fullText, expanded: expanded, epoch: epoch})
	return func() tea.Msg {
		item, err := m.app.Agent.QueueInput(kind, expanded)
		if errors.Is(err, agent.ErrNotRunning) {
			return queueSubmitMsg{kind: kind, text: text, fullText: fullText, expanded: expanded, epoch: epoch, fallback: true}
		}
		return queueSubmitMsg{kind: kind, text: text, fullText: fullText, expanded: expanded, itemID: item.ID, epoch: epoch, accepted: err == nil, err: err}
	}
}

func (m *Model) hasQueueAttempt(item protocol.QueuedInput) bool {
	for _, attempt := range m.queueAttempts {
		if attempt.kind == item.Kind && attempt.expanded == item.Text {
			return true
		}
	}
	return false
}

func (m *Model) removeQueueAttempt(msg queueSubmitMsg) {
	for i, attempt := range m.queueAttempts {
		if attempt.epoch != msg.epoch || attempt.kind != msg.kind || attempt.text != msg.text || attempt.expanded != msg.expanded {
			continue
		}
		m.queueAttempts = append(m.queueAttempts[:i], m.queueAttempts[i+1:]...)
		return
	}
}

func (m *Model) waitForQueueSettlement() tea.Cmd {
	if m.queueSettleWaiting || m.app == nil || m.app.Agent == nil {
		return nil
	}
	m.queueSettleWaiting = true
	epoch := m.queueEpoch
	agent := m.app.Agent
	ctx := m.ctx
	return func() tea.Msg {
		return queueSettledMsg{epoch: epoch, err: agent.WaitIdle(ctx)}
	}
}

func queueMessageFullText(msg queueSubmitMsg) string {
	if msg.fullText != "" {
		return msg.fullText
	}
	return msg.text
}

func (m *Model) startQueueFallback() tea.Cmd {
	if len(m.queueFallbacks) == 0 || m.app == nil {
		return nil
	}
	msg := m.queueFallbacks[0]
	m.queueFallbacks = m.queueFallbacks[1:]
	fullText := queueMessageFullText(msg)
	m.rememberInputHistory(fullText)
	// The fallback is semantically the steer that narrowly missed admission;
	// defer any already queued collaboration-mode transition until this prompt's
	// own turn_done boundary.
	m.modeSwitchReady = false
	m.beginOptimisticRun()
	m.planPrompt = false
	m.pushLine(styleUser.Render("› " + msg.text))
	var pastedTexts []pastedTextAttachment
	if m.editor.Value() == msg.text {
		m.editor.Reset()
		pastedTexts = m.takePastedTextAttachments()
	}
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	generation := m.runGeneration
	promptCmd := func() tea.Msg {
		_, beforeID, _ := m.app.Agent.ActiveTurn()
		err := m.app.Agent.Prompt(ctx, msg.expanded)
		_, turnID, running := m.app.Agent.ActiveTurn()
		return promptDoneMsg{
			generation: generation, turnID: turnID,
			admitted: running || (turnID != "" && turnID != beforeID),
			text:     msg.text, historyText: fullText, pastedTexts: pastedTexts, err: err,
		}
	}
	return promptCmd
}

func (m *Model) beginOptimisticRun() uint64 {
	m.runGeneration++
	m.activeTurnID = ""
	m.abortNoticePending = false
	m.turnUsageSeen = false
	m.busy = true
	m.toolRunning = false
	m.activeToolCallID = ""
	m.activeToolStartMessage = ""
	m.clearActiveToolText()
	m.lastErrorText = ""
	m.runStartedAt = m.currentTime()
	return m.runGeneration
}

func clonePromptImages(images []protocol.ContentBlock) []protocol.ContentBlock {
	cloned := make([]protocol.ContentBlock, len(images))
	for i, image := range images {
		cloned[i] = image
		cloned[i].Data = slices.Clone(image.Data)
	}
	return cloned
}

func (m *Model) takePromptImages() []protocol.ContentBlock {
	images := clonePromptImages(m.promptImages)
	m.promptImages = nil
	return images
}

func (m *Model) startPrompt(text string) tea.Cmd {
	return m.startPromptWithDisplay(text, text)
}

// startPromptWithDisplay submits prompt to the agent while retaining displayText
// for input history and pre-admission error recovery. Slash commands use this to
// keep their internal task prompt out of the composer and live transcript.
func (m *Model) startPromptWithDisplay(promptText, displayText string) tea.Cmd {
	generation := m.beginOptimisticRun()
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	pastedTexts := m.takePastedTextAttachments()
	imageCount := len(m.promptImages)
	historyText := stripImageAttachmentTokens(expandPastedTextAttachments(displayText, pastedTexts), imageCount)
	m.rememberInputHistory(historyText)
	promptText = stripImageAttachmentTokens(expandPastedTextAttachments(promptText, pastedTexts), imageCount)
	prompt := m.expandedPrompt(promptText)
	images := m.takePromptImages()
	return func() tea.Msg {
		err := m.app.Agent.PromptContent(ctx, prompt, images)
		_, turnID, _ := m.app.Agent.ActiveTurn()
		admitted := err == nil || !errors.Is(err, agent.ErrPromptRejected)
		return promptDoneMsg{
			generation: generation, turnID: turnID, admitted: admitted,
			text: displayText, historyText: historyText, attachments: images, pastedTexts: pastedTexts, err: err,
		}
	}
}

func (m *Model) startPromptWithMode(text string, mode protocol.CollaborationMode) tea.Cmd {
	generation := m.beginOptimisticRun()
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	pastedTexts := m.takePastedTextAttachments()
	historyText := stripImageAttachmentTokens(expandPastedTextAttachments(text, pastedTexts), len(m.promptImages))
	prompt := m.expandedPrompt(historyText)
	images := m.takePromptImages()
	return func() tea.Msg {
		err := m.app.Agent.PromptContentWithMode(ctx, prompt, images, mode)
		_, turnID, _ := m.app.Agent.ActiveTurn()
		admitted := err == nil || !errors.Is(err, agent.ErrPromptRejected)
		return promptDoneMsg{
			generation: generation, turnID: turnID, admitted: admitted,
			text: text, historyText: historyText, attachments: images, pastedTexts: pastedTexts, err: err,
		}
	}
}

func (m *Model) startCompact() tea.Cmd {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("compact: wait for the current turn to finish"))
		return nil
	}
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	m.beginOptimisticRun()
	m.compactGeneration++
	generation := m.compactGeneration
	m.compacting = true
	m.compactStatus = "compacting context"
	return func() tea.Msg {
		result, err := m.app.Agent.Compact(ctx)
		return compactDoneMsg{generation: generation, result: result, err: err}
	}
}

func (m *Model) abort() {
	if m.cancelRun != nil {
		m.cancelRun()
	}
	if m.app != nil && m.app.Agent != nil {
		m.app.Agent.Abort()
	}
}

func (m *Model) requestAbort() {
	// Invalidate optimistic command completions before joining the agent. This
	// also covers the goal worker's inter-turn delay, where no EvAborted event
	// exists to release the UI projection.
	m.runGeneration++
	m.compactGeneration++
	// Close queue admission and drain accepted input before cancelling the run.
	// An enqueue racing this key press is therefore either present in the
	// returned snapshot or rejected while its unchanged draft stays visible.
	m.queueEpoch++
	m.queueSettleWaiting = false
	queue := protocol.InputQueue{}
	if m.app != nil && m.app.Agent != nil {
		queue = m.app.Agent.ClearPendingInputs()
	}
	draft := m.editor.Value()
	fallbacks := slices.Clone(m.queueFallbacks)
	m.queueFallbacks = nil
	m.abort()
	m.pendingInputs = protocol.InputQueue{}
	m.restoreAbortedInputs(queue, fallbacks, draft)
	m.setRunIdle()
	m.abortNoticePending = true
	m.pushLine(styleError.Render("aborted"))
}

func (m *Model) restoreAbortedInputs(queue protocol.InputQueue, fallbacks []queueSubmitMsg, draft string) {
	parts := make([]string, 0, len(queue.Items)+len(fallbacks)+1)
	for _, item := range queue.Items {
		parts = append(parts, m.originalQueuedText(item))
	}
	for _, fallback := range fallbacks {
		parts = append(parts, queueMessageFullText(fallback))
	}
	if draft != "" {
		draft = m.expandedPastedText(draft)
		duplicateAcceptedDraft := len(parts) > 0 && parts[len(parts)-1] == draft
		if !duplicateAcceptedDraft {
			parts = append(parts, draft)
		}
	}
	m.queueAttempts = nil
	clear(m.queueOriginalText)
	clear(m.queueRendered)
	if len(parts) > 0 {
		m.setComposerValueCollapsingLargeText(strings.Join(parts, "\n\n"))
		m.refreshInputCompletions()
	}
}

func (m *Model) originalQueuedText(item protocol.QueuedInput) string {
	if original := m.queueOriginalText[item.ID]; original != "" {
		return original
	}
	for _, attempt := range m.queueAttempts {
		if attempt.kind == item.Kind && attempt.expanded == item.Text {
			if attempt.fullText != "" {
				return attempt.fullText
			}
			return attempt.text
		}
	}
	return item.Text
}
