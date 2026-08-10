package protocol

// ConfigDiagnostic is a non-fatal auxiliary configuration warning.
type ConfigDiagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}
