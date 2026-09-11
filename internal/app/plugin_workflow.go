package app

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	internalplugin "github.com/elmissouri16/snow-core/internal/plugin"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// appPluginHooks keeps one stable manager reference and supplies app-owned
// branch state without teaching the agent or JavaScript runtime about stores.
type appPluginHooks struct {
	manager *internalplugin.Manager
	app     *App
}

func (h *appPluginHooks) HasHook(phase string, child bool) bool {
	if phase == "before_request" && h.app != nil && h.app.extensions != nil {
		if p := h.app.extensions.toolPolicy.Load(); p != nil && p.err != nil {
			return true
		}
	}
	return h.manager.HasHook(phase, child)
}
func (h *appPluginHooks) RunHooks(ctx context.Context, request plugin.HookRequest) (plugin.HookRequest, []protocol.PluginTransform, error) {
	if h.app == nil {
		return h.manager.RunHooks(ctx, request)
	}
	if request.Phase == "before_request" {
		if s := h.app.extensions; s != nil {
			if p := s.toolPolicy.Load(); p != nil && p.err != nil {
				return request, nil, p.err
			}
		}
	}
	return h.manager.RunHooksWithWorkflow(ctx, request, h.app.loadPluginWorkflow)
}
func (h *appPluginHooks) RenderTool(ctx context.Context, name string, data json.RawMessage) (*protocol.PluginNode, error) {
	return h.manager.RenderTool(ctx, name, data)
}
func (h *appPluginHooks) ToolPolicy() func(string) error {
	if h.app == nil || h.app.extensions == nil {
		return func(string) error { return nil }
	}
	snapshot := h.app.extensions.toolPolicy.Load()
	return func(name string) error { return snapshot.check(name) }
}

type pluginToolPolicySnapshot struct {
	restrictions map[string]*session.ToolRestriction
	err          error
}

func (p *pluginToolPolicySnapshot) check(name string) error {
	if p == nil {
		return nil
	}
	if p.err != nil {
		return p.err
	}
	// Stable attribution when multiple owners reject the same tool.
	var ids []string
	for id := range p.restrictions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		r := p.restrictions[id]
		if (r.Allow != nil && !slices.Contains(*r.Allow, name)) || slices.Contains(r.Deny, name) {
			return fmt.Errorf("tool %q restricted by plugin %s", name, id)
		}
	}
	return nil
}

// Caller holds session exclusion/admission or is performing startup wiring.
// All data in the published snapshot is immutable and defensively copied by
// WorkflowState. A projection failure never silently removes restrictions.
func (a *App) refreshPluginToolPolicy() {
	if a.extensions == nil {
		return
	}
	p := &pluginToolPolicySnapshot{restrictions: map[string]*session.ToolRestriction{}}
	store, supported := a.Session.(session.WorkflowStateStore)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, info := range a.PluginInfos() {
		if !supported {
			if slices.Contains(info.Capabilities, "tool_policy") {
				p.err = fmt.Errorf("plugin %s: workflow store unavailable", info.ID)
			}
			continue
		}
		state, err := store.WorkflowState(ctx, info.ID)
		if err != nil {
			p.err = fmt.Errorf("plugin %s restriction projection: %w", info.ID, err)
			break
		}
		if state.Restriction != nil {
			p.restrictions[info.ID] = state.Restriction
		}
	}
	a.extensions.toolPolicy.Store(p)
}

func (a *App) loadPluginWorkflow(ctx context.Context, id string, keys []string) (map[string]json.RawMessage, error) {
	if len(keys) > 64 {
		return nil, errors.New("workflow hook key limit exceeded")
	}
	// Hooks also run under session transitions holding the exclusive session
	// guard. Do not reacquire it here; core admission pins the active store.
	store, ok := a.Session.(session.WorkflowStateStore)
	if !ok {
		return nil, plugin.ErrUnavailable
	}
	state, err := store.WorkflowState(ctx, id)
	if err != nil {
		return nil, err
	}
	values := make(map[string]json.RawMessage, len(keys))
	for _, key := range keys {
		if len(key) == 0 || len(key) > 128 || strings.ContainsRune(key, 0) {
			return nil, errors.New("invalid workflow key")
		}
		value := state.Values[key]
		if value == nil {
			value = json.RawMessage("null")
		}
		values[key] = value
	}
	raw, err := jsonv2.Marshal(values)
	if err != nil {
		return nil, err
	}
	if len(raw) > 128<<10 {
		return nil, errors.New("workflow hook snapshot exceeds 128 KiB")
	}
	return values, nil
}

