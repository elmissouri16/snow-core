package protocol

// PluginStatus separates the saved JavaScript registration from the immutable
// running catalog. Changes to Enabled take effect on the next launch.
type PluginStatus struct {
	ID              string `json:"id"`
	Path            string `json:"path"`
	Scope           string `json:"scope"`
	Enabled         bool   `json:"enabled"`
	Loaded          bool   `json:"loaded"`
	CanToggle       bool   `json:"can_toggle"`
	RestartRequired bool   `json:"restart_required"`
}
