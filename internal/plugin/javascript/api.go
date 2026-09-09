package javascript

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dop251/goja"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type toolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Uses        []string        `json:"uses"`
	Child       bool            `json:"child,omitzero"`
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type scriptResult struct {
	Content []textBlock     `json:"content"`
	IsError bool            `json:"isError,omitzero"`
	Details json.RawMessage `json:"details,omitempty"`
}

func (r *Runtime) fail(err error) {
	err = r.plainError(err)
	r.lastError = boundText(err.Error(), MaxLogBytes)
	r.lastThrown = r.vm.NewGoError(errors.New(r.lastError))
	panic(r.lastThrown)
}
func (r *Runtime) installAPI() error {
	jsonObject := r.vm.Get("JSON").ToObject(r.vm)
	r.stringify, _ = goja.AssertFunction(jsonObject.Get("stringify"))
	r.parse, _ = goja.AssertFunction(jsonObject.Get("parse"))
	api := r.vm.NewObject()
	config, err := r.decode(r.pkg.Config)
	if err != nil {
		return err
	}
	for name, value := range map[string]any{
		"config": config,
		"registerTool": func(call goja.FunctionCall) goja.Value {
			if err := r.registerTool(call.Argument(0)); err != nil {
				r.fail(err)
			}
			return goja.Undefined()
		},
		"on": func(call goja.FunctionCall) goja.Value {
			if err := r.subscribe(call.Argument(0).String(), call.Argument(1)); err != nil {
				r.fail(err)
			}
			return goja.Undefined()
		},
		"onClose": func(call goja.FunctionCall) goja.Value {
			if !r.registering || r.onClose != nil {
				r.fail(errors.New("onClose may be registered once during startup"))
			}
			fn, ok := goja.AssertFunction(call.Argument(0))
			if !ok {
				r.fail(errors.New("onClose requires a function"))
			}
			r.onClose = fn
			return goja.Undefined()
		},
		"log": func(call goja.FunctionCall) goja.Value {
			level := call.Argument(0).String()
			if !slices.Contains([]string{"info", "warning", "error"}, level) {
				r.fail(errors.New("log level must be info, warning, or error"))
			}
			if r.logs < MaxLogs {
				r.logs++
				r.diagnostic(level, call.Argument(1).String())
			}
			return goja.Undefined()
		},
	} {
		if err := api.DefineDataProperty(name, r.vm.ToValue(value), goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_TRUE); err != nil {
			return err
		}
	}
	if r.pkg.Manifest.APIVersion == 2 {
		if err := r.installExtensions(api); err != nil {
			return err
		}
	}
	return r.vm.GlobalObject().DefineDataProperty("snow", api, goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_TRUE)
}

func (r *Runtime) registerTool(value goja.Value) error {
	if !r.registering {
		return errors.New("registerTool is only available during startup")
	}
	if r.tools >= MaxTools {
		return errors.New("plugin exceeds 64 registered tools")
	}
	obj, ok := value.(*goja.Object)
	if !ok {
		return errors.New("tool definition must be an object")
	}
	fn, ok := goja.AssertFunction(obj.Get("execute"))
	if !ok {
		return errors.New("tool execute must be a function")
	}
	// Copy only metadata. Never export closures or host objects into Go.
	metadata := r.vm.NewObject()
	for _, key := range []string{"name", "description", "parameters", "uses", "child"} {
		if err := metadata.Set(key, obj.Get(key)); err != nil {
			return err
		}
	}
	raw, err := r.encode(metadata, MaxManifestBytes)
	if err != nil {
		return err
	}
	var def toolDefinition
	if err = jsonv2.Unmarshal(raw, &def); err != nil {
		return err
	}
	if def.Description == "" {
		return errors.New("tool description is required")
	}
	var schema map[string]any
	if err = jsonv2.Unmarshal(def.Parameters, &schema); err != nil || schema == nil {
		return errors.New("parameters must be a JSON schema object")
	}
	seen := map[string]bool{}
	for _, name := range def.Uses {
		if (!slices.Contains(r.pkg.Manifest.HostTools, name) && !(r.pkg.Manifest.APIVersion == 2 && slices.Contains(r.pkg.Manifest.Capabilities, name))) || seen[name] {
			return fmt.Errorf("tool uses undeclared or duplicate host capability %q", name)
		}
		seen[name] = true
	}
	if def.Child && r.pkg.Manifest.APIVersion == 2 {
		r.childDefinitions = append(r.childDefinitions, def)
	}
	if r.opts.ChildTools != nil && (!def.Child || !slices.Contains(r.opts.ChildTools, def.Name)) {
		return nil
	}
	risk := "read"
	if slices.Contains(def.Uses, "bash") {
		risk = "exec"
	} else if slices.Contains(def.Uses, "write") || slices.Contains(def.Uses, "edit") {
		risk = "write"
	} else if slices.Contains(def.Uses, "webfetch") {
		risk = "network"
	}
	if r.pkg.Manifest.APIVersion == 2 {
		for _, use := range def.Uses {
			if !slices.Contains([]string{"read", "glob", "grep", "storage", "ui"}, use) {
				risk = "exec"
			}
		}
	}
	err = r.registrar.RegisterTool(plugin.ToolDefinition{
		Name: def.Name, Description: def.Description, Parameters: def.Parameters, Risk: risk,
		Executor: func(ctx context.Context, tc plugin.ToolContext, args json.RawMessage) (plugin.ToolResult, error) {
			if r.pkg.Manifest.APIVersion == 2 {
				return r.invokeAsyncTool(ctx, fn, def, tc, args)
			}
			var result plugin.ToolResult
			err := r.submit(ctx, 120*time.Second, func() error {
				var err error
				result, err = r.invoke(fn, def.Uses, tc, args)
				return err
			})
			return result, err
		},
	})
	if err == nil {
		r.tools++
	}
	return err
}

