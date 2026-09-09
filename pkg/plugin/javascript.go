package plugin

import "encoding/json"

// JavaScriptSpec selects a local package. Paths are resolved by the declaring
// configuration scope; Config is copied into the plugin, never a Go object.
type JavaScriptSpec struct {
	Path     string          `json:"path"`
	Disabled bool            `json:"disabled,omitzero"`
	Config   json.RawMessage `json:"config,omitempty"`
}
