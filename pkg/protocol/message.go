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
	// RoleAgent is an attributed collaboration mailbox message. Providers render
	// it as a sealed compatibility envelope rather than an ordinary user prompt.
	RoleAgent Role = "agent"
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
	// BlockProviderData is durable, non-rendered provider continuity state.
	// Surfaces must not display or emit its Data as an event/log payload.
	BlockProviderData ContentBlockType = "provider_data"
	// BlockPlan stores proposed-plan Markdown without the transport tags.
	BlockPlan ContentBlockType = "plan"
)

// ContentBlock is a single typed unit of message content.
type ContentBlock struct {
	Type ContentBlockType `json:"type"`

	// Text / thinking
	Text string `json:"text,omitempty"`
	// PlanComplete distinguishes an official completed plan from interrupted
	// plan-shaped output. Providers only reconstruct tags for completed plans.
	PlanComplete bool `json:"plan_complete,omitempty"`

	// Image
	MIMEType string `json:"mime_type,omitempty"`
	Data     []byte `json:"data,omitempty"` // base64 in JSONL on disk

	// Tool call / opaque provider continuity identifier
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
}

// Cost tracks per-class token cost. Values are expressed in the currency
// declared by Currency, normally USD.
type Cost struct {
	Currency   string  `json:"currency,omitempty"`
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Total      float64 `json:"total"`
}

// ModelPricing contains provider/catalog pricing in currency units per
// million tokens. A nil pricing value means usage is still tracked but cost
// cannot be estimated authoritatively.
type ModelPricing struct {
	Currency             string  `json:"currency,omitempty"`
	InputPerMillion      float64 `json:"input_per_million"`
	OutputPerMillion     float64 `json:"output_per_million"`
	CacheReadPerMillion  float64 `json:"cache_read_per_million"`
	CacheWritePerMillion float64 `json:"cache_write_per_million"`
}

// Clone returns an independent cost value.
func (c *Cost) Clone() *Cost {
	if c == nil {
		return nil
	}
	out := *c
	return &out
}

// Usage tracks token usage for one provider request or an aggregate. Input is
// the total prompt/input count; CacheRead and CacheWrite are subsets when the
// provider reports them. Cost uses the non-cached remainder for Input.
type Usage struct {
	Input      int   `json:"input"`
	Output     int   `json:"output"`
	Reasoning  int   `json:"reasoning,omitempty"`
	CacheRead  int   `json:"cache_read"`
	CacheWrite int   `json:"cache_write"`
	Total      int   `json:"total_tokens"`
	Requests   int   `json:"requests,omitempty"`
	Cost       *Cost `json:"cost,omitempty"`
}

// Clone returns an independent usage value.
func (u *Usage) Clone() *Usage {
	if u == nil {
		return nil
	}
	out := *u
	if u.Cost != nil {
		cost := *u.Cost
		out.Cost = &cost
	}
	return &out
}

// Add returns the sum of two usage records. A zero Requests value is treated
// as one request when aggregating a provider usage record.
func (u Usage) Add(v Usage) Usage {
	priorRequests := u.Requests
	if priorRequests == 0 && (u.Input != 0 || u.Output != 0 || u.Reasoning != 0 || u.CacheRead != 0 || u.CacheWrite != 0 || u.Total != 0 || u.Cost != nil) {
		priorRequests = 1
	}
	u.Input += v.Input
	u.Output += v.Output
	u.Reasoning += v.Reasoning
	u.CacheRead += v.CacheRead
	u.CacheWrite += v.CacheWrite
	vTotal := v.Total
	if vTotal == 0 {
		vTotal = v.Input + v.Output
	}
	u.Total += vTotal
	vRequests := v.Requests
	if vRequests == 0 {
		vRequests = 1
	}
	u.Requests = priorRequests + vRequests
	// u is a value receiver but its Cost pointer aliases the caller's record;
	// clone before summing so Add never mutates the caller's data. A missing
	// cost on one record keeps the accumulated cost instead of dropping it.
	if u.Cost != nil {
		u.Cost = u.Cost.Clone()
		if v.Cost != nil {
			u.Cost.Input += v.Cost.Input
			u.Cost.Output += v.Cost.Output
			u.Cost.CacheRead += v.Cost.CacheRead
			u.Cost.CacheWrite += v.Cost.CacheWrite
			u.Cost.Total += v.Cost.Total
		}
	} else if v.Cost != nil {
		u.Cost = v.Cost.Clone()
	}
	return u
}

// CostFor estimates cost using optional catalog pricing. It returns nil when
// no pricing is available.
func (u Usage) CostFor(pricing *ModelPricing) *Cost {
	if pricing == nil {
		return nil
	}
	uncached := u.Input - u.CacheRead - u.CacheWrite
	if uncached < 0 {
		uncached = 0
	}
	currency := pricing.Currency
	if currency == "" {
		currency = "USD"
	}
	cost := &Cost{
		Currency:   currency,
		Input:      float64(uncached) * pricing.InputPerMillion / 1_000_000,
		Output:     float64(u.Output) * pricing.OutputPerMillion / 1_000_000,
		CacheRead:  float64(u.CacheRead) * pricing.CacheReadPerMillion / 1_000_000,
		CacheWrite: float64(u.CacheWrite) * pricing.CacheWritePerMillion / 1_000_000,
	}
	cost.Total = cost.Input + cost.Output + cost.CacheRead + cost.CacheWrite
	return cost
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

// NewAgentMessage builds a durable attributed mailbox entry.
func NewAgentMessage(id, parentID string, envelope AgentMessage) Message {
	return Message{
		ID: id, ParentID: parentID, Role: RoleAgent,
		Content:   []ContentBlock{{Type: BlockText, Text: envelope.SealedText()}},
		Timestamp: envelope.CreatedAt,
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
