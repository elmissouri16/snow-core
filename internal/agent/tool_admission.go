package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// admitTool is the shared pre-dispatch boundary for model and plugin calls.
// It does not persist messages, run tools, or advance the provider loop.
func (a *Agent) admitTool(ctx context.Context, name, callID string, raw json.RawMessage, args map[string]any, mode protocol.CollaborationMode, origin *protocol.PluginOrigin, fingerprint string) (tools.Tool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := a.checkManagedGoalToolCall(name, raw); err != nil {
		return nil, err
	}
	if err := a.pluginToolPolicy()(name); err != nil {
		return nil, err
	}
	if (name == "ask_user" || name == "request_user_input") && !a.PluginInputAllowed() {
		return nil, fmt.Errorf("interactive user input is unavailable during automatic goal turns")
	}
	tool, ok := a.opts.Registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("Error: unknown tool %q", name)
	}
	metadata, ok := tools.Metadata(a.opts.Registry, name)
	if !ok {
		return nil, fmt.Errorf("Error: tool metadata unavailable for %q", name)
	}
	if !collaborationToolAllowed(mode, metadata) {
		return nil, fmt.Errorf("%s", collaborationToolDeniedMessage(name))
	}
	if origin == nil {
		if script, ok := tool.(tools.PluginInvocation); ok {
			origin, fingerprint = script.PluginInvocation(callID)
		}
	}
	if origin != nil || (a.opts.PluginHooks != nil && a.opts.PluginHooks.HasHook("before_tool", a.opts.Identity != nil)) {
		if err := a.validatePluginArguments(tool.Schema().Parameters, args); err != nil {
			return nil, err
		}
	}
	analysis := permission.Analysis{Rememberable: true}
	if preflight, ok := tool.(tools.PreflightTool); ok {
		var err error
		analysis, err = preflight.Preflight(ctx, raw, a.opts.ToolHost)
		if err != nil {
			return nil, fmt.Errorf("Error: tool preflight failed: %w", err)
		}
	}
	paths := append(extractPaths(args), analysis.Paths...)
	slices.Sort(paths)
	paths = slices.Compact(paths)
	risk := metadata.Risk
	if risk == "" {
		risk = riskFor(name)
	}
	req := permission.Request{
		Tool: name, Args: raw, Paths: paths, Risk: risk, Agent: a.opts.Identity.Clone(),
		Reason: analysis.Summary, Effects: slices.Clone(analysis.Effects), Capabilities: slices.Clone(analysis.Capabilities),
		Unknown: analysis.Unknown, Rememberable: analysis.Rememberable, ScopeKey: analysis.ScopeKey, ScopeLabel: analysis.ScopeLabel,
		Plugin: origin,
	}
	if origin != nil {
		// Keep exact host arguments and existing analysis scope, but never reuse
		// approvals from another package, configuration, plugin tool, or operation.
		cwd := ""
		if a.opts.ToolHost != nil {
			cwd = a.opts.ToolHost.CWD()
		}
		scope, err := jsonv2.Marshal([]any{fingerprint, origin.PluginID, origin.ToolName, name, string(raw), req.ScopeKey, cwd})
		if err != nil {
			return nil, err
		}
		req.ScopeKey = fmt.Sprintf("js:%x", sha256.Sum256(scope))
		req.ScopeLabel = origin.PluginID + ": " + origin.ToolName
		req.Reason = "Plugin " + origin.PluginID + " via " + origin.ToolName + ": " + req.Reason
	}
	decision, err := a.opts.InvocationPolicy.Evaluate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("Permission denied: %w", err)
	}
	if decision.Denied {
		reason := decision.Reason
		if reason == "" {
			reason = "blocked by invocation policy"
		}
		return nil, fmt.Errorf("Permission denied: %s", reason)
	}
	allowed, err := a.opts.Permission.Authorize(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("Permission denied: %w", err)
	}
	if allowed != permission.DecisionAllow && allowed != permission.DecisionAllowSession && allowed != permission.DecisionAllowAlways {
		return nil, fmt.Errorf("Permission denied: denied by permission policy")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return tool, nil
}

// InvokeBuiltin is attached only to a live top-level tool invocation. The
// JavaScript adapter supplies a second, per-handler capability boundary.
func (h *progressHost) InvokeBuiltin(ctx context.Context, name string, raw json.RawMessage) (tools.ToolResult, error) {
	if !tools.JavaScriptBuiltin(name) {
		return tools.ToolResult{}, fmt.Errorf("host tool %q is unavailable to JavaScript", name)
	}
	parent, ok := h.agent.opts.Registry.Get(h.name)
	if !ok {
		return tools.ToolResult{}, fmt.Errorf("plugin tool is no longer registered")
	}
	script, ok := parent.(tools.PluginInvocation)
	if !ok {
		return tools.ToolResult{}, fmt.Errorf("host invocation requires a JavaScript tool")
	}
	origin, fingerprint := script.PluginInvocation(h.callID)
	if origin == nil {
		return tools.ToolResult{}, fmt.Errorf("host invocation requires a JavaScript tool")
	}
	origin.HostTool = name
	var args map[string]any
	if err := jsonv2.Unmarshal(raw, &args); err != nil || args == nil {
		return tools.ToolResult{}, fmt.Errorf("host arguments must be a JSON object")
	}
	tool, err := h.agent.admitTool(ctx, name, h.callID, raw, args, h.CollaborationMode(), origin, fingerprint)
	if err != nil {
		return tools.ToolResult{}, err
	}
	desc, ok := tools.Metadata(h.agent.opts.Registry, name)
	if !ok || desc.Source != tools.SourceBuiltin {
		return tools.ToolResult{}, fmt.Errorf("host operation is not a built-in")
	}
	h.EmitProgress(tools.ToolProgressEvent{Message: "Plugin operation: " + name})
	return runPluginBuiltin(ctx, tool, raw, h), nil
}

func runPluginBuiltin(ctx context.Context, tool tools.Tool, raw json.RawMessage, host tools.ToolHost) (result tools.ToolResult) {
	defer func() {
		if recover() != nil {
			result = tools.ErrorResult(fmt.Errorf("host operation panicked"))
		}
	}()
	result, err := tool.Run(ctx, raw, host)
	if err != nil {
		return tools.ErrorResult(err)
	}
	// Details are host-private; they must never cross into the script.
	result.Details = nil
	return result
}
