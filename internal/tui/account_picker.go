package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type accountCatalogMsg struct {
	generation uint64
	app        *app.App
	account    string
	models     []protocol.Model
	err        error
}

func (m *Model) accountSelectionError() string {
	if m.app == nil {
		return "account: app is not ready"
	}
	if m.app.ProviderID != chatgpt.ProviderID {
		return "/account is available only with the ChatGPT provider"
	}
	if m.busy || m.compacting || m.modeSwitching || m.sessionOpLoading || (m.app.Agent != nil && m.app.Agent.IsRunning()) {
		return "account: wait for the current turn or transition to finish"
	}
	return ""
}

func (m *Model) startAccountPick(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 2 && args[0] == "prefer" {
		if err := m.preferAccount(args[1]); err != nil {
			m.pushLine(styleError.Render("account: " + err.Error()))
		}
		return m, nil
	}
	if len(args) > 1 {
		m.pushLine(styleError.Render("Usage: /account [name] | /account prefer NAME"))
		return m, nil
	}
	if err := m.accountSelectionError(); err != "" {
		m.pushLine(styleError.Render(err))
		return m, nil
	}
	if len(args) == 1 {
		return m, m.selectAccount(args[0])
	}
	// Local inspection only: opening the picker must not authorize, refresh a
	// token, or discover models. Named login is a separate /login operation.
	accounts, err := m.app.ChatGPTAccounts(m.ctx)
	if err != nil {
		m.pushLine(styleError.Render("account list: " + err.Error()))
		return m, nil
	}
	m.closeTranscriptSelectionContextMenu()
	m.accountList = nil
	for _, account := range accounts {
		// The shared inventory also reports an absent selected/default alias.
		// Only stored, locally inspectable accounts are choices in this picker.
		if account.Status.State != auth.StateMissing {
			m.accountList = append(m.accountList, account)
		}
	}
	m.accountIndex = 0
	m.accountError = ""
	for i, account := range m.accountList {
		if account.Selected {
			m.accountIndex = i
			break
		}
	}
	m.pickAccount = true
	m.compVisible = false
	return m, nil
}

