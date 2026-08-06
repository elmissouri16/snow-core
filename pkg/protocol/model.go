package protocol

import (
	"context"
	"encoding/json"
)

// Model describes a resolvable LLM model exposed by a provider.
type Model struct {
	Provider         string `json:"provider"`
	ID               string `json:"id"`
	DisplayName      string `json:"display_name,omitempty"`
	ContextWindow    int    `json:"context_window,omitempty"`
	MaxOutputTokens  int    `json:"max_output_tokens,omitempty"`
	SupportsTools    bool   `json:"supports_tools"`
	SupportsThinking bool   `json:"supports_thinking"`
	SupportsVision   bool   `json:"supports_vision"`
}

// ThinkingLevel controls reasoning effort.
type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
)

// ToolSchema describes a tool the model may call.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema object
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
	EvStreamUsage         StreamEventType = "usage"
	EvStreamDone          StreamEventType = "done"
	EvStreamError         StreamEventType = "error"
)

// StreamEvent is one normalized event from a provider stream.
type StreamEvent struct {
	Type       StreamEventType
	Text       string
	ToolCallID string
	ToolName   string
	Arguments  json.RawMessage // cumulative or final per adapter contract
	Usage      *Usage
	StopReason StopReason
	Err        error
}

// EventStream yields normalized provider events. Next blocks until the next
// event or EOF/error. Close releases resources.
type EventStream interface {
	Next(ctx context.Context) (StreamEvent, error)
	Close() error
}
