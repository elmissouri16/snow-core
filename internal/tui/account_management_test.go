package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
)

// Only local inspection and login are allowed by this fixture. Unexpected
// refresh or other driver operations fail rather than accessing a provider.
type accountLoginFixture struct{ auth.Driver }

func (accountLoginFixture) Descriptor() auth.Descriptor {
	return auth.Descriptor{ProviderID: "chatgpt", Required: true, Kinds: []auth.CredentialType{auth.CredentialOAuth}}
}
func (accountLoginFixture) Validate(auth.Credential) error { return nil }
func (accountLoginFixture) Inspect(c auth.Credential) (auth.Status, error) {
	return auth.Status{State: auth.StateConfigured, Method: c.Type, AccountID: c.AccountID}, nil
}
func (accountLoginFixture) Login(context.Context, auth.LoginRequest, auth.Interaction) (auth.Credential, error) {
	return auth.Credential{Type: auth.CredentialOAuth, Access: "new-account-token", AccountID: "new-workspace"}, nil
}

func TestTUINamedChatGPTLoginPreservesSelectedCredential(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.app.AuthService = auth.NewService(m.app.Auth)
	if err := m.app.AuthService.Register(accountLoginFixture{}); err != nil {
		t.Fatal(err)
	}
	original := auth.Credential{Type: auth.CredentialOAuth, Access: "original-token", AccountID: "original-workspace"}
	if err := m.app.Auth.Put("chatgpt", original); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.runCommand("/login chatgpt backup")
	if cmd != nil || !m.pickChatGPTAuth || m.loginChatGPTAccount != "backup" {
		t.Fatalf("named login did not open chooser: account=%q", m.loginChatGPTAccount)
	}
	if !strings.Contains(stripANSI(m.renderLoginModal()), "Save as: backup") {
		t.Fatal("chooser hides target account")
	}
	// Select the fresh browser flow rather than a discovered local workspace.
	m.authIndex = len(m.authAccounts)
	_, cmd = m.handleChatGPTAuthPick(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("OAuth did not start")
	}
	m.Update(cmd())
	m.oauthWG.Wait()
	stored, exists := m.app.Auth.Get("chatgpt/account/backup")
	if !exists || stored.AccountID != "new-workspace" {
		t.Fatal("named login not saved under target alias")
	}
	unchanged, _ := m.app.Auth.Get("chatgpt")
	if unchanged.Access != original.Access || m.app.SelectedChatGPTAccount() != "default" || m.app.Cfg.ChatGPTAccounts.Preferred != "" {
		t.Fatal("named login replaced or selected the existing credential")
	}
	if !strings.Contains(stripANSI(strings.Join(m.lines, "\n")), "Saved without switching") {
		t.Fatal("named login completion did not explain selection")
	}
	_, _ = m.runCommand("/login chatgpt another")
	m.authIndex = len(m.authAccounts)
	_, cmd = m.handleChatGPTAuthPick(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(cmd())
	m.oauthWG.Wait()
	if _, exists := m.app.Auth.Get("chatgpt/account/another"); exists {
		t.Fatal("same upstream account was duplicated under another alias")
	}
	_, _ = m.runCommand("/login chatgpt BadAlias")
	if m.pickChatGPTAuth || !strings.Contains(stripANSI(strings.Join(m.lines, "\n")), "login:") {
		t.Fatal("invalid alias opened authorization")
	}
	_, _ = m.runCommand("/login chatgpt backup")
	_, _ = m.handleChatGPTAuthPick(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.pickChatGPTAuth || m.loginChatGPTAccount != "" {
		t.Fatal("cancel retained the named login target")
	}
}

func TestTUIPreferredAccountPersistsWithoutSwitching(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.app.ProviderID = chatgpt.ProviderID
	m.app.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	for _, name := range []string{"default", "backup"} {
		key, _ := auth.AccountKey("chatgpt", name)
		if err := m.app.Auth.Put(key, auth.Credential{Type: auth.CredentialOAuth, Access: "token", AccountID: name}); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = m.runCommand("/account prefer backup")
	persisted, err := config.Load(m.app.ConfigPath)
	if err != nil || persisted.ChatGPTAccounts.Preferred != "backup" || m.app.PersistedCfg.ChatGPTAccounts.Preferred != "backup" || m.app.SelectedChatGPTAccount() != "default" {
		t.Fatalf("preference not saved independently: %v", err)
	}
	_, _ = m.runCommand("/account")
	for i, account := range m.accountList {
		if account.Name == "default" {
			m.accountIndex = i
		}
	}
	_, cmd := m.handleAccountPick(tea.KeyPressMsg{Code: 'p', Text: "p"})
	persisted, err = config.Load(m.app.ConfigPath)
	if cmd != nil || !m.pickAccount || err != nil || persisted.ChatGPTAccounts.Preferred != "default" {
		t.Fatalf("picker preference failed: %v", err)
	}
	for _, account := range m.accountList {
		if account.Preferred != (account.Name == "default") {
			t.Fatal("picker preference labels stale")
		}
	}
	if err := m.preferAccount("missing"); err == nil {
		t.Fatal("unstored account accepted as preferred")
	}
	// A failed disk update must not change effective or picker preference.
	m.app.ConfigPath = t.TempDir()
	if err := m.preferAccount("backup"); err == nil || m.app.Cfg.ChatGPTAccounts.Preferred != "default" || m.app.PersistedCfg.ChatGPTAccounts.Preferred != "default" {
		t.Fatal("failed preference write changed runtime configuration")
	}
}
