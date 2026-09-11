package javascript

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const MaxFixtureBytes = 1 << 20
const MaxFixtureCases = 128

// FixtureSuite describes isolated, deterministic executions of the real JS
// adapter. Only explicitly declared mock host operations are available.
type FixtureSuite struct {
	Version int           `json:"version"`
	Tests   []FixtureCase `json:"tests"`
}
type FixtureCase struct {
	Name     string                                `json:"name"`
	Storage  map[string]map[string]json.RawMessage `json:"storage,omitempty"`
	Workflow map[string]json.RawMessage            `json:"workflow,omitempty"`
	Steps    []FixtureStep                         `json:"steps"`
}
type FixtureStep struct {
	Kind    string          `json:"kind"`
	Name    string          `json:"name,omitempty"`
	Input   string          `json:"input,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
	Request json.RawMessage `json:"request,omitempty"`
	Event   *plugin.Event   `json:"event,omitempty"`
	Calls   []FixtureCall   `json:"calls,omitempty"`
	Expect  json.RawMessage `json:"expect,omitempty"`
	Error   string          `json:"error,omitempty"`
	Select  string          `json:"select,omitempty"`
}
type FixtureCall struct {
	Operation string          `json:"operation"`
	Args      json.RawMessage `json:"args,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Memory    bool            `json:"memory,omitzero"`
}
type FixtureReport struct {
	Version  int                 `json:"version"`
	PluginID string              `json:"pluginId"`
	Passed   int                 `json:"passed"`
	Failed   int                 `json:"failed"`
	Tests    []FixtureCaseResult `json:"tests"`
}
type FixtureCaseResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

// ReadFixtures bounds and strictly decodes fixtures. It does not run code.
func ReadFixtures(reader io.Reader) (FixtureSuite, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, MaxFixtureBytes+1))
	if err != nil {
		return FixtureSuite{}, err
	}
	if len(raw) > MaxFixtureBytes {
		return FixtureSuite{}, errors.New("plugin fixtures exceed 1 MiB")
	}
	var suite FixtureSuite
	if err := jsonv2.Unmarshal(raw, &suite, jsonv2.RejectUnknownMembers(true)); err != nil {
		return suite, fmt.Errorf("plugin fixtures: %w", err)
	}
	if suite.Version != 1 {
		return suite, errors.New("plugin fixtures require version 1")
	}
	if len(suite.Tests) == 0 || len(suite.Tests) > MaxFixtureCases {
		return suite, errors.New("plugin fixtures require 1–128 tests")
	}
	names := map[string]bool{}
	for _, test := range suite.Tests {
		if strings.TrimSpace(test.Name) == "" || len(test.Name) > 256 || names[test.Name] {
			return suite, errors.New("plugin fixture names must be unique, nonempty, and at most 256 bytes")
		}
		names[test.Name] = true
		if len(test.Steps) == 0 || len(test.Steps) > 1024 {
			return suite, fmt.Errorf("fixture %q requires 1–1024 steps", test.Name)
		}
		for _, step := range test.Steps {
			switch step.Kind {
			case "command", "tool", "hook", "ready", "event", "metadata":
			default:
				return suite, fmt.Errorf("fixture %q: unknown step kind %q", test.Name, step.Kind)
			}
			if step.Error != "" && step.Expect != nil {
				return suite, errors.New("fixture step cannot expect both a result and an error")
			}
			for _, call := range step.Calls {
				if call.Operation == "" || (call.Memory && (call.Result != nil || call.Error != "")) || (call.Result != nil && call.Error != "") {
					return suite, errors.New("fixture calls require an operation and at most one of result, error, or memory")
				}
			}
		}
	}
	return suite, nil
}

