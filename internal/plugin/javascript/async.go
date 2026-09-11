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

type asyncResult struct {
	raw json.RawMessage
	err error
}
type invocation struct {
	ctx         context.Context
	identity    plugin.Invocation
	environment plugin.Environment
	host        plugin.ExtensionHost
	active      atomic.Bool
	hosts       sync.WaitGroup
	hostMu      sync.Mutex
	result      chan asyncResult
	calls       int  // worker-owned
	toolPending bool // worker-owned
	tool        *plugin.ToolContext
}

// Async context tracking restores the authority of the callback which awaits
// a promise, rather than the authority of the callback resolving that promise.
type asyncTracker struct {
	state *extensionState
	stack []*invocation
}

func (t *asyncTracker) Grab() any { return t.state.current }
func (t *asyncTracker) Resumed(value any) {
	t.stack = append(t.stack, t.state.current)
	t.state.current, _ = value.(*invocation)
}
func (t *asyncTracker) Exited() {
	t.state.current = t.stack[len(t.stack)-1]
	t.stack = t.stack[:len(t.stack)-1]
}

func (r *Runtime) invokeAsync(ctx context.Context, name, kind string, uses []string, budget time.Duration, fn goja.Callable, input json.RawMessage, tc *plugin.ToolContext) (json.RawMessage, error) {
	if err := r.beginReloadActivity(); err != nil {
		return nil, err
	}
	defer r.endReloadActivity()
	if len(r.pkg.Manifest.HostTools) > 0 {
		uses = append(slices.Clone(uses), "tools")
	}
	if r.extension.pending.Add(1) > 32 {
		r.extension.pending.Add(-1)
		return nil, errors.New("plugin pending invocation limit exceeded")
	}
	defer r.extension.pending.Add(-1)
	if len(input) > r.opts.MaxOutputBytes {
		return nil, errors.New("plugin input exceeds limit")
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	stop := context.AfterFunc(r.extension.lifetime, cancel)
	defer func() { stop(); cancel() }()
	r.extension.mu.RLock()
	host := r.extension.host
	r.extension.mu.RUnlock()
	env := plugin.Environment{Kind: "root"}
	if host != nil {
		// Pure transition gates execute while the app holds exclusive session
		// admission. An immutable snapshot avoids reentering its session lock.
		if snapshot, ok := host.(interface{ HookEnvironment() plugin.Environment }); kind == "hook" && ok {
			env = snapshot.HookEnvironment()
		} else {
			env = host.Environment()
		}
	}
	if r.opts.ChildTools != nil {
		env.Kind = "child"
	}
	op := &invocation{ctx: ctx, identity: plugin.Invocation{Generation: env.Generation, PluginID: r.pkg.Manifest.ID, Name: name, Kind: kind, Fingerprint: r.pkg.Fingerprint, Uses: slices.Clone(uses)}, host: host, environment: env, result: make(chan asyncResult, 1), tool: tc}
	if tc != nil {
		op.identity.ToolCallID = tc.ToolCallID
		op.environment.SessionID = tc.SessionID
		op.environment.CWD = tc.CWD
	}
	op.active.Store(true)
	defer func() { op.hostMu.Lock(); op.active.Store(false); cancel(); op.hostMu.Unlock(); op.hosts.Wait() }()
	err := r.submit(ctx, time.Second, func() error {
		previous := r.extension.current
		r.extension.current = op
		defer func() { r.extension.current = previous }()
		arg, err := r.decode(input)
		if err != nil {
			return err
		}
		contextValue, err := r.asyncContext(op)
		if err != nil {
			return err
		}
		value, err := fn(goja.Undefined(), arg, contextValue)
		if err != nil {
			return err
		}
		// Promise.resolve handles native promises and thenables on the VM owner.
		promise := r.vm.Get("Promise").ToObject(r.vm)
		resolve, _ := goja.AssertFunction(promise.Get("resolve"))
		resolved, err := resolve(promise, value)
		if err != nil {
			return err
		}
		then, _ := goja.AssertFunction(resolved.ToObject(r.vm).Get("then"))
		_, err = then(resolved, r.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			raw, encodeErr := r.encode(call.Argument(0), r.opts.MaxOutputBytes)
			if goja.IsUndefined(call.Argument(0)) {
				raw, encodeErr = []byte("null"), nil
			}
			if op.active.Swap(false) {
				r.extension.settled = append(r.extension.settled, settledInvocation{op, asyncResult{raw: raw, err: encodeErr}})
			}
			return goja.Undefined()
		}), r.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			message := "JavaScript promise rejected"
			v := call.Argument(0)
			if object, ok := v.(*goja.Object); ok {
				if text := object.Get("message"); text != nil && !goja.IsUndefined(text) {
					message = boundText(text.String(), MaxLogBytes)
				}
			} else {
				message = boundText(v.String(), MaxLogBytes)
			}
			if op.active.Swap(false) {
				r.extension.settled = append(r.extension.settled, settledInvocation{op, asyncResult{err: errors.New(message)}})
			}
			return goja.Undefined()
		}))
		return err
	})
	if err != nil {
		return nil, err
	}
	select {
	case result := <-op.result:
		return result.raw, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return nil, errors.New("plugin runtime closed")
	}
}

