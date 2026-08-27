package tui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/pkg/protocol"
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
	for _, name := range []string{"/compact", "/context", "/mcp", "/sessions", "/resume", "/new", "/permissions", "/settings", "/skills", "/tree", "/thinking"} {
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

func TestModelPaletteNavigationReachesAllCommands(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 16
	m.editor.SetValue("/")
	m.refreshPalette()
	m.layout()

	if got, want := len(m.compMatches), len(commands); got != want {
		t.Fatalf("palette matches=%d want complete registry of %d", got, want)
	}
	if len(m.compMatches) <= 10 {
		t.Fatalf("test requires more than one legacy palette page, got %d commands", len(m.compMatches))
	}

	for range len(m.compMatches) - 1 {
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	last := m.compMatches[len(m.compMatches)-1]
	if m.compIndex != len(m.compMatches)-1 {
		t.Fatalf("down navigation stopped at index %d want %d (%s)", m.compIndex, len(m.compMatches)-1, last)
	}
	if overlay := stripANSI(m.renderOverlays()); !strings.Contains(overlay, last) {
		t.Fatalf("selection-following palette did not render %s: %q", last, overlay)
	}

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.compIndex != 0 {
		t.Fatalf("down from final command index=%d want wrapped index 0", m.compIndex)
	}
}

func TestModelPaletteLogoutRunsPickerWithoutArgument(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := m.app.Auth.Put("opencode-go", auth.Credential{Type: auth.CredentialAPIKey, Key: "sk-x"}); err != nil {
		t.Fatal(err)
	}
	m.editor.SetValue("/logout")
	m.refreshPalette()
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.editor.Value() != "/logout" {
		t.Fatalf("Tab value = %q, want argument-free command", m.editor.Value())
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickProvider || !m.providerLogout {
		t.Fatalf("logout picker open=%v purpose=%v", m.pickProvider, m.providerLogout)
	}
}

// TestModelPaletteLoginRunsNotInserts verifies that picking /login from the
// palette RUNS it (opening the provider picker) instead of inserting "/login "
// into the editor for argument completion.
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
	m.width, m.height = 120, 30
	m.inlineTranscript = true
	m.layout()
	if got := m.managedFrameHeight(); got != m.height {
		t.Fatalf("inline provider picker frame height=%d want terminal height %d", got, m.height)
	}
	pickerView := stripANSI(m.View())
	for _, provider := range []string{"opencode-go", "opencode-zen", "openai-compatible", "chatgpt"} {
		if !strings.Contains(pickerView, provider) {
			t.Fatalf("inline provider picker truncated %q: %q", provider, pickerView)
		}
	}

	// Navigate to opencode-go and select it.
	if len(m.providers) == 0 {
		t.Fatal("expected providers in picker")
	}
	idx := -1
	compatibleFound := false
	for i, p := range m.providers {
		if p == "opencode-go" {
			idx = i
		}
		if p == "openai-compatible" {
			compatibleFound = true
		}
	}
	if !compatibleFound {
		t.Fatalf("openai-compatible not in picker: %v", m.providers)
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

func TestModelPickerShowsPrivacyDescription(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	m.pickModel = true
	m.modelList = []protocol.Model{{Provider: "opencode-zen", ID: "big-pickle", Description: "Privacy warning: training use."}}
	if got := stripANSI(m.renderModelPicker()); !strings.Contains(got, "Privacy warning: training use.") {
		t.Fatalf("picker=%q", got)
	}
}

func TestOpenCodeZenLoginAllowsAnonymousMode(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.editor.SetValue("/login opencode-zen")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginMode || m.loginProvider != "opencode-zen" {
		t.Fatalf("login mode=%v provider=%q", m.loginMode, m.loginProvider)
	}
	if got := stripANSI(m.renderLoginModal()); !strings.Contains(got, "key is optional") {
		t.Fatalf("optional hint missing from login card: %q", got)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.loginMode {
		t.Fatal("blank optional login should finish")
	}
	if _, ok := m.app.Auth.Get("opencode-zen"); ok {
		t.Fatal("anonymous mode should not persist a credential")
	}
	if got := strings.Join(m.lines, "\n"); !strings.Contains(got, "anonymous/keyless") {
		t.Fatalf("anonymous status missing: %q", got)
	}
}

func TestInlineCommandPaletteWindowFollowsSelection(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 120, 30
	m.inlineTranscript = true
	m.editor.SetValue("/")
	m.compMatches = []string{"/a0", "/a1", "/a2", "/a3", "/a4", "/a5", "/a6", "/a7", "/a8", "/a9"}
	m.compIndex = len(m.compMatches) - 1
	m.compVisible = true
	m.layout()

	view := stripANSI(m.View())
	if !strings.Contains(view, "/a9") || !strings.Contains(view, "/") {
		t.Fatalf("inline command palette lost selection or composer: %q", view)
	}
	if got := m.managedFrameHeight(); got != m.height {
		t.Fatalf("inline command palette frame height=%d want terminal height %d", got, m.height)
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

	// The generic Responses login captures and persists its endpoint before the
	// masked optional key, then refreshes models without rendering the secret.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer compatible-secret" {
			t.Errorf("discovery authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"compatible-model"}]}`))
	}))
	defer server.Close()
	m.editor.SetValue("/login openai-compatible")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginProfileMode {
		t.Fatal("compatible login did not request a profile name")
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // blank keeps the legacy name
	if !m.loginEndpointMode || m.loginProvider != "openai-compatible" {
		t.Fatalf("compatible endpoint mode=%v provider=%q", m.loginEndpointMode, m.loginProvider)
	}
	m.editor.SetValue(server.URL + "/v1")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.loginEndpointMode || !m.loginMode {
		t.Fatalf("compatible key step endpoint=%v key=%v", m.loginEndpointMode, m.loginMode)
	}
	for _, r := range "compatible-secret" {
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("compatible login did not schedule model discovery")
	}
	m.Update(cmd())
	cred, ok := m.app.Auth.Get("openai-compatible")
	if !ok || cred.Key != "compatible-secret" {
		t.Fatalf("compatible credential=%+v ok=%v", cred, ok)
	}
	if got := m.app.PersistedCfg.Providers["openai-compatible"].BaseURL; got != server.URL+"/v1" {
		t.Fatalf("compatible endpoint=%q", got)
	}
	persisted, err := os.ReadFile(m.app.ConfigPath)
	if err != nil || !strings.Contains(string(persisted), server.URL+"/v1") {
		t.Fatalf("persisted config=%q err=%v", persisted, err)
	}
	foundModel := false
	for _, model := range m.app.AllModels {
		foundModel = foundModel || model.Provider == "openai-compatible" && model.ID == "compatible-model"
	}
	if !foundModel {
		t.Fatalf("discovered models=%+v", m.app.AllModels)
	}
	status := m.providerStatus("openai-compatible")
	if strings.Contains(status, "compatible-secret") || !strings.Contains(status, "endpoint configured") || !strings.Contains(status, "stored") {
		t.Fatalf("compatible status leaked or missed configuration: %q", status)
	}
	m.editor.SetValue("/login openai-compatible")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginEndpointMode || m.editor.Value() != server.URL+"/v1" {
		t.Fatalf("saved endpoint was not prefilled: mode=%v value=%q", m.loginEndpointMode, m.editor.Value())
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}) // endpoint -> profile
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}) // profile -> close
	m.editor.SetValue("/logout openai-compatible")
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("compatible logout did not return command")
	}
	m.Update(cmd())
	if _, ok := m.app.Auth.Get("openai-compatible"); ok {
		t.Fatal("compatible logout did not remove key")
	}

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

func TestOpenAICompatibleTUILoginAllowsKeylessAndRejectsInvalidEndpoint(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.editor.SetValue("/login openai-compatible")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // legacy profile name
	m.editor.SetValue("relative/path")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !m.loginEndpointMode {
		t.Fatalf("invalid endpoint cmd=%v mode=%v", cmd != nil, m.loginEndpointMode)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("keyless discovery authorization=%q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"local-model"}]}`))
	}))
	defer server.Close()
	m.editor.SetValue(server.URL)
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginMode {
		t.Fatal("valid endpoint did not advance to optional key")
	}
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("empty optional key did not configure keyless endpoint")
	}
	m.Update(cmd())
	if _, ok := m.app.Auth.Get("openai-compatible"); ok {
		t.Fatal("keyless login stored a credential")
	}
	if got := m.app.PersistedCfg.Providers["openai-compatible"].BaseURL; got != server.URL {
		t.Fatalf("keyless endpoint=%q", got)
	}
}