func (h *appExtensionHost) callWorkflow(ctx context.Context, inv plugin.Invocation, op string, raw json.RawMessage) (json.RawMessage, error) {
	if h.child {
		return nil, plugin.ErrUnavailable
	}
	if op == "tools.list" {
		return h.listPluginTools()
	}
	store, ok := h.store.(session.WorkflowStateStore)
	if !ok {
		return nil, plugin.ErrUnavailable
	}
	if op == "workflow.get" {
		var arg struct {
			Key string `json:"key"`
		}
		if err := jsonv2.Unmarshal(raw, &arg, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		if len(arg.Key) == 0 || len(arg.Key) > 128 || strings.ContainsRune(arg.Key, 0) {
			return nil, errors.New("invalid workflow key")
		}
		state, err := store.WorkflowState(ctx, inv.PluginID)
		if err != nil {
			return nil, err
		}
		if value := state.Values[arg.Key]; value != nil {
			return value, nil
		}
		return json.RawMessage("null"), nil
	}
	if inv.Kind != "command" {
		return nil, errors.New("workflow and restriction writes require an explicit root command")
	}
	var update session.WorkflowUpdate
	switch op {
	case "workflow.set":
		var arg struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err := jsonv2.Unmarshal(raw, &arg, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		update.Set = map[string]json.RawMessage{arg.Key: arg.Value}
	case "workflow.delete":
		var arg struct {
			Key string `json:"key"`
		}
		if err := jsonv2.Unmarshal(raw, &arg, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		update.Delete = []string{arg.Key}
	case "workflow.update":
		var arg struct {
			Set             map[string]json.RawMessage `json:"set"`
			Delete          []string                   `json:"delete"`
			ToolRestriction json.RawMessage            `json:"toolRestriction"`
		}
		if err := jsonv2.Unmarshal(raw, &arg, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		update.Set, update.Delete = arg.Set, arg.Delete
		if len(arg.ToolRestriction) > 0 {
			if !plugin.AllowsOperation(inv.Uses, "tools.restrict") || !slices.Contains(h.info.Capabilities, "tool_policy") {
				return nil, errors.New("toolRestriction requires tool_policy capability")
			}
			if string(arg.ToolRestriction) == "null" {
				update.ClearRestriction = true
			} else {
				update.Restriction = new(session.ToolRestriction)
				if err := jsonv2.Unmarshal(arg.ToolRestriction, update.Restriction, jsonv2.RejectUnknownMembers(true)); err != nil {
					return nil, err
				}
			}
		}
	case "tools.restrict":
		update.Restriction = new(session.ToolRestriction)
		if err := jsonv2.Unmarshal(raw, update.Restriction, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
	case "tools.clearRestriction":
		var arg struct{}
		if err := jsonv2.Unmarshal(raw, &arg, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		update.ClearRestriction = true
	default:
		return nil, plugin.ErrUnavailable
	}
	unlock, err := h.agent.LockAdmissionContext(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := h.agent.PluginWorkflowBusyAdmitted(); err != nil {
		return nil, err
	}
	if update.Restriction != nil || update.ClearRestriction {
		if h.app.Subagents != nil && h.app.Subagents.HasActive() {
			return nil, errors.New("finish child work before changing tool restrictions")
		}
		if err := h.validateToolRestriction(update.Restriction); err != nil {
			return nil, err
		}
	}
	state, err := store.WorkflowState(ctx, inv.PluginID)
	if err != nil {
		return nil, err
	}
	result, err := store.ApplyWorkflowState(ctx, inv.PluginID, state.BranchID, state.TipID, update)
	if err != nil {
		return nil, err
	}
	h.app.refreshPluginToolPolicy()
	h.agent.Publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	return jsonv2.Marshal(map[string]string{"branchId": result.BranchID, "tipId": result.TipID})
}

func (h *appExtensionHost) validateToolRestriction(r *session.ToolRestriction) error {
	if r == nil {
		return nil
	}
	var allow []string
	if r.Allow != nil {
		allow = *r.Allow
	}
	if len(allow) > 512 || len(r.Deny) > 512 {
		return errors.New("tool restriction name limit exceeded")
	}
	for _, names := range [][]string{allow, r.Deny} {
		for _, name := range names {
			if len(name) == 0 || len(name) > 256 {
				return errors.New("invalid tool restriction name")
			}
			for _, c := range name {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
					return errors.New("tool restrictions require exact canonical names")
				}
			}
		}
	}
	for _, name := range allow {
		if _, ok := h.app.Registry.Get(name); !ok {
			return fmt.Errorf("unknown allowlisted tool %q", name)
		}
	}
	return nil
}
func (h *appExtensionHost) listPluginTools() (json.RawMessage, error) {
	type item struct {
		Name    string           `json:"name"`
		Source  tools.Source     `json:"source"`
		Effect  tools.ToolEffect `json:"effect"`
		Allowed bool             `json:"allowed"`
		Reason  string           `json:"reason,omitempty"`
	}
	var result []item
	policy := h.services.toolPolicy.Load()
	for _, desc := range h.app.Registry.Descriptors() {
		entry := item{Name: desc.Schema.Name, Source: desc.Source, Effect: desc.Effect, Allowed: true}
		if err := policy.check(entry.Name); err != nil {
			entry.Allowed = false
			entry.Reason = err.Error()
		}
		if !h.agent.ToolAvailable(entry.Name) {
			entry.Allowed = false
			if entry.Reason == "" {
				entry.Reason = "native mode or operator policy"
			}
		}
		result = append(result, entry)
	}
	slices.SortFunc(result, func(a, b item) int { return strings.Compare(a.Name, b.Name) })
	raw, err := jsonv2.Marshal(result)
	if err == nil && len(raw) > h.app.Cfg.ToolOutputLimit() {
		return nil, errors.New("tool catalog exceeds host output limit")
	}
	return raw, err
}
