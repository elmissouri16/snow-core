package javascript

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

// fixtureHost never delegates: even in-memory operations must be in the ledger.
type fixtureHost struct {
	mu          sync.Mutex
	pkg         *Package
	calls       []FixtureCall
	cursor      int
	failure     error
	storage     map[string]map[string]json.RawMessage
	workflow    map[string]json.RawMessage
	restriction json.RawMessage
	revision    int
}

func newFixtureHost(p *Package, test FixtureCase) *fixtureHost {
	// Copy fixtures so cases and repeated suite executions cannot share mutations.
	raw, _ := jsonv2.Marshal(test)
	var copy FixtureCase
	_ = jsonv2.Unmarshal(raw, &copy)
	if copy.Storage == nil {
		copy.Storage = map[string]map[string]json.RawMessage{}
	}
	if copy.Workflow == nil {
		copy.Workflow = map[string]json.RawMessage{}
	}
	return &fixtureHost{pkg: p, storage: copy.Storage, workflow: copy.Workflow}
}
func (h *fixtureHost) Environment() plugin.Environment {
	return plugin.Environment{SessionID: "fixture", CWD: "/fixture", Kind: "root", UI: true, Generation: 1}
}
func (h *fixtureHost) begin(calls []FixtureCall) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = calls
	h.cursor = 0
	h.failure = nil
}
func (h *fixtureHost) finish() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failure != nil {
		return h.failure
	}
	if h.cursor != len(h.calls) {
		return fmt.Errorf("mock host ledger: consumed %d of %d calls", h.cursor, len(h.calls))
	}
	return nil
}
func (h *fixtureHost) Call(ctx context.Context, inv plugin.Invocation, op string, args json.RawMessage) (result json.RawMessage, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	defer func() {
		if err != nil && result == nil {
			h.failure = errors.Join(h.failure, err)
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	capability := plugin.CapabilityForOperation(op)
	if inv.PluginID != h.pkg.Manifest.ID || inv.Kind == "hook" || inv.Kind == "renderer" || !plugin.AllowsOperation(inv.Uses, op) {
		return nil, fmt.Errorf("mock host rejected unauthorized invocation of %s", op)
	}
	if capability != "read" && !slices.Contains(h.pkg.Manifest.Capabilities, capability) && !(op == "tools.call" && h.pkg.Manifest.APIVersion == 1) {
		return nil, fmt.Errorf("manifest does not declare %s", capability)
	}

	writesWorkflow := op == "workflow.set" || op == "workflow.delete" || op == "workflow.update" || op == "tools.restrict" || op == "tools.clearRestriction"
	if writesWorkflow && inv.Kind != "command" {
		return nil, errors.New("mock workflow mutations require an explicit root command")
	}
	if op == "workflow.update" {
		var update map[string]json.RawMessage
		if err := jsonv2.Unmarshal(args, &update); err != nil {
			return nil, err
		}
		if _, present := update["toolRestriction"]; present && (!slices.Contains(h.pkg.Manifest.Capabilities, "tool_policy") || !slices.Contains(inv.Uses, "tool_policy")) {
			return nil, errors.New("combined workflow update requires declared tool_policy capability")
		}
	}
	if op == "tools.call" {
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := jsonv2.Unmarshal(args, &call); err != nil {
			return nil, err
		}
		if !slices.Contains(h.pkg.Manifest.HostTools, call.Name) || !slices.Contains(inv.Uses, call.Name) {
			return nil, fmt.Errorf("undeclared mock host tool %q", call.Name)
		}
	}
	if h.cursor >= len(h.calls) {
		return nil, fmt.Errorf("unexpected host call %s", op)
	}
	expected := h.calls[h.cursor]
	h.cursor++
	want := expected.Args
	if want == nil {
		want = []byte(`{}`)
	}
	if expected.Operation != op || !fixtureJSONEqual(want, args) {
		return nil, fmt.Errorf("host call %d mismatch: want %s %s; got %s %s", h.cursor, expected.Operation, boundText(string(want), 1024), op, boundText(string(args), 1024))
	}
	if expected.Error != "" {
		// A declared mock error is not a harness failure, even if JS catches it.
		return json.RawMessage(`null`), errors.New(expected.Error)
	}
	if expected.Memory {
		return h.memory(op, args)
	}
	if expected.Result == nil {
		return json.RawMessage(`null`), nil
	}
	return bytes.Clone(expected.Result), nil
}
func (h *fixtureHost) memory(op string, args json.RawMessage) (json.RawMessage, error) {
	switch {
	case strings.HasPrefix(op, "storage."):
		var a struct {
			Scope string          `json:"scope"`
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err := jsonv2.Unmarshal(args, &a, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		if a.Scope == "" {
			a.Scope = "project"
		}
		if !slices.Contains([]string{"global", "project", "session"}, a.Scope) {
			return nil, errors.New("unknown mock storage scope")
		}
		if h.storage[a.Scope] == nil {
			h.storage[a.Scope] = map[string]json.RawMessage{}
		}
		return fixtureMemoryValue(h.storage[a.Scope], strings.TrimPrefix(op, "storage."), a.Key, a.Value)
	case op == "workflow.get" || op == "workflow.set" || op == "workflow.delete":
		var a struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err := jsonv2.Unmarshal(args, &a, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		result, err := fixtureMemoryValue(h.workflow, strings.TrimPrefix(op, "workflow."), a.Key, a.Value)
		if err != nil || op == "workflow.get" {
			return result, err
		}
		return h.receipt()
	case op == "workflow.update":
		var a struct {
			Set             map[string]json.RawMessage `json:"set"`
			Delete          []string                   `json:"delete"`
			ToolRestriction json.RawMessage            `json:"toolRestriction"`
		}
		if err := jsonv2.Unmarshal(args, &a, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		for key, value := range a.Set {
			h.workflow[key] = bytes.Clone(value)
		}
		for _, key := range a.Delete {
			delete(h.workflow, key)
		}
		if a.ToolRestriction != nil {
			h.restriction = bytes.Clone(a.ToolRestriction)
		}
		return h.receipt()
	case op == "tools.restrict":
		h.restriction = bytes.Clone(args)
		return h.receipt()
	case op == "tools.clearRestriction":
		h.restriction = nil
		return h.receipt()
	}
	return nil, fmt.Errorf("memory mock does not implement %s; supply an explicit result", op)
}
func fixtureMemoryValue(values map[string]json.RawMessage, op, key string, value json.RawMessage) (json.RawMessage, error) {
	switch op {
	case "get":
		if found := values[key]; found != nil {
			return bytes.Clone(found), nil
		}
	case "set":
		if value == nil {
			return nil, errors.New("mock set requires value")
		}
		values[key] = bytes.Clone(value)
	case "delete":
		delete(values, key)
	default:
		return nil, fmt.Errorf("unknown mock memory operation %s", op)
	}
	return json.RawMessage(`null`), nil
}

func (h *fixtureHost) workflowSnapshot(keys []string) map[string]json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(keys) == 0 {
		return nil
	}
	values := make(map[string]json.RawMessage, len(keys))
	for _, key := range keys {
		value := h.workflow[key]
		if value == nil {
			value = json.RawMessage(`null`)
		}
		values[key] = bytes.Clone(value)
	}
	return values
}

// Synthetic receipts model API shape only; no branch ancestry is simulated.
func (h *fixtureHost) receipt() (json.RawMessage, error) {
	h.revision++
	return jsonv2.Marshal(map[string]string{"branchId": "fixture", "tipId": fmt.Sprintf("fixture-%d", h.revision)})
}
