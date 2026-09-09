package tools

import (
	"context"
	"encoding/json"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// PluginInvocation supplies host-owned script identity and an opaque scope.
type PluginInvocation interface {
	PluginInvocation(callID string) (*protocol.PluginOrigin, string)
}

// BuiltinInvoker is provided by the agent on an active tool host.
type BuiltinInvoker interface {
	InvokeBuiltin(context.Context, string, json.RawMessage) (ToolResult, error)
}

func JavaScriptBuiltin(name string) bool {
	switch name {
	case "read", "write", "edit", "grep", "glob", "bash", "webfetch":
		return true
	}
	return false
}
