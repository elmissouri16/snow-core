// Package protocol defines the stable, versioned data types shared across
// all snow surfaces: SDK, TUI, print/JSON mode, RPC, providers, and sessions.
//
// This package is part of the public API surface (pkg/protocol). Keep it
// dependency-free: only the Go standard library is allowed here.
package protocol

import (
	"encoding/json"
	"time"
)

// Role identifies the speaker of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool_result"
	RoleSystem    Role = "system" // rare; prefer context assembly
	RoleCustom    Role = "custom" // extensions / harness notes
)

// StopReason describes why an assistant turn ended.
type StopReason string

const (
	StopStop    StopReason = "stop"
	StopLength  StopReason = "length"
	StopToolUse StopReason = "tool_use"
	StopError   StopReason = "error"
	StopAborted StopReason = "aborted"
	StopPending StopReason = "pending" // in-memory partial only, never persisted as terminal
)

// ContentBlockType enumerates content block kinds.
type ContentBlockType string

const (
	BlockText     ContentBlockType = "text"
	BlockImage    ContentBlockType = "image"
	BlockThinking ContentBlockType = "thinking"
	BlockToolCall ContentBlockType = "tool_call"
)

// ContentBlock is a single typed unit of message content.
type ContentBlock struct {
	Type ContentBlockType `json:"type"`

	// Text / thinking
	Text string `json:"text,omitempty"`

	// Image
	MIMEType string `json:"mime_type,omitempty"`
	Data     []byte `json:"data,omitempty"` // base64 in JSONL on disk

	// Tool call
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
}

// Cost tracks per-class token cost.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Total      float64 `json:"total"`
}

// Usage tracks token usage for an assistant turn.
type Usage struct {
	Input      int   `json:"input"`
	Output     int   `json:"output"`
	CacheRead  int   `json:"cache_read"`
	CacheWrite int   `json:"cache_write"`
	Total      int   `json:"total_tokens"`
	Cost       *Cost `json:"cost,omitempty"`
}

// Message is a durable conversation entry.
type Message struct {
	ID        string         `json:"id"`
	ParentID  string         `json:"parent_id,omitempty"`
	Role      Role           `json:"role"`
	Content   []ContentBlock `json:"content"`
	Timestamp int64          `json:"ts"` // unix ms

	// Assistant metadata
	Provider   string     `json:"provider,omitempty"`
	Model      string     `json:"model,omitempty"`
	StopReason StopReason `json:"stop_reason,omitempty"`
	Error      string     `json:"error,omitempty"`
	Usage      *Usage     `json:"usage,omitempty"`

	// Tool result metadata
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
}

// NewUserMessage builds a simple user text message.
func NewUserMessage(id, parentID, text string) Message {
	return Message{
		ID:        id,
		ParentID:  parentID,
		Role:      RoleUser,
		Content:   []ContentBlock{{Type: BlockText, Text: text}},
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewTextBlock returns a text content block.
func NewTextBlock(text string) ContentBlock {
	return ContentBlock{Type: BlockText, Text: text}
}

// NewToolResultMessage builds a tool_result message.
func NewToolResultMessage(id, parentID, toolCallID, toolName string, content []ContentBlock, isError bool) Message {
	return Message{
		ID:         id,
		ParentID:   parentID,
		Role:       RoleTool,
		Content:    content,
		Timestamp:  time.Now().UnixMilli(),
		ToolCallID: toolCallID,
		ToolName:   toolName,
		IsError:    isError,
	}
}

// NewAssistantMessage builds an assistant message.
func NewAssistantMessage(id, parentID, provider, model string, content []ContentBlock, stop StopReason, usage *Usage) Message {
	return Message{
		ID:         id,
		ParentID:   parentID,
		Role:       RoleAssistant,
		Content:    content,
		Timestamp:  time.Now().UnixMilli(),
		Provider:   provider,
		Model:      model,
		StopReason: stop,
		Usage:      usage,
	}
}
