package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *Model) authOperationPending() bool {
	return m.oauthLoading || m.compatibleLoginPending || m.logoutPending
}

func (m *Model) rememberLoginStep(step loginNavigationStep, provider, value string) {
	m.loginNavigation = append(m.loginNavigation, loginNavigationEntry{
		step:     step,
		provider: provider,
		value:    value,
	})
}

func (m *Model) loginEscapeAction() string {
	if len(m.loginNavigation) > 0 {
		return "back"
	}
	return "cancel"
}

func (m *Model) clearLoginNavigation() {
	clear(m.loginNavigation)
	m.loginNavigation = nil
}

func (m *Model) clearLoginStepState() {
	m.loginFieldGeneration++
	m.pickProvider = false
	m.providerLogout = false
	m.providers = nil
	m.pickChatGPTAuth = false
	m.authAccounts = nil
	m.authIndex = 0
	m.loginProfileMode = false
	m.loginEndpointMode = false
	m.loginMode = false
	m.loginProvider = ""
	m.loginEndpoint = ""
	m.loginError = ""
	m.secretBuf.Reset()
	m.editor.Reset()
	m.editor.Placeholder = "Type a message…"
}

func (m *Model) cancelLoginFlow() {
	m.clearLoginStepState()
	m.clearLoginNavigation()
	m.oauthBackRequested = false
}

func (m *Model) restorePreviousLoginStep() bool {
	if len(m.loginNavigation) == 0 {
		return false
	}
	last := len(m.loginNavigation) - 1
	entry := m.loginNavigation[last]
	m.loginNavigation[last] = loginNavigationEntry{}
	m.loginNavigation = m.loginNavigation[:last]
	m.clearLoginStepState()

	switch entry.step {
	case loginNavigationProvider:
		m.providers = m.supportedProviders()
		if len(m.providers) == 0 {
			m.clearLoginNavigation()
			return true
		}
		m.provIndex = 0
		for i, provider := range m.providers {
			if provider == entry.provider {
				m.provIndex = i
				break
			}
		}
		m.pickProvider = true
	case loginNavigationProfile:
		m.beginCompatibleProfileCapture()
		m.editor.SetValue(entry.value)
		m.editor.CursorEnd()
	case loginNavigationEndpoint:
		m.beginCompatibleEndpointCapture(entry.provider)
		m.editor.SetValue(entry.value)
		m.editor.CursorEnd()
	default:
		m.clearLoginNavigation()
	}
	return true
}

// handleProviderPick navigates the /login provider list.
func (m *Model) handleProviderPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if next, handled := movePicker(m.provIndex, len(m.providers), pickerKeyAction(msg), m.loginPickerVisibleChoices()); handled {
		m.provIndex = next
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.cancelLoginFlow()
	case tea.KeyEnter:
		if len(m.providers) == 0 {
			m.pickProvider = false
			return m, nil
		}
		provider := m.providers[m.provIndex]
		logout := m.providerLogout
		m.pickProvider = false
		m.providerLogout = false
		m.providers = nil
		if logout {
			return m.doLogout([]string{provider})
		}
		if !m.isSupportedProvider(provider) {
			m.pushLine(styleError.Render("login: " + provider + " is not supported yet"))
			return m, nil
		}
		m.rememberLoginStep(loginNavigationProvider, provider, "")
		if provider == chatgpt.ProviderID {
			return m.startChatGPTAuthPick()
		}
		if provider == openaicompat.ProviderID {
			m.beginCompatibleProfileCapture()
		} else if m.isOpenAICompatibleProfile(provider) {
			m.beginCompatibleEndpointCapture(provider)
		} else {
			m.beginKeyCapture(provider)
		}
	}
	return m, nil
}

