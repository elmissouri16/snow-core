package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestSettingsPanelNavigationModelReturnAndPermissionPersistence(t *testing.T) {
	home := testHome(t)
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.inlineTranscript = true
	m.layout()

	_, _ = m.runCommand("/settings")
	if !m.pickSettings {
		t.Fatal("/settings did not open the panel")
	}
	view := stripANSI(m.renderSettings())
	for _, want := range []string{"Model", "Thinking effort", "Reasoning summary", "Text verbosity", "Permission mode", "Subagents", "Concurrent subagents", "Agent Skills", "ChatGPT only"} {
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

	// Permission changes apply now, persist globally, and become the baseline
	// for a subsequently attached session.
	m.settingsIndex = settingsPermission
	m.cycleSetting(1) // allow -> deny
	if m.app.Perm.Mode() != permission.ModeDeny {
		t.Fatalf("permission mode = %q, want deny", m.app.Perm.Mode())
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
	cfg, err := config.Load(filepath.Join(home, "config.json"))
	if err != nil || cfg.PermissionMode != "deny" || !cfg.Subagents.Enabled || cfg.Subagents.MaxConcurrentThreads != 5 || !cfg.Skills.Disabled {
		t.Fatalf("persisted settings permission=%q subagents=%v concurrency=%d skills_disabled=%v err=%v", cfg.PermissionMode, cfg.Subagents.Enabled, cfg.Subagents.MaxConcurrentThreads, cfg.Skills.Disabled, err)
	}
	replacement := session.NewMemoryStore(session.Options{CWD: m.app.CWD()})
	if err := m.app.SetSession(replacement); err != nil {
		t.Fatal(err)
	}
	if m.app.Perm.Mode() != permission.ModeDeny {
		t.Fatalf("new-session baseline = %q, want deny", m.app.Perm.Mode())
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
	if m.app.Perm.Mode() != permission.ModeAllow || m.app.Cfg.PermissionMode != "allow" {
		t.Fatalf("failed save changed permission runtime=%q config=%q", m.app.Perm.Mode(), m.app.Cfg.PermissionMode)
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
