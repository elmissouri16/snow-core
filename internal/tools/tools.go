// Package tools defines the Tool contract, the host interface tools execute
// against, and the registry used by the agent loop.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/pkg/protocol"
)

// ToolSchema is the JSON-schema-backed description of a tool.
type ToolSchema = protocol.ToolSchema

// ToolResult is what a tool returns. Content is sent back to the model;
// Details is tool-private metadata for the UI and is not sent to the model.
type ToolResult struct {
	Content []protocol.ContentBlock
	IsError bool
	Details any
}

// TextResult builds a simple text tool result.
func TextResult(text string) ToolResult {
	return ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(text)}}
}

// ErrorResult builds an error tool result.
func ErrorResult(err error) ToolResult {
	return ToolResult{
		Content: []protocol.ContentBlock{protocol.NewTextBlock("Error: " + err.Error())},
		IsError: true,
	}
}

// ToolProgressEvent is emitted via the host during long-running tools.
type ToolProgressEvent struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Message    string `json:"message,omitempty"`
	Done       bool   `json:"done"`
	IsError    bool   `json:"is_error,omitempty"`
}

// ToolHost gives tools access to environment and safety services.
type ToolHost interface {
	CWD() string
	// Roots returns path roots the tool may touch (cwd + explicit allows).
	Roots() []string
	Permission() permission.Service
	EmitProgress(event ToolProgressEvent)
	Environ() []string
}

// Tool is a model-invoked capability.
type Tool interface {
	Schema() ToolSchema
	Run(ctx context.Context, args json.RawMessage, host ToolHost) (ToolResult, error)
}

// Registry holds the set of tools available to the agent.
type Registry interface {
	Register(t Tool) error
	Get(name string) (Tool, bool)
	Schemas() []ToolSchema
	List() []Tool
}

// SimpleRegistry is a thread-safe in-memory registry.
type SimpleRegistry struct {
	mu   sync.RWMutex
	m    map[string]Tool
	keys []string
}

// NewRegistry returns an empty thread-safe registry.
func NewRegistry() *SimpleRegistry {
	return &SimpleRegistry{m: make(map[string]Tool)}
}

// Register adds a tool, failing on duplicate names.
func (r *SimpleRegistry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Schema().Name
	if name == "" {
		return fmt.Errorf("tool has empty name")
	}
	if _, ok := r.m[name]; ok {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.m[name] = t
	r.keys = append(r.keys, name)
	return nil
}

// Get returns a tool by name.
func (r *SimpleRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.m[name]
	return t, ok
}

// Schemas returns schemas in registration order.
func (r *SimpleRegistry) Schemas() []ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolSchema, 0, len(r.keys))
	for _, k := range r.keys {
		out = append(out, r.m[k].Schema())
	}
	return out
}

// List returns tools in registration order.
func (r *SimpleRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.keys))
	for _, k := range r.keys {
		out = append(out, r.m[k])
	}
	return out
}
