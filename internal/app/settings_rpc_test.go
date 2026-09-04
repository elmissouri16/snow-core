package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRPCSettingsUpdatePersistsPartialRestartSettings(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	a.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(a.ConfigPath, a.PersistedCfg); err != nil {
		t.Fatal(err)
	}
	before, err := a.RPCSettings()
	if err != nil {
		t.Fatal(err)
	}
	if before.RestartRequired {
		t.Fatalf("initial settings require restart: %+v", before)
	}

	enabled := !before.SubagentsEnabled
	concurrency := before.SubagentConcurrent + 1
	skillsEnabled := !before.SkillsEnabled
	after, err := a.UpdateRPCSettings(SettingsUpdate{
		SubagentsEnabled: &enabled, SubagentsMaxConcurrent: &concurrency, SkillsEnabled: &skillsEnabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if after.SubagentsEnabled != enabled || after.SubagentConcurrent != concurrency || after.SkillsEnabled != skillsEnabled {
		t.Fatalf("updated settings = %+v", after)
	}
	if after.SubagentAgentLimit < concurrency {
		t.Fatalf("agent limit %d below concurrency %d", after.SubagentAgentLimit, concurrency)
	}
	if !after.SubagentsRestartRequired || !after.SkillsRestartRequired || !after.RestartRequired {
		t.Fatalf("restart flags = %+v", after)
	}
	persisted, err := config.Load(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Subagents.Enabled != enabled || persisted.Subagents.MaxConcurrentThreads != concurrency || persisted.Skills.Disabled == skillsEnabled {
		t.Fatalf("persisted settings = %+v", persisted)
	}
}

func TestRPCSettingsUpdateRejectsEmptyAndInvalidUpdates(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	if _, err := a.UpdateRPCSettings(SettingsUpdate{}); err == nil {
		t.Fatal("empty update accepted")
	}
	invalid := config.MaxConcurrentSubagents + 1
	if _, err := a.UpdateRPCSettings(SettingsUpdate{SubagentsMaxConcurrent: &invalid}); err == nil {
		t.Fatal("invalid concurrency accepted")
	}
}

func TestRPCSettingsUpdatePreservesConcurrentUnrelatedConfig(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	a.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(a.ConfigPath, a.PersistedCfg); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Update(a.ConfigPath, func(latest *config.Config) error {
		latest.TUI.Theme = "nord"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	enabled := !a.Cfg.Subagents.Enabled
	if _, err := a.UpdateRPCSettings(SettingsUpdate{SubagentsEnabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	persisted, err := config.Load(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.TUI.Theme != "nord" {
		t.Fatalf("concurrent theme write lost: %q", persisted.TUI.Theme)
	}
}

func TestRPCSettingsUpdatePersistsLiveClientChoices(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	defaultProvider := a.PersistedCfg.DefaultProvider
	defaultModel := a.PersistedCfg.DefaultModel
	defaultThinking := a.PersistedCfg.Thinking
	a.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(a.ConfigPath, a.PersistedCfg); err != nil {
		t.Fatal(err)
	}

	providerID := "fake"
	modelID := "fake-1"
	thinking := protocol.ThinkingOff
	summary := protocol.ReasoningSummaryDetailed
	verbosity := protocol.TextVerbosityHigh
	debugEnabled := true
	after, err := a.UpdateRPCSettingsContext(t.Context(), SettingsUpdate{
		Provider:         &providerID,
		Model:            &modelID,
		Thinking:         &thinking,
		ReasoningSummary: &summary,
		TextVerbosity:    &verbosity,
		DebugEnabled:     &debugEnabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if after.Provider != providerID || after.Model != modelID || after.Thinking != thinking ||
		after.ReasoningSummary != summary || after.TextVerbosity != verbosity || !after.DebugEnabled {
		t.Fatalf("updated live settings = %+v", after)
	}

	persisted, err := config.Load(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.DefaultProvider != defaultProvider || persisted.DefaultModel != defaultModel || persisted.Thinking != defaultThinking {
		t.Fatalf("global selection fallback changed: provider=%q model=%q thinking=%q", persisted.DefaultProvider, persisted.DefaultModel, persisted.Thinking)
	}
	selection, ok := persisted.ProjectSelections[a.CWD()]
	if !ok {
		t.Fatalf("project selection for %q was not persisted: %+v", a.CWD(), persisted.ProjectSelections)
	}
	if selection.Provider != providerID || selection.Model != modelID || selection.Thinking != string(thinking) {
		t.Fatalf("persisted project selection = %+v", selection)
	}
	if persisted.ReasoningSummary != string(summary) || persisted.TextVerbosity != string(verbosity) || !persisted.Debug.Enabled {
		t.Fatalf("persisted global choices = summary:%q verbosity:%q debug:%v", persisted.ReasoningSummary, persisted.TextVerbosity, persisted.Debug.Enabled)
	}

	reloaded, err := New(t.Context(), Options{
		ConfigPath: a.ConfigPath, CWD: a.CWD(), NoSession: true,
		Permission: "deny", NoMCP: true, NoPlugins: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reloaded.Close() })
	reloadedSettings, err := reloaded.RPCSettings()
	if err != nil {
		t.Fatal(err)
	}
	if reloadedSettings.Provider != providerID || reloadedSettings.Model != modelID || reloadedSettings.Thinking != thinking ||
		reloadedSettings.ReasoningSummary != summary || reloadedSettings.TextVerbosity != verbosity || !reloadedSettings.DebugEnabled {
		t.Fatalf("reloaded settings = %+v", reloadedSettings)
	}
}

func TestRPCSettingsPersistenceFailureRollsBackLiveChoices(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	beforeModel := a.Agent.Model()
	beforeThinking := a.Agent.Thinking()
	beforeSummary := a.Agent.ReasoningSummary()
	beforeVerbosity := a.Agent.TextVerbosity()
	beforeDebug := a.DebugStatus().Enabled
	a.ConfigPath = t.TempDir() // Loading a directory as the config must fail.

	modelID := "fake-2"
	thinking := protocol.ThinkingOff
	summary := protocol.ReasoningSummaryDetailed
	verbosity := protocol.TextVerbosityHigh
	debugEnabled := !beforeDebug
	if _, err := a.UpdateRPCSettingsContext(t.Context(), SettingsUpdate{
		Model:            &modelID,
		Thinking:         &thinking,
		ReasoningSummary: &summary,
		TextVerbosity:    &verbosity,
		DebugEnabled:     &debugEnabled,
	}); err == nil {
		t.Fatal("settings update unexpectedly persisted to a directory")
	}
	if got := a.Agent.Model(); got.ID != beforeModel.ID || got.Provider != beforeModel.Provider {
		t.Fatalf("model after rollback = %s/%s, want %s/%s", got.Provider, got.ID, beforeModel.Provider, beforeModel.ID)
	}
	if got := a.Agent.Thinking(); got != beforeThinking {
		t.Fatalf("thinking after rollback = %q, want %q", got, beforeThinking)
	}
	if got := a.Agent.ReasoningSummary(); got != beforeSummary {
		t.Fatalf("reasoning summary after rollback = %q, want %q", got, beforeSummary)
	}
	if got := a.Agent.TextVerbosity(); got != beforeVerbosity {
		t.Fatalf("text verbosity after rollback = %q, want %q", got, beforeVerbosity)
	}
	if got := a.DebugStatus().Enabled; got != beforeDebug {
		t.Fatalf("debug after failed persistence = %v, want %v", got, beforeDebug)
	}
}

func TestRPCSettingsProviderChangeRequiresModel(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	providerID := "missing-provider"
	if _, err := a.UpdateRPCSettingsContext(t.Context(), SettingsUpdate{Provider: &providerID}); err == nil {
		t.Fatal("provider-only cross-provider update accepted")
	}
}

func TestRPCSettingsModelEventCallbackCanReadSnapshot(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	a.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(a.ConfigPath, a.PersistedCfg); err != nil {
		t.Fatal(err)
	}
	callback := make(chan error, 1)
	unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
		if event.Type != protocol.EvModelChanged {
			return
		}
		_, err := a.RPCSettings()
		callback <- err
	})
	t.Cleanup(unsubscribe)
	modelID := "fake-2"
	thinking := protocol.ThinkingOff
	if _, err := a.UpdateRPCSettingsContext(t.Context(), SettingsUpdate{Model: &modelID, Thinking: &thinking}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-callback:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("model event callback could not read settings snapshot")
	}
}

func TestRPCSettingsUpdatePersistsStartupUpdateCheck(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	a.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(a.ConfigPath, a.PersistedCfg); err != nil {
		t.Fatal(err)
	}

	settings, err := a.UpdateRPCSettings(SettingsUpdate{UpdateCheckOnStartup: new(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.UpdateCheckOnStartup {
		t.Fatalf("startup update check was not enabled: %+v", settings)
	}

	settings, err = a.UpdateRPCSettings(SettingsUpdate{UpdateCheckOnStartup: new(false)})
	if err != nil {
		t.Fatal(err)
	}
	if settings.UpdateCheckOnStartup {
		t.Fatalf("startup update check was not disabled: %+v", settings)
	}
	persisted, err := config.Load(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Updates != (config.UpdateConfig{}) {
		t.Fatalf("persisted updates = %+v", persisted.Updates)
	}
}