func chatGPTAccountChoices(sources []chatgpt.AuthSource) []chatGPTAccountChoice {
	choices := make([]chatGPTAccountChoice, 0, len(sources))
	index := make(map[string]int, len(sources))
	for _, source := range sources {
		accountID := strings.TrimSpace(source.Status.AccountID)
		if accountID == "" {
			continue
		}
		if i, ok := index[accountID]; ok {
			duplicate := false
			for _, name := range choices[i].Sources {
				if name == source.Name {
					duplicate = true
					break
				}
			}
			if !duplicate {
				choices[i].Sources = append(choices[i].Sources, source.Name)
			}
			continue
		}
		index[accountID] = len(choices)
		choices = append(choices, chatGPTAccountChoice{AccountID: accountID, Sources: []string{source.Name}})
	}
	return choices
}

// startChatGPTAuthPick discovers account IDs used by OpenCode/Pi/Codex, then
// starts a fresh Snow OAuth flow constrained to the selected account. Tokens
// from other clients are never copied into Snow by this TUI flow.
func (m *Model) startChatGPTAuthPick() (tea.Model, tea.Cmd) {
	m.authAccounts = chatGPTAccountChoices(chatgpt.DiscoverAuthSources())
	m.authIndex = 0
	m.pickChatGPTAuth = true
	m.loginError = ""
	m.compVisible = false
	m.editor.Reset()
	return m, nil
}

// handleChatGPTAuthPick selects and imports a discovered local credential.
func (m *Model) handleChatGPTAuthPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if m.oauthLoading {
		if msg.Type == tea.KeyEsc && m.oauthCancel != nil {
			m.oauthBackRequested = true
			m.oauthCancel()
		}
		return m, nil
	}
	count := len(m.authAccounts) + 2
	if next, handled := movePicker(m.authIndex, count, pickerKeyAction(msg), m.loginPickerVisibleChoices()); handled {
		m.authIndex = next
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		if !m.restorePreviousLoginStep() {
			m.cancelLoginFlow()
		}
	case tea.KeyEnter:
		if m.authIndex < len(m.authAccounts) {
			account := m.authAccounts[m.authIndex]
			return m, m.startChatGPTOAuth(chatgpt.LoginBrowser, []string{account.AccountID})
		}
		method := chatgpt.LoginBrowser
		if m.authIndex-len(m.authAccounts) == 1 {
			method = chatgpt.LoginDevice
		}
		return m, m.startChatGPTOAuth(method, nil)
	}
	return m, nil
}

func (m *Model) startChatGPTOAuth(method chatgpt.LoginMethod, allowedWorkspaceIDs []string) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	events := make(chan tea.Msg, 8)
	m.oauthLoading, m.oauthCancel, m.oauthEvents = true, cancel, events
	m.oauthBackRequested = false
	m.oauthProgress = chatgpt.LoginProgress{}
	m.loginError = ""
	go func() {
		request := auth.LoginRequest{Method: string(method), Params: map[string][]string{"allowed_workspace_id": allowedWorkspaceIDs}}
		resolved, err := m.app.Login(ctx, chatgpt.ProviderID, request, tuiOAuthInteraction{events: events})
		if err != nil && method == chatgpt.LoginBrowser && (strings.Contains(err.Error(), "callback port 1455 is unavailable") || errors.Is(err, auth.ErrInteractionUnavailable)) && ctx.Err() == nil {
			request.Method = string(chatgpt.LoginDevice)
			resolved, err = m.app.Login(ctx, chatgpt.ProviderID, request, tuiOAuthInteraction{events: events})
		}
		status := chatGPTStatus(resolved)
		// Once Login has persisted the credential, cancellation/fallback of the
		// optional catalog refresh must not turn the committed login into failure.
		events <- oauthDoneMsg{status: status, err: err}
	}()
	return waitOAuthEvent(events)
}

func chatGPTStatus(status auth.Status) chatgpt.AuthStatus {
	return chatgpt.AuthStatus{Provider: chatgpt.ProviderID, Authenticated: status.Configured(), Expired: status.State == auth.StateExpired, Refreshable: status.Refreshable, AccountID: status.AccountID, ExpiresAt: status.ExpiresAt}
}