func (r *Runtime) asyncContext(op *invocation) (*goja.Object, error) {
	ctx := r.vm.NewObject()
	for key, value := range map[string]any{"sessionId": op.environment.SessionID, "cwd": op.environment.CWD, "kind": op.environment.Kind, "toolCallId": op.identity.ToolCallID} {
		if err := ctx.Set(key, value); err != nil {
			return nil, err
		}
	}
	groups := map[string][]string{
		"agent":     {"state", "prompt", "steer", "followUp", "pending", "abort"},
		"models":    {"list", "set"},
		"session":   {"messages", "branches", "rename", "fork", "selectBranch", "renameBranch", "deleteBranch", "compact"},
		"goals":     {"get", "create", "edit", "pause", "resume", "clear"},
		"subagents": {"models", "spawn", "list", "get", "messages", "message", "followUp", "wait", "interrupt", "close", "resume"},
		"storage":   {"get", "set", "delete"},
		"workflow":  {"get", "set", "delete", "update"},
		"tools":     {"call", "list", "restrict", "clearRestriction"},
		"ui":        {"update", "open", "close", "notify", "input", "select", "confirm", "form", "editorGet", "editorSet", "editorInsert", "theme"},
	}
	for group, methods := range groups {
		object := r.vm.NewObject()
		if group == "ui" {
			_ = object.Set("available", op.environment.UI)
		}
		for _, method := range methods {
			operation := group + "." + method
			if err := object.Set(method, func(call goja.FunctionCall) goja.Value { return r.asyncHostCall(op, operation, call) }); err != nil {
				return nil, err
			}
		}
		if err := ctx.Set(group, object); err != nil {
			return nil, err
		}
	}
	if err := ctx.Set("sleep", func(call goja.FunctionCall) goja.Value { return r.asyncHostCall(op, "sleep", call) }); err != nil {
		return nil, err
	}
	if err := ctx.Set("progress", func(call goja.FunctionCall) goja.Value {
		r.checkInvocation(op)
		if op.tool != nil && op.tool.Progress != nil {
			if err := op.tool.Progress(plugin.ProgressUpdate{Message: boundText(call.Argument(0).String(), r.opts.MaxProgressBytes)}); err != nil {
				r.fail(err)
			}
		}
		return goja.Undefined()
	}); err != nil {
		return nil, err
	}
	return ctx, nil
}

func (r *Runtime) checkInvocation(op *invocation) {
	if !op.active.Load() || r.extension.current != op {
		r.fail(errors.New("plugin invocation is no longer active"))
	}
	if err := op.ctx.Err(); err != nil {
		r.fail(err)
	}
}