// preferAccount changes only future-runtime selection. It must not bypass
// continuity checks by switching this session as a side effect.
func (m *Model) preferAccount(name string) error {
	if m.app == nil {
		return fmt.Errorf("app is not ready")
	}
	if err := auth.ValidateAccountName(name); err != nil {
		return err
	}
	accounts, err := m.app.ChatGPTAccounts(m.ctx)
	if err != nil {
		return err
	}
	stored := false
	for _, account := range accounts {
		if account.Name == name && account.Status.State != auth.StateMissing {
			stored = true
			break
		}
	}
	if !stored {
		return fmt.Errorf("account %q is not stored; use /login chatgpt %s first", name, name)
	}
	candidate, err := m.persistConfig(func(latest *config.Config) error {
		latest.ChatGPTAccounts.Preferred = name
		return nil
	})
	if err != nil {
		return fmt.Errorf("save preferred account: %w", err)
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.ChatGPTAccounts.Preferred = name
	for i := range m.accountList {
		m.accountList[i].Preferred = m.accountList[i].Name == name
	}
	m.accountError = ""
	m.pushLine(styleFooter.Render("Preferred ChatGPT account: " + name + " (future runs; current account unchanged)"))
	return nil
}

func (m *Model) closeAccountPick() {
	m.pickAccount = false
	m.accountList = nil
	m.accountIndex = 0
	m.accountError = ""
}

func (m *Model) handleAccountPick(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if msg.Code == tea.KeyEscape {
		m.closeAccountPick()
		return m, nil
	}
	if m.sessionOpLoading {
		return m, nil
	}
	visible := m.loginPickerVisibleChoices()
	if next, handled := movePicker(m.accountIndex, len(m.accountList), pickerKeyAction(msg), visible); handled {
		m.accountIndex = next
		return m, nil
	}
	if len(m.accountList) == 0 {
		return m, nil
	}
	m.accountIndex = clampPickerIndex(m.accountIndex, len(m.accountList))
	if msg.Text == "p" {
		if err := m.preferAccount(m.accountList[m.accountIndex].Name); err != nil {
			m.accountError = err.Error()
		}
		return m, nil
	}
	if msg.Code != tea.KeyEnter {
		return m, nil
	}
	return m, m.selectAccount(m.accountList[m.accountIndex].Name)
}

func (m *Model) selectAccount(name string) tea.Cmd {
	selectionError := m.accountSelectionError()
	if selectionError == "" {
		if err := m.app.SelectChatGPTAccount(m.ctx, name); errors.Is(err, app.ErrChatGPTAccountContinuity) && m.app.Session.Path() != "" {
			return m.startAccountSession(name)
		} else if err != nil {
			selectionError = "account: " + err.Error()
		}
	}
	if selectionError != "" {
		if m.pickAccount {
			m.accountError = selectionError
		} else {
			m.pushLine(styleError.Render(selectionError))
		}
		return nil
	}
	return m.accountSelected()
}

func (m *Model) accountSelected() tea.Cmd {
	m.closeAccountPick()
	m.accountGeneration++
	m.modelList = uniquePickerModels(m.app.AllModels, m.app.ProviderID)
	m.pushLine(styleFooter.Render("ChatGPT account: " + sanitizeTerminalLine(m.app.SelectedChatGPTAccount())))
	// Refresh the invalidated catalog off the UI thread. Never select a model
	// from that response; the shared layer owns model validity on account switch.
	generation, application, ctx := m.accountGeneration, m.app, m.ctx
	account := application.SelectedChatGPTAccount()
	return func() tea.Msg {
		models, err := application.LoadProviderCatalog(ctx, chatgpt.ProviderID)
		return accountCatalogMsg{generation: generation, app: application, account: account, models: models, err: err}
	}
}

func (m *Model) applyAccountCatalog(msg accountCatalogMsg) (tea.Model, tea.Cmd) {
	if msg.generation != m.accountGeneration || msg.app != m.app || m.app == nil ||
		m.app.ProviderID != chatgpt.ProviderID || msg.account != m.app.SelectedChatGPTAccount() {
		return m, nil
	}
	if m.modelModalVisible() {
		return m.Update(modelListMsg{generation: m.pickerGeneration, models: msg.models, err: msg.err})
	}
	m.modelList = uniquePickerModels(msg.models, m.app.ProviderID)
	if msg.err != nil {
		m.pushLine(styleError.Render("account selected; model catalog: " + msg.err.Error()))
	}
	return m, nil
}

func (m *Model) renderAccountPicker() string {
	if !m.pickAccount {
		return ""
	}
	geometry := m.pickerCardGeometry()
	header := renderPickerCardHeader("ChatGPT accounts", fmt.Sprintf("%d stored", len(m.accountList)), geometry.innerWidth)
	bodyHeight := max(1, geometry.innerHeight-4)
	choices := make([]string, 0, len(m.accountList))
	for _, account := range m.accountList {
		labels := []string{string(account.Status.State)}
		if account.QuotaKind != "" {
			quota := "quota observed: " + account.QuotaKind
			if !account.ResetsAt.IsZero() {
				quota += "; reset " + account.ResetsAt.Local().Format("Jan 2 15:04 MST")
			}
			labels = append(labels, quota)
		}
		if account.Selected {
			labels = append(labels, "selected")
		}
		if account.Preferred {
			labels = append(labels, "preferred")
		}
		choices = append(choices, sanitizeTerminalLine(account.Name+"  ("+strings.Join(labels, " · ")+")"))
	}
	body := renderLoginChoiceList(choices, m.accountIndex, geometry.innerWidth, bodyHeight)
	if len(choices) == 0 {
		body = wrapLoginLines([]string{" No stored ChatGPT accounts.", " Use /login chatgpt NAME"}, geometry.innerWidth, bodyHeight)
	}
	instruction := " Switch accounts; working context carried over"
	if m.sessionOpLoading {
		instruction = " Switching account…"
	}
	if m.accountError != "" {
		instruction = " " + sanitizeTerminalLine(m.accountError)
	}
	instructionView := styleHeaderDim.Render(truncateDisplayText(instruction, geometry.innerWidth))
	if m.accountError != "" {
		instructionView = styleError.Render(truncateDisplayText(instruction, geometry.innerWidth))
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		instructionView,
		styleSep.Render(strings.Repeat("─", geometry.innerWidth)),
		fixedLoginBody(body, geometry.innerWidth, bodyHeight),
		styleFooter.Render(truncateDisplayText(" ↑/↓ navigate · Enter select · p prefer · Esc cancel ", geometry.innerWidth)),
	)
	return renderPickerCard(content, geometry)
}
