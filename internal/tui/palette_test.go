package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestCompleteCommandFuzzyMatchesAfterPrefixMatches(t *testing.T) {
	got := completeCommand("cmp")
	if len(got) == 0 || got[0] != "/compact" {
		t.Fatalf("fuzzy compact match = %v", got)
	}
	if got := renderCompletions(nil, 0, 40); !strings.Contains(stripANSI(got), "no matching commands") {
		t.Fatalf("empty palette = %q", got)
	}
}

func TestCompleteCommand(t *testing.T) {
	all := completeCommand("")
	if len(all) != len(commands) {
		t.Fatalf("empty prefix should return all %d commands, got %d", len(commands), len(all))
	}
	mo := completeCommand("mo")
	if len(mo) != 1 || mo[0] != "/model" {
		t.Fatalf("prefix 'mo' = %v, want [/model]", mo)
	}
	perm := completeCommand("per")
	if len(perm) != 1 || perm[0] != "/permissions" {
		t.Fatalf("prefix 'per' = %v, want [/permissions]", perm)
	}
	permissions := completeCommand("permissions")
	if len(permissions) != 1 || permissions[0] != "/permissions" {
		t.Fatalf("prefix 'permissions' = %v, want [/permissions]", permissions)
	}
	if got := completeCommand("xx"); len(got) != 0 {
		t.Fatalf("prefix 'xx' = %v, want none", got)
	}
	// Case-insensitive.
	lo := completeCommand("LO")
	if len(lo) < 2 {
		t.Fatalf("prefix 'LO' = %v, want /login and /logout", lo)
	}
	if lo[0] != "/login" || lo[1] != "/logout" {
		t.Fatalf("prefix 'LO' order = %v, want [/login /logout]", lo)
	}
}

func TestCommandRegistryIsCanonical(t *testing.T) {
	seen := make(map[string]bool, len(commands))
	for _, command := range commands {
		if seen[command.name] {
			t.Fatalf("duplicate command %q", command.name)
		}
		seen[command.name] = true
	}
	for _, name := range []string{"/compact", "/mcp", "/sessions", "/resume", "/new", "/permissions", "/settings", "/skills", "/tree", "/thinking"} {
		if _, ok := commandByExact(name); !ok {
			t.Errorf("missing canonical command %s", name)
		}
	}
	for _, name := range []string{"/session", "/permission", "/name"} {
		if _, ok := commandByExact(name); ok {
			t.Errorf("legacy or out-of-scope command %s is still registered", name)
		}
	}
}

