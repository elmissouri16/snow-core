package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/session"
)

// reconcileChatGPTLogin handles an explicit login that replaced the selected
// alias's upstream identity. Detached named login must never switch accounts;
// same-identity token rotation needs no new context or extra catalog request.
func (m *Model) reconcileChatGPTLogin(account string) tea.Cmd {
	if m.app == nil || m.app.ProviderID != chatgpt.ProviderID ||
		(account != "" && account != m.app.SelectedChatGPTAccount()) {
		return nil
	}
	if reason := m.accountSelectionError(); reason != "" {
		m.pushLine(styleError.Render("Login saved; " + reason))
		return nil
	}
	account = m.app.SelectedChatGPTAccount()
	err := m.app.SelectChatGPTAccount(m.ctx, account)
	if errors.Is(err, app.ErrChatGPTAccountContinuity) && m.app.Session.Path() != "" {
		return m.startAccountSession(account)
	}
	if err != nil {
		m.pushLine(styleError.Render("Login saved; account attachment: " + err.Error()))
	}
	return nil
}

type accountSessionMsg struct {
	generation    uint64
	application   *app.App
	sourceID      string
	sourceAccount string
	targetAccount string
	store         session.Store
	err           error
}

// Only a deliberate manual selection can start this transition. A quota or
// transport error never enters it. The previous durable session stays saved;
// provider-private continuity is not copied to the fresh store.
func (m *Model) startAccountSession(name string) tea.Cmd {
	m.sessionOpLoading = true
	m.accountError = ""
	m.sessionOpGeneration++
	m.lastStatus = "switching account; carrying over conversation context…"
	generation, application := m.sessionOpGeneration, m.app
	sourceID, sourceAccount := application.Session.ID(), application.SelectedChatGPTAccount()
	cwd := application.CWD()
	ctx := m.ctx
	return func() tea.Msg {
		message := accountSessionMsg{generation: generation, application: application, sourceID: sourceID, sourceAccount: sourceAccount, targetAccount: name}
		if err := ctx.Err(); err != nil {
			message.err = err
			return message
		}
		message.store, message.err = session.NewFileIndex(session.DefaultSessionsRoot()).Create(cwd)
		if ctx.Err() != nil && message.store != nil {
			_ = message.store.Close()
			message.store = nil
			message.err = ctx.Err()
		}
		return message
	}
}

func (m *Model) applyAccountSession(msg accountSessionMsg) (tea.Model, tea.Cmd) {
	if msg.generation != m.sessionOpGeneration || msg.application != m.app {
		if msg.store != nil {
			_ = msg.store.Close()
		}
		return m, nil
	}
	m.sessionOpLoading = false
	fail := func(text string) (tea.Model, tea.Cmd) {
		if msg.store != nil {
			_ = msg.store.Close()
		}
		if m.pickAccount {
			m.accountError = text
		} else {
			m.pushLine(styleError.Render("account: " + text))
		}
		return m, nil
	}
	if msg.err != nil {
		return fail(msg.err.Error())
	}
	if msg.store == nil {
		return fail("fresh session was not created")
	}
	if msg.sourceID != m.app.Session.ID() || msg.sourceAccount != m.app.SelectedChatGPTAccount() {
		return fail("conversation or account changed during selection; choose again")
	}
	if err := m.app.SelectChatGPTAccountSession(m.ctx, msg.targetAccount, msg.store); err != nil {
		return fail(err.Error())
	}
	m.resetSessionProjection()
	m.pushLine(styleFooter.Render("Conversation context carried over as a working checkpoint. Private continuation restarted; previous conversation saved in /sessions."))
	m.lastStatus = "switched ChatGPT account"
	return m, m.accountSelected()
}
