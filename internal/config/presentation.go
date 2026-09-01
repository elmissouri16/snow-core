package config

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// ResolvedTheme is a path-free semantic palette suitable for trusted host UIs.
// Colors are terminal-agnostic adaptive pairs; callers choose light or dark.
type ResolvedTheme struct {
	Name        string
	DisplayName string
	Scope       string // builtin, global, or project
	Colors      ThemeColors
}

var builtInThemeColors = map[string]ThemeColors{
	"default": {
		Accent: AdaptiveColor{Light: "#0969DA", Dark: "#58A6FF"}, Muted: AdaptiveColor{Light: "#57606A", Dark: "#8B949E"},
		Foreground: AdaptiveColor{Light: "#24292F", Dark: "#F0F6FC"}, Warning: AdaptiveColor{Light: "#9A6700", Dark: "#E3B341"},
		Error: AdaptiveColor{Light: "#CF222E", Dark: "#FF7B72"}, Success: AdaptiveColor{Light: "#1A7F37", Dark: "#7EE787"},
		Separator: AdaptiveColor{Light: "#8C959F", Dark: "#6E7681"},
	},
	"frost": {
		Accent: AdaptiveColor{Light: "#006A7A", Dark: "#67E8F9"}, Muted: AdaptiveColor{Light: "#52606D", Dark: "#94A3B8"},
		Foreground: AdaptiveColor{Light: "#172B4D", Dark: "#E6F6FF"}, Warning: AdaptiveColor{Light: "#8A4B00", Dark: "#FBBF24"},
		Error: AdaptiveColor{Light: "#B42318", Dark: "#FDA4AF"}, Success: AdaptiveColor{Light: "#166534", Dark: "#86EFAC"},
		Separator: AdaptiveColor{Light: "#8091A5", Dark: "#64748B"},
	},
	"ember": {
		Accent: AdaptiveColor{Light: "#9A3412", Dark: "#FDBA74"}, Muted: AdaptiveColor{Light: "#62564B", Dark: "#B8A99A"},
		Foreground: AdaptiveColor{Light: "#29211A", Dark: "#FFF7ED"}, Warning: AdaptiveColor{Light: "#7C4A03", Dark: "#FDE047"},
		Error: AdaptiveColor{Light: "#B42318", Dark: "#FB7185"}, Success: AdaptiveColor{Light: "#166534", Dark: "#86EFAC"},
		Separator: AdaptiveColor{Light: "#8A7968", Dark: "#7C6F64"},
	},
	"aurora": {
		Accent: AdaptiveColor{Light: "#6D28D9", Dark: "#C4B5FD"}, Muted: AdaptiveColor{Light: "#5B5668", Dark: "#A7A0B8"},
		Foreground: AdaptiveColor{Light: "#211B2E", Dark: "#FAF5FF"}, Warning: AdaptiveColor{Light: "#854D0E", Dark: "#FDE047"},
		Error: AdaptiveColor{Light: "#BE123C", Dark: "#FDA4AF"}, Success: AdaptiveColor{Light: "#166534", Dark: "#86EFAC"},
		Separator: AdaptiveColor{Light: "#877F96", Dark: "#756E86"},
	},
	"high-contrast": {
		Accent: AdaptiveColor{Light: "#004FB3", Dark: "#00D7FF"}, Muted: AdaptiveColor{Light: "#30363D", Dark: "#FFFFFF"},
		Foreground: AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}, Warning: AdaptiveColor{Light: "#6F4E00", Dark: "#FFFF00"},
		Error: AdaptiveColor{Light: "#A4001D", Dark: "#FF6B6B"}, Success: AdaptiveColor{Light: "#006B2D", Dark: "#00FF66"},
		Separator: AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"},
	},
}

// ResolveBuiltInTheme resolves current and legacy built-in names.
func ResolveBuiltInTheme(name string) (ResolvedTheme, error) {
	if name == "" {
		name = "default"
	}
	canonical := name
	switch name {
	case "dark", "light":
		canonical = "default"
	case "nord":
		canonical = "frost"
	case "gruvbox":
		canonical = "ember"
	case "dracula":
		canonical = "aurora"
	}
	colors, ok := builtInThemeColors[canonical]
	if !ok {
		return ResolvedTheme{}, fmt.Errorf("unsupported TUI theme %q", name)
	}
	return ResolvedTheme{Name: name, DisplayName: ThemeDisplayName(name), Scope: "builtin", Colors: colors}, nil
}

