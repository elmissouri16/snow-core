package tui

import (
	"context"
	"errors"
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

func (m *Model) updateComposerEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Forward to the editor, then refresh the palette from the new text. Keep
	// the returned command: textarea uses it to read the clipboard for paste.
	if keyMatches(msg, m.keys.Paste) {
		msg = tea.KeyMsg{Type: tea.KeyCtrlV}
	}
	textMayChange := composerEditorKeyMayChange(msg, m.editor.KeyMap)
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
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
		if m.pasteCmdOverride != nil {
			cmd = m.pasteCmdOverride
			return m, tea.Batch(routeTextareaCmd(textareaTargetComposer, "", "", cmd), mentionCmd)
		}
		m.imagePasteGeneration++
		generation := m.imagePasteGeneration
		imageCmd := m.imagePasteCmdOverride
		if imageCmd == nil {
			imageCmd = func() tea.Msg {
				block, err := readClipboardImageFunc()
				return clipboardImageMsg{generation: generation, block: block, err: err}
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
		m.compMatches = completeCommand(text[1:])
		if len(m.compMatches) > 10 {
			m.compMatches = m.compMatches[:10]
		}
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

// refreshInputCompletions keeps slash commands, leading $skill directives,
// and @ file references mutually exclusive while the editor changes.
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
		m.loginProfileMode = false
		m.loginProvider = ""
		m.editor.Reset()
		m.editor.Placeholder = "Type a message…"
		m.pushLine(styleFooter.Render("login cancelled"))
		return m, nil
	case tea.KeyEnter:
		profileID := strings.TrimSpace(m.editor.Value())
		if profileID == "" {
			profileID = openaicompat.ProviderID
		}
		if err := config.ValidateProviderProfileID(profileID); err != nil {
			m.pushLine(styleError.Render("login: " + err.Error()))
			return m, nil
		}
		if configured, exists := m.app.PersistedCfg.Providers[profileID]; exists && !config.IsOpenAICompatibleProfile(profileID, configured) {
			m.pushLine(styleError.Render("login: provider name " + profileID + " is already used by another provider type"))
			return m, nil
		}
		m.loginProfileMode = false
		m.beginCompatibleEndpointCapture(profileID)
		return m, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if msg.Type == tea.KeyCtrlV {
		m.editor.Err = nil
		if m.pasteCmdOverride != nil {
			cmd = m.pasteCmdOverride
		}
		return m, routeTextareaCmd(textareaTargetComposer, "", "", cmd)
	}
	return m, cmd
}

func (m *Model) handleLoginEndpointKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.loginEndpointMode = false
		m.loginEndpoint = ""
		m.loginProvider = ""
		m.editor.Reset()
		m.editor.Placeholder = "Type a message…"
		m.pushLine(styleFooter.Render("login cancelled"))
		return m, nil
	case tea.KeyEnter:
		endpoint := strings.TrimSpace(m.editor.Value())
		compatible, err := openaicompat.New(openaicompat.Config{BaseURL: endpoint})
		if err != nil || !compatible.Configured() {
			if err == nil {
				err = errors.New("endpoint is required")
			}
			m.pushLine(styleError.Render("login: invalid openai-compatible endpoint: " + err.Error()))
			return m, nil
		}
		m.loginEndpoint = endpoint
		m.loginEndpointMode = false
		m.editor.Reset()
		m.editor.Placeholder = "Type a message…"
		m.beginKeyCapture(m.loginProvider)
		return m, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if msg.Type == tea.KeyCtrlV {
		m.editor.Err = nil
		if m.pasteCmdOverride != nil {
			cmd = m.pasteCmdOverride
		}
		return m, routeTextareaCmd(textareaTargetComposer, "", "", cmd)
	}
	return m, cmd
}

// handleLoginKey captures a masked API key.
func (m *Model) handleLoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.loginMode = false
		m.loginEndpoint = ""
		m.loginProvider = ""
		m.secretBuf.Reset()
		m.editor.Reset()
		m.pushLine(styleFooter.Render("login cancelled"))
		return m, nil
	case tea.KeyEnter:
		secret := m.secretBuf.String()
		m.loginMode = false
		m.secretBuf.Reset()
		m.editor.Reset()
		if m.loginEndpoint != "" {
			return m.finishCompatibleLogin(secret)
		}
		if strings.TrimSpace(secret) == "" {
			if m.providerAuthOptional(m.loginProvider) {
				if credential, ok := m.app.Auth.Get(m.loginProvider); ok && credential.Valid() {
					m.pushLine(styleFooter.Render("kept stored API key for " + m.loginProvider))
				} else if status, err := m.app.AuthStatus(m.ctx, m.loginProvider); err == nil && status.Configured() {
					m.pushLine(styleFooter.Render(m.loginProvider + ": no stored key; explicit or environment credential remains active"))
				} else {
					m.pushLine(styleFooter.Render(m.loginProvider + ": using anonymous/keyless access"))
				}
				return m, nil
			}
			m.pushLine(styleError.Render("login: empty API key"))
			return m, nil
		}
		if _, err := m.app.Login(m.ctx, m.loginProvider, auth.LoginRequest{Method: "api_key"}, fixedAuthInteraction{value: secret}); err != nil {
			m.pushLine(styleError.Render("login: " + err.Error()))
			return m, nil
		}
		m.pushLine(styleFooter.Render("stored API key for " + m.loginProvider + " (0600)"))
		return m, nil
	case tea.KeyBackspace:
		b := m.secretBuf.String()
		if len(b) > 0 {
			m.secretBuf.Reset()
			m.secretBuf.WriteString(b[:len(b)-1])
		}
		return m, nil
	case tea.KeyCtrlC:
		m.loginMode = false
		m.loginEndpoint = ""
		m.loginProvider = ""
		m.secretBuf.Reset()
		m.editor.Reset()
		m.pushLine(styleFooter.Render("login cancelled"))
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.secretBuf.WriteString(string(msg.Runes))
	} else if msg.Type == tea.KeySpace {
		m.secretBuf.WriteString(" ")
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
	m.pushLine(styleFooter.Render(profileID + " endpoint saved · discovering models…"))
	app := m.app
	ctx := m.ctx
	return m, func() tea.Msg {
		return compatibleLoginDoneMsg{generation: generation, provider: profileID, endpoint: endpoint, err: app.RefreshProviderModels(ctx, profileID)}
	}
}

