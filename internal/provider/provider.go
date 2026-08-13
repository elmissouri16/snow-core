// Package provider defines the provider abstraction and the credential
// resolution contract. Adapters live in subpackages (opencodego, chatgpt, fake).
package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

// Provider is an LLM backend adapter.
// UsageLimitedError marks quota/rate-limit failures that should pause goal
// continuation without treating the objective as intrinsically blocked.
type UsageLimitedError interface {
	error
	UsageLimited() bool
}
type LimitError struct {
	Provider string
	Status   int
	Message  string
}

// ContextWindowExceededError marks a request rejected because its input context
// is too large. Agents may use it for one bounded compaction retry.
type ContextWindowExceededError interface {
	error
	ContextWindowExceeded() bool
}

// IsContextWindowExceeded conservatively recognizes structured markers and the
// bounded diagnostics retained by adapters that do not expose provider codes.
func IsContextWindowExceeded(err error) bool {
	if err == nil {
		return false
	}
	var marked ContextWindowExceededError
	if errors.As(err, &marked) && marked.ContextWindowExceeded() {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, phrase := range []string{"maximum context length", "context length exceeded", "context window exceeded", "context_length_exceeded", "context_window_exceeded", "prompt is too long", "prompt too long", "prompt_too_long", "input is too long", "input too long", "input_too_long", "too many tokens in prompt"} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s: usage limited (HTTP %d): %s", e.Provider, e.Status, e.Message)
}
func (*LimitError) UsageLimited() bool { return true }

// RedactSecrets removes exact active credentials from untrusted provider text
// before it reaches errors, events, or logs. Longer values are replaced first
// so overlapping credentials cannot leave a suffix behind.
func RedactSecrets(message string, secrets ...string) string {
	filtered := make([]string, 0, len(secrets))
	seen := make(map[string]bool, len(secrets))
	for _, secret := range secrets {
		if secret != "" && !seen[secret] {
			seen[secret] = true
			filtered = append(filtered, secret)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return len(filtered[i]) > len(filtered[j]) })
	for _, secret := range filtered {
		message = strings.ReplaceAll(message, secret, "[redacted]")
	}
	return message
}

// Provider is an LLM backend adapter.
type Provider interface {
	// ID returns the stable provider identifier, e.g. "opencode-go".
	ID() string

	// ListModels returns the model catalog for this provider.
	ListModels(ctx context.Context) ([]protocol.Model, error)

	// Resolve ensures credentials are valid (including OAuth refresh) and
	// returns the exact credential Chat must use for this request.
	Resolve(ctx context.Context, creds auth.Credential) (auth.Credential, error)

	// Chat starts a streaming chat. Callers must Close the returned stream.
	Chat(ctx context.Context, creds auth.Credential, req protocol.ChatRequest) (protocol.EventStream, error)
}
