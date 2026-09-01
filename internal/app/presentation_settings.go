package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RPCThemes returns the bounded path-free palette catalog available to this runtime.
func (a *App) RPCThemes() (protocol.RPCThemeCatalog, error) {
	if a == nil {
		return protocol.RPCThemeCatalog{}, errors.New("app: theme services unavailable")
	}
	themes, _ := config.ResolveThemes(config.GlobalDir(), a.ProjectInputRoot, a.ProjectAllowed)
	catalog := protocol.RPCThemeCatalog{Selected: a.Cfg.TUI.Theme, Themes: make([]protocol.RPCThemeDescriptor, 0, len(themes))}
	for _, theme := range themes {
		catalog.Themes = append(catalog.Themes, rpcThemeDescriptor(theme))
	}
	return catalog, nil
}

func rpcThemeDescriptor(theme config.ResolvedTheme) protocol.RPCThemeDescriptor {
	pair := func(color config.AdaptiveColor) protocol.RPCAdaptiveColor {
		return protocol.RPCAdaptiveColor{Light: color.Light, Dark: color.Dark}
	}
	return protocol.RPCThemeDescriptor{
		Name: theme.Name, DisplayName: theme.DisplayName, Scope: theme.Scope,
		Colors: protocol.RPCThemeColors{
			Accent: pair(theme.Colors.Accent), Muted: pair(theme.Colors.Muted),
			Foreground: pair(theme.Colors.Foreground), Warning: pair(theme.Colors.Warning),
			Error: pair(theme.Colors.Error), Success: pair(theme.Colors.Success),
			Separator: pair(theme.Colors.Separator),
		},
	}
}

// RPCKeybindings returns the canonical layered 31-action binding catalog.
func (a *App) RPCKeybindings() (protocol.RPCKeybindings, error) {
	if a == nil {
		return protocol.RPCKeybindings{}, errors.New("app: keybinding services unavailable")
	}
	layers, _ := config.LoadKeybindingLayers(config.GlobalDir(), a.ProjectInputRoot, a.ProjectAllowed)
	return rpcKeybindings(layers), nil
}

func rpcKeybindings(layers config.KeybindingLayers) protocol.RPCKeybindings {
	result := protocol.RPCKeybindings{ProjectAllowed: layers.ProjectAllowed, Actions: make([]protocol.RPCKeybindingAction, 0, len(layers.Actions))}
	for _, name := range layers.Actions {
		source := "default"
		if _, ok := layers.Global[name]; ok {
			source = "global"
		}
		if layers.ProjectAllowed {
			if _, ok := layers.Project[name]; ok {
				source = "project"
			}
		}
		result.Actions = append(result.Actions, protocol.RPCKeybindingAction{
			Name: name, Global: cloneRPCKeybindings(layers.Global[name]), Project: cloneRPCKeybindings(layers.Project[name]),
			Effective: cloneRPCKeybindings(layers.Effective[name]), Source: source,
		})
	}
	return result
}

func cloneRPCKeybindings(bindings []string) []string {
	if bindings == nil {
		return []string{}
	}
	return slices.Clone(bindings)
}

// UpdateRPCKeybindings atomically applies one scoped partial mutation.
func (a *App) UpdateRPCKeybindings(params protocol.RPCKeybindingsUpdateParams) (protocol.RPCKeybindings, error) {
	if a == nil {
		return protocol.RPCKeybindings{}, errors.New("app: keybinding services unavailable")
	}
	if params.Scope != "global" && params.Scope != "project" {
		return protocol.RPCKeybindings{}, errors.New("keybindings_update scope must be global or project")
	}
	if params.Scope == "project" && !a.ProjectAllowed {
		return protocol.RPCKeybindings{}, errors.New("keybindings_update project scope requires a trusted project")
	}
	if len(params.Bindings) == 0 && len(params.Reset) == 0 {
		return protocol.RPCKeybindings{}, errors.New("keybindings_update requires bindings or reset")
	}
	if len(params.Bindings) > len(config.KeybindingActions) || len(params.Reset) > len(config.KeybindingActions) {
		return protocol.RPCKeybindings{}, errors.New("keybindings_update exceeds the action limit")
	}
	for action, values := range params.Bindings {
		if err := config.ValidateKeybindingAction(action); err != nil {
			return protocol.RPCKeybindings{}, err
		}
		if len(values) == 0 {
			return protocol.RPCKeybindings{}, fmt.Errorf("keybindings_update action %q cannot be empty; use reset", action)
		}
		if slices.Contains(params.Reset, action) {
			return protocol.RPCKeybindings{}, fmt.Errorf("keybindings_update action %q is both bound and reset", action)
		}
	}
	for _, action := range params.Reset {
		if err := config.ValidateKeybindingAction(action); err != nil {
			return protocol.RPCKeybindings{}, err
		}
	}

	globalRoot := config.GlobalDir()
	scope := config.KeybindingWriteScope{
		Path: filepath.Join(globalRoot, "keybindings.yaml"), ConfinedRoot: globalRoot, Global: true,
		CoordinationRoot: globalRoot, CoordinationPath: filepath.Join(globalRoot, "keybindings.yaml"),
	}
	if params.Scope == "project" {
		scope = config.KeybindingWriteScope{
			Path: filepath.Join(a.ProjectInputRoot, ".snow", "keybindings.yaml"), ConfinedRoot: a.ProjectInputRoot,
			CoordinationRoot: globalRoot, CoordinationPath: filepath.Join(globalRoot, "keybindings.yaml"),
		}
	}
	scope.Validate = func(candidate config.KeybindingsFile) error {
		latest, _ := config.LoadKeybindingLayers(globalRoot, a.ProjectInputRoot, a.ProjectAllowed)
		global, project := latest.Global, latest.Project
		if params.Scope == "global" {
			global = candidate.Bindings
		} else {
			project = candidate.Bindings
		}
		_, err := config.ResolveKeybindings(global, project, a.ProjectAllowed)
		return err
	}
	_, err := config.UpdateKeybindings(scope, func(file *config.KeybindingsFile) error {
		if file.Bindings == nil {
			file.Bindings = map[string][]string{}
		}
		for _, action := range params.Reset {
			delete(file.Bindings, action)
		}
		for action, values := range params.Bindings {
			file.Bindings[action] = slices.Clone(values)
		}
		return nil
	})
	if err != nil {
		return protocol.RPCKeybindings{}, err
	}
	return a.RPCKeybindings()
}