func TestOpenAICompatibleTUILoginCreatesNamedProfile(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer x-secret" {
			t.Errorf("named profile authorization=%q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"x-model"}]}`))
	}))
	defer server.Close()

	m.editor.SetValue("/login openai-compatible")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginProfileMode {
		t.Fatal("named login did not enter profile-name capture")
	}
	m.editor.SetValue("x-provider")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.loginEndpointMode || m.loginProvider != "x-provider" {
		t.Fatalf("endpoint mode=%v provider=%q", m.loginEndpointMode, m.loginProvider)
	}
	m.editor.SetValue(server.URL + "/v1")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "x-secret" {
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("named profile login did not schedule discovery")
	}
	m.Update(cmd())

	configured := m.app.PersistedCfg.Providers["x-provider"]
	if configured.Type != config.ProviderTypeOpenAICompatible || configured.BaseURL != server.URL+"/v1" {
		t.Fatalf("named profile config=%+v", configured)
	}
	credential, ok := m.app.Auth.Get("x-provider")
	if !ok || credential.Key != "x-secret" {
		t.Fatalf("named credential=%+v ok=%v", credential, ok)
	}
	if _, legacy := m.app.Auth.Get(openaicompat.ProviderID); legacy {
		t.Fatal("named profile credential leaked into legacy profile")
	}
	found := false
	for _, model := range m.app.AllModels {
		found = found || model.Provider == "x-provider" && model.ID == "x-model"
	}
	if !found {
		t.Fatalf("named profile models=%+v", m.app.AllModels)
	}
	if status := m.providerStatus("x-provider"); !strings.Contains(status, "endpoint configured") || !strings.Contains(status, "stored") {
		t.Fatalf("named profile status=%q", status)
	}
}

