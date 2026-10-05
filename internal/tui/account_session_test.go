package tui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func boundAccountModel(t *testing.T) (*Model, *atomic.Int32) {
	t.Helper()
	home := testHome(t)
	calls := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				t.Errorf("read provider request: %v", err)
			}
			if !strings.Contains(string(body), "preserve-this-task") {
				t.Error("provider request lost the conversation's working context")
			}
			for _, forbidden := range []string{"account-private", "never-copy-private"} {
				if strings.Contains(string(body), forbidden) {
					t.Error("provider request replayed private source data")
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
			return
		}
		model := "gpt-test"
		if r.Header.Get("Authorization") == "Bearer withdrawn-token" {
			model = "replacement-model"
		}
		fmt.Fprintf(w, `{"models":[{"slug":%q,"visibility":"list"}]}`, model)
	}))
	t.Cleanup(server.Close)
	store, err := auth.NewFileStore(filepath.Join(home, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"default", "backup"} {
		key, _ := auth.AccountKey("chatgpt", name)
		if err := store.Put(key, auth.Credential{Type: auth.CredentialOAuth, Access: "token-" + name, AccountID: "workspace-" + name}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := app.New(t.Context(), app.Options{Provider: "chatgpt", Model: "gpt-test", AuthPath: store.Path(), BaseURL: server.URL, CWD: t.TempDir(), Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true, Subagents: new(false)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	m := newModel(t.Context(), app.Options{})
	m.app, m.width, m.height = a, 100, 30
	m.asyncIO = true
	m.layout()
	appendAccountContinuity(t, a)
	return m, calls
}

func appendAccountContinuity(t *testing.T, a *app.App) {
	t.Helper()
	name := a.SelectedChatGPTAccount()
	key, _ := auth.AccountKey("chatgpt", name)
	credential, _ := a.Auth.Get(key)
	if err := a.Session.(session.MetadataStore).SetMetadata("chatgpt_account_binding", auth.AccountIdentity(credential)); err != nil {
		t.Fatal(err)
	}
	message := protocol.NewAssistantMessage("saved answer", "root", "chatgpt", a.Model.ID, []protocol.ContentBlock{
		{Type: protocol.BlockProviderData, Data: []byte(`{"encrypted":"account-private"}`), Text: "never-copy-private-text", Arguments: []byte(`{"secret":"never-copy-private-arguments"}`)},
		{Type: protocol.BlockThinking, Text: "never-copy-private-thinking"},
		{Type: protocol.BlockText, Text: "preserve-this-task: implement account switching"},
	}, protocol.StopStop, nil)
	message.ParentID = a.Session.BranchTip()
	if err := a.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, ParentID: message.ParentID, Message: &message}); err != nil {
		t.Fatal(err)
	}
}

func TestAccountSwitchOpensFreshConversationInOneAction(t *testing.T) {
	m, calls := boundAccountModel(t)
	originalID, originalPath := m.app.Session.ID(), m.app.Session.Path()
	originalHistory, err := m.app.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	originalModel := m.app.Model.ID
	beforeCalls := calls.Load()
	_, _ = m.runCommand("/account")
	for i, account := range m.accountList {
		if account.Name == "backup" {
			m.accountIndex = i
		}
	}
	_, cmd := m.handleAccountPick(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || !m.sessionOpLoading || m.app.SelectedChatGPTAccount() != "default" || m.app.Session.ID() != originalID {
		t.Fatal("selection did not prepare a safe account transition")
	}
	_, catalog := m.Update(cmd())
	if catalog == nil || m.sessionOpLoading || m.pickAccount || m.app.SelectedChatGPTAccount() != "backup" || m.app.Session.ID() == originalID {
		t.Fatalf("one-action switch failed: %v", m.lines)
	}
	messages, err := m.app.Session.Messages()
	if err != nil || len(messages) != 1 || messages[0].Role != protocol.RoleCustom || len(messages[0].Content) != 1 || messages[0].Content[0].Type != protocol.BlockText || m.app.Model.ID != originalModel || calls.Load() != beforeCalls {
		t.Fatal("transition did not use only a text checkpoint, changed models, or performed discovery on the UI thread")
	}
	checkpoint := messages[0].Content[0].Text
	if !strings.Contains(checkpoint, "preserve-this-task") || !strings.Contains(checkpoint, originalID) || strings.Contains(checkpoint, "account-private") || strings.Contains(checkpoint, "never-copy-private") {
		t.Fatal("handoff lost working context or copied private source fields")
	}
	m.Update(catalog())
	if err := m.app.Agent.Prompt(t.Context(), "continue the task"); err != nil {
		t.Fatalf("first request after account handoff: %v", err)
	}
	if !strings.Contains(stripANSI(strings.Join(m.lines, "\n")), "previous conversation saved") {
		t.Fatal("fresh-session transition not explained")
	}
	previous, err := session.OpenSQLiteStore(originalPath, m.app.CWD(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer previous.Close()
	messages, err = previous.Messages()
	if err != nil || !reflect.DeepEqual(messages, originalHistory) {
		t.Fatal("previous account's exact history, including complete private blocks, was not preserved")
	}
	// Same-account selection never creates another session. After backup emits
	// private state, switching back to default uses the same one-action path.
	backupID := m.app.Session.ID()
	_, cmd = m.runCommand("/account backup")
	m.Update(cmd())
	if m.app.Session.ID() != backupID {
		t.Fatal("same-account selection created a needless session")
	}
	appendAccountContinuity(t, m.app)
	_, cmd = m.runCommand("/account default")
	_, catalog = m.Update(cmd())
	if m.app.SelectedChatGPTAccount() != "default" || m.app.Session.ID() == backupID {
		t.Fatal("reverse switch still required a separate /new")
	}
	m.Update(catalog())
}

func TestAccountSwitchFailureLeavesOriginalSelectionsIntact(t *testing.T) {
	m, _ := boundAccountModel(t)
	originalID := m.app.Session.ID()
	_, cmd := m.runCommand("/account backup")
	result := cmd().(accountSessionMsg)
	if err := m.app.Auth.Delete("chatgpt/account/backup"); err != nil {
		t.Fatal(err)
	}
	_, next := m.Update(result)
	if next != nil || m.app.Session.ID() != originalID || m.app.SelectedChatGPTAccount() != "default" {
		t.Fatal("credential removal committed a partial session/account switch")
	}
	messages, err := m.app.Session.Messages()
	if err != nil || len(messages) != 1 {
		t.Fatal("failed transition changed original history")
	}
}

func TestAccountSwitchRejectsNonFreshAndStaleTargets(t *testing.T) {
	m, _ := boundAccountModel(t)
	originalID := m.app.Session.ID()
	if err := m.app.SelectChatGPTAccountSession(t.Context(), "backup", m.app.Session); err == nil {
		t.Fatal("combined transition accepted nonfresh private history")
	}
	_, cmd := m.runCommand("/account backup")
	result := cmd().(accountSessionMsg)
	m.sessionOpGeneration++
	m.Update(result)
	if m.app.Session.ID() != originalID || m.app.SelectedChatGPTAccount() != "default" {
		t.Fatal("stale completion changed session/account")
	}
}

func TestAccountHandoffCarriesToolEvidenceWithoutReplayingCalls(t *testing.T) {
	m, _ := boundAccountModel(t)
	call := protocol.NewAssistantMessage("old-tool-call", m.app.Session.BranchTip(), "chatgpt", m.app.Model.ID, []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "old-call", Name: "read", Arguments: []byte(`{"path":"main.go"}`)}}, protocol.StopStop, nil)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: call.ID, ParentID: call.ParentID, Message: &call}); err != nil {
		t.Fatal(err)
	}
	result := protocol.NewToolResultMessage("old-tool-result", m.app.Session.BranchTip(), "old-call", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "old-tool-evidence: reviewed main.go"}}, false)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: result.ID, ParentID: result.ParentID, Message: &result}); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.runCommand("/account backup")
	_, catalog := m.Update(cmd())
	if catalog == nil {
		t.Fatal("completed tool exchange prevented handoff")
	}
	messages, err := m.app.Session.Messages()
	if err != nil || len(messages) != 1 || len(messages[0].Content) != 1 || messages[0].Content[0].Type != protocol.BlockText || !strings.Contains(messages[0].Content[0].Text, "old-tool-evidence") {
		t.Fatal("tool evidence was lost or replayed as an executable exchange")
	}
	m.Update(catalog())
	if err := m.app.Agent.Prompt(t.Context(), "continue the task"); err != nil {
		t.Fatal(err)
	}
}