func (r *Runtime) asyncHostCall(op *invocation, operation string, call goja.FunctionCall) goja.Value {
	r.checkInvocation(op)
	if op.identity.Kind == "hook" || op.identity.Kind == "renderer" {
		r.fail(errors.New("hooks and renderers cannot perform host operations"))
	}
	if !plugin.AllowsOperation(op.identity.Uses, operation) {
		r.fail(fmt.Errorf("undeclared capability for %s", operation))
	}
	if op.environment.Kind == "child" && operation != "tools.call" && !strings.HasPrefix(operation, "storage.") && operation != "sleep" {
		r.fail(errors.New("host operation unavailable in child tool profile"))
	}
	limit := MaxHostCalls
	if op.identity.Kind == "command" {
		limit = 4096
	}
	if op.calls >= limit {
		r.fail(errors.New("host call limit exceeded"))
	}
	arg := call.Argument(0)
	if goja.IsUndefined(arg) {
		arg = r.vm.NewObject()
	}
	raw, err := r.encode(arg, r.opts.MaxOutputBytes)
	if err != nil {
		r.fail(err)
	}
	if operation == "tools.call" {
		if op.toolPending {
			r.fail(errors.New("await the previous tool call before starting another"))
		}
		name := call.Argument(0).String()
		if !slices.Contains(r.pkg.Manifest.HostTools, name) || !slices.Contains(op.identity.Uses, name) {
			r.fail(fmt.Errorf("undeclared host tool %q", name))
		}
		args := call.Argument(1)
		if goja.IsUndefined(args) {
			args = r.vm.NewObject()
		}
		encoded, encodeErr := r.encode(args, r.opts.MaxOutputBytes)
		if encodeErr != nil {
			r.fail(encodeErr)
		}
		raw, err = jsonv2.Marshal(struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}{name, encoded})
		if err != nil {
			r.fail(err)
		}
	}
	if op.host == nil {
		r.fail(errors.New("host I/O unavailable in this runtime"))
	}
	op.calls++
	if operation == "tools.call" {
		op.toolPending = true
	}
	promise, resolve, reject := r.vm.NewPromise()
	op.hostMu.Lock()
	if !op.active.Load() || op.ctx.Err() != nil {
		op.hostMu.Unlock()
		r.fail(errors.New("plugin invocation is no longer active"))
	}
	op.hosts.Go(func() {
		result, hostErr := callExtensionHost(op.ctx, op.host, op.identity, operation, raw)
		if len(result) > r.opts.MaxOutputBytes {
			result = nil
			hostErr = errors.New("host result exceeds limit")
		}
		if !op.active.Load() || op.ctx.Err() != nil {
			return
		}
		err := r.submit(op.ctx, time.Second, func() error {
			if !op.active.Load() {
				return nil
			}
			if operation == "tools.call" {
				op.toolPending = false
			}
			previous := r.extension.current
			r.extension.current = op
			defer func() { r.extension.current = previous }()
			if hostErr != nil {
				return reject(r.vm.NewGoError(errors.New(boundText(hostErr.Error(), MaxLogBytes))))
			}
			if len(result) == 0 {
				result = []byte("null")
			}
			value, err := r.decode(result)
			if err != nil {
				return reject(r.vm.NewGoError(err))
			}
			return resolve(value)
		})
		if err != nil && op.active.Swap(false) {
			op.result <- asyncResult{err: err}
		}
	})
	op.hostMu.Unlock()
	return r.vm.ToValue(promise)
}

func decodeScriptResult(raw json.RawMessage) (plugin.ToolResult, error) {
	if string(raw) == "null" {
		return plugin.ToolResult{Content: []protocol.ContentBlock{}}, nil
	}
	var text string
	if jsonv2.Unmarshal(raw, &text) == nil {
		return plugin.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(text)}}, nil
	}
	var wire scriptResult
	if err := jsonv2.Unmarshal(raw, &wire, jsonv2.RejectUnknownMembers(true)); err != nil {
		return plugin.ToolResult{}, fmt.Errorf("invalid plugin result: %w", err)
	}
	if wire.Content == nil {
		return plugin.ToolResult{}, errors.New("result requires content array")
	}
	out := plugin.ToolResult{IsError: wire.IsError}
	if len(wire.Details) > 0 {
		out.Details = wire.Details
	}
	for _, block := range wire.Content {
		if block.Type != "text" {
			return plugin.ToolResult{}, errors.New("plugin results support text only")
		}
		out.Content = append(out.Content, protocol.NewTextBlock(block.Text))
	}
	return out, nil
}

func (r *Runtime) invokeAsyncTool(ctx context.Context, fn goja.Callable, def toolDefinition, tc plugin.ToolContext, args json.RawMessage) (plugin.ToolResult, error) {
	uses := slices.Clone(def.Uses)
	if len(r.pkg.Manifest.HostTools) > 0 {
		uses = append(uses, "tools")
	}
	raw, err := r.invokeAsync(ctx, def.Name, "tool", uses, 120*time.Second, fn, args, &tc)
	if err != nil {
		return plugin.ToolResult{}, err
	}
	return decodeScriptResult(raw)
}

// Publish settlement only after execute has detached its cancellation watcher.
// A consumer may immediately cancel the invocation when it receives a result.
type settledInvocation struct {
	op     *invocation
	result asyncResult
}

func (r *Runtime) flushSettled() {
	settled := r.extension.settled
	r.extension.settled = nil
	for _, item := range settled {
		item.op.result <- item.result
	}
}

func callExtensionHost(ctx context.Context, host plugin.ExtensionHost, inv plugin.Invocation, operation string, raw json.RawMessage) (result json.RawMessage, err error) {
	defer func() {
		if recover() != nil {
			result = nil
			err = errors.New("plugin host callback panicked")
		}
	}()
	return host.Call(ctx, inv, operation, raw)
}