func waitOAuthEvent(events <-chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-events } }

var (
	oauthBrowserCommand = exec.CommandContext
	oauthBrowserReap    = func(cmd *exec.Cmd) { go func() { _ = cmd.Wait() }() }
)

func openOAuthBrowser(ctx context.Context, target string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{target}
	default:
		name, args = "xdg-open", []string{target}
	}
	cmd := oauthBrowserCommand(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	oauthBrowserReap(cmd)
	return nil
}

// renderChatGPTAuthPicker renders the centered, secret-free account/method
// chooser or OAuth progress state. Tokens are never included in this string.
func (m *Model) renderChatGPTAuthPicker() string {
	return m.renderChatGPTAuthPickerCard()
}

func (m *Model) beginCompatibleProfileCapture() {
	m.loginFieldGeneration++
	m.loginProfileMode = true
	m.loginEndpointMode = false
	m.loginMode = false
	m.loginProvider = openaicompat.ProviderID
	m.loginEndpoint = ""
	m.loginError = ""
	m.secretBuf.Reset()
	m.editor.Reset()
	m.editor.Placeholder = "x-provider"
	m.compVisible = false
	m.pickProvider = false
}

func (m *Model) beginCompatibleEndpointCapture(profileID string) {
	m.loginFieldGeneration++
	m.loginEndpointMode = true
	m.loginProfileMode = false
	m.loginMode = false
	m.loginProvider = profileID
	m.loginEndpoint = ""
	m.loginError = ""
	m.secretBuf.Reset()
	m.editor.Reset()
	if m.app != nil {
		if configured, ok := m.app.Cfg.Providers[profileID]; ok {
			m.editor.SetValue(sanitizeTerminalLine(configured.BaseURL))
			m.editor.CursorEnd()
		}
	}
	m.editor.Placeholder = "https://gateway.example/v1"
	m.compVisible = false
	m.pickProvider = false
}

// beginKeyCapture switches the editor into masked API-key capture mode.
func (m *Model) beginKeyCapture(provider string) {
	m.loginFieldGeneration++
	m.loginMode = true
	m.loginProvider = provider
	m.loginError = ""
	m.secretBuf.Reset()
	m.editor.Reset()
	m.editor.Placeholder = "Type a message…"
	m.compVisible = false
	m.pickProvider = false
}

// supportedProviders is registry-driven; adding a module with login methods
// automatically exposes it in the picker.
func (m *Model) supportedProviders() []string {
	if m.app == nil {
		return nil
	}
	descriptors := m.app.AuthProviders()
	providers := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if len(descriptor.Methods) > 0 {
			providers = append(providers, descriptor.ProviderID)
		}
	}
	return providers
}

func (m *Model) isOpenAICompatibleProfile(providerID string) bool {
	if providerID == openaicompat.ProviderID {
		return true
	}
	if m.app == nil {
		return false
	}
	providerConfig, ok := m.app.Cfg.Providers[providerID]
	return ok && config.IsOpenAICompatibleProfile(providerID, providerConfig)
}

func (m *Model) providerAuthOptional(providerID string) bool {
	if m.app == nil {
		return false
	}
	for _, descriptor := range m.app.AuthProviders() {
		if descriptor.ProviderID == providerID {
			return !descriptor.Required
		}
	}
	return false
}

func (m *Model) isSupportedProvider(providerID string) bool {
	for _, descriptor := range m.app.AuthProviders() {
		if descriptor.ProviderID == providerID {
			return len(descriptor.Methods) > 0
		}
	}
	return false
}