func TestAccountHandoffRejectsIncompleteSourceWithoutSwitching(t *testing.T) {
	m, _ := boundAccountModel(t)
	originalID := m.app.Session.ID()
	partial := protocol.NewAssistantMessage("partial", m.app.Session.BranchTip(), "chatgpt", m.app.Model.ID, []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "pending-call", Name: "write", Arguments: []byte(`{"path":"main.go"}`)}}, protocol.StopPending, nil)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: partial.ID, ParentID: partial.ParentID, Message: &partial}); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.runCommand("/account backup")
	_, next := m.Update(cmd())
	if next != nil || m.app.Session.ID() != originalID || m.app.SelectedChatGPTAccount() != "default" {
		t.Fatal("partial owning turn was silently abandoned by handoff")
	}
}

func TestSelectedChatGPTLoginReconcilesReplacedIdentity(t *testing.T) {
	m, _ := boundAccountModel(t)
	originalID := m.app.Session.ID()
	credential, _ := m.app.Auth.Get("chatgpt")
	credential.AccountID, credential.Access = "replacement-workspace", "replacement-token"
	if err := m.app.Auth.Put("chatgpt", credential); err != nil {
		t.Fatal(err)
	}
	status, err := m.app.AuthStatus(t.Context(), "chatgpt")
	if err != nil {
		t.Fatal(err)
	}
	m.oauthLoading = true
	_, cmd := m.Update(oauthDoneMsg{account: "default", status: chatGPTStatus(status)})
	if cmd == nil || !m.sessionOpLoading {
		t.Fatal("replacement login left incompatible private state for the next prompt")
	}
	_, catalog := m.Update(cmd())
	if catalog == nil || m.app.Session.ID() == originalID || m.app.SelectedChatGPTAccount() != "default" {
		t.Fatal("replacement login did not reconcile the selected conversation")
	}
	m.Update(catalog())
	if err := m.app.Agent.Prompt(t.Context(), "continue the task after login"); err != nil {
		t.Fatalf("selected-login replacement still failed its next request: %v", err)
	}
}