func TestIsCommandPrefix(t *testing.T) {
	cases := map[string]bool{
		"/":         true,
		"/model":    true,
		"/per":      true,
		"/model x":  false,
		"/model x ": false,
		"model":     false,
		"hello":     false,
	}
	for in, want := range cases {
		if got := isCommandPrefix(in); got != want {
			t.Errorf("isCommandPrefix(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestModelPaletteNavigation(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	// Typing "/per" opens the palette with /permissions among matches.
	m.editor.SetValue("/per")
	m.refreshPalette()
	if !m.compVisible {
		t.Fatal("palette should be visible for '/per'")
	}
	if len(m.compMatches) != 1 || m.compMatches[0] != "/permissions" {
		t.Fatalf("matches for '/per' = %v, want [/permissions]", m.compMatches)
	}

	// Down arrow moves the selection (wraps).
	before := m.compIndex
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.compIndex != (before+1)%len(m.compMatches) {
		t.Fatalf("down: index %d -> %d, want wrap to %d", before, m.compIndex, (before+1)%len(m.compMatches))
	}
	// Tab inserts the highlighted command without executing it.
	m.compIndex = 0
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.editor.Value() != "/permissions" {
		t.Fatalf("Tab inserted %q, want /permissions", m.editor.Value())
	}
	if !m.pickPermissionMode {
		// Completion fills the command; Enter is still the execution key.
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if !m.pickPermissionMode {
		t.Fatal("Enter after Tab should execute /permissions interactively")
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	// Up wraps backward when the palette is open.
	m.editor.SetValue("/per")
	m.refreshPalette()
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyUp})

	// Enter with exactly one match for "/model" runs it immediately.
	m.editor.SetValue("/model")
	m.refreshPalette()
	if len(m.compMatches) != 1 || m.compMatches[0] != "/model" {
		t.Fatalf("'/model' should have exactly 1 match, got %v", m.compMatches)
	}
	m.editor.SetValue("/model")
	m.refreshPalette()
	_, quit := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if quit != nil {
		t.Fatal("picking /model should not quit")
	}
}

// TestModelPaletteLoginRunsNotInserts verifies that picking /login from the
// palette RUNS it (opening the provider picker) instead of inserting "/login "
// into the editor for argument completion.
func TestModelPaletteTabAddsArgumentSpace(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.editor.SetValue("/logout")
	m.refreshPalette()
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.editor.Value() != "/logout " {
		t.Fatalf("Tab value = %q, want trailing argument space", m.editor.Value())
	}
}

func TestModelPaletteLoginRunsNotInserts(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/login")
	m.refreshPalette()
	if !m.compVisible {
		t.Fatal("palette should be visible for '/login'")
	}
	idx := -1
	for i, c := range m.compMatches {
		if c == "/login" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("/login not in matches: %v", m.compMatches)
	}
	m.compIndex = idx
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.pickProvider {
		t.Fatalf("picking /login should open the provider picker (pickProvider=%v loginMode=%v)", m.pickProvider, m.loginMode)
	}
	if m.loginMode {
		t.Fatal("picking /login must not enter key-capture mode directly")
	}
	if got := m.editor.Value(); got != "" {
		t.Fatalf("editor = %q, want empty (command ran, not inserted)", got)
	}
	if m.compVisible {
		t.Fatal("palette should close after running /login")
	}
}

func TestModelEscClosesPalette(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/")
	m.refreshPalette()
	if !m.compVisible {
		t.Fatal("palette should open on '/'")
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.compVisible {
		t.Fatal("Esc should close the palette")
	}
}

func TestModelLoginFlow(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	// /login with no args opens the provider picker.
	m.editor.SetValue("/login")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickProvider {
		t.Fatal("login with no args should open the provider picker")
	}

	// Navigate to opencode-go and select it.
	if len(m.providers) == 0 {
		t.Fatal("expected providers in picker")
	}
	idx := -1
	for i, p := range m.providers {
		if p == "opencode-go" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("opencode-go not in picker: %v", m.providers)
	}
	m.provIndex = idx
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginMode || m.loginProvider != "opencode-go" {
		t.Fatalf("expected login mode for opencode-go, got mode=%v provider=%q", m.loginMode, m.loginProvider)
	}
	if m.pickProvider {
		t.Fatal("picker should close after selecting a provider")
	}

	// Type a masked secret.
	for _, r := range "sk-test-123" {
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.secretBuf.String(); got != "sk-test-123" {
		t.Fatalf("secretBuf = %q, want sk-test-123", got)
	}

	// Enter submits and persists.
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.loginMode {
		t.Fatal("login mode should end after submit")
	}
	cred, ok := m.app.Auth.Get("opencode-go")
	if !ok || cred.Key != "sk-test-123" {
		t.Fatalf("stored cred = %+v ok=%v, want key sk-test-123", cred, ok)
	}

	// The raw secret must never appear in the transcript.
	joined := strings.Join(m.lines, "\n")
	if strings.Contains(joined, "sk-test-123") {
		t.Fatalf("secret leaked into transcript: %q", joined)
	}
}

func TestModelProviderPickerNavigation(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/login")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickProvider {
		t.Fatal("expected provider picker")
	}
	before := m.provIndex
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.provIndex != (before+1)%len(m.providers) {
		t.Fatalf("down: index %d -> %d", before, m.provIndex)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.provIndex != before {
		t.Fatalf("up should return to %d, got %d", before, m.provIndex)
	}
	// Esc closes the picker without entering login mode.
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pickProvider || m.loginMode {
		t.Fatal("Esc should close picker without login")
	}
}

func TestModelLoginPickerDirectArg(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	// Direct provider arg skips the picker.
	m.editor.SetValue("/login opencode-go")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginMode || m.loginProvider != "opencode-go" || m.pickProvider {
		t.Fatalf("direct arg should enter capture, mode=%v provider=%q pick=%v", m.loginMode, m.loginProvider, m.pickProvider)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	// Unsupported provider errors without entering capture.
	m.editor.SetValue("/login nope")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.loginMode || m.pickProvider {
		t.Fatal("unsupported provider should not enter login")
	}

	// ChatGPT is OAuth-only; it must never be sent through the API-key mask.
	m.editor.SetValue("/login chatgpt")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.loginMode || m.pickProvider {
		t.Fatal("chatgpt OAuth should not enter API-key capture")
	}
	if !m.pickChatGPTAuth {
		t.Fatal("chatgpt direct login should offer OAuth actions")
	}
}

func TestModelChatGPTImportPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".pi", "agent", "auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"openai-codex":{"type":"oauth","access":"source-access","refresh":"source-refresh","accountId":"source-account"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()
	m.editor.SetValue("/login chatgpt")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickChatGPTAuth || len(m.authSources) != 1 || m.authSources[0].Name != "Pi" {
		t.Fatalf("unexpected auth picker: pick=%v sources=%+v", m.pickChatGPTAuth, m.authSources)
	}
	view := m.View()
	if strings.Contains(view, "source-access") || !strings.Contains(view, "Pi") || !strings.Contains(view, "source-account") {
		t.Fatalf("picker leaked token or missed source: %q", view)
	}
	m.authIndex = 2 // browser, device, then the discovered Pi import
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.oauthLoading || cmd == nil {
		t.Fatal("import should refresh the catalog asynchronously")
	}
	m.oauthCancel()
	m.Update(cmd())
	cred, ok := m.app.Auth.Get("chatgpt")
	if !ok || cred.Access != "source-access" || cred.AccountID != "source-account" {
		t.Fatalf("imported credential = %+v, ok=%v", cred, ok)
	}
}

func TestModelPickerShowsChatGPTAuthStatus(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.editor.SetValue("/login")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	view := m.View()
	if !strings.Contains(view, "chatgpt") || !strings.Contains(view, "OAuth not configured") {
		t.Fatalf("picker should show chatgpt OAuth status: %q", view)
	}
	if !strings.Contains(view, "opencode-go") {
		t.Fatalf("picker should show opencode-go: %q", view)
	}
}

func TestModelPickerShowsStoredChatGPTOAuth(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := m.app.Auth.Put("chatgpt", auth.Credential{
		Type:    auth.CredentialOAuth,
		Access:  "opaque-access-token",
		Refresh: "refresh-token",
		Extra:   map[string]any{"account_id": "account-123"},
	}); err != nil {
		t.Fatal(err)
	}
	m.width = 100
	m.height = 30
	m.layout()

	m.editor.SetValue("/login")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	view := m.View()
	if !strings.Contains(view, "authenticated via OAuth") || !strings.Contains(view, "account-123") {
		t.Fatalf("picker should show stored ChatGPT OAuth status: %q", view)
	}
}

func TestModelLoginMaskedView(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.loginMode = true
	m.loginProvider = "opencode-go"
	for _, r := range "abc" {
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	view := m.View()
	if strings.Contains(view, "abc") {
		t.Fatalf("masked view leaked secret: %q", view)
	}
	if !strings.Contains(view, "•••") {
		t.Fatalf("masked view missing bullets: %q", view)
	}
}

func TestModelLoginEscCancels(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/login opencode-go")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginMode {
		t.Fatal("expected login mode")
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.loginMode {
		t.Fatal("Esc should cancel login")
	}
	if _, ok := m.app.Auth.Get("opencode-go"); ok {
		t.Fatal("cancelled login must not persist a credential")
	}
}

func TestModelLogoutFlow(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	// Seed a credential.
	if err := m.app.Auth.Put("opencode-go", auth.Credential{Type: auth.CredentialAPIKey, Key: "sk-x"}); err != nil {
		t.Fatal(err)
	}

	m.editor.SetValue("/logout opencode-go")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("logout should run as an async command")
	}
	m.Update(cmd())
	if _, ok := m.app.Auth.Get("opencode-go"); ok {
		t.Fatal("credential should be removed after logout")
	}
}

func TestHelpUsesRegistry(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/help")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	joined := strings.Join(m.lines, "\n")
	for _, c := range commands {
		if !strings.Contains(joined, c.name) {
			t.Errorf("/help output missing command %s", c.name)
		}
	}
}

// TestModelArgCommandInsertsNotRuns verifies only commands whose no-arg form
// is meaningless are inserted into the editor for argument completion.
func TestModelArgCommandInsertsNotRuns(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	for _, command := range []string{"/logout"} {
		m.editor.SetValue(command)
		m.refreshPalette()
		idx := -1
		for i, c := range m.compMatches {
			if c == command {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatalf("%s not in matches: %v", command, m.compMatches)
		}
		m.compIndex = idx
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		if got := m.editor.Value(); !strings.HasPrefix(got, command+" ") {
			t.Fatalf("editor = %q, want it to contain %q for args", got, command+" ")
		}
		if m.compVisible {
			t.Fatal("palette should close after picking", command)
		}
	}

	// /model has a cached catalog and opens the picker with no argument.
	m.modelList = []protocol.Model{{Provider: "fake", ID: "fake-1"}}
	m.editor.SetValue("/model")
	m.refreshPalette()
	m.compIndex = 0
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.editor.Value(); got != "" {
		t.Fatalf("picking /model should run it, editor = %q", got)
	}
	if m.compVisible || !m.pickModel {
		t.Fatal("palette should close and model picker should open")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
}

var _ = protocol.AgentEvent{}
