package protocol

import (
	"context"
	"encoding/json"
	"fmt"
)

// Model describes a resolvable LLM model exposed by a provider.
type ModelUpgrade struct {
	Model   string `json:"model"`
	Message string `json:"message,omitempty"`
}

type Model struct {
	Provider         string        `json:"provider"`
	ID               string        `json:"id"`
	DisplayName      string        `json:"display_name,omitempty"`
	Description      string        `json:"description,omitempty"`
	ContextWindow    int           `json:"context_window,omitempty"`
	MaxContextWindow int           `json:"max_context_window,omitempty"`
	MaxOutputTokens  int           `json:"max_output_tokens,omitempty"`
	SupportsTools    bool          `json:"supports_tools"`
	SupportsThinking bool          `json:"supports_thinking"`
	DefaultThinking  ThinkingLevel `json:"default_thinking,omitempty"`
	// ThinkingLevels contains the normalized non-off effort levels that the
	// model is known to accept. An empty list is intentionally conservative:
	// callers must not guess that an effort is supported from a model name.
	ThinkingLevels    []ThinkingLevel `json:"thinking_levels,omitempty"`
	SupportsVision    bool            `json:"supports_vision"`
	SupportsVerbosity bool            `json:"supports_verbosity,omitempty"`
	// SupportsReasoningSummary is nil for legacy/bundled model metadata, which
	// preserves the historical behavior of sending reasoning.summary. An
	// authenticated catalog can explicitly advertise true or false.
	SupportsReasoningSummary *bool         `json:"supports_reasoning_summary,omitempty"`
	Upgrade                  *ModelUpgrade `json:"upgrade,omitempty"`
	Pricing                  *ModelPricing `json:"pricing,omitempty"`
}

// Clone returns a deep defensive copy of model metadata.
func (m Model) Clone() Model {
	out := m
	out.ThinkingLevels = append([]ThinkingLevel(nil), m.ThinkingLevels...)
	if m.SupportsReasoningSummary != nil {
		supported := *m.SupportsReasoningSummary
		out.SupportsReasoningSummary = &supported
	}
	if m.Upgrade != nil {
		upgrade := *m.Upgrade
		out.Upgrade = &upgrade
	}
	if m.Pricing != nil {
		pricing := *m.Pricing
		out.Pricing = &pricing
	}
	return out
}

// ThinkingLevel controls reasoning effort.
type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
	ThinkingMax     ThinkingLevel = "max"
	ThinkingUltra   ThinkingLevel = "ultra"
)

// KnownThinkingLevels returns the normalized levels accepted by the public
// configuration and provider contracts. The returned slice is independent.
func KnownThinkingLevels() []ThinkingLevel {
	return []ThinkingLevel{
		ThinkingOff,
		ThinkingMinimal,
		ThinkingLow,
		ThinkingMedium,
		ThinkingHigh,
		ThinkingXHigh,
		ThinkingMax,
		ThinkingUltra,
	}
}

// NormalizeThinkingLevel treats an omitted effort as the default off setting.
// This preserves compatibility for callers that construct ChatRequest values
// directly instead of going through app/config parsing.
func NormalizeThinkingLevel(level ThinkingLevel) ThinkingLevel {
	if level == "" {
		return ThinkingOff
	}
	return level
}

// ParseThinkingLevel validates a user-facing effort value.
func ParseThinkingLevel(value string) (ThinkingLevel, error) {
	level := NormalizeThinkingLevel(ThinkingLevel(value))
	switch level {
	case ThinkingOff, ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingXHigh, ThinkingMax, ThinkingUltra:
		return level, nil
	default:
		return ThinkingOff, fmt.Errorf("invalid thinking level %q (want off|minimal|low|medium|high|xhigh|max|ultra)", value)
	}
}

// ReasoningSummary controls the provider-generated reasoning summary. Off is
// a Snow-level setting that omits the summary request entirely.
type ReasoningSummary string

const (
	ReasoningSummaryOff      ReasoningSummary = "off"
	ReasoningSummaryAuto     ReasoningSummary = "auto"
	ReasoningSummaryConcise  ReasoningSummary = "concise"
	ReasoningSummaryDetailed ReasoningSummary = "detailed"
)

// KnownReasoningSummaries returns every normalized public setting.
func KnownReasoningSummaries() []ReasoningSummary {
	return []ReasoningSummary{
		ReasoningSummaryOff,
		ReasoningSummaryAuto,
		ReasoningSummaryConcise,
		ReasoningSummaryDetailed,
	}
}

// NormalizeReasoningSummary preserves the historical provider behavior for
// callers that omit this newer field.
func NormalizeReasoningSummary(summary ReasoningSummary) ReasoningSummary {
	if summary == "" {
		return ReasoningSummaryAuto
	}
	return summary
}

// ParseReasoningSummary validates a user-facing summary value.
func ParseReasoningSummary(value string) (ReasoningSummary, error) {
	summary := NormalizeReasoningSummary(ReasoningSummary(value))
	switch summary {
	case ReasoningSummaryOff, ReasoningSummaryAuto, ReasoningSummaryConcise, ReasoningSummaryDetailed:
		return summary, nil
	default:
		return ReasoningSummaryAuto, fmt.Errorf("invalid reasoning summary %q (want off|auto|concise|detailed)", value)
	}
}

// TextVerbosity controls the response text detail requested from providers
// that support it.
type TextVerbosity string