func (m *Model) expandedPrompt(text string) string {
	if m.app == nil {
		return text
	}
	return expandMentionPrompt(text, m.app.CWD(), m.mentionFiles)
}

func (m *Model) submitQueuedInput(text string, kind protocol.QueuedInputKind) tea.Cmd {
	expanded := m.expandedPrompt(text)
	epoch := m.queueEpoch
	m.queueAttempts = append(m.queueAttempts, queuedTUIAttempt{kind: kind, text: text, expanded: expanded, epoch: epoch})
	return func() tea.Msg {
		item, err := m.app.Agent.QueueInput(kind, expanded)
		if errors.Is(err, agent.ErrNotRunning) {
			return queueSubmitMsg{kind: kind, text: text, expanded: expanded, epoch: epoch, fallback: true}
		}
		return queueSubmitMsg{kind: kind, text: text, expanded: expanded, itemID: item.ID, epoch: epoch, accepted: err == nil, err: err}
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

func (m *Model) startQueueFallback() tea.Cmd {
	if len(m.queueFallbacks) == 0 || m.app == nil {
		return nil
	}
	msg := m.queueFallbacks[0]
	m.queueFallbacks = m.queueFallbacks[1:]
	// The fallback is semantically the steer that narrowly missed admission;
	// defer any already queued collaboration-mode transition until this prompt's
	// own turn_done boundary.
	m.modeSwitchReady = false
	m.beginOptimisticRun()
	m.planPrompt = false
	m.pushLine(styleUser.Render("› " + msg.text))
	if m.editor.Value() == msg.text {
		m.editor.Reset()
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
		return promptDoneMsg{generation: generation, turnID: turnID, admitted: running || (turnID != "" && turnID != beforeID), err: err}
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
	m.lastErrorText = ""
	m.runStartedAt = m.currentTime()
	return m.runGeneration
}

func clonePromptImages(images []protocol.ContentBlock) []protocol.ContentBlock {
	cloned := make([]protocol.ContentBlock, len(images))
	for i, image := range images {
		cloned[i] = image
		cloned[i].Data = append([]byte(nil), image.Data...)
	}
	return cloned
}

func (m *Model) takePromptImages() []protocol.ContentBlock {
	images := clonePromptImages(m.promptImages)
	m.promptImages = nil
	return images
}

func (m *Model) startPrompt(text string) tea.Cmd {
	generation := m.beginOptimisticRun()
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	prompt := m.expandedPrompt(stripImageAttachmentTokens(text, len(m.promptImages)))
	images := m.takePromptImages()
	return func() tea.Msg {
		err := m.app.Agent.PromptContent(ctx, prompt, images)
		_, turnID, _ := m.app.Agent.ActiveTurn()
		admitted := err == nil || !errors.Is(err, agent.ErrPromptRejected)
		return promptDoneMsg{generation: generation, turnID: turnID, admitted: admitted, text: text, attachments: images, err: err}
	}
}

func (m *Model) startPromptWithMode(text string, mode protocol.CollaborationMode) tea.Cmd {
	generation := m.beginOptimisticRun()
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	prompt := m.expandedPrompt(stripImageAttachmentTokens(text, len(m.promptImages)))
	images := m.takePromptImages()
	return func() tea.Msg {
		err := m.app.Agent.PromptContentWithMode(ctx, prompt, images, mode)
		_, turnID, _ := m.app.Agent.ActiveTurn()
		admitted := err == nil || !errors.Is(err, agent.ErrPromptRejected)
		return promptDoneMsg{generation: generation, turnID: turnID, admitted: admitted, text: text, attachments: images, err: err}
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
	fallbacks := append([]queueSubmitMsg(nil), m.queueFallbacks...)
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
		parts = append(parts, fallback.text)
	}
	if draft != "" {
		duplicateAcceptedDraft := len(parts) > 0 && parts[len(parts)-1] == draft
		if !duplicateAcceptedDraft {
			parts = append(parts, draft)
		}
	}
	m.queueAttempts = nil
	clear(m.queueOriginalText)
	clear(m.queueRendered)
	if len(parts) > 0 {
		m.editor.SetValue(strings.Join(parts, "\n\n"))
		m.editor.CursorEnd()
		m.refreshInputCompletions()
	}
}

func (m *Model) originalQueuedText(item protocol.QueuedInput) string {
	if original := m.queueOriginalText[item.ID]; original != "" {
		return original
	}
	for _, attempt := range m.queueAttempts {
		if attempt.kind == item.Kind && attempt.expanded == item.Text {
			return attempt.text
		}
	}
	return item.Text
}
