package protocol

import "encoding/json"

// AgentEventType enumerates cross-surface agent events emitted by the core.
// These are the only observation channel for TUI / SDK / print / RPC.
type AgentEventType string

const (
	EvSessionUpdated    AgentEventType = "session_updated"
	EvTextDelta         AgentEventType = "text_delta"
	EvThinkingDelta     AgentEventType = "thinking_delta"
	EvToolStart         AgentEventType = "tool_start"
	EvToolProgress      AgentEventType = "tool_progress"
	EvToolEnd           AgentEventType = "tool_end"
	EvPermissionRequest AgentEventType = "permission_request"
	EvUsage             AgentEventType = "usage"
	EvTurnDone          AgentEventType = "turn_done"
	EvError             AgentEventType = "error"
	EvAborted           AgentEventType = "aborted"
	EvModelChanged      AgentEventType = "model_changed"
)

// ToolProgress is emitted by tools running through the agent.
type ToolProgress struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Message    string `json:"message,omitempty"`
	Done       bool   `json:"done"`
	IsError    bool   `json:"is_error,omitempty"`
}

// AgentEvent is a single event delivered to subscribers.
type AgentEvent struct {
	Type AgentEventType `json:"type"`

	Text       string      `json:"text,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolName   string      `json:"tool_name,omitempty"`
	Message    string      `json:"message,omitempty"` // error / progress text
	Usage      *Usage      `json:"usage,omitempty"`
	Model      *Model      `json:"model,omitempty"`
	Permission *Permission `json:"permission,omitempty"`
	IsError    bool        `json:"is_error,omitempty"`
}

// PermissionRequest is embedded in permission_request events.
type PermissionRequest struct {
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
	Paths  []string        `json:"paths,omitempty"`
	Risk   string          `json:"risk"`
	Reason string          `json:"reason,omitempty"`
}

// Permission is the resolved view for a permission_request event.
type Permission struct {
	Request PermissionRequest `json:"request"`
}
