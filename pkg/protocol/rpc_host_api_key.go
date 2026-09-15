package protocol

// HostAPIKeyInspectRequest asks only for local capability/status and an opaque
// file-metadata CAS revision. It never returns an API key.
type HostAPIKeyInspectRequest struct {
	ProviderID string `json:"provider_id"`
}

// HostAPIKeySetRequest is secret input, not a response or diagnostic value.
// Transports must never log/echo it or serialize it to any public event stream.
// Secret serialization is necessary only on the explicitly trusted local RPC
// transport; public HTTP must reject this operation before parsing its body.
type HostAPIKeySetRequest struct {
	ProviderID       string `json:"provider_id"`
	ExpectedRevision string `json:"expected_revision"`
	Secret           string `json:"secret"`
	ConfirmReplace   bool   `json:"confirm_replace"`
}

// String and GoString prevent accidental printf-style request disclosure.
// They are not a substitute for excluding requests from JSON/event logging.
func (HostAPIKeySetRequest) String() string   { return "HostAPIKeySetRequest{[redacted]}" }
func (HostAPIKeySetRequest) GoString() string { return "HostAPIKeySetRequest{[redacted]}" }

// HostAPIKeyStatusResponse contains only local capability/status and opaque
// metadata revision; neither the stored nor the submitted credential is echoed.
// A revision is "missing" for an absent auth file, otherwise 64 lowercase hex
// characters derived solely from file metadata (not file contents).
type HostAPIKeyStatusResponse struct {
	ProviderID      string             `json:"provider_id"`
	APIKeySupported bool               `json:"api_key_supported"`
	ReplaceRequired bool               `json:"replace_required"`
	Revision        string             `json:"revision"`
	Status          HostProviderStatus `json:"status"`
	CheckedLocally  bool               `json:"checked_locally"`
	AppliesTo       string             `json:"applies_to"` // future_runtime; restart existing workers
}
