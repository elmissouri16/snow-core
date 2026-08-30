// Package auth defines the credential model and the credential store used
// by providers and the CLI/SDK login flows.
package auth

import (
	"encoding/json"
	"strings"
	"unicode"
)

// CredentialType enumerates supported credential kinds.
type CredentialType string

const (
	CredentialAPIKey CredentialType = "api_key"
	CredentialOAuth  CredentialType = "oauth"
)

// Credential is one provider's stored credential. It is also re-exported
// as provider.Credential for adapters.
type Credential struct {
	Provider  string         `json:"-"`
	Type      CredentialType `json:"type"`
	Key       string         `json:"key,omitempty"`
	Access    string         `json:"access,omitempty"`
	Refresh   string         `json:"refresh,omitempty"`
	Expires   int64          `json:"expires,omitzero"`    // unix seconds; 0 = unknown
	AccountID string         `json:"accountId,omitempty"` // compatible with pi/Codex OAuth entries
	Extra     map[string]any `json:"extra,omitempty"`
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
// UpdateFunc atomically transforms one provider credential. save=false leaves
// the store unchanged and returns next to the caller (useful when a concurrent
// login already replaced an expired token).
type UpdateFunc func(current Credential, exists bool) (next Credential, save bool, err error)

// Store persists credentials keyed by provider.
type Store interface {
	Get(provider string) (Credential, bool)
	Put(provider string, cred Credential) error
	Delete(provider string) error
	// Update serializes the complete read/transform/write cycle. Implementations
	// may hold a cross-process lock while fn performs a bounded token refresh.
	Update(provider string, fn UpdateFunc) (Credential, bool, error)
	// Path returns the store file path (for diagnostics; never print secrets).
	Path() string
}

// RefreshLockStore optionally serializes provider token refreshes separately
// from the credential-file read/write lock. Network refresh can then proceed
// without blocking unrelated credential operations while remaining safe for
// one-time refresh-token rotation across processes.
type RefreshLockStore interface {
	WithRefreshLock(provider string, fn func() error) error
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
	a.Extra = redactExtra(a.Extra)
	return json.Marshal(a)
}

func redactExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	out := make(map[string]any, len(extra))
	for key, value := range extra {
		if secretExtraKey(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = redactExtraValue(value)
	}
	return out
}

func redactExtraValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return redactExtra(value)
	case map[string]string:
		out := make(map[string]any, len(value))
		for key, nested := range value {
			if secretExtraKey(key) {
				out[key] = "[redacted]"
			} else {
				out[key] = nested
			}
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = redactExtraValue(value[i])
		}
		return out
	default:
		return value
	}
}

func secretExtraKey(key string) bool {
	var normalized strings.Builder
	for i, r := range key {
		if r == '_' || r == '-' || r == '.' || unicode.IsSpace(r) {
			normalized.WriteByte(' ')
			continue
		}
		if i > 0 && unicode.IsUpper(r) {
			normalized.WriteByte(' ')
		}
		normalized.WriteRune(unicode.ToLower(r))
	}
	for word := range strings.FieldsSeq(normalized.String()) {
		switch word {
		case "token", "secret", "password", "passwd", "credential", "authorization", "cookie", "key":
			return true
		}
	}
	return false
}
