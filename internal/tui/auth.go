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

// handleProviderPick navigates the /login provider list.
func (m *Model) handleProviderPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	switch msg.Type {
	case tea.KeyUp:
		if len(m.providers) > 0 {
			m.provIndex = (m.provIndex - 1 + len(m.providers)) % len(m.providers)
		}
	case tea.KeyDown:
		if len(m.providers) > 0 {
			m.provIndex = (m.provIndex + 1) % len(m.providers)
		}
	case tea.KeyTab:
		if len(m.providers) > 0 {
			m.provIndex = (m.provIndex + 1) % len(m.providers)
		}
	case tea.KeyEsc:
		m.pickProvider = false
		m.providerLogout = false
		m.providers = nil
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
		if provider == chatgpt.ProviderID {
			return m.startChatGPTAuthPick()
		}
		if !m.isSupportedProvider(provider) {
			m.pushLine(styleError.Render("login: " + provider + " is not supported yet"))
			return m, nil
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
	m.compVisible = false
	m.editor.Reset()
	m.pushLine(styleFooter.Render("select an existing ChatGPT account or sign-in method (↑/↓ navigate, Enter select, Esc cancel)"))
	return m, nil
}

// handleChatGPTAuthPick selects and imports a discovered local credential.
func (m *Model) handleChatGPTAuthPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if m.oauthLoading {
		if msg.Type == tea.KeyEsc && m.oauthCancel != nil {
			m.oauthCancel()
		}
		return m, nil
	}
	count := len(m.authAccounts) + 2
	switch msg.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		m.authIndex = (m.authIndex - 1 + count) % count
	case tea.KeyDown, tea.KeyTab:
		m.authIndex = (m.authIndex + 1) % count
	case tea.KeyEsc:
		m.pickChatGPTAuth = false
		m.authAccounts = nil
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

// renderChatGPTAuthPicker renders discovered source names and secret-free
// status metadata. Tokens are never included in this string.
func (m *Model) renderChatGPTAuthPicker() string {
	if !m.pickChatGPTAuth {
		return ""
	}
	if m.oauthLoading {
		line := m.oauthProgress.Message
		if m.oauthProgress.URL != "" {
			line += "\n" + m.oauthProgress.URL
		}
		if m.oauthProgress.UserCode != "" {
			line += "\nDevice code: " + m.oauthProgress.UserCode
		}
		return styleCompletionSelected.Render(line + "\n\nEsc cancel")
	}
	lines := make([]string, 0, len(m.authAccounts)+2)
	for _, account := range m.authAccounts {
		lines = append(lines, "Authorize account "+account.AccountID+" for Snow  (used by "+strings.Join(account.Sources, ", ")+")")
	}
	lines = append(lines, "Sign in with browser (any ChatGPT account)", "Sign in with device code")
	var b strings.Builder
	b.WriteString(styleHeader.Render("ChatGPT account"))
	b.WriteByte('\n')
	for i, line := range lines {
		if i == m.authIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteByte('\n')
	}
	b.WriteString(styleFooter.Render("(Snow obtains its own OAuth token · Enter authorize · Esc cancel)"))
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *Model) beginCompatibleProfileCapture() {
	m.loginProfileMode = true
	m.loginEndpointMode = false
	m.loginMode = false
	m.loginProvider = openaicompat.ProviderID
	m.loginEndpoint = ""
	m.secretBuf.Reset()
	m.editor.Reset()
	m.editor.Placeholder = "x-provider"
	m.compVisible = false
	m.pickProvider = false
	m.pushLine(styleFooter.Render("OpenAI-compatible profile name: lowercase letters, digits, ._- · Enter uses openai-compatible · Esc cancel"))
}

func (m *Model) beginCompatibleEndpointCapture(profileID string) {
	m.loginEndpointMode = true
	m.loginProfileMode = false
	m.loginMode = false
	m.loginProvider = profileID
	m.loginEndpoint = ""
	m.secretBuf.Reset()
	m.editor.Reset()
	if m.app != nil {
		if configured, ok := m.app.Cfg.Providers[profileID]; ok {
			m.editor.SetValue(configured.BaseURL)
			m.editor.CursorEnd()
		}
	}
	m.editor.Placeholder = "https://gateway.example/v1"
	m.compVisible = false
	m.pickProvider = false
	m.pushLine(styleFooter.Render(profileID + " endpoint: enter API root, /responses, or /chat/completions URL · Enter continue · Esc cancel"))
}

// beginKeyCapture switches the editor into masked API-key capture mode.
func (m *Model) beginKeyCapture(provider string) {
	m.loginMode = true
	m.loginProvider = provider
	m.secretBuf.Reset()
	m.editor.Reset()
	m.editor.Placeholder = "Type a message…"
	m.compVisible = false
	m.pickProvider = false
	hint := "type key then Enter · Esc to cancel"
	optional := m.isOpenAICompatibleProfile(provider) || m.loginEndpoint != ""
	if m.app != nil {
		for _, descriptor := range m.app.AuthProviders() {
			if descriptor.ProviderID == provider && !descriptor.Required {
				optional = true
				break
			}
		}
	}
	if optional {
		hint = "type optional key, or press Enter to keep existing/fallback/keyless · Esc to cancel"
	}
	m.pushLine(styleFooter.Render("API key for " + provider + " (hidden): " + hint))
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

// renderProviderPicker renders the /login or /logout provider list.
func (m *Model) renderProviderPicker() string {
	if !m.pickProvider || len(m.providers) == 0 {
		return ""
	}
	var b strings.Builder
	title := "login provider"
	hint := "(↑/↓ choose · Enter sign in · Esc cancel)"
	if m.providerLogout {
		title = "logout provider"
		hint = "(↑/↓ choose · Enter log out · Esc cancel)"
	}
	b.WriteString(styleHeader.Render(title))
	b.WriteString("\n")
	for i, p := range m.providers {
		line := m.providerStatus(p)
		if i == m.provIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	b.WriteString(styleFooter.Render(hint))
	return strings.TrimSuffix(b.String(), "\n")
}

// doLogout opens a picker for /logout or directly removes /logout <provider>.
func (m *Model) doLogout(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.providers = m.storedCredentialProviders()
		if len(m.providers) == 0 {
			m.pushLine(styleFooter.Render("logout: no stored credentials"))
			return m, nil
		}
		m.provIndex = 0
		m.providerLogout = true
		m.pickProvider = true
		m.compVisible = false
		return m, nil
	}
	if len(args) != 1 {
		m.pushLine(styleError.Render("usage: /logout [provider]"))
		return m, nil
	}
	provider := args[0]
	app := m.app
	ctx := m.ctx
	return m, func() tea.Msg {
		return logoutDoneMsg{provider: provider, err: app.Logout(ctx, provider)}
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
	m.modelList = nil
	m.modelQuery = ""
	m.modelSearchActive = false
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

// startModelPick opens the interactive model picker from the app's current
// combined provider catalog. The fallback fetch is only for tests/SDKs without
// an app catalog snapshot.
func (m *Model) startModelPick() (tea.Model, tea.Cmd) {
	if m.compatibleLoginPending {
		m.lastStatus = "waiting for openai-compatible model discovery"
		m.pushLine(styleFooter.Render(m.lastStatus))
		return m, nil
	}
	var models []protocol.Model
	if m.app != nil {
		models = append([]protocol.Model(nil), m.app.AllModels...)
		if len(models) == 0 {
			models = append([]protocol.Model(nil), m.app.Models...)
		}
	}
	if len(models) == 0 && m.app != nil {
		if m.asyncIO {
			m.pickModel = true
			m.modelLoading = true
			m.modelList = nil
			m.modelQuery = ""
			m.modelSearchActive = false
			m.pickerGeneration++
			generation := m.pickerGeneration
			return m, func() tea.Msg {
				fetched, err := m.app.Provider.ListModels(m.ctx)
				return modelListMsg{generation: generation, models: fetched, err: err}
			}
		}
		fetched, err := m.app.Provider.ListModels(m.ctx)
		if err != nil {
			m.pushLine(styleError.Render("model list: " + err.Error()))
			return m, nil
		}
		models = fetched
		m.app.Models = uniquePickerModels(models, m.app.ProviderID)
	}
	models = uniquePickerModels(models, m.app.ProviderID)
	if len(models) == 0 {
		m.pushLine(styleError.Render("no models available"))
		return m, nil
	}
	m.modelList = models
	m.modelIndex = 0
	m.modelQuery = ""
	m.modelSearchActive = false
	for i, mm := range models {
		if mm.Provider == m.app.Model.Provider && mm.ID == m.app.Model.ID {
			m.modelIndex = i
			break
		}
	}
	m.pickModel = true
	m.compVisible = false
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
