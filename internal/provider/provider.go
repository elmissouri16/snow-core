// Package provider defines the provider abstraction and the credential
// resolution contract. Adapters live in subpackages (opencodego, chatgpt, fake).
package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
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

// TransientError marks a provider failure that may succeed when the same
// side-effect-free model request is attempted again. Tool execution is not
// covered by this marker and remains governed by the agent's unknown-outcome
// recovery rules.
type TransientError interface {
	error
	Transient() bool
}

// IsTransientError conservatively recognizes structured provider and network
// failures. Every member of an errors.Join value must be transient; a retryable
// provider failure joined with a persistence or accounting error is not safe to
// retry automatically. Caller cancellation and deadlines are never retried.
func IsTransientError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !IsTransientError(child) {
				return false
			}
		}
		return true
	}
	if marked, ok := err.(TransientError); ok {
		return marked.Transient()
	}
	if limited, ok := err.(UsageLimitedError); ok && limited.UsageLimited() {
		return false
	}
	if networkErr, ok := err.(net.Error); ok {
		return networkErr.Timeout() || networkErr.Temporary()
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return IsTransientError(wrapped.Unwrap())
	}
	return false
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

// Transport is a credential-aware provider adapter. It is intentionally kept
// below the authenticated runtime boundary so protocol packages can focus on
// vendor HTTP/SSE behavior while auth.Service owns credential selection.
type Transport interface {
	ID() string
	ListModels(ctx context.Context) ([]protocol.Model, error)
	Chat(ctx context.Context, creds auth.Credential, req protocol.ChatRequest) (protocol.EventStream, error)
}

// Provider is the credential-free interface consumed by the Snow agent.
// Implementations are normally produced by NewAuthenticated.
type Provider interface {
	ID() string
	ListModels(ctx context.Context) ([]protocol.Model, error)
	Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error)
}
