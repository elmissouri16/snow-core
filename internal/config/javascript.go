package config

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

// validChildPluginTool accepts only canonical plugin namespaces in role
// allowlists. Actual availability, child opt-in, fingerprint, and host-tool
// authority are checked against the loaded catalog when a child is spawned.
func validChildPluginTool(name string) bool {
	rest, ok := strings.CutPrefix(name, "plugin_")
	if !ok || len(rest) > 129 {
		return false
	}
	// Both identifiers may contain underscores, so accept any valid split.
	for i, r := range rest {
		if r == '_' && plugin.ValidateIdentifier("plugin", rest[:i]) == nil && plugin.ValidateIdentifier("tool", rest[i+1:]) == nil {
			return true
		}
	}
	return false
}

// UpdateJavaScriptPlugins edits only the new JS section using the shared lock.
func UpdateJavaScriptPlugins(path string, global bool, update func(map[string]plugin.JavaScriptSpec) error) error {
	return updateSection(path, global, "js_plugins", func(raw json.RawMessage) (json.RawMessage, error) {
		specs := map[string]plugin.JavaScriptSpec{}
		if len(raw) > 0 && string(raw) != "null" {
			if err := jsonv2.Unmarshal(raw, &specs); err != nil {
				return nil, err
			}
		}
		if err := update(specs); err != nil {
			return nil, err
		}
		return jsonv2.Marshal(specs)
	})
}
