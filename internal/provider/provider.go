// Package provider defines the provider abstraction and the credential
// resolution contract. Adapters live in subpackages (opencodego, chatgpt, fake).
package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Provider is an LLM backend adapter.
// UsageLimitedError marks terminal quota/payment failures that should stop goal
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

// RetryKind distinguishes temporary transport outages from temporary provider
// throttling. Terminal quota/payment failures continue to use LimitError.
type RetryKind string

const (
	RetryTransient RetryKind = "transient"
	RetryRateLimit RetryKind = "rate_limit"
)

// RetryAdvice is safe, bounded scheduling metadata attached to a provider
// failure. RetryAfter is a minimum delay; the agent may wait longer because of
// its own backoff policy.
type RetryAdvice struct {
	Kind       RetryKind
	RetryAfter time.Duration
}

// RetryableError exposes structured scheduling advice without requiring callers
// to parse untrusted provider error strings.
type RetryableError interface {
	error
	RetryAdvice() RetryAdvice
}

// RateLimitError represents temporary throttling, as distinct from exhausted
// quota or payment limits. Provider diagnostics must already be redacted and
// bounded before constructing this value.
type RateLimitError struct {
	Provider   string
	Status     int
	Message    string
	RetryAfter time.Duration
}

// AdvisedError attaches retry metadata while preserving the original cause for
// errors.Is/errors.As and cancellation-safe joined-error classification.
type AdvisedError struct {
	Err    error
	Advice RetryAdvice
}

// CauseError preserves errors.Is/errors.As while keeping a separately redacted
// public diagnostic instead of delegating Error() to an untrusted cause.
type CauseError struct {
	Message string
	Cause   error
}

func (e *CauseError) Error() string {
	if e == nil || e.Message == "" {
		return "provider request failed"
	}
	return e.Message
}
func (e *CauseError) Unwrap() error { return e.Cause }

func (e *AdvisedError) Error() string {
	if e == nil || e.Err == nil {
		return "provider request failed"
	}
	return e.Err.Error()
}
func (e *AdvisedError) Unwrap() error            { return e.Err }
func (e *AdvisedError) RetryAdvice() RetryAdvice { return e.Advice }

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

// RetryAdviceFor conservatively recognizes structured provider and network
// failures. Every member of an errors.Join value must be retryable; a provider
// failure joined with persistence or accounting failure is not safe to retry.
// Caller cancellation and deadlines are never retried.
func RetryAdviceFor(err error) (RetryAdvice, bool) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return RetryAdvice{}, false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return RetryAdvice{}, false
		}
		advice := RetryAdvice{Kind: RetryTransient}
		for _, child := range children {
			childAdvice, retryable := RetryAdviceFor(child)
			if !retryable {
				return RetryAdvice{}, false
			}
			if childAdvice.Kind == RetryRateLimit {
				advice.Kind = RetryRateLimit
			}
			if childAdvice.RetryAfter > advice.RetryAfter {
				advice.RetryAfter = childAdvice.RetryAfter
			}
		}
		return advice, true
	}
	if marked, ok := err.(RetryableError); ok {
		advice := marked.RetryAdvice()
		if advice.Kind != RetryTransient && advice.Kind != RetryRateLimit {
			return RetryAdvice{}, false
		}
		if advice.RetryAfter < 0 {
			advice.RetryAfter = 0
		}
		return advice, true
	}
	if limited, ok := err.(UsageLimitedError); ok && limited.UsageLimited() {
		return RetryAdvice{}, false
	}
	if marked, ok := err.(TransientError); ok && marked.Transient() {
		return RetryAdvice{Kind: RetryTransient}, true
	}
	if networkErr, ok := err.(net.Error); ok && (networkErr.Timeout() || networkErr.Temporary()) {
		return RetryAdvice{Kind: RetryTransient}, true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return RetryAdviceFor(wrapped.Unwrap())
	}
	return RetryAdvice{}, false
}

// IsTransientError is the compatibility predicate for temporary transport and
// overload failures. Temporary throttling is intentionally excluded so goal
// exhaustion can classify it separately from an outage.
func IsTransientError(err error) bool {
	advice, ok := RetryAdviceFor(err)
	return ok && advice.Kind == RetryTransient
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

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s: temporarily rate limited (HTTP %d): %s", e.Provider, e.Status, e.Message)
}
func (e *RateLimitError) RetryAdvice() RetryAdvice {
	return RetryAdvice{Kind: RetryRateLimit, RetryAfter: e.RetryAfter}
}

// HTTPRetryAdvice classifies temporary HTTP failures. HTTP 402 and other
// terminal quota/auth/validation responses intentionally return false.
func HTTPRetryAdvice(status int, header http.Header, now time.Time, maxRetryAfter time.Duration) (RetryAdvice, bool) {
	kind := RetryTransient
	switch {
	case status == http.StatusTooManyRequests:
		kind = RetryRateLimit
	case status == http.StatusRequestTimeout || status == http.StatusTooEarly || status >= 500 && status <= 599:
	default:
		return RetryAdvice{}, false
	}
	return RetryAdvice{Kind: kind, RetryAfter: ParseRetryAfter(header, now, maxRetryAfter)}, true
}

// ParseRetryAfter accepts the standard Retry-After seconds/date forms and the
// common retry-after-ms extension. Values are clamped to max when max is
// positive; malformed, negative, and past values are ignored.
func ParseRetryAfter(header http.Header, now time.Time, max time.Duration) time.Duration {
	if header == nil {
		return 0
	}
	var delay time.Duration
	if raw := strings.TrimSpace(header.Get("retry-after-ms")); raw != "" {
		if millis, err := strconv.ParseInt(raw, 10, 64); err == nil && millis > 0 {
			if millis > int64((1<<63-1)/time.Millisecond) {
				delay = time.Duration(1<<63 - 1)
			} else {
				delay = time.Duration(millis) * time.Millisecond
			}
		}
	}
	if raw := strings.TrimSpace(header.Get("Retry-After")); raw != "" {
		var standard time.Duration
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
			if seconds > int64((1<<63-1)/time.Second) {
				standard = time.Duration(1<<63 - 1)
			} else {
				standard = time.Duration(seconds) * time.Second
			}
		} else if when, err := http.ParseTime(raw); err == nil && when.After(now) {
			standard = when.Sub(now)
		}
		if standard > delay {
			delay = standard
		}
	}
	if max > 0 && delay > max {
		return max
	}
	return delay
}

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
