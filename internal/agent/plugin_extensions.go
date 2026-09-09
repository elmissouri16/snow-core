package agent

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// PluginHooks is optional. The core knows phases and public data, not Goja.
type PluginHooks interface {
	HasHook(string, bool) bool
	RunHooks(context.Context, plugin.HookRequest) (plugin.HookRequest, []protocol.PluginTransform, error)
}

func (a *Agent) pluginHook(ctx context.Context, request plugin.HookRequest) (plugin.HookRequest, []protocol.PluginTransform, error) {
	if a.opts.PluginHooks == nil || !a.opts.PluginHooks.HasHook(request.Phase, a.opts.Identity != nil) {
		return request, nil, nil
	}
	request.Agent = a.opts.Identity.Clone()
	return a.opts.PluginHooks.RunHooks(ctx, request)
}

type pluginToolChainKey struct{}

// InvokePluginTool shares admission with model calls. Commands may invoke
// tools only at idle; nested calls retain their original invocation context.
func (a *Agent) InvokePluginTool(ctx context.Context, inv plugin.Invocation, name string, raw json.RawMessage) (tools.ToolResult, error) {
	chain, _ := ctx.Value(pluginToolChainKey{}).([]string)
	if len(chain) >= 16 || slices.Contains(chain, name) {
		return tools.ToolResult{}, errors.New("recursive plugin tool call")
	}
	if inv.ToolCallID == "" && len(chain) == 0 {
		unlock, err := a.LockAdmissionContext(ctx)
		if err != nil {
			return tools.ToolResult{}, err
		}
		defer unlock()
		if a.IsRunning() {
			return tools.ToolResult{}, errors.New("agent busy: invoke command tools after the turn completes")
		}
	}
	ctx, audit, ownAudit := pluginAuditContext(ctx)
	ctx = context.WithValue(ctx, pluginToolChainKey{}, append(slices.Clone(chain), name))
	request, changes, err := a.pluginHook(ctx, plugin.HookRequest{Phase: "before_tool", Tool: name, Arguments: raw})
	if err != nil {
		return tools.ToolResult{}, err
	}
	raw = request.Arguments
	var args map[string]any
	if err := jsonv2.Unmarshal(raw, &args); err != nil || args == nil {
		return tools.ToolResult{}, errors.New("host tool arguments must be an object")
	}
	origin := &protocol.PluginOrigin{PluginID: inv.PluginID, ToolName: inv.Name, ParentToolCallID: inv.ToolCallID, HostTool: name}
	tool, err := a.admitTool(ctx, name, inv.ToolCallID, raw, args, a.Mode(), origin, inv.Fingerprint)
	if err != nil {
		return tools.ToolResult{}, err
	}
	result := a.runTool(ctx, tool, raw, inv.ToolCallID, name)
	post, postChanges, postErr := a.pluginHook(ctx, plugin.HookRequest{Phase: "after_tool", Tool: name, Arguments: raw, Content: result.Content, IsError: result.IsError})
	changes = append(changes, postChanges...)
	audit.add(changes)
	if ownAudit {
		if err := a.persistPluginAudit(audit.changes); err != nil {
			return result, fmt.Errorf("tool executed; audit persistence failed: %w", err)
		}
	}
	if postErr == nil {
		result.Content = post.Content
	}
	// Direct command calls have no model tool-call/result pair. Return their
	// actual outcome even if a post hook fails, never suggest rerunning the tool.
	if postErr != nil {
		return result, fmt.Errorf("tool executed; post-tool plugin processing failed: %w", postErr)
	}
	return result, nil
}

func (a *Agent) PluginInputAllowed() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.turnOrigin != "goal"
}
