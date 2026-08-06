package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/snow-core/snow/internal/tools"
)

// Tool adapts a plugin Host into the tools.Tool interface so plugins can be
// registered in the agent's tool registry.
type Tool struct {
	schema tools.ToolSchema
	host   *Host
	// callMu serializes calls; plugins are single-threaded by default.
	callMu sync.Mutex
}

// NewTool wraps a host for one of its tools.
func NewTool(host *Host, schema tools.ToolSchema) *Tool {
	return &Tool{host: host, schema: schema}
}

// Schema implements tools.Tool.
func (t *Tool) Schema() tools.ToolSchema { return t.schema }

// Run implements tools.Tool by forwarding to the plugin.
func (t *Tool) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	t.callMu.Lock()
	defer t.callMu.Unlock()
	res, err := t.host.Call(ctx, t.schema.Name, args)
	if err != nil {
		if res.IsError {
			return res, nil // plugin returned a structured error result
		}
		return tools.ErrorResult(fmt.Errorf("plugin %s: %w", t.schema.Name, err)), nil
	}
	return res, nil
}

// RegisterTools registers every tool exposed by the plugin host into reg.
func RegisterTools(reg tools.Registry, host *Host) error {
	for _, s := range host.ToolSchemas() {
		if err := reg.Register(NewTool(host, s)); err != nil {
			return fmt.Errorf("plugin register %s: %w", s.Name, err)
		}
	}
	return nil
}