func (m *Model) providerStatus(providerID string) string {
	if m.app == nil {
		return providerID + "  (unavailable)"
	}
	status, err := m.app.AuthStatus(m.ctx, providerID)
	if err != nil {
		return providerID + "  (invalid auth: " + err.Error() + ")"
	}
	kind := status.Method
	optional := false
	for _, descriptor := range m.app.AuthProviders() {
		if descriptor.ProviderID == providerID {
			optional = !descriptor.Required
			if kind == "" && len(descriptor.Kinds) > 0 {
				kind = descriptor.Kinds[0]
			}
			break
		}
	}
	summary := status.Summary
	if kind == auth.CredentialOAuth {
		switch status.State {
		case auth.StateMissing:
			summary = "OAuth not configured"
		case auth.StateExpired:
			summary = "OAuth expired"
		}
	} else if kind == auth.CredentialAPIKey {
		summary = "no key"
		if optional {
			summary = "keyless"
			if providerID == "opencode-zen" {
				summary = "anonymous"
			}
		}
		if credential, ok := m.app.Auth.Get(providerID); ok && credential.Valid() {
			summary = "stored ✓"
		} else if status.State == auth.StateConfigured {
			summary = "configured"
		}
	}
	if summary == "" {
		summary = string(status.State)
	}
	if m.isOpenAICompatibleProfile(providerID) {
		endpointStatus := "endpoint required"
		if configured, ok := m.app.PersistedCfg.Providers[providerID]; ok && strings.TrimSpace(configured.BaseURL) != "" {
			endpointStatus = "endpoint configured"
		}
		summary = endpointStatus + " · " + summary
	}
	return providerID + "  (" + summary + ")"
}

// renderProviderPicker renders the centered /login or /logout provider list.
func (m *Model) renderProviderPicker() string {
	return m.renderProviderPickerCard()
}

// doLogout opens a picker for /logout or directly removes /logout <provider>.
func (m *Model) doLogout(args []string) (tea.Model, tea.Cmd) {
	if m.authOperationPending() {
		m.pushLine(styleError.Render("logout: wait for the current authentication operation to finish"))
		return m, nil
	}
	if len(args) == 0 {
		m.providers = m.storedCredentialProviders()
		if len(m.providers) == 0 {
			m.pushLine(styleFooter.Render("logout: no stored credentials"))
			return m, nil
		}
		m.provIndex = 0
		m.providerLogout = true
		m.pickProvider = true
		m.loginError = ""
		m.compVisible = false
		return m, nil
	}
	if len(args) != 1 {
		m.pushLine(styleError.Render("usage: /logout [provider]"))
		return m, nil
	}
	provider := args[0]
	m.logoutGeneration++
	generation := m.logoutGeneration
	m.logoutPending = true
	m.logoutProvider = provider
	m.loginError = ""
	app := m.app
	ctx := m.ctx
	return m, func() tea.Msg {
		return logoutDoneMsg{generation: generation, provider: provider, err: app.Logout(ctx, provider)}
	}
}

func (m *Model) storedCredentialProviders() []string {
	supported := m.supportedProviders()
	providers := make([]string, 0, len(supported))
	for _, provider := range supported {
		if _, ok := m.app.Auth.Get(provider); ok {
			providers = append(providers, provider)
		}
	}
	return providers
}

func (m *Model) cycleThinkingEffort() (tea.Model, tea.Cmd) {
	if m.app == nil {
		return m, nil
	}
	levels := m.app.Agent.Model().SupportedThinkingLevels()
	if len(levels) == 0 {
		return m, nil
	}
	current := m.app.Agent.Thinking()
	next := levels[0]
	for i, level := range levels {
		if level == current {
			next = levels[(i+1)%len(levels)]
			break
		}
	}
	if err := m.setThinking(next, false); err != nil {
		m.pushLine(styleError.Render(err.Error()))
		return m, nil
	}
	m.thinkingFlash = true
	m.thinkingFlashSeq++
	seq := m.thinkingFlashSeq
	return m, tea.Tick(650*time.Millisecond, func(time.Time) tea.Msg {
		return clearThinkingFlashMsg(seq)
	})
}

func (m *Model) startThinkingPick() (tea.Model, tea.Cmd) {
	if m.app == nil {
		return m, nil
	}
	m.startThinkingPickForModel(m.app.Agent.Model(), false)
	return m, nil
}

