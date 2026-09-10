package app

import "github.com/elmissouri16/snow-core/internal/config"

// TerminalSettingsUpdate changes global presentation preferences only. Nil
// fields preserve both concurrent disk changes and runtime-only overrides.
type TerminalSettingsUpdate struct {
	Title         *bool
	Progress      *bool
	Notifications *string
}

// UpdateTerminalSettings persists only the changed fields before applying them
// live, preserving unrelated preferences from concurrent Snow processes.
func (a *App) UpdateTerminalSettings(update TerminalSettingsUpdate) error {
	if update.Notifications != nil {
		if err := config.ValidateTerminalNotifications(*update.Notifications); err != nil {
			return err
		}
	}
	a.settingsMutationMu.Lock()
	defer a.settingsMutationMu.Unlock()
	mutate := func(cfg *config.Config) error {
		if update.Title != nil {
			cfg.TUI.TerminalTitle = *update.Title
		}
		if update.Progress != nil {
			cfg.TUI.TerminalProgress = *update.Progress
		}
		if update.Notifications != nil {
			cfg.TUI.Notifications = *update.Notifications
		}
		return nil
	}
	a.settingsMu.Lock()
	candidate := a.PersistedCfg
	a.settingsMu.Unlock()
	if a.ConfigPath != "" {
		var err error
		candidate, err = config.Update(a.ConfigPath, mutate)
		if err != nil {
			return err
		}
	} else {
		_ = mutate(&candidate)
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	a.PersistedCfg = candidate
	return mutate(&a.Cfg)
}
