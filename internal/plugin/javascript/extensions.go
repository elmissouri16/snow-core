package javascript

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type commandHandler struct {
	info protocol.PluginCommand
	uses []string
	fn   goja.Callable
}
type hookHandler struct {
	phase    string
	children bool
	fn       goja.Callable
}
type extensionState struct {
	settled   []settledInvocation // worker-owned
	pending   atomic.Int32
	mu        sync.RWMutex
	host      plugin.ExtensionHost
	lifetime  context.Context
	cancel    context.CancelFunc
	commands  []commandHandler
	hooks     []hookHandler
	views     []protocol.PluginView
	themes    []protocol.PluginTheme
	renderers map[string]goja.Callable
	ready     goja.Callable
	current   *invocation // worker-owned
	observing bool        // worker-owned
}

func newExtensionState() *extensionState {
	ctx, cancel := context.WithCancel(context.Background())
	return &extensionState{lifetime: ctx, cancel: cancel, renderers: map[string]goja.Callable{}}
}

func (r *Runtime) installExtensions(api *goja.Object) error {
	for name, fn := range map[string]func(goja.FunctionCall) goja.Value{
		"registerCommand":      r.registerCommand,
		"registerHook":         r.registerHook,
		"registerView":         r.registerView,
		"registerTheme":        r.registerTheme,
		"registerToolRenderer": r.registerRenderer,
		"onReady": func(call goja.FunctionCall) goja.Value {
			r.requireRegistration("")
			fn, ok := goja.AssertFunction(call.Argument(0))
			if !ok || r.extension.ready != nil {
				r.fail(errors.New("onReady requires one startup handler"))
			}
			if r.opts.ChildTools == nil {
				r.extension.ready = fn
			}
			return goja.Undefined()
		},
	} {
		if err := api.Set(name, fn); err != nil {
			return err
		}
	}
	kind := "root"
	if r.opts.ChildTools != nil {
		kind = "child"
	}
	return api.Set("runtime", map[string]any{"apiVersion": 2, "kind": kind})
}
func (r *Runtime) requireRegistration(capability string) {
	if !r.registering {
		r.fail(errors.New("extensions must be registered during startup"))
	}
	if capability != "" && !slices.Contains(r.pkg.Manifest.Capabilities, capability) {
		r.fail(fmt.Errorf("manifest must declare %s capability", capability))
	}
}
func (r *Runtime) metadata(value goja.Value, keys []string, out any) *goja.Object {
	object, ok := value.(*goja.Object)
	if !ok {
		r.fail(errors.New("registration requires an object"))
	}
	metadata := r.vm.NewObject()
	for _, key := range keys {
		if err := metadata.Set(key, object.Get(key)); err != nil {
			r.fail(err)
		}
	}
	raw, err := r.encode(metadata, MaxManifestBytes)
	if err != nil {
		r.fail(err)
	}
	if err := jsonv2.Unmarshal(raw, out); err != nil {
		r.fail(err)
	}
	return object
}
func (r *Runtime) registerCommand(call goja.FunctionCall) goja.Value {
	r.requireRegistration("commands")
	var def struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		ArgumentHint string   `json:"argumentHint"`
		Alias        string   `json:"alias"`
		Shortcut     string   `json:"shortcut"`
		Uses         []string `json:"uses"`
		TimeoutMS    int      `json:"timeoutMS"`
	}
	object := r.metadata(call.Argument(0), []string{"name", "description", "argumentHint", "alias", "shortcut", "uses", "timeoutMS"}, &def)
	if err := plugin.ValidateIdentifier("command", def.Name); err != nil {
		r.fail(err)
	}
	if def.Description == "" || def.Description == "undefined" {
		r.fail(errors.New("command description is required"))
	}
	fn, ok := goja.AssertFunction(object.Get("run"))
	if !ok {
		r.fail(errors.New("command run must be a function"))
	}
	if def.Alias != "" {
		if err := plugin.ValidateIdentifier("alias", def.Alias); err != nil {
			r.fail(err)
		}
	}
	if def.Shortcut != "" && (!strings.HasPrefix(def.Shortcut, "alt+") || len(def.Shortcut) != 5 || def.Shortcut[4] < 'a' || def.Shortcut[4] > 'z') {
		r.fail(errors.New("shortcut must be alt+ followed by one lowercase letter"))
	}
	if def.TimeoutMS == 0 {
		def.TimeoutMS = int((10 * time.Minute) / time.Millisecond)
	}
	if def.TimeoutMS < 1 || def.TimeoutMS > int((30*time.Minute)/time.Millisecond) {
		r.fail(errors.New("command timeoutMS must be between 1 and 1800000"))
	}
	if len(r.extension.commands) >= 64 {
		r.fail(errors.New("command limit exceeded"))
	}
	for _, item := range r.extension.commands {
		if item.info.Name == def.Name {
			r.fail(errors.New("duplicate command"))
		}
	}
	for _, use := range def.Uses {
		if !slices.Contains(r.pkg.Manifest.Capabilities, use) && !slices.Contains(r.pkg.Manifest.HostTools, use) {
			r.fail(fmt.Errorf("undeclared command capability %s", use))
		}
	}
	if r.opts.ChildTools == nil {
		info := protocol.PluginCommand{ID: r.pkg.Manifest.ID + ":" + def.Name, PluginID: r.pkg.Manifest.ID, Name: def.Name, Description: def.Description, ArgumentHint: def.ArgumentHint, Alias: def.Alias, Shortcut: def.Shortcut, TimeoutMS: def.TimeoutMS}
		r.extension.commands = append(r.extension.commands, commandHandler{info: info, uses: def.Uses, fn: fn})
	}
	return goja.Undefined()
}
func (r *Runtime) registerHook(call goja.FunctionCall) goja.Value {
	r.requireRegistration("hooks")
	phase := call.Argument(0).String()
	if !slices.Contains([]string{"before_prompt", "before_request", "before_tool", "after_tool"}, phase) {
		r.fail(errors.New("unknown hook phase"))
	}
	fn, ok := goja.AssertFunction(call.Argument(1))
	if !ok {
		r.fail(errors.New("hook requires a handler"))
	}
	children := false
	if object, ok := call.Argument(2).(*goja.Object); ok {
		if value := object.Get("includeSubagents"); value != nil {
			children = value.ToBoolean()
		}
	}
	if len(r.extension.hooks) >= 64 {
		r.fail(errors.New("hook limit exceeded"))
	}
	if r.opts.ChildTools == nil {
		r.extension.hooks = append(r.extension.hooks, hookHandler{phase: phase, children: children, fn: fn})
	}
	return goja.Undefined()
}
func (r *Runtime) registerView(call goja.FunctionCall) goja.Value {
	r.requireRegistration("ui")
	var view protocol.PluginView
	r.metadata(call.Argument(0), []string{"name", "title", "placement"}, &view)
	if err := plugin.ValidateIdentifier("view", view.Name); err != nil {
		r.fail(err)
	}
	if !slices.Contains([]string{"header", "footer", "sidebar", "above_input", "screen"}, view.Placement) {
		r.fail(errors.New("invalid view placement"))
	}
	if len(r.extension.views) >= 16 {
		r.fail(errors.New("view limit exceeded"))
	}
	for _, item := range r.extension.views {
		if item.Name == view.Name {
			r.fail(errors.New("duplicate view"))
		}
	}
	view.PluginID, view.ID = r.pkg.Manifest.ID, r.pkg.Manifest.ID+":"+view.Name
	if r.opts.ChildTools == nil {
		r.extension.views = append(r.extension.views, view)
	}
	return goja.Undefined()
}
func (r *Runtime) registerTheme(call goja.FunctionCall) goja.Value {
	r.requireRegistration("ui")
	var theme protocol.PluginTheme
	r.metadata(call.Argument(0), []string{"name", "colors"}, &theme)
	if err := validateTheme(theme); err != nil {
		r.fail(err)
	}
	if len(r.extension.themes) >= 8 {
		r.fail(errors.New("theme limit exceeded"))
	}
	for _, item := range r.extension.themes {
		if item.Name == theme.Name {
			r.fail(errors.New("duplicate theme"))
		}
	}
	theme.PluginID, theme.ID = r.pkg.Manifest.ID, r.pkg.Manifest.ID+":"+theme.Name
	if r.opts.ChildTools == nil {
		r.extension.themes = append(r.extension.themes, theme)
	}
	return goja.Undefined()
}
func (r *Runtime) registerRenderer(call goja.FunctionCall) goja.Value {
	r.requireRegistration("ui")
	name := call.Argument(0).String()
	if err := plugin.ValidateIdentifier("renderer tool", name); err != nil {
		r.fail(err)
	}
	fn, ok := goja.AssertFunction(call.Argument(1))
	if !ok || r.extension.renderers[name] != nil {
		r.fail(errors.New("renderer requires a unique tool and function"))
	}
	if len(r.extension.renderers) >= MaxTools {
		r.fail(errors.New("renderer limit exceeded"))
	}
	if r.opts.ChildTools == nil {
		r.extension.renderers[name] = fn
	}
	return goja.Undefined()
}

