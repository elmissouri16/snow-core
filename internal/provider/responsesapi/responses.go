package responsesapi

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"

	providerpkg "github.com/snow-core/snow/internal/provider"
)

const (
	maxCodexSSELineBytes           = 4 << 20
	maxCodexSSEEventBytes          = 8 << 20
	maxCodexSSEEventFragments      = 4096
	maxCodexToolArgumentBytes      = 1 << 20
	maxCodexTotalToolArgumentBytes = 4 << 20
	maxCodexStreamToolCalls        = 128
	maxCodexIdentityBytes          = 4096
	maxCodexReasoningBytes         = 4 << 20
	maxCodexReasoningItems         = 128
	maxResponseTextBytes           = 16 << 20
	maxStreamErrorBytes            = 500
)

// RequestOptions selects provider-specific optional Responses fields.
type RequestOptions struct {
	ProviderID                string
	IncludeEncryptedReasoning bool
	AllowLegacyVerbosity      bool
	PromptCacheKey            string
	ToolChoice                string
	ParallelToolCalls         *bool
	OmitMaxOutputTokens       bool
	OmitTemperature           bool
}

func providerLabel(id string) string {
	if strings.TrimSpace(id) == "" {
		return "responses"
	}
	return id
}

// ResponseError preserves bounded provider diagnostics without retaining
// request payloads or credentials. Adapters may use the structured fields for
// retry classification while callers receive the same safe Error string.
type ResponseError struct {
	Provider  string
	Message   string
	Code      string
	RequestID string
	Status    int
	Attempts  int
}

func (e *ResponseError) ContextWindowExceeded() bool {
	if e == nil {
		return false
	}
	code := strings.ToLower(strings.TrimSpace(e.Code))
	switch code {
	case "context_length_exceeded", "context_window_exceeded", "prompt_too_long", "input_too_long":
		return true
	}
	message := strings.ToLower(e.Message)
	for _, phrase := range []string{"maximum context length", "context length exceeded", "context window exceeded", "prompt is too long", "prompt too long", "input is too long", "input too long", "too many tokens in prompt"} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

// Transient reports whether retrying a side-effect-free provider request is
// appropriate. Quota/rate-limit responses are deliberately excluded; adapters
// expose those through provider.UsageLimitedError instead.
func (e *ResponseError) Transient() bool {
	if e == nil {
		return false
	}
	switch e.Status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	code := strings.ToLower(strings.TrimSpace(e.Code))
	if code != "" {
		return code == "network_error" || code == "stream_truncated" || code == "stream_idle" ||
			strings.Contains(code, "overload") || strings.Contains(code, "service_unavailable") ||
			strings.Contains(code, "upstream") || strings.Contains(code, "timeout")
	}
	message := strings.ToLower(e.Message)
	return strings.Contains(message, "overload") || strings.Contains(message, "service unavailable") ||
		strings.Contains(message, "upstream connect") || strings.Contains(message, "temporarily unavailable")
}

func (e *ResponseError) Error() string {
	if e == nil {
		return "responses: request failed"
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "request failed"
	}
	var details []string
	if e.Status > 0 {
		details = append(details, fmt.Sprintf("HTTP %d", e.Status))
	}
	if e.Code != "" {
		details = append(details, "code "+e.Code)
	}
	if e.RequestID != "" {
		details = append(details, "request ID "+e.RequestID)
	}
	if e.Attempts > 1 {
		details = append(details, fmt.Sprintf("%d attempts", e.Attempts))
	}
	if len(details) > 0 {
		message += " (" + strings.Join(details, ", ") + ")"
	}
	return providerLabel(e.Provider) + ": " + message
}

// NewResponseError bounds and redacts untrusted provider diagnostics.
func NewResponseError(providerID string, status int, message, code, requestID string, secrets ...string) *ResponseError {
	return &ResponseError{
		Provider:  providerLabel(providerID),
		Message:   SanitizeErrorText(message, maxStreamErrorBytes, secrets...),
		Code:      safeErrorMetadata(providerpkg.RedactSecrets(code, secrets...)),
		RequestID: safeErrorMetadata(providerpkg.RedactSecrets(requestID, secrets...)),
		Status:    status,
	}
}

// SanitizeErrorText redacts credentials before truncation, removes terminal
// control characters, and bounds untrusted provider text. A trailing credential
// prefix is also removed so a bounded read cannot expose the start of a secret
// that crossed its cutoff.
func SanitizeErrorText(value string, maxBytes int, secrets ...string) string {
	value = providerpkg.RedactSecrets(value, secrets...)
	if maxBytes > 0 && len(value) > maxBytes {
		value = value[:maxBytes]
	}
	for _, secret := range secrets {
		limit := min(len(secret), len(value))
		for size := limit; size >= 1; size-- {
			if strings.HasSuffix(value, secret[:size]) {
				value = strings.TrimSuffix(value, secret[:size]) + "[redacted]"
				break
			}
		}
	}
	value = strings.ToValidUTF8(value, "�")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	return truncateUTF8(value, maxBytes)
}

func safeErrorMetadata(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	return truncateUTF8(value, 200)
}