func TestSelectedLoginHandoffRevalidatesDefaultEntitlement(t *testing.T) {
	m, calls := boundAccountModel(t)
	credential, _ := m.app.Auth.Get("chatgpt")
	credential.AccountID, credential.Access = "replacement-workspace", "withdrawn-token"
	if err := m.app.Auth.Put("chatgpt", credential); err != nil {
		t.Fatal(err)
	}
	m.oauthLoading = true
	_, cmd := m.Update(oauthDoneMsg{account: "default"})
	_, catalog := m.Update(cmd())
	m.Update(catalog())
	beforeCalls := calls.Load()
	if err := m.app.Agent.Prompt(t.Context(), "continue the task"); err == nil {
		t.Fatal("default-alias handoff did not revalidate the chosen model for the replacement identity")
	}
	if calls.Load() != beforeCalls || m.app.Model.ID != "gpt-test" {
		t.Fatal("unavailable model reached transport or was silently substituted")
	}
}

func TestChatGPTLoginHandoffOnlyForSelectedReplacedIdentity(t *testing.T) {
	for _, scenario := range []string{"same-identity rotation", "detached account", "failed login"} {
		t.Run(scenario, func(t *testing.T) {
			m, calls := boundAccountModel(t)
			originalID, beforeCalls := m.app.Session.ID(), calls.Load()
			account, credentialKey := "default", "chatgpt"
			if scenario == "detached account" {
				account, credentialKey = "backup", "chatgpt/account/backup"
			}
			credential, _ := m.app.Auth.Get(credentialKey)
			credential.Access = "rotated-token"
			if err := m.app.Auth.Put(credentialKey, credential); err != nil {
				t.Fatal(err)
			}
			msg := oauthDoneMsg{account: account}
			if scenario == "failed login" {
				msg.err = fmt.Errorf("authorization declined")
			}
			m.oauthLoading = true
			_, cmd := m.Update(msg)
			if cmd != nil || m.sessionOpLoading || m.app.Session.ID() != originalID || m.app.SelectedChatGPTAccount() != "default" || calls.Load() != beforeCalls {
				t.Fatal("login unnecessarily changed accounts/context or performed another discovery")
			}
		})
	}
}

func TestAccountSwitchDoesNotDiscardEphemeralPrivateHistory(t *testing.T) {
	m, _ := boundAccountModel(t)
	if err := m.switchSession(session.NewMemoryStore(session.Options{CWD: m.app.CWD()})); err != nil {
		t.Fatal(err)
	}
	appendAccountContinuity(t, m.app)
	originalID := m.app.Session.ID()
	_, cmd := m.runCommand("/account backup")
	if cmd != nil || m.app.Session.ID() != originalID || m.app.SelectedChatGPTAccount() != "default" {
		t.Fatal("ephemeral history was discarded by automatic fresh-session transition")
	}
}
