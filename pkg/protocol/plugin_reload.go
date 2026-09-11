package protocol

// PluginReloadResult distinguishes rejection (no replacement) from failures
// after the irreversible catalog commit. Applied results may carry diagnostics
// from old-runtime cleanup or replacement readiness; they are not rollbacks.
type PluginReloadResult struct {
	PluginID    string                   `json:"plugin_id"`
	Applied     bool                     `json:"applied"`
	Generation  uint64                   `json:"generation"`
	Fingerprint string                   `json:"fingerprint"`
	Diagnostics []PluginReloadDiagnostic `json:"diagnostics,omitempty"`
}
type PluginReloadDiagnostic struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
}