func (r *Runtime) invoke(fn goja.Callable, uses []string, tc plugin.ToolContext, args json.RawMessage) (plugin.ToolResult, error) {
	if len(args) > r.opts.MaxOutputBytes {
		return plugin.ToolResult{}, errors.New("plugin arguments exceed output limit")
	}
	argument, err := r.decode(args)
	if err != nil {
		return plugin.ToolResult{}, err
	}
	active := true
	defer func() { active = false }()
	calls := 0
	contextObject := r.vm.NewObject()
	for key, value := range map[string]any{"sessionId": tc.SessionID, "cwd": tc.CWD, "toolCallId": tc.ToolCallID} {
		if err = contextObject.Set(key, value); err != nil {
			return plugin.ToolResult{}, err
		}
	}
	ctx := r.currentContext
	check := func() {
		if !active || r.currentContext != ctx {
			r.fail(errors.New("tool context is no longer active"))
		}
		if err := ctx.Err(); err != nil {
			r.fail(err)
		}
	}
	if err = contextObject.Set("progress", func(call goja.FunctionCall) goja.Value {
		check()
		message := boundText(call.Argument(0).String(), r.opts.MaxProgressBytes)
		if tc.Progress != nil {
			if err := tc.Progress(plugin.ProgressUpdate{Message: message}); err != nil {
				r.fail(err)
			}
		}
		return goja.Undefined()
	}); err != nil {
		return plugin.ToolResult{}, err
	}
	if err = contextObject.Set("callTool", func(call goja.FunctionCall) goja.Value {
		check()
		name := call.Argument(0).String()
		if !slices.Contains(uses, name) {
			r.fail(fmt.Errorf("host tool %q is not declared in this tool's uses", name))
		}
		if calls >= MaxHostCalls {
			r.fail(errors.New("host call limit exceeded"))
		}
		calls++
		if tc.CallTool == nil {
			r.fail(errors.New("host I/O unavailable in this runtime"))
		}
		raw, err := r.encode(call.Argument(1), r.opts.MaxOutputBytes)
		if err != nil {
			r.fail(err)
		}
		var object map[string]any
		if err = jsonv2.Unmarshal(raw, &object); err != nil || object == nil {
			r.fail(errors.New("host arguments must be an object"))
		}
		result, err := tc.CallTool(ctx, name, raw)
		if err != nil {
			r.fail(err)
		}
		check()
		wire := scriptResult{IsError: result.IsError}
		for _, block := range result.Content {
			if block.Type != protocol.BlockText {
				r.fail(errors.New("JavaScript supports text results only"))
			}
			wire.Content = append(wire.Content, textBlock{Type: "text", Text: block.Text})
		}
		bytes, err := jsonv2.Marshal(wire)
		if err != nil {
			r.fail(err)
		}
		if len(bytes) > r.opts.MaxOutputBytes {
			r.fail(errors.New("host result exceeds output limit"))
		}
		value, err := r.decode(bytes)
		if err != nil {
			r.fail(err)
		}
		return value
	}); err != nil {
		return plugin.ToolResult{}, err
	}
	value, err := fn(goja.Undefined(), argument, contextObject)
	if err != nil {
		return plugin.ToolResult{}, err
	}
	if err = r.requireSync(value); err != nil {
		return plugin.ToolResult{}, err
	}
	raw, err := r.encode(value, r.opts.MaxOutputBytes)
	if err != nil {
		return plugin.ToolResult{}, err
	}
	var wire scriptResult
	if err = jsonv2.Unmarshal(raw, &wire, jsonv2.RejectUnknownMembers(true)); err != nil {
		return plugin.ToolResult{}, fmt.Errorf("invalid tool result: %w", err)
	}
	if wire.Content == nil {
		return plugin.ToolResult{}, errors.New("tool result requires a content array")
	}
	if r.pkg.Manifest.APIVersion == 1 && wire.Details != nil {
		return plugin.ToolResult{}, errors.New("details require JavaScript API 2")
	}
	result := plugin.ToolResult{IsError: wire.IsError}
	if len(wire.Details) > 0 {
		result.Details = wire.Details
	}
	for _, block := range wire.Content {
		if block.Type != "text" {
			return plugin.ToolResult{}, errors.New("JavaScript results support text blocks only")
		}
		result.Content = append(result.Content, protocol.NewTextBlock(block.Text))
	}
	return result, nil
}

func (r *Runtime) requireSync(value goja.Value) error {
	if object, ok := value.(*goja.Object); ok {
		if object.ExportType() == promiseType {
			return errors.New("async/Promise handlers are unsupported; return a synchronous result")
		}
		then := object.Get("then")
		if then != nil && !goja.IsUndefined(then) && !goja.IsNull(then) {
			return errors.New("thenable handler results are unsupported")
		}
	}
	return nil
}
func (r *Runtime) encode(value goja.Value, limit int) ([]byte, error) {
	out, err := r.stringify(goja.Undefined(), value)
	if err != nil {
		return nil, err
	}
	if goja.IsUndefined(out) {
		return nil, errors.New("value is not JSON serializable")
	}
	s := out.String()
	if len(s) > limit {
		return nil, fmt.Errorf("JSON value exceeds %d bytes", limit)
	}
	return []byte(s), nil
}
func (r *Runtime) decode(raw []byte) (goja.Value, error) {
	return r.parse(goja.Undefined(), r.vm.ToValue(string(raw)))
}
