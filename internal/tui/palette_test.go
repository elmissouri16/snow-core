package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

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
	found := false
	for _, c := range perm {
		if c == "/permission" {
			found = true
		}
	}
	if !found {
		t.Fatalf("prefix 'per' should include /permission, got %v", perm)
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

	// Typing "/per" opens the palette with /permission among matches.
	m.editor.SetValue("/per")
	m.refreshPalette()
	if !m.compVisible {
		t.Fatal("palette should be visible for '/per'")
	}
	found := false
	for _, c := range m.compMatches {
		if c == "/permission" {
			found = true
		}
	}
	if !found {
		t.Fatalf("matches for '/per' = %v, want /permission", m.compMatches)
	}

	// Down arrow moves the selection (wraps).
	before := m.compIndex
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.compIndex != (before+1)%len(m.compMatches) {
		t.Fatalf("down: index %d -> %d, want wrap to %d", before, m.compIndex, (before+1)%len(m.compMatches))
	}
	// Tab also navigates.
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	// Up wraps backward.
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
}

func TestModelPickerShowsChatGPTDisabled(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.editor.SetValue("/login")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	view := m.View()
	if !strings.Contains(view, "chatgpt") || !strings.Contains(view, "not supported yet") {
		t.Fatalf("picker should show chatgpt as not supported: %q", view)
	}
	if !strings.Contains(view, "opencode-go") {
		t.Fatalf("picker should show opencode-go: %q", view)
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
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
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

// TestModelArgCommandInsertsNotRuns verifies commands with arg hints are
// inserted into the editor for argument completion rather than executed.
func TestModelArgCommandInsertsNotRuns(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/per")
	m.refreshPalette()
	// Pick the first match by setting selection to /permission if present.
	idx := -1
	for i, c := range m.compMatches {
		if c == "/permission" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("/permission not in matches: %v", m.compMatches)
	}
	m.compIndex = idx
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.editor.Value(); !strings.HasPrefix(got, "/permission ") {
		t.Fatalf("editor = %q, want it to contain '/permission ' for args", got)
	}
	if m.compVisible {
		t.Fatal("palette should close after picking")
	}
}

var _ = protocol.AgentEvent{}