func TestOpenAICompatibleLoginIgnoresStaleDiscoveryCompletion(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.compatibleLoginGeneration = 2
	m.compatibleLoginPending = true
	_, cmd := m.startModelPick()
	if cmd != nil || m.pickModel {
		t.Fatalf("model picker opened during discovery: cmd=%v picker=%v", cmd != nil, m.pickModel)
	}
	before := m.app.Agent.Model()
	_, _ = m.runCommand("/model blocked-model")
	if after := m.app.Agent.Model(); after.ID != before.ID || after.Provider != before.Provider {
		t.Fatalf("direct model command changed selection during discovery: before=%+v after=%+v", before, after)
	}
	m.Update(compatibleLoginDoneMsg{generation: 1, provider: "old-provider", err: errors.New("old failure")})
	if !m.compatibleLoginPending || strings.Contains(strings.Join(m.lines, "\n"), "old failure") {
		t.Fatalf("stale completion changed state: pending=%v lines=%q", m.compatibleLoginPending, m.lines)
	}
	m.Update(compatibleLoginDoneMsg{generation: 2, provider: "new-provider"})
	if m.compatibleLoginPending {
		t.Fatal("current completion did not clear pending state")
	}
}

func TestModelChatGPTAccountAuthorizationPicker(t *testing.T) {
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
	if !m.pickChatGPTAuth || len(m.authAccounts) != 1 || m.authAccounts[0].AccountID != "source-account" {
		t.Fatalf("unexpected auth picker: pick=%v accounts=%+v", m.pickChatGPTAuth, m.authAccounts)
	}
	view := m.View()
	if strings.Contains(view, "source-access") || !strings.Contains(view, "Pi") || !strings.Contains(view, "source-account") || !strings.Contains(view, "own OAuth token") {
		t.Fatalf("picker leaked token or missed account authorization: %q", view)
	}
	if m.authIndex != 0 {
		t.Fatalf("known Pi account should be selected by default, index=%d", m.authIndex)
	}
	if _, ok := m.app.Auth.Get("chatgpt"); ok {
		t.Fatal("opening the account picker must not import another client's token")
	}
}