func (m *Model) startThinkingPickForModel(model protocol.Model, returnToModel bool) {
	model = model.Clone()
	m.thinkingModel = &model
	m.thinkingReturnToModel = returnToModel
	m.thinkingList = model.SupportedThinkingLevels()
	m.thinkingIndex = 0
	current := m.app.Agent.Thinking()
	if returnToModel && model.DefaultThinking != "" && model.SupportsThinkingLevel(model.DefaultThinking) &&
		(m.app.Agent.Model().Provider != model.Provider || m.app.Agent.Model().ID != model.ID) {
		current = model.DefaultThinking
	} else if !model.SupportsThinkingLevel(current) && model.DefaultThinking != "" && model.SupportsThinkingLevel(model.DefaultThinking) {
		current = model.DefaultThinking
	}
	for i, level := range m.thinkingList {
		if level == current {
			m.thinkingIndex = i
			break
		}
	}
	m.pickThinking = true
	m.compVisible = false
}

func (m *Model) handleThinkingPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if len(m.thinkingList) == 0 {
		m.pickThinking = false
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.thinkingIndex = (m.thinkingIndex - 1 + len(m.thinkingList)) % len(m.thinkingList)
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.thinkingIndex = (m.thinkingIndex + 1) % len(m.thinkingList)
	case tea.KeyEnter:
		level := m.thinkingList[m.thinkingIndex]
		selected := m.thinkingModel
		returnToModel := m.thinkingReturnToModel
		returnToSettings := returnToModel && m.settingsReturnToPanel
		m.clearThinkingPick()
		if returnToModel && selected != nil {
			m.clearModelPick()
			m.settingsReturnToPanel = false
			err := m.applyModelAndThinking(*selected, level)
			if returnToSettings {
				m.pickSettings = true
				if err != nil {
					m.settingsError = err.Error()
					m.settingsStatus = ""
				} else {
					m.settingsError = ""
					m.settingsStatus = "model and thinking effort saved"
				}
			} else if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			}
		} else if err := m.applyThinking(level); err != nil {
			m.pushLine(styleError.Render(err.Error()))
		}
	case tea.KeyEsc:
		returnToModel := m.thinkingReturnToModel
		m.clearThinkingPick()
		if returnToModel {
			m.pickModel = true
		}
	}
	return m, nil
}

func (m *Model) clearModelPick() {
	m.pickModel = false
	m.modelLoading = false
	m.modelList = nil
	m.modelQuery = ""
	m.pickerGeneration++
}

func (m *Model) clearThinkingPick() {
	m.pickThinking = false
	m.thinkingList = nil
	m.thinkingModel = nil
	m.thinkingReturnToModel = false
}

func (m *Model) applyThinking(level protocol.ThinkingLevel) error {
	return m.setThinking(level, true)
}

func (m *Model) persistConfig(mutate func(*config.Config) error) (config.Config, error) {
	if m.app.ConfigPath != "" {
		return config.Update(m.app.ConfigPath, mutate)
	}
	candidate := m.app.PersistedCfg
	if err := mutate(&candidate); err != nil {
		return config.Config{}, err
	}
	return candidate, nil
}

func (m *Model) persistProjectSelection(selection config.ProjectSelection) (config.Config, error) {
	return m.persistConfig(func(latest *config.Config) error {
		candidate, err := config.WithProjectSelection(*latest, m.app.CWD(), selection)
		if err != nil {
			return err
		}
		*latest = candidate
		return nil
	})
}

