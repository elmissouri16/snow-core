package tui

import (
	"slices"

	"github.com/charmbracelet/bubbles/key"
	"github.com/elmissouri16/snow-core/internal/config"
)

func (m *Model) pluginKeybindingActions() []keybindingAction {
	if m.plugins == nil {
		return keybindingActions
	}
	actions := slices.Clone(keybindingActions)
	for _, command := range m.plugins.commands {
		actions = append(actions, keybindingAction{name: "plugin:" + command.ID, label: command.ID, group: "Plugins"})
	}
	return actions
}
func (m *Model) pluginDefaultKeys() tuiKeyMap {
	keys := tuiKeys
	keys.Plugins = map[string]key.Binding{}
	if m.plugins != nil {
		for _, command := range m.plugins.commands {
			if command.Shortcut != "" {
				candidate, err := applyKeybindingOverrides(keys, map[string][]string{"plugin:" + command.ID: {command.Shortcut}})
				if err == nil {
					keys = candidate
				}
			}
		}
	}
	return keys
}
func (m *Model) defaultBinding(action string) []string {
	if config.PluginKeybindingAction(action) {
		return slices.Clone(m.pluginDefaultKeys().Plugins[action].Keys())
	}
	return slices.Clone(config.DefaultKeybindings()[action])
}