const (
	TextVerbosityLow    TextVerbosity = "low"
	TextVerbosityMedium TextVerbosity = "medium"
	TextVerbosityHigh   TextVerbosity = "high"
)

// KnownTextVerbosities returns every normalized public setting.
func KnownTextVerbosities() []TextVerbosity {
	return []TextVerbosity{TextVerbosityLow, TextVerbosityMedium, TextVerbosityHigh}
}

// NormalizeTextVerbosity preserves Snow's historical low-verbosity request
// when the setting is omitted.
func NormalizeTextVerbosity(verbosity TextVerbosity) TextVerbosity {
	if verbosity == "" {
		return TextVerbosityLow
	}
	return verbosity
}

// ParseTextVerbosity validates a user-facing verbosity value.
func ParseTextVerbosity(value string) (TextVerbosity, error) {
	verbosity := NormalizeTextVerbosity(TextVerbosity(value))
	switch verbosity {
	case TextVerbosityLow, TextVerbosityMedium, TextVerbosityHigh:
		return verbosity, nil
	default:
		return TextVerbosityLow, fmt.Errorf("invalid text verbosity %q (want low|medium|high)", value)
	}
}

// SupportedThinkingLevels returns off plus the model's advertised effort
// levels. Off is always available because Snow omits the provider effort field
// for that setting. Unknown or invalid catalog values are ignored.
func (m Model) SupportedThinkingLevels() []ThinkingLevel {
	out := []ThinkingLevel{ThinkingOff}
	if !m.SupportsThinking {
		return out
	}
	seen := map[ThinkingLevel]bool{ThinkingOff: true}
	for _, level := range m.ThinkingLevels {
		level = NormalizeThinkingLevel(level)
		if level == ThinkingOff || seen[level] {
			continue
		}
		switch level {
		case ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingXHigh, ThinkingMax, ThinkingUltra:
			seen[level] = true
			out = append(out, level)
		}
	}
	return out
}

// SupportsThinkingLevel reports whether the model can accept the normalized
// effort. Empty or missing catalog metadata supports only off.
func (m Model) SupportsThinkingLevel(level ThinkingLevel) bool {
	level = NormalizeThinkingLevel(level)
	if level == ThinkingOff {
		return true
	}
	switch level {
	case ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingXHigh, ThinkingMax, ThinkingUltra:
	default:
		return false
	}
	for _, supported := range m.SupportedThinkingLevels() {
		if supported == level {
			return true
		}
	}
	return false
}

// ToolDiscoveryMode controls whether a tool schema is always sent to the model
// or selected on demand by the host's tool router. The empty value is treated
// as always so existing tool definitions remain backward-compatible.
type ToolDiscoveryMode string

const (
	ToolDiscoveryAlways   ToolDiscoveryMode = "always"
	ToolDiscoveryDeferred ToolDiscoveryMode = "deferred"
)

// ToolDiscovery is host-side retrieval metadata. Provider adapters must not
// serialize this metadata as part of a model-facing function definition.
type ToolDiscovery struct {
	Mode      ToolDiscoveryMode `json:"mode,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Keywords  []string          `json:"keywords,omitempty"`
}

// ToolSchema describes a tool the model may call. Discovery is used only by
// the host; Name, Description, and Parameters are the provider-facing schema.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema object
	Discovery   *ToolDiscovery  `json:"discovery,omitempty"`
}

// ChatRequest is the normalized input to a provider chat call.
type ChatRequest struct {
	Model       Model
	Messages    []Message
	Tools       []ToolSchema
	System      string
	MaxTokens   int
	Temperature *float64
	Thinking    ThinkingLevel
	// ReasoningSummary and TextVerbosity are normalized provider preferences.
	// Adapters that do not support them may ignore them.
	ReasoningSummary ReasoningSummary
	TextVerbosity    TextVerbosity
	InternalContext  []InternalContextFragment
	// SessionAffinityKey is a stable, non-secret host hint for provider-side
	// prompt caching and request affinity. Providers may ignore it.
	SessionAffinityKey string
	// Extra carries adapter-specific options; keep opaque to the core.
	Extra map[string]any
}

// StreamEventType enumerates provider stream events.
type StreamEventType string

const (
	EvStreamTextDelta     StreamEventType = "text_delta"
	EvStreamThinkingDelta StreamEventType = "thinking_delta"
	EvStreamToolCallDelta StreamEventType = "tool_call_delta"
	EvStreamToolCallDone  StreamEventType = "tool_call_done"
	// EvStreamProviderData carries non-rendered opaque continuity state from a
	// provider into persistence. Agent-facing event buses must never publish it.
	EvStreamProviderData StreamEventType = "provider_data"
	EvStreamUsage        StreamEventType = "usage"
	EvStreamDone         StreamEventType = "done"
	EvStreamError        StreamEventType = "error"
)

// StreamEvent is one normalized event from a provider stream.
type StreamEvent struct {
	Type         StreamEventType
	Text         string
	ToolCallID   string
	ToolName     string
	Arguments    json.RawMessage // cumulative or final per adapter contract
	ProviderData *ContentBlock   // opaque persistence-only data; never an AgentEvent
	Usage        *Usage
	StopReason   StopReason
	Err          error
}

// EventStream yields normalized provider events. A successful stream emits one
// EvStreamDone before EOF; EOF without that terminal event is truncation. Next
// blocks until the next event or EOF/error. Close releases resources.
type EventStream interface {
	Next(ctx context.Context) (StreamEvent, error)
	Close() error
}