func TestChatGPTAccountChoicesDeduplicateWorkspace(t *testing.T) {
	choices := chatGPTAccountChoices([]chatgpt.AuthSource{
		{Name: "OpenCode", Status: chatgpt.AuthStatus{AccountID: "workspace"}},
		{Name: "Pi", Status: chatgpt.AuthStatus{AccountID: "workspace"}},
		{Name: "Codex", Status: chatgpt.AuthStatus{AccountID: "other"}},
	})
	if len(choices) != 2 || choices[0].AccountID != "workspace" || strings.Join(choices[0].Sources, ",") != "OpenCode,Pi" || choices[1].AccountID != "other" {
		t.Fatalf("account choices=%+v", choices)
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

	// Seed two credentials so picker selection proves it removes only the
	// selected provider.
	if err := m.app.Auth.Put("opencode-go", auth.Credential{Type: auth.CredentialAPIKey, Key: "sk-x"}); err != nil {
		t.Fatal(err)
	}
	if err := m.app.Auth.Put("chatgpt", auth.Credential{Type: auth.CredentialOAuth, Access: "access"}); err != nil {
		t.Fatal(err)
	}

	m.editor.SetValue("/logout")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickProvider || !m.providerLogout || len(m.providers) != 2 {
		t.Fatalf("logout picker open=%v purpose=%v providers=%v", m.pickProvider, m.providerLogout, m.providers)
	}
	if got := stripANSI(m.renderProviderPicker()); !strings.Contains(got, "Logout") || !strings.Contains(got, "opencode-go") || !strings.Contains(got, "chatgpt") {
		t.Fatalf("logout picker = %q", got)
	}
	m.provIndex = 0
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.pickProvider || m.providerLogout || !m.logoutPending {
		t.Fatalf("picker selection cmd=%v open=%v purpose=%v pending=%v", cmd != nil, m.pickProvider, m.providerLogout, m.logoutPending)
	}
	if card := stripANSI(m.renderLoginModal()); !strings.Contains(card, "Removing stored credential") {
		t.Fatalf("logout progress card=%q", card)
	}
	_, blockedCmd := m.startLogin(nil)
	if blockedCmd != nil || m.pickProvider {
		t.Fatalf("login raced pending logout: cmd=%v picker=%v", blockedCmd != nil, m.pickProvider)
	}
	m.Update(cmd())
	if m.logoutPending || m.loginModalVisible() {
		t.Fatalf("logout completion pending=%v modal=%v", m.logoutPending, m.loginModalVisible())
	}
	if _, ok := m.app.Auth.Get("opencode-go"); ok {
		t.Fatal("selected credential should be removed after logout")
	}
	if _, ok := m.app.Auth.Get("chatgpt"); !ok {
		t.Fatal("unselected credential should remain stored")
	}

	// The explicit form remains available for scripts and experienced users.
	m.editor.SetValue("/logout chatgpt")
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("direct logout should run as an async command")
	}
	m.Update(cmd())
	if _, ok := m.app.Auth.Get("chatgpt"); ok {
		t.Fatal("direct logout should remove the credential")
	}
}

func TestModelLogoutPickerHandlesNoStoredCredentials(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.editor.SetValue("/logout")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.pickProvider {
		t.Fatalf("empty logout cmd=%v picker=%v", cmd != nil, m.pickProvider)
	}
	if got := stripANSI(strings.Join(m.lines, "\n")); !strings.Contains(got, "no stored credentials") {
		t.Fatalf("logout status = %q", got)
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

func TestModelNoArgCommandsOpenPickers(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

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
