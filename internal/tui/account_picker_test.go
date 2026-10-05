package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
)

func TestAccountCommandInventoryAndGates(t *testing.T) {
	if matches := completeCommand("account"); !slices.Contains(matches, "/account") {
		t.Fatalf("account completion missing: %v", matches)
	}
	if spec, ok := commandByExact("/account"); !ok || spec.needsArgs() {
		t.Fatalf("account command must open its picker without arguments: %+v, %v", spec, ok)
	}
	m := modelPickerTestModel(t, 100, 30)
	if !strings.Contains(strings.Join(m.helpLines(), "\n"), "/account [name]") {
		t.Fatal("account missing from help")
	}
	_, cmd := m.runCommand("/account")
	if cmd != nil || m.pickAccount || !strings.Contains(stripANSI(strings.Join(m.lines, "\n")), "only with the ChatGPT provider") {
		t.Fatalf("non-ChatGPT gate picker=%v cmd=%v lines=%v", m.pickAccount, cmd != nil, m.lines)
	}
	m.app.ProviderID = chatgpt.ProviderID
	m.busy = true
	m.editor.SetValue("/account")
	_, cmd = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.pickAccount || m.editor.Value() != "" || !strings.Contains(stripANSI(strings.Join(m.lines, "\n")), "wait for the current turn") {
		t.Fatalf("busy gate picker=%v cmd=%v draft=%q lines=%v", m.pickAccount, cmd != nil, m.editor.Value(), m.lines)
	}
}

func TestAccountPickerLocalInspectionAndCancellation(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.app.ProviderID = chatgpt.ProviderID
	for _, name := range []string{"default", "work"} {
		key, err := auth.AccountKey(chatgpt.ProviderID, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.app.Auth.Put(key, auth.Credential{Type: auth.CredentialOAuth, Access: "local-only-token", AccountID: "workspace-" + name}); err != nil {
			t.Fatal(err)
		}
	}
	beforeModel := m.app.Model.ID
	beforeHeight := m.transcript.Height()
	m.asyncIO = true
	_, cmd := m.runCommand("/account")
	if cmd != nil || !m.pickAccount || len(m.accountList) != 2 || m.loginModalVisible() || m.oauthLoading {
		t.Fatalf("local picker cmd=%v open=%v accounts=%v login=%v", cmd != nil, m.pickAccount, m.accountList, m.loginModalVisible())
	}
	m.layout()
	if m.transcript.Height() != beforeHeight {
		t.Fatal("account modal changed transcript geometry")
	}
	if m.renderOverlays() != "" || !m.composerCoveredByModal() || !m.headerControlsBlocked() || !m.anyUpdateBlockingModalVisible() {
		t.Fatal("account modal did not exclusively own the frame")
	}
	card := m.renderAccountPicker()
	plain := stripANSI(card)
	for _, want := range []string{"ChatGPT accounts", "default", "work", "configured", "selected"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("card missing %q: %q", want, plain)
		}
	}
	assertModelCardBounds(t, m, card)
	frame := m.viewContent()
	x := (m.managedFrameWidth() - transcriptSelectionBlockWidth(card)) / 2
	y := (m.managedFrameHeight() - lipgloss.Height(card)) / 2
	lines := strings.Split(stripANSI(frame), "\n")
	if y >= len(lines) || xansi.StringWidth(lines[y]) <= x || []rune(lines[y])[x] != '╭' {
		t.Fatalf("account picker is not centered at (%d,%d)", x, y)
	}
	_, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.pickAccount || m.accountList != nil || m.composerCoveredByModal() || m.app.Model.ID != beforeModel {
		t.Fatal("cancel did not return to unchanged chat")
	}
}

