package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const maxRPCProviderIDBytes = 256

type settingsRuntimeState struct {
	provider         string
	model            protocol.Model
	thinking         protocol.ThinkingLevel
	reasoningSummary protocol.ReasoningSummary
	textVerbosity    protocol.TextVerbosity
}

func (a *App) applySettingsUpdate(ctx context.Context, update SettingsUpdate) error {
	normalized, err := normalizeSettingsUpdate(update)
	if err != nil {
		return err
	}
	if normalized.Theme != nil {
		if err := a.validateThemeSelection(*normalized.Theme); err != nil {
			return err
		}
	}
	providerID, model, _ := a.ActiveModelsSnapshot()
	before := settingsRuntimeState{
		provider:         providerID,
		model:            model,
		thinking:         a.Agent.Thinking(),
		reasoningSummary: a.Agent.ReasoningSummary(),
		textVerbosity:    a.Agent.TextVerbosity(),
	}
	selectionChanged := normalized.Provider != nil || normalized.Model != nil || normalized.Thinking != nil
	reasoningChanged := normalized.ReasoningSummary != nil
	verbosityChanged := normalized.TextVerbosity != nil

	if selectionChanged {
		if err := a.applyRuntimeSelection(ctx, before, normalized); err != nil {
			return err
		}
	}
	if reasoningChanged {
		if err := a.Agent.SetReasoningSummary(*normalized.ReasoningSummary); err != nil {
			return a.rollbackSettingsError(before, selectionChanged, false, false, err)
		}
	}
	if verbosityChanged {
		if err := a.Agent.SetTextVerbosity(*normalized.TextVerbosity); err != nil {
			return a.rollbackSettingsError(before, selectionChanged, reasoningChanged, false, err)
		}
	}

	candidate, err := a.persistSettingsUpdate(normalized, selectionChanged)
	if err != nil {
		return a.rollbackSettingsError(before, selectionChanged, reasoningChanged, verbosityChanged, fmt.Errorf("persist settings: %w", err))
	}

	a.settingsMu.Lock()
	a.PersistedCfg = candidate
	if selectionChanged {
		active := a.Agent.Model()
		a.Cfg.DefaultProvider = active.Provider
		a.Cfg.DefaultModel = active.ID
		a.Cfg.Thinking = string(a.Agent.Thinking())
	}
	if reasoningChanged {
		a.Cfg.ReasoningSummary = candidate.ReasoningSummary
	}
	if verbosityChanged {
		a.Cfg.TextVerbosity = candidate.TextVerbosity
	}
	if normalized.Theme != nil {
		a.Cfg.TUI.Theme = candidate.TUI.Theme
	}
	if normalized.DebugEnabled != nil {
		a.Cfg.Debug.Enabled = candidate.Debug.Enabled
	}
	a.Cfg.Subagents.Enabled = candidate.Subagents.Enabled
	a.Cfg.Subagents.MaxConcurrentThreads = candidate.Subagents.MaxConcurrentThreads
	a.Cfg.Subagents.MaxAgentsPerSession = candidate.Subagents.MaxAgentsPerSession
	a.Cfg.Skills.Disabled = candidate.Skills.Disabled
	a.settingsMu.Unlock()
	if normalized.DebugEnabled != nil {
		a.SetDebugEnabled(candidate.Debug.Enabled)
	}
	return nil
}

func normalizeSettingsUpdate(update SettingsUpdate) (SettingsUpdate, error) {
	if update.Provider != nil {
		providerID := strings.TrimSpace(*update.Provider)
		if providerID == "" {
			return SettingsUpdate{}, errors.New("settings_update provider must not be blank")
		}
		if len(providerID) > maxRPCProviderIDBytes {
			return SettingsUpdate{}, fmt.Errorf("settings_update provider exceeds %d bytes", maxRPCProviderIDBytes)
		}
		update.Provider = new(providerID)
	}
	if update.Model != nil {
		modelID := strings.TrimSpace(*update.Model)
		if modelID == "" {
			return SettingsUpdate{}, errors.New("settings_update model must not be blank")
		}
		if len(modelID) > protocol.MaxAgentMetadataBytes {
			return SettingsUpdate{}, fmt.Errorf("settings_update model exceeds %d bytes", protocol.MaxAgentMetadataBytes)
		}
		update.Model = new(modelID)
	}
	if update.Thinking != nil {
		value, err := protocol.ParseThinkingLevel(string(*update.Thinking))
		if err != nil {
			return SettingsUpdate{}, err
		}
		update.Thinking = new(value)
	}
	if update.ReasoningSummary != nil {
		value, err := protocol.ParseReasoningSummary(string(*update.ReasoningSummary))
		if err != nil {
			return SettingsUpdate{}, err
		}
		update.ReasoningSummary = new(value)
	}
	if update.TextVerbosity != nil {
		value, err := protocol.ParseTextVerbosity(string(*update.TextVerbosity))
		if err != nil {
			return SettingsUpdate{}, err
		}
		update.TextVerbosity = new(value)
	}
	if update.Theme != nil {
		value := strings.TrimSpace(*update.Theme)
		if value == "" {
			value = "default"
		}
		if err := config.ValidateTUITheme(value); err != nil {
			return SettingsUpdate{}, err
		}
		update.Theme = new(value)
	}
	return update, nil
}

