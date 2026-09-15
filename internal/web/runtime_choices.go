package web

// RuntimeModelChoice is a bounded, explicitly discovered provider/model pair.
type RuntimeModelChoice struct {
	Provider      string `json:"provider"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int    `json:"context_window"`
}

// RuntimeSessionChoice contains only public metadata from the worker's current-CWD inventory.
type RuntimeSessionChoice struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	UpdatedAt int64  `json:"updated_at"`
	Active    bool   `json:"active"`
}

// RuntimeChoices is refreshed only by explicit discovery, never by snapshot polling.
type RuntimeChoices struct {
	ProjectID         string                 `json:"project_id"`
	InstanceID        string                 `json:"instance_id"`
	Models            []RuntimeModelChoice   `json:"models"`
	ModelsPartial     bool                   `json:"models_partial"`
	ModelsTruncated   bool                   `json:"models_truncated"`
	Sessions          []RuntimeSessionChoice `json:"sessions"`
	SessionsAvailable bool                   `json:"sessions_available"`
	SessionsTruncated bool                   `json:"sessions_truncated"`
	Telemetry         *RuntimeTelemetry      `json:"telemetry"`
}

// RuntimeTelemetry excludes context category text and private metadata.
// Cost is a recorded estimate which may omit unpriced requests, not a bill.
// Available describes usage totals; ContextAvailable separately describes the context estimate.
type RuntimeTelemetry struct {
	Cost             *RuntimeCost `json:"cost,omitempty"`
	InputTokens      int          `json:"input_tokens"`
	OutputTokens     int          `json:"output_tokens"`
	TotalTokens      int          `json:"total_tokens"`
	ContextTokens    int          `json:"context_tokens"`
	ContextWindow    int          `json:"context_window"`
	Estimated        bool         `json:"estimated"`
	Available        bool         `json:"available"`
	ContextAvailable bool         `json:"context_available"`
}