// ReadFixturesFile reads a bounded regular fixture file through the same pinned
// directory and file-identity checks as package loading. Symlinks are rejected.
func ReadFixturesFile(ctx context.Context, path string) (FixtureSuite, error) {
	if err := ctx.Err(); err != nil {
		return FixtureSuite{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return FixtureSuite{}, err
	}
	root, err := openDirectory(filepath.Dir(abs), "")
	if err != nil {
		return FixtureSuite{}, err
	}
	defer root.Close()
	raw, err := readRegular(root, filepath.Base(abs), MaxFixtureBytes)
	if err != nil {
		return FixtureSuite{}, err
	}
	if err := ctx.Err(); err != nil {
		return FixtureSuite{}, err
	}
	return ReadFixtures(bytes.NewReader(raw))
}

// RunFixtures runs a fresh Goja runtime per test without any live host. The
// suite is bounded to 60 seconds and each case to five seconds. Event steps
// invoke production observer callbacks serially, not the asynchronous queue.
func RunFixtures(ctx context.Context, p *Package, suite FixtureSuite) (FixtureReport, error) {
	// Validate programmatic callers using the same bounded contract as the CLI.
	if p == nil {
		return FixtureReport{}, errors.New("plugin fixture package is nil")
	}
	raw, err := jsonv2.Marshal(suite)
	if err != nil {
		return FixtureReport{}, err
	}
	suite, err = ReadFixtures(bytes.NewReader(raw))
	if err != nil {
		return FixtureReport{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	report := FixtureReport{Version: 1, PluginID: p.Manifest.ID, Tests: []FixtureCaseResult{}}
	for _, test := range suite.Tests {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		caseCtx, caseCancel := context.WithTimeout(ctx, 5*time.Second)
		err := runFixtureCase(caseCtx, p, test)
		caseCancel()
		item := FixtureCaseResult{Name: test.Name, Passed: err == nil}
		if err != nil {
			item.Error = boundText(err.Error(), 4096)
			report.Failed++
		} else {
			report.Passed++
		}
		report.Tests = append(report.Tests, item)
	}
	return report, nil
}

func runFixtureCase(ctx context.Context, p *Package, test FixtureCase) (err error) {
	host := newFixtureHost(p, test)
	reg := &fixtureRegistrar{tools: map[string]plugin.ToolDefinition{}}
	r := New(p, Options{})
	defer func() { err = errors.Join(err, r.Close(ctx)) }()
	if err = r.Register(ctx, reg); err != nil {
		return fmt.Errorf("registration: %w", err)
	}
	r.BindHost(host)
	for i, step := range test.Steps {
		host.begin(step.Calls)
		value, callErr := runFixtureStep(ctx, r, reg, host, step)
		if ledgerErr := host.finish(); ledgerErr != nil {
			return fmt.Errorf("step %d (%s): %w", i+1, step.Kind, ledgerErr)
		}
		if step.Error != "" {
			if callErr == nil || !strings.Contains(callErr.Error(), step.Error) {
				return fmt.Errorf("step %d: expected error containing %q; got %v", i+1, step.Error, callErr)
			}
			continue
		}
		if callErr != nil {
			return fmt.Errorf("step %d (%s): %w", i+1, step.Kind, callErr)
		}
		if step.Expect != nil {
			if step.Select != "" {
				var selectErr error
				value, selectErr = fixtureSelect(value, step.Select)
				if selectErr != nil {
					return fmt.Errorf("step %d: %w", i+1, selectErr)
				}
			}
			actual, marshalErr := jsonv2.Marshal(value)
			if marshalErr != nil {
				return marshalErr
			}
			if !fixtureJSONEqual(step.Expect, actual) {
				return fmt.Errorf("step %d: result mismatch: want %s; got %s", i+1, boundText(string(step.Expect), 1024), boundText(string(actual), 1024))
			}
		}
	}
	return nil
}

func runFixtureStep(ctx context.Context, r *Runtime, reg *fixtureRegistrar, host *fixtureHost, step FixtureStep) (any, error) {
	switch step.Kind {
	case "metadata":
		toolMetadata := []any{}
		for _, name := range slices.Sorted(maps.Keys(reg.tools)) {
			def := reg.tools[name]
			toolMetadata = append(toolMetadata, map[string]any{"name": name, "description": def.Description, "parameters": def.Parameters, "risk": def.Risk})
		}
		return map[string]any{"plugin": r.ExtensionInfo(), "tools": toolMetadata, "events": reg.events}, nil
	case "ready":
		return nil, r.Ready(ctx)
	case "command":
		result, err := r.RunCommand(ctx, step.Name, step.Input)
		return fixtureResult(result), err
	case "tool":
		def, ok := reg.tools[step.Name]
		if !ok {
			return nil, fmt.Errorf("unknown plugin tool %q", step.Name)
		}
		args := step.Args
		if args == nil {
			args = json.RawMessage(`{}`)
		}
		tc := plugin.ToolContext{Context: ctx, SessionID: "fixture", CWD: "/fixture", ToolCallID: "fixture-call"}
		tc.CallTool = func(ctx context.Context, name string, args json.RawMessage) (plugin.ToolResult, error) {
			raw, _ := jsonv2.Marshal(struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}{name, args})
			result, err := host.Call(ctx, plugin.Invocation{PluginID: r.pkg.Manifest.ID, Name: step.Name, Kind: "tool", Uses: append([]string{"tools"}, r.pkg.Manifest.HostTools...)}, "tools.call", raw)
			if err != nil {
				return plugin.ToolResult{}, err
			}
			return decodeScriptResult(result)
		}
		result, err := def.Executor(ctx, tc, args)
		return fixtureResult(result), err
	case "hook":
		var req plugin.HookRequest
		if err := jsonv2.Unmarshal(step.Request, &req, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		if !r.HasHook(req.Phase, req.Agent != nil) {
			return nil, fmt.Errorf("no registered hook for %q", req.Phase)
		}
		if req.Workflow == nil {
			req.Workflow = host.workflowSnapshot(r.HookWorkflowKeys(req.Phase))
		}
		return r.RunHook(ctx, req)
	case "event":
		if step.Event == nil {
			return nil, errors.New("event step requires an event")
		}
		event := *step.Event
		if event.Version == 0 {
			event.Version = plugin.ProtocolVersion
		}
		if event.Payload.Type == "" {
			event.Payload.Type = event.Type
		}
		raw, err := jsonv2.Marshal(event)
		if err != nil {
			return nil, err
		}
		count := 0
		for i, typ := range reg.events {
			if typ != event.Type {
				continue
			}
			count++
			fn := r.subscriptions[i].handler
			if r.pkg.Manifest.APIVersion == 2 {
				_, err = r.invokeAsync(ctx, string(event.Type), "observer", r.observerUses(), 5*time.Second, fn, raw, nil)
			} else {
				err = r.submit(ctx, 100*time.Millisecond, func() error {
					value, e := r.decode(raw)
					if e != nil {
						return e
					}
					result, e := fn(goja.Undefined(), value)
					if e != nil {
						return e
					}
					return r.requireSync(result)
				})
			}
			if err != nil {
				return nil, err
			}
		}
		if count == 0 {
			return nil, fmt.Errorf("no observer registered for %q", event.Type)
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unknown fixture step %q", step.Kind)
}

func fixtureResult(result plugin.ToolResult) any {
	return struct {
		Content []protocol.ContentBlock `json:"content"`
		IsError bool                    `json:"isError,omitzero"`
		Details any                     `json:"details,omitempty"`
	}{result.Content, result.IsError, result.Details}
}
func fixtureJSONEqual(a, b []byte) bool {
	var av, bv any
	return jsonv2.Unmarshal(a, &av) == nil && jsonv2.Unmarshal(b, &bv) == nil && reflect.DeepEqual(av, bv)
}

type fixtureRegistrar struct {
	tools  map[string]plugin.ToolDefinition
	events []plugin.EventType
}

func (r *fixtureRegistrar) RegisterTool(def plugin.ToolDefinition) error {
	if err := plugin.ValidateIdentifier("tool", def.Name); err != nil {
		return err
	}
	if _, ok := r.tools[def.Name]; ok {
		return errors.New("duplicate tool")
	}
	r.tools[def.Name] = def
	return nil
}
func (r *fixtureRegistrar) Subscribe(typ plugin.EventType, _ plugin.EventHandler) func() {
	r.events = append(r.events, typ)
	return func() {}
}

// fixtureSelect implements JSON Pointer for stable assertions on metadata or a
// result field without coupling fixtures to environment-dependent paths.
func fixtureSelect(value any, pointer string) (any, error) {
	if !strings.HasPrefix(pointer, "/") {
		return nil, errors.New("fixture select must be a JSON Pointer starting with /")
	}
	raw, err := jsonv2.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := jsonv2.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	value = decoded
	for token := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch current := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = current[token]
			if !ok {
				return nil, fmt.Errorf("fixture select %q: missing key %q", pointer, token)
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(current) {
				return nil, fmt.Errorf("fixture select %q: invalid index %q", pointer, token)
			}
			value = current[index]
		default:
			return nil, fmt.Errorf("fixture select %q: cannot descend into scalar", pointer)
		}
	}
	return value, nil
}