func (a *App) validateThemeSelection(name string) error {
	themes, _ := config.ResolveThemes(config.GlobalDir(), a.ProjectInputRoot, a.ProjectAllowed)
	for _, theme := range themes {
		if theme.Name == name {
			return nil
		}
	}
	return fmt.Errorf("settings_update theme %q is not available", name)
}

func (a *App) applyRuntimeSelection(ctx context.Context, before settingsRuntimeState, update SettingsUpdate) error {
	targetProvider := before.provider
	if update.Provider != nil {
		targetProvider = *update.Provider
	}
	if targetProvider != before.provider && update.Model == nil {
		return errors.New("settings_update requires model when changing provider")
	}
	targetModel := before.model
	if update.Model != nil {
		targetModel = a.settingsModel(targetProvider, *update.Model)
	} else {
		targetModel.Provider = targetProvider
	}
	targetThinking := before.thinking
	if update.Thinking != nil {
		targetThinking = *update.Thinking
	} else if !targetModel.SupportsThinkingLevel(targetThinking) {
		targetThinking = protocol.ThinkingOff
	}
	if update.Provider != nil || update.Model != nil {
		if err := a.SetProviderModelThinkingContext(ctx, targetProvider, targetModel, targetThinking); err != nil {
			return err
		}
		return nil
	}
	return a.Agent.SetThinking(targetThinking)
}

func (a *App) settingsModel(providerID, modelID string) protocol.Model {
	a.stateMu.Lock()
	catalog := cloneModels(a.modelCatalog[providerID])
	a.stateMu.Unlock()
	for _, candidate := range catalog {
		if candidate.ID == modelID {
			return candidate.Clone()
		}
	}
	return protocol.Model{Provider: providerID, ID: modelID, SupportsTools: true}
}

func (a *App) persistSettingsUpdate(update SettingsUpdate, selectionChanged bool) (config.Config, error) {
	mutate := func(latest *config.Config) error {
		if selectionChanged {
			model := a.Agent.Model()
			candidate, err := config.WithProjectSelection(*latest, a.CWD(), config.ProjectSelection{
				Provider: model.Provider,
				Model:    model.ID,
				Thinking: string(a.Agent.Thinking()),
			})
			if err != nil {
				return err
			}
			*latest = candidate
		}
		if update.ReasoningSummary != nil {
			latest.ReasoningSummary = string(*update.ReasoningSummary)
		}
		if update.TextVerbosity != nil {
			latest.TextVerbosity = string(*update.TextVerbosity)
		}
		if update.Theme != nil {
			latest.TUI.Theme = *update.Theme
		}
		if update.DebugEnabled != nil {
			latest.Debug.Enabled = *update.DebugEnabled
		}
		if update.SubagentsEnabled != nil {
			latest.Subagents.Enabled = *update.SubagentsEnabled
		}
		if update.SubagentsMaxConcurrent != nil {
			latest.Subagents.MaxConcurrentThreads = *update.SubagentsMaxConcurrent
			latest.Subagents.MaxAgentsPerSession = max(latest.Subagents.MaxAgentsPerSession, *update.SubagentsMaxConcurrent)
		}
		if update.SkillsEnabled != nil {
			latest.Skills.Disabled = !*update.SkillsEnabled
		}
		return latest.Subagents.ValidateSubagents()
	}
	if a.ConfigPath != "" {
		return config.Update(a.ConfigPath, mutate)
	}
	a.settingsMu.Lock()
	candidate := a.PersistedCfg
	a.settingsMu.Unlock()
	if err := mutate(&candidate); err != nil {
		return config.Config{}, err
	}
	return candidate, nil
}

func (a *App) rollbackSettingsError(before settingsRuntimeState, selection, reasoning, verbosity bool, cause error) error {
	var rollbackErr error
	if selection {
		rollbackErr = errors.Join(rollbackErr, a.SetProviderModelThinking(before.provider, before.model, before.thinking))
	}
	if reasoning {
		rollbackErr = errors.Join(rollbackErr, a.Agent.SetReasoningSummary(before.reasoningSummary))
	}
	if verbosity {
		rollbackErr = errors.Join(rollbackErr, a.Agent.SetTextVerbosity(before.textVerbosity))
	}
	if rollbackErr != nil {
		return fmt.Errorf("%w (rollback failed: %v)", cause, rollbackErr)
	}
	return cause
}
