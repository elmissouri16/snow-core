package app

import (
	"context"
	"errors"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// SettingsUpdate contains the secret-free persisted settings exposed over RPC.
// Nil fields are left unchanged. Provider/model/response controls apply live;
// subagent and Skills manager changes remain restart-applied.
type SettingsUpdate struct {
	Provider               *string
	Model                  *string
	Thinking               *protocol.ThinkingLevel
	ReasoningSummary       *protocol.ReasoningSummary
	TextVerbosity          *protocol.TextVerbosity
	Theme                  *string
	DebugEnabled           *bool
	SubagentsEnabled       *bool
	SubagentsMaxConcurrent *int
	SkillsEnabled          *bool
}

// SettingsSnapshot is a secret-free view of the settings shown by the TUI.
// Runtime values use their existing live commands; restart-applied values show
// both the persisted target and whether the running process still differs.
type SettingsSnapshot struct {
	Provider                 string
	Model                    string
	Thinking                 protocol.ThinkingLevel
	ReasoningSummary         protocol.ReasoningSummary
	TextVerbosity            protocol.TextVerbosity
	Theme                    string
	PermissionMode           string
	DebugEnabled             bool
	SubagentsEnabled         bool
	SubagentConcurrent       int
	SubagentAgentLimit       int
	SkillsEnabled            bool
	SubagentsRestartRequired bool
	SkillsRestartRequired    bool
	RestartRequired          bool
}

type restartSettingsBaseline struct {
	subagentsEnabled bool
	concurrent       int
	agentLimit       int
	skillsEnabled    bool
}

// RPCSettings returns the current secret-free settings view.
func (a *App) RPCSettings() (SettingsSnapshot, error) {
	if a == nil || a.Agent == nil || a.Perm == nil {
		return SettingsSnapshot{}, errors.New("app: settings services unavailable")
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	a.ensureSettingsBaselineLocked()
	return a.settingsSnapshotLocked(), nil
}

// UpdateRPCSettings atomically persists a partial settings update and applies
// live settings to the running agent. Restart-applied manager settings retain
// their existing restart-required behavior.
func (a *App) UpdateRPCSettings(update SettingsUpdate) (SettingsSnapshot, error) {
	return a.UpdateRPCSettingsContext(context.Background(), update)
}

// UpdateRPCSettingsContext is the cancellation-aware settings mutation used by
// RPC commands that may need to discover an inactive provider catalog.
func (a *App) UpdateRPCSettingsContext(ctx context.Context, update SettingsUpdate) (SettingsSnapshot, error) {
	if a == nil || a.Agent == nil || a.Perm == nil {
		return SettingsSnapshot{}, errors.New("app: settings services unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if update.empty() {
		return SettingsSnapshot{}, errors.New("settings_update requires at least one setting")
	}
	a.settingsMutationMu.Lock()
	defer a.settingsMutationMu.Unlock()
	a.settingsMu.Lock()
	a.ensureSettingsBaselineLocked()
	a.settingsMu.Unlock()
	if err := a.applySettingsUpdate(ctx, update); err != nil {
		return SettingsSnapshot{}, err
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	return a.settingsSnapshotLocked(), nil
}

func (update SettingsUpdate) empty() bool {
	return update.Provider == nil && update.Model == nil && update.Thinking == nil &&
		update.ReasoningSummary == nil && update.TextVerbosity == nil && update.Theme == nil && update.DebugEnabled == nil &&
		update.SubagentsEnabled == nil && update.SubagentsMaxConcurrent == nil && update.SkillsEnabled == nil
}

func (a *App) ensureSettingsBaselineLocked() {
	if a.settingsBaseline != nil {
		return
	}
	a.settingsBaseline = &restartSettingsBaseline{
		subagentsEnabled: a.Cfg.Subagents.Enabled,
		concurrent:       a.Cfg.Subagents.MaxConcurrentThreads,
		agentLimit:       a.Cfg.Subagents.MaxAgentsPerSession,
		skillsEnabled:    !a.Cfg.Skills.Disabled,
	}
}

func (a *App) settingsSnapshotLocked() SettingsSnapshot {
	model := a.Agent.Model()
	subagentsRestart := a.Cfg.Subagents.Enabled != a.settingsBaseline.subagentsEnabled ||
		a.Cfg.Subagents.MaxConcurrentThreads != a.settingsBaseline.concurrent ||
		a.Cfg.Subagents.MaxAgentsPerSession != a.settingsBaseline.agentLimit
	skillsRestart := !a.Cfg.Skills.Disabled != a.settingsBaseline.skillsEnabled
	return SettingsSnapshot{
		Provider:                 model.Provider,
		Model:                    model.ID,
		Thinking:                 a.Agent.Thinking(),
		ReasoningSummary:         a.Agent.ReasoningSummary(),
		TextVerbosity:            a.Agent.TextVerbosity(),
		Theme:                    a.Cfg.TUI.Theme,
		PermissionMode:           string(a.Perm.Mode()),
		DebugEnabled:             a.DebugStatus().Enabled,
		SubagentsEnabled:         a.Cfg.Subagents.Enabled,
		SubagentConcurrent:       a.Cfg.Subagents.MaxConcurrentThreads,
		SubagentAgentLimit:       a.Cfg.Subagents.MaxAgentsPerSession,
		SkillsEnabled:            !a.Cfg.Skills.Disabled,
		SubagentsRestartRequired: subagentsRestart,
		SkillsRestartRequired:    skillsRestart,
		RestartRequired:          subagentsRestart || skillsRestart,
	}
}