// ResolveCustomTheme overlays a validated custom descriptor on its built-in base.
func ResolveCustomTheme(custom ThemeFile, scope string) (ResolvedTheme, error) {
	if err := validateTheme(custom); err != nil {
		return ResolvedTheme{}, err
	}
	base, err := ResolveBuiltInTheme(custom.Extends)
	if err != nil {
		return ResolvedTheme{}, err
	}
	colors := base.Colors
	overlayAdaptive(&colors.Accent, custom.Colors.Accent)
	overlayAdaptive(&colors.Muted, custom.Colors.Muted)
	overlayAdaptive(&colors.Foreground, custom.Colors.Foreground)
	overlayAdaptive(&colors.Warning, custom.Colors.Warning)
	overlayAdaptive(&colors.Error, custom.Colors.Error)
	overlayAdaptive(&colors.Success, custom.Colors.Success)
	overlayAdaptive(&colors.Separator, custom.Colors.Separator)
	return ResolvedTheme{Name: custom.Name, DisplayName: custom.Name, Scope: scope, Colors: colors}, nil
}

func overlayAdaptive(target *AdaptiveColor, overlay AdaptiveColor) {
	if overlay.Light == "" && overlay.Dark == "" {
		return
	}
	if overlay.Light == "" {
		overlay.Light = overlay.Dark
	}
	if overlay.Dark == "" {
		overlay.Dark = overlay.Light
	}
	*target = overlay
}

// ThemeDisplayName is the stable human label for one palette.
func ThemeDisplayName(name string) string {
	switch name {
	case "default":
		return "Snow"
	case "frost":
		return "Frost"
	case "ember":
		return "Ember"
	case "aurora":
		return "Aurora"
	default:
		return name
	}
}

// ResolveThemes loads trusted custom themes and returns a bounded, path-free catalog.
func ResolveThemes(globalDir, projectRoot string, projectAllowed bool) ([]ResolvedTheme, []Diagnostic) {
	custom, diagnostics := LoadThemes(globalDir, projectRoot, projectAllowed)
	result := make([]ResolvedTheme, 0, len(builtInTUIThemes)+len(custom))
	for _, name := range builtInTUIThemes {
		resolved, _ := ResolveBuiltInTheme(name)
		result = append(result, resolved)
	}
	names := slices.Sorted(maps.Keys(custom))
	projectThemeDir := filepath.Clean(filepath.Join(projectRoot, ".snow", "themes"))
	for _, name := range names {
		theme := custom[name]
		scope := "global"
		if projectAllowed && filepath.Clean(filepath.Dir(theme.Path)) == projectThemeDir {
			scope = "project"
		}
		resolved, err := ResolveCustomTheme(theme, scope)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: "tui.theme", Message: err.Error()})
			continue
		}
		result = append(result, resolved)
	}
	return result, diagnostics
}

// KeybindingLayers is a detached global/project/effective snapshot.
type KeybindingLayers struct {
	Actions        []string
	Global         map[string][]string
	Project        map[string][]string
	Effective      map[string][]string
	ProjectAllowed bool
}

// ResolveKeybindings merges canonical defaults and overrides and validates contextual collisions.
func ResolveKeybindings(global, project map[string][]string, projectAllowed bool) (map[string][]string, error) {
	effective := defaultAuxBindings()
	for action, values := range global {
		effective[action] = slices.Clone(values)
	}
	if projectAllowed {
		for action, values := range project {
			effective[action] = slices.Clone(values)
		}
	}
	effective["abort"] = appendUnique(effective["abort"], "ctrl+c")
	effective["abort"] = appendUnique(effective["abort"], "esc")
	effective["quit"] = appendUnique(effective["quit"], "ctrl+c")
	effective["close"] = appendUnique(effective["close"], "esc")
	if err := validateEffectiveAuxBindings(effective); err != nil {
		return nil, err
	}
	return cloneBindings(effective), nil
}

// LoadKeybindingLayers returns path-free raw scopes and their effective map.
func LoadKeybindingLayers(globalDir, projectRoot string, projectAllowed bool) (KeybindingLayers, []Diagnostic) {
	global := map[string][]string{}
	project := map[string][]string{}
	scopes, diagnostics := LoadKeybindingScopes(globalDir, projectRoot, projectAllowed)
	globalPath := filepath.Clean(filepath.Join(globalDir, "keybindings.yaml"))
	projectPath := filepath.Clean(filepath.Join(projectRoot, ".snow", "keybindings.yaml"))
	for _, scope := range scopes {
		switch filepath.Clean(scope.Path) {
		case globalPath:
			global = cloneBindings(scope.File.Bindings)
		case projectPath:
			project = cloneBindings(scope.File.Bindings)
		}
	}
	effective, err := ResolveKeybindings(global, project, projectAllowed)
	if err != nil {
		diagnostics = append(diagnostics, Diagnostic{Path: "keybindings", Message: err.Error()})
		effective = defaultAuxBindings()
	}
	return KeybindingLayers{
		Actions: slices.Clone(KeybindingActions), Global: global, Project: project,
		Effective: effective, ProjectAllowed: projectAllowed,
	}, diagnostics
}

// ValidateKeybindingAction reports whether name belongs to the stable inventory.
func ValidateKeybindingAction(name string) error {
	if !slices.Contains(KeybindingActions, strings.TrimSpace(name)) {
		return fmt.Errorf("unknown keybinding action %q", name)
	}
	return nil
}