func (m *Model) setThinking(level protocol.ThinkingLevel, announce bool) error {
	if m.app == nil {
		return fmt.Errorf("thinking: app is not ready")
	}
	old := m.app.Agent.Thinking()
	if err := m.app.Agent.SetThinking(level); err != nil {
		return err
	}
	candidate, err := m.persistProjectSelection(config.ProjectSelection{
		Provider: m.app.ProviderID,
		Model:    m.app.Agent.Model().ID,
		Thinking: string(m.app.Agent.Thinking()),
	})
	if err != nil {
		_ = m.app.Agent.SetThinking(old)
		return fmt.Errorf("persist thinking: %w", err)
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.Thinking = string(m.app.Agent.Thinking())
	m.app.Cfg.DefaultProvider = m.app.ProviderID
	m.app.Cfg.DefaultModel = m.app.Agent.Model().ID
	if announce {
		m.pushLine(styleFooter.Render("thinking: " + string(m.app.Agent.Thinking())))
	}
	return nil
}

func (m *Model) renderThinkingPicker() string {
	if !m.pickThinking || len(m.thinkingList) == 0 {
		return ""
	}
	var b strings.Builder
	title := "thinking effort"
	if m.thinkingModel != nil {
		title += " for " + m.thinkingModel.ID
	}
	b.WriteString(styleHeaderDim.Render(title) + "\n")
	for i, level := range m.thinkingList {
		line := string(level)
		if i == m.thinkingIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	hint := "(↑/↓ choose, Enter apply, Esc cancel)"
	if m.thinkingReturnToModel {
		hint = "(↑/↓ choose, Enter apply, Esc back)"
	}
	b.WriteString(styleFooter.Render(hint))
	return strings.TrimSuffix(b.String(), "\n")
}

// startModelPick opens immediately from cached catalogs, then resolves missing
// inactive catalogs asynchronously so ordinary startup does not wait for them.
func (m *Model) startModelPick() (tea.Model, tea.Cmd) {
	if m.compatibleLoginPending {
		m.lastStatus = "waiting for openai-compatible model discovery"
		m.pushLine(styleFooter.Render(m.lastStatus))
		return m, nil
	}
	if m.app == nil {
		m.pushLine(styleError.Render("model catalog unavailable"))
		return m, nil
	}
	models := append([]protocol.Model(nil), m.app.AllModels...)
	if len(models) == 0 {
		models = append(models, m.app.Models...)
	}
	models = uniquePickerModels(models, m.app.ProviderID)
	m.modelList = models
	m.modelIndex = 0
	m.modelQuery = ""
	for i, candidate := range models {
		if candidate.Provider == m.app.Model.Provider && candidate.ID == m.app.Model.ID {
			m.modelIndex = i
			break
		}
	}
	m.pickModel = true
	m.modelLoading = false
	m.compVisible = false
	m.pickerGeneration++
	generation := m.pickerGeneration
	if m.asyncIO {
		m.modelLoading = true
		return m, func() tea.Msg {
			fetched, err := m.app.LoadProviderCatalogs(m.ctx)
			return modelListMsg{generation: generation, models: fetched, err: err}
		}
	}
	if len(models) > 0 {
		return m, nil
	}
	fetched, err := m.app.LoadProviderCatalogs(m.ctx)
	if len(fetched) > 0 {
		m.modelList = uniquePickerModels(fetched, m.app.ProviderID)
	}
	if err != nil && len(m.modelList) == 0 {
		m.pickModel = false
		m.pushLine(styleError.Render("model list: " + err.Error()))
		return m, nil
	}
	if len(m.modelList) == 0 {
		m.pickModel = false
		m.pushLine(styleError.Render("no models available"))
	}
	return m, nil
}

func uniquePickerModels(models []protocol.Model, defaultProvider string) []protocol.Model {
	out := make([]protocol.Model, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		if model.Provider == "" {
			model.Provider = defaultProvider
		}
		if model.ID == "" {
			continue
		}
		key := model.Provider + "\x00" + model.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model.Clone())
	}
	return out
}

func (m *Model) filteredModels() []protocol.Model {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(m.modelQuery)))
	if len(terms) == 0 {
		return m.modelList
	}
	matches := make([]protocol.Model, 0, len(m.modelList))
	for _, model := range m.modelList {
		haystack := strings.ToLower(strings.Join([]string{model.Provider, model.ID, model.DisplayName, model.Description}, " "))
		matched := true
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, model)
		}
	}
	return matches
}
