package javascript

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func validateExtensionManifest(m Manifest) error {
	if m.APIVersion == 1 && (len(m.Capabilities) > 0 || len(m.Settings) > 0) {
		return errors.New("capabilities and settings require JavaScript API 2")
	}
	seen := map[string]bool{}
	for _, capability := range m.Capabilities {
		if !slices.Contains([]string{"commands", "hooks", "ui", "tools", "agent", "session", "goals", "subagents", "storage"}, capability) || seen[capability] {
			return fmt.Errorf("invalid or duplicate capability %q", capability)
		}
		seen[capability] = true
	}
	if len(m.Settings) > 64 {
		return errors.New("settings limit exceeded")
	}
	clear(seen)
	for _, setting := range m.Settings {
		if err := plugin.ValidateIdentifier("setting", setting.Name); err != nil {
			return err
		}
		if seen[setting.Name] {
			return errors.New("duplicate setting")
		}
		seen[setting.Name] = true
		if !slices.Contains([]string{"string", "number", "boolean", "enum"}, setting.Type) {
			return errors.New("unsupported setting type")
		}
		if setting.Type == "enum" && (len(setting.Choices) == 0 || len(setting.Choices) > 64) {
			return errors.New("enum requires 1..64 choices")
		}
	}
	return nil
}
func applySettingDefaults(settings []protocol.PluginSetting, config map[string]any) error {
	for _, setting := range settings {
		value, present := config[setting.Name]
		if !present && len(setting.Default) > 0 {
			if err := jsonv2.Unmarshal(setting.Default, &value); err != nil {
				return fmt.Errorf("setting %s: %w", setting.Name, err)
			}
			config[setting.Name], present = value, true
		}
		if !present {
			continue
		}
		valid := false
		switch setting.Type {
		case "string":
			_, valid = value.(string)
		case "number":
			_, valid = value.(float64)
		case "boolean":
			_, valid = value.(bool)
		case "enum":
			text, ok := value.(string)
			valid = ok && slices.Contains(setting.Choices, text)
		}
		if !valid {
			return fmt.Errorf("setting %s requires %s", setting.Name, setting.Type)
		}
	}
	return nil
}

var themeColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validateTheme(theme protocol.PluginTheme) error {
	if err := plugin.ValidateIdentifier("theme", theme.Name); err != nil {
		return err
	}
	roles := []string{"accent", "muted", "foreground", "warning", "error", "success", "separator"}
	if len(theme.Colors) != len(roles) {
		return errors.New("theme requires all seven semantic colors")
	}
	for _, role := range roles {
		color := theme.Colors[role]
		if !themeColor.MatchString(color.Light) || !themeColor.MatchString(color.Dark) {
			return fmt.Errorf("theme %s requires light/dark #RRGGBB", role)
		}
	}
	return nil
}

// ValidateSettings checks the same typed values used at package startup.
func ValidateSettings(settings []protocol.PluginSetting, values map[string]any) error {
	return applySettingDefaults(settings, values)
}