func TestAccountPickerEmptyAndBoundedNavigation(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.app.ProviderID = chatgpt.ProviderID
	_, cmd := m.runCommand("/account")
	if cmd != nil || !m.pickAccount {
		t.Fatal("empty picker did not open locally")
	}
	if plain := stripANSI(m.renderAccountPicker()); !strings.Contains(plain, "/login chatgpt NAME") {
		t.Fatalf("missing creation guidance: %q", plain)
	}
	_, cmd = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || !m.pickAccount || m.oauthLoading || m.loginModalVisible() {
		t.Fatal("empty Enter must not start login")
	}
	for i := range 40 {
		m.accountList = append(m.accountList, app.ChatGPTAccountStatus{
			Name: fmt.Sprintf("account-%02d", i), Selected: i == 0, Preferred: i == 1,
			Status: auth.Status{State: auth.StateExpired},
		})
	}
	for _, size := range [][2]int{{100, 30}, {50, 16}, {20, 8}, {5, 3}} {
		m.width, m.height = size[0], size[1]
		m.layout()
		_, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd})
		if m.accountIndex != len(m.accountList)-1 {
			t.Fatalf("End index=%d", m.accountIndex)
		}
		assertModelCardBounds(t, m, m.renderAccountPicker())
		if size[0] >= 50 && !strings.Contains(stripANSI(m.renderAccountPicker()), "account-39") {
			t.Fatal("selected account is outside the visible window")
		}
	}
	m.width, m.height = 100, 30
	_, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyHome})
	if plain := stripANSI(m.renderAccountPicker()); !strings.Contains(plain, "preferred") || !strings.Contains(plain, "expired") {
		t.Fatalf("missing preference/status: %q", plain)
	}
}

func TestAccountSelectionFailureKeepsPicker(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.app.ProviderID = chatgpt.ProviderID
	m.pickAccount = true
	m.accountList = []app.ChatGPTAccountStatus{{Name: "does-not-exist"}}
	_, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || !m.pickAccount || m.accountError == "" {
		t.Fatalf("selection failure cmd=%v open=%v error=%q", cmd != nil, m.pickAccount, m.accountError)
	}
	if !strings.Contains(stripANSI(m.renderAccountPicker()), "account:") {
		t.Fatal("selection error not shown in picker")
	}
}

func TestAccountSelectionRefreshesCatalogAsyncWithoutChoosingModel(t *testing.T) {
	home := testHome(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"models":[{"slug":"gpt-test","visibility":"list"},{"slug":"gpt-other","visibility":"list"}]}`)
	}))
	defer server.Close()
	store, err := auth.NewFileStore(filepath.Join(home, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"default", "work"} {
		key, err := auth.AccountKey(chatgpt.ProviderID, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Put(key, auth.Credential{Type: auth.CredentialOAuth, Access: "local-token-" + name, AccountID: "workspace-" + name}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := app.New(t.Context(), app.Options{
		Provider: chatgpt.ProviderID, Model: "gpt-test", AuthPath: store.Path(),
		BaseURL: server.URL, CWD: t.TempDir(), NoSession: true, Permission: "deny",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	var inactiveRequests atomic.Int32
	inactive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inactiveRequests.Add(1)
		http.Error(w, "LM Studio unavailable", http.StatusServiceUnavailable)
	}))
	defer inactive.Close()
	if err := a.ConfigureOpenAICompatibleProfile("lm-studio", inactive.URL); err != nil {
		t.Fatal(err)
	}
	m := newModel(t.Context(), app.Options{})
	m.app = a
	m.width, m.height = 100, 30
	m.asyncIO = true
	m.layout()
	beforeModel, beforeRequests := a.Model.ID, requests.Load()
	_, cmd := m.runCommand("/account")
	if cmd != nil || requests.Load() != beforeRequests || !m.pickAccount {
		t.Fatal("opening account picker performed network I/O")
	}
	for i, account := range m.accountList {
		if account.Name == "work" {
			m.accountIndex = i
		}
	}
	_, cmd = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || m.pickAccount || a.SelectedChatGPTAccount() != "work" || requests.Load() != beforeRequests {
		t.Fatalf("selection cmd=%v picker=%v account=%q requests=%d", cmd != nil, m.pickAccount, a.SelectedChatGPTAccount(), requests.Load())
	}
	m.Update(cmd())
	if requests.Load() <= beforeRequests || a.Model.ID != beforeModel || m.pickModel || m.pickAccount {
		t.Fatalf("refresh requests=%d model=%q pickModel=%v pickAccount=%v", requests.Load(), a.Model.ID, m.pickModel, m.pickAccount)
	}
	if len(m.modelList) < 2 {
		t.Fatalf("refreshed catalog=%v", m.modelList)
	}
	if inactiveRequests.Load() != 0 || strings.Contains(stripANSI(strings.Join(m.lines, "\n")), "model catalog:") {
		t.Fatalf("account discovery contacted inactive provider: requests=%d lines=%v", inactiveRequests.Load(), m.lines)
	}
	// The inactive fixture really fails when explicitly included in browsing;
	// account selection must not surface that unrelated failure.
	if _, err := a.LoadProviderCatalogs(t.Context()); err == nil || inactiveRequests.Load() == 0 {
		t.Fatalf("unavailable inactive provider fixture was not exercised: %v", err)
	}
}
