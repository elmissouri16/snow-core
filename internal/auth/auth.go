// Package auth defines the credential model and the credential store used
// by providers and the CLI/SDK login flows.
package auth

import "encoding/json"

// CredentialType enumerates supported credential kinds.
type CredentialType string

const (
	CredentialAPIKey CredentialType = "api_key"
	CredentialOAuth  CredentialType = "oauth"
)

// Credential is one provider's stored credential. It is also re-exported
// as provider.Credential for adapters.
type Credential struct {
	Provider string         `json:"-"`
	Type     CredentialType `json:"type"`
	Key      string         `json:"key,omitempty"`
	Access   string         `json:"access,omitempty"`
	Refresh  string         `json:"refresh,omitempty"`
	Expires  int64          `json:"expires,omitempty"` // unix seconds; 0 = unknown
	Extra    map[string]any `json:"extra,omitempty"`
}

// Valid reports whether the credential has at least one usable token.
func (c Credential) Valid() bool {
	switch c.Type {
	case CredentialAPIKey:
		return c.Key != ""
	case CredentialOAuth:
		return c.Access != ""
	}
	return false
}

// Store persists credentials keyed by provider.
type Store interface {
	Get(provider string) (Credential, bool)
	Put(provider string, cred Credential) error
	Delete(provider string) error
	// Path returns the store file path (for diagnostics; never print secrets).
	Path() string
}

// MarshalJSON redacts secret fields for safe logging.
func (c Credential) MarshalJSON() ([]byte, error) {
	type alias Credential
	a := alias(c)
	if a.Key != "" {
		a.Key = "[redacted]"
	}
	if a.Access != "" {
		a.Access = "[redacted]"
	}
	if a.Refresh != "" {
		a.Refresh = "[redacted]"
	}
	return json.Marshal(a)
}
