package protocol

// HostDefaultsRequest selects operator-owned defaults, never a project file.
// CWD is required only for project scope and is canonicalized on the host.
type HostDefaultsRequest struct {
	Scope string `json:"scope"`
	CWD   string `json:"cwd,omitempty"`
}

type HostProviderModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// HostStringDefault exposes only an allowed value and its inheritance source.
// A nil Explicit means this scope inherits. Source is builtin, global or project.
type HostStringDefault struct {
	Explicit  *string `json:"explicit"`
	Effective string  `json:"effective"`
	Source    string  `json:"source"`
}
type HostProviderModelDefault struct {
	Explicit  *HostProviderModel `json:"explicit"`
	Effective HostProviderModel  `json:"effective"`
	Source    string             `json:"source"`
}
type HostProjectDefaults struct {
	ProviderModel HostProviderModelDefault `json:"provider_model"`
	Thinking      HostStringDefault        `json:"thinking"`
}
type HostGlobalDefaults struct {
	ProviderModel    HostProviderModelDefault `json:"provider_model"`
	Thinking         HostStringDefault        `json:"thinking"`
	ReasoningSummary HostStringDefault        `json:"reasoning_summary"`
	TextVerbosity    HostStringDefault        `json:"text_verbosity"`
}
type HostDefaultsResponse struct {
	Scope     string               `json:"scope"`
	CWD       string               `json:"cwd,omitempty"`
	Revision  string               `json:"revision"`
	AppliesTo string               `json:"applies_to"` // future_runtime, not a new conversation in an existing worker
	Global    *HostGlobalDefaults  `json:"global,omitempty"`
	Project   *HostProjectDefaults `json:"project,omitempty"`
}

// Omitted operations leave values unchanged. Op is set or reset. Reset removes
// the explicit scope value and must not carry Value; set requires Value.
type HostStringOperation struct {
	Op    string  `json:"op"`
	Value *string `json:"value,omitempty"`
}
type HostProviderModelOperation struct {
	Op    string             `json:"op"`
	Value *HostProviderModel `json:"value,omitempty"`
}
type HostProjectDefaultsPatch struct {
	ProviderModel *HostProviderModelOperation `json:"provider_model,omitempty"`
	Thinking      *HostStringOperation        `json:"thinking,omitempty"`
}
type HostGlobalDefaultsPatch struct {
	ProviderModel    *HostProviderModelOperation `json:"provider_model,omitempty"`
	Thinking         *HostStringOperation        `json:"thinking,omitempty"`
	ReasoningSummary *HostStringOperation        `json:"reasoning_summary,omitempty"`
	TextVerbosity    *HostStringOperation        `json:"text_verbosity,omitempty"`
}
type HostDefaultsUpdateRequest struct {
	Scope    string                    `json:"scope"`
	CWD      string                    `json:"cwd,omitempty"`
	Revision string                    `json:"revision"`
	Global   *HostGlobalDefaultsPatch  `json:"global,omitempty"`
	Project  *HostProjectDefaultsPatch `json:"project,omitempty"`
}

// HostProviderStatus is intentionally not the richer CLI authentication status.
// CheckedLocally never implies remote acceptance. No account, expiration,
// refreshability, environment, headers, endpoints or credential data is exposed.
type HostProviderStatus struct {
	ProviderID     string `json:"provider_id"`
	State          string `json:"state"`  // configured, expired, unavailable
	Reason         string `json:"reason"` // fixed local reason code
	CheckedLocally bool   `json:"checked_locally"`
}
type HostProviderStatusResponse struct {
	Providers      []HostProviderStatus `json:"providers"`
	CheckedLocally bool                 `json:"checked_locally"`
}
