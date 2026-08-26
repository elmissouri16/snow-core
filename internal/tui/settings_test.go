package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCompatibleLoginConfigureFailureDoesNotOverwriteCredential(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	store := m.app.AuthService.Store()
	if err := store.Put("openai-compatible", auth.Credential{Type: auth.CredentialAPIKey, Key: "concurrent-key"}); err != nil {
		t.Fatal(err)
	}
	m.loginProvider = "openai-compatible"
	m.loginEndpoint = "not-an-absolute-url"
	_, cmd := m.finishCompatibleLogin("attempt-key")
	if cmd != nil {
		t.Fatal("invalid endpoint unexpectedly scheduled discovery")
	}
	credential, ok := store.Get("openai-compatible")
	if !ok || credential.Key != "concurrent-key" {
		t.Fatalf("existing credential was overwritten: found=%v expected-key=%v", ok, credential.Key == "concurrent-key")
	}
}

func TestSubagentSettingPreservesRuntimeOnlyOverrides(t *testing.T) {
	home := testHome(t)
	enabled := true
	project := t.TempDir()
	a, err := app.New(context.Background(), app.Options{
		Provider:               "fake",
		NoSession:              true,
		Permission:             "allow",
		CWD:                    project,
		ConfigPath:             filepath.Join(home, "config.json"),
		Subagents:              &enabled,
		SubagentMaxConcurrency: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(context.Background(), app.Options{})
	m.app = a

	if err := m.setSubagentsEnabled(false); err != nil {
		t.Fatal(err)
	}
	if m.app.Cfg.Subagents.Enabled || m.app.Cfg.Subagents.MaxConcurrentThreads != 9 {
		t.Fatalf("runtime subagent overlay was discarded: %+v", m.app.Cfg.Subagents)
	}
	persisted, err := config.Load(m.app.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Subagents.Enabled || persisted.Subagents.MaxConcurrentThreads != config.DefaultSubagents().MaxConcurrentThreads {
		t.Fatalf("persisted subagent config = %+v", persisted.Subagents)
	}
}

func TestSettingsPanelNavigationAndSessionPermission(t *testing.T) {
	home := testHome(t)
	m := newModel(context.Background(), app.Options{})
	a, err := app.New(context.Background(), app.Options{
		Provider: "fake", NoSession: true, CWD: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	m.app = a
	t.Cleanup(func() { _ = a.Close() })
	m.width, m.height = 100, 30
	m.inlineTranscript = true
	m.layout()

	_, _ = m.runCommand("/settings")
	if !m.pickSettings {
		t.Fatal("/settings did not open the panel")
	}
	view := stripANSI(m.renderSettings())
	for _, want := range []string{"Model", "Thinking effort", "Reasoning summary", "Text verbosity", "Session permission", "Subagents", "Concurrent subagents", "Agent Skills", "ChatGPT only"} {
		if !strings.Contains(view, want) {
			t.Fatalf("settings panel missing %q: %q", want, view)
		}
	}
	m.layout()
	fullView := stripANSI(m.View())
	if got := m.managedFrameHeight(); got != m.height {
		t.Fatalf("inline settings frame height=%d want terminal height %d", got, m.height)
	}
	for _, want := range []string{"Model", "Theme", "Agent Skills"} {
		if !strings.Contains(fullView, want) {
			t.Fatalf("inline settings frame truncated %q: %q", want, fullView)
		}
	}

	// The model row reuses the model picker, and Esc returns to settings.
	_, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickModel || m.pickSettings {
		t.Fatalf("model handoff = picker:%v settings:%v", m.pickModel, m.pickSettings)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.pickSettings || m.pickModel {
		t.Fatalf("model return = picker:%v settings:%v", m.pickModel, m.pickSettings)
	}
	_, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickSettings || m.pickModel || m.settingsError != "" {
		t.Fatalf("model selection return = picker:%v settings:%v error:%q", m.pickModel, m.pickSettings, m.settingsError)
	}

	// Provider-specific rows stay disabled outside ChatGPT.
	m.settingsIndex = settingsReasoningSummary
	m.cycleSetting(1)
	if got := m.app.Agent.ReasoningSummary(); got != protocol.ReasoningSummaryAuto {
		t.Fatalf("disabled summary changed to %q", got)
	}

	// Permission changes apply to the active session without changing the
	// ask-mode baseline for a subsequently attached session.
	m.settingsIndex = settingsPermission
	m.cycleSetting(1) // ask -> allow
	if m.app.Perm.Mode() != permission.ModeAllow {
		t.Fatalf("permission mode = %q, want allow", m.app.Perm.Mode())
	}
	m.settingsIndex = settingsSubagents
	m.cycleSetting(1)
	if !m.app.Cfg.Subagents.Enabled || m.settingsError != "" {
		t.Fatalf("subagents enabled=%v error=%q", m.app.Cfg.Subagents.Enabled, m.settingsError)
	}
	m.settingsIndex = settingsSubagentConcurrency
	m.cycleSetting(1) // 4 -> 5 concurrent children
	if m.app.Cfg.Subagents.MaxConcurrentThreads != 5 || m.settingsError != "" {
		t.Fatalf("subagent concurrency=%d error=%q", m.app.Cfg.Subagents.MaxConcurrentThreads, m.settingsError)
	}
	m.settingsIndex = settingsSkills
	m.cycleSetting(1)
	if !m.app.Cfg.Skills.Disabled || m.settingsError != "" {
		t.Fatalf("skills disabled=%v error=%q", m.app.Cfg.Skills.Disabled, m.settingsError)
	}
	configPath := filepath.Join(home, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil || !cfg.Subagents.Enabled || cfg.Subagents.MaxConcurrentThreads != 5 || !cfg.Skills.Disabled {
		t.Fatalf("persisted settings subagents=%v concurrency=%d skills_disabled=%v err=%v", cfg.Subagents.Enabled, cfg.Subagents.MaxConcurrentThreads, cfg.Skills.Disabled, err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "permission_mode") {
		t.Fatalf("session permission leaked into global config: %s", data)
	}
	replacement := session.NewMemoryStore(session.Options{CWD: m.app.CWD()})
	if err := m.app.SetSession(replacement); err != nil {
		t.Fatal(err)
	}
	if m.app.Perm.Mode() != permission.ModeAsk {
		t.Fatalf("new-session baseline = %q, want ask", m.app.Perm.Mode())
	}
}

func TestSettingsChatGPTValuesPersistAndReload(t *testing.T) {
	home := testHome(t)
	m := newModel(context.Background(), app.Options{})
	a, err := app.New(context.Background(), app.Options{
		Provider: "chatgpt", NoSession: true, Permission: "allow", CWD: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	m.app = a
	t.Cleanup(func() { _ = a.Close() })
	_, _ = m.startSettings()
	lineCount := len(m.lines)

	m.settingsIndex = settingsReasoningSummary
	m.cycleSetting(1) // auto -> concise
	m.settingsIndex = settingsTextVerbosity
	m.cycleSetting(1) // low -> medium
	if m.settingsError != "" {
		t.Fatalf("settings error = %q", m.settingsError)
	}
	if len(m.lines) != lineCount {
		t.Fatalf("settings changes added transcript lines: before=%d after=%d", lineCount, len(m.lines))
	}
	if a.Agent.ReasoningSummary() != protocol.ReasoningSummaryConcise || a.Agent.TextVerbosity() != protocol.TextVerbosityMedium {
		t.Fatalf("runtime response settings = summary:%q verbosity:%q", a.Agent.ReasoningSummary(), a.Agent.TextVerbosity())
	}

	cfg, err := config.Load(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReasoningSummary != "concise" || cfg.TextVerbosity != "medium" {
		t.Fatalf("persisted response settings = summary:%q verbosity:%q", cfg.ReasoningSummary, cfg.TextVerbosity)
	}

	reopened, err := app.New(context.Background(), app.Options{
		Provider: "chatgpt", NoSession: true, Permission: "allow", CWD: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Agent.ReasoningSummary() != protocol.ReasoningSummaryConcise || reopened.Agent.TextVerbosity() != protocol.TextVerbosityMedium {
		t.Fatalf("reloaded response settings = summary:%q verbosity:%q", reopened.Agent.ReasoningSummary(), reopened.Agent.TextVerbosity())
	}
}

func TestSettingsSaveFailureRollsBackAndStaysOpen(t *testing.T) {
	testHome(t)
	m := newModel(context.Background(), app.Options{})
	a, err := app.New(context.Background(), app.Options{
		Provider: "chatgpt", NoSession: true, Permission: "allow", CWD: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	m.app = a
	t.Cleanup(func() { _ = a.Close() })
	_, _ = m.startSettings()

	blocked := filepath.Join(t.TempDir(), "config.json")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	m.app.ConfigPath = blocked
	m.settingsIndex = settingsReasoningSummary
	m.cycleSetting(1)
	if !m.pickSettings || m.settingsError == "" {
		t.Fatalf("save failure panel=%v error=%q", m.pickSettings, m.settingsError)
	}
	if got := m.app.Agent.ReasoningSummary(); got != protocol.ReasoningSummaryAuto {
		t.Fatalf("failed save left runtime summary at %q", got)
	}

	m.settingsIndex = settingsPermission
	m.cycleSetting(1)
	if m.app.Perm.Mode() != permission.ModeDeny || m.settingsError != "" {
		t.Fatalf("session permission depended on global config save: runtime=%q error=%q", m.app.Perm.Mode(), m.settingsError)
	}

	oldModel := m.app.Agent.Model()
	m.settingsIndex = settingsModel
	_, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.modelList) < 2 {
		t.Fatalf("chatgpt model catalog too small for rollback test: %d", len(m.modelList))
	}
	m.modelIndex = (m.modelIndex + 1) % len(m.modelList)
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pickThinking {
		_, _ = m.handleThinkingPick(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if !m.pickSettings || m.settingsError == "" {
		t.Fatalf("failed model save panel=%v error=%q", m.pickSettings, m.settingsError)
	}
	if got := m.app.Agent.Model(); got.Provider != oldModel.Provider || got.ID != oldModel.ID {
		t.Fatalf("failed model save left runtime model at %+v, want %+v", got, oldModel)
	}
}

func TestSettingsUnavailableDuringActiveTurn(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	_, _ = m.runCommand("/settings")
	if m.pickSettings {
		t.Fatal("settings opened during an active turn")
	}
	if len(m.lines) == 0 || !strings.Contains(stripANSI(m.lines[len(m.lines)-1]), "wait for the current turn") {
		t.Fatalf("busy settings error = %v", m.lines)
	}
}