func (r *Runtime) ExtensionInfo() protocol.PluginInfo {
	m := r.pkg.Manifest
	info := protocol.PluginInfo{Path: r.pkg.Path, ID: m.ID, Name: m.Name, Version: m.Version, APIVersion: m.APIVersion, Capabilities: m.Capabilities, HostTools: m.HostTools, Views: r.extension.views, Themes: r.extension.themes, Settings: m.Settings, Config: r.pkg.Config}
	for _, c := range r.extension.commands {
		info.Commands = append(info.Commands, c.info)
	}
	raw, _ := jsonv2.Marshal(info)
	var copy protocol.PluginInfo
	_ = jsonv2.Unmarshal(raw, &copy)
	return copy
}
func (r *Runtime) BindHost(host plugin.ExtensionHost) {
	r.extension.mu.Lock()
	r.extension.host = host
	r.extension.mu.Unlock()
}
func (r *Runtime) Ready(ctx context.Context) error {
	if r.extension.ready == nil {
		return nil
	}
	_, err := r.invokeAsync(ctx, "ready", "observer", r.observerUses(), 10*time.Second, r.extension.ready, []byte("{}"), nil)
	return err
}
func (r *Runtime) RunCommand(ctx context.Context, name, input string) (plugin.ToolResult, error) {
	for _, cmd := range r.extension.commands {
		if cmd.info.Name == name {
			raw, _ := jsonv2.Marshal(input)
			result, err := r.invokeAsync(ctx, name, "command", cmd.uses, time.Duration(cmd.info.TimeoutMS)*time.Millisecond, cmd.fn, raw, nil)
			if err != nil {
				return plugin.ToolResult{}, err
			}
			return decodeScriptResult(result)
		}
	}
	return plugin.ToolResult{}, errors.New("unknown plugin command")
}
func (r *Runtime) HasHook(phase string, child bool) bool {
	for _, h := range r.extension.hooks {
		if h.phase == phase && (!child || h.children) {
			return true
		}
	}
	return false
}
func (r *Runtime) RunHook(ctx context.Context, request plugin.HookRequest) (plugin.HookResult, error) {
	var combined plugin.HookResult
	for _, h := range r.extension.hooks {
		if h.phase != request.Phase || (request.Agent != nil && !h.children) {
			continue
		}
		raw, err := jsonv2.Marshal(request)
		if err != nil {
			return combined, err
		}
		result, err := r.invokeAsync(ctx, h.phase, "hook", nil, time.Second, h.fn, raw, nil)
		if err != nil {
			return combined, err
		}
		var change plugin.HookResult
		if err := jsonv2.Unmarshal(result, &change, jsonv2.RejectUnknownMembers(true)); err != nil {
			return combined, err
		}
		if change.Block != "" {
			return change, nil
		}
		if change.Text != nil {
			combined.Text, request.Text = change.Text, *change.Text
		}
		if change.Arguments != nil {
			combined.Arguments, request.Arguments = change.Arguments, change.Arguments
		}
		if change.Content != nil {
			combined.Content, request.Content = change.Content, change.Content
		}
		if change.Context != nil {
			combined.Context = append(combined.Context, change.Context...)
			request.Context = append(request.Context, change.Context...)
		}
	}
	return combined, nil
}
func (r *Runtime) RenderTool(ctx context.Context, name string, data json.RawMessage) (*protocol.PluginNode, error) {
	fn := r.extension.renderers[name]
	if fn == nil {
		return nil, nil
	}
	raw, err := r.invokeAsync(ctx, name, "renderer", nil, time.Second, fn, data, nil)
	if err != nil {
		return nil, err
	}
	var node protocol.PluginNode
	if err := jsonv2.Unmarshal(raw, &node, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *Runtime) observerUses() []string {
	uses := append(slices.Clone(r.pkg.Manifest.Capabilities), r.pkg.Manifest.HostTools...)
	if len(r.pkg.Manifest.HostTools) > 0 {
		uses = append(uses, "tools")
	}
	return uses
}
