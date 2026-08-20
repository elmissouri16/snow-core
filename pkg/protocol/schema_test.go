package protocol

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func loadRPCSchemas(t *testing.T) map[string]*jsonschema.Schema {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve schema test path")
	}
	dir := filepath.Join(filepath.Dir(source), "schema", "rpc", "v1")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]*jsonschema.Schema, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var schema jsonschema.Schema
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		if schema.ID == "" {
			t.Fatalf("%s has no $id", entry.Name())
		}
		out[schema.ID] = &schema
	}
	return out
}

func resolveRPCSchema(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	schemas := loadRPCSchemas(t)
	id := "https://snow-core.dev/schemas/rpc/v1/" + name
	root := schemas[id]
	if root == nil {
		t.Fatalf("schema %s not found", id)
	}
	resolved, err := root.Resolve(&jsonschema.ResolveOptions{Loader: func(uri *url.URL) (*jsonschema.Schema, error) {
		if schema := schemas[uri.String()]; schema != nil {
			return schema, nil
		}
		return nil, fmt.Errorf("unexpected non-local schema reference %s", uri)
	}})
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	return resolved
}

func jsonValue(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestRPCSchemasResolveWithoutNetwork(t *testing.T) {
	for _, name := range []string{
		"agent-event.schema.json",
		"handshake.schema.json",
		"mcp-server.schema.json",
		"message.schema.json",
		"model.schema.json",
		"output.schema.json",
		"prompt-completed.schema.json",
		"request.schema.json",
		"response.schema.json",
		"session-branch.schema.json",
		"skills.schema.json",
		"session-info.schema.json",
	} {
		resolveRPCSchema(t, name)
	}
}

func TestRepresentativeRPCValuesConformToSchemas(t *testing.T) {
	output := resolveRPCSchema(t, "output.schema.json")
	enabled := false
	budget := int64(100)
	values := []any{
		NewRPCReady("test"),
		RPCResponse{ID: "p1", Type: "response", Command: "prompt", Success: true},
		RPCPromptCompleted{Type: RPCTypePromptCompleted, RequestID: "p1", Status: RPCPromptCompletedStatus},
		RPCPromptCompleted{Type: RPCTypePromptCompleted, Status: RPCPromptCompletedStatus},
		AgentEvent{Type: EvModeChanged, Mode: &CollaborationModeState{Mode: ModeDefault, ReasoningEffort: ThinkingOff}},
		AgentEvent{Type: EvPermissionRequest, Permission: &Permission{Request: PermissionRequest{ID: "perm-1", Tool: "bash", Args: json.RawMessage(`{"command":"echo ok"}`), Risk: "exec"}}},
		AgentEvent{Type: EvUserInputRequest, UserInput: &UserInputRequest{ID: "ask", Questions: []UserInputQuestion{{ID: "q", Header: "Q", Question: "Choose", Options: []UserInputOption{{Label: "A"}}}}}},
		AgentEvent{Type: EvThreadGoalUpdated, ThreadGoal: &ThreadGoalUpdate{Goal: &ThreadGoal{SessionID: "s", BranchID: "b", GoalID: "g", Objective: "ship", Status: GoalActive, TokenBudget: &budget, TokensUsed: 1, SecondsUsed: 2, CreatedAt: 3, UpdatedAt: 4}}},
		AgentEvent{Type: EvSubagentStatus, Subagent: &SubagentState{Agent: AgentRef{ThreadID: "child", ParentThreadID: "root", Path: "/root/child", ParentPath: "/root", Depth: 1}, Status: AgentRunning, CreatedAt: 1}},
		RPCResponse{ID: "m1", Type: "response", Command: "models_list", Success: true, Data: RPCModelList{Provider: "fake", Current: "fake-1", Models: []Model{{Provider: "fake", ID: "fake-1", SupportsTools: true}}}},
		RPCResponse{ID: "sm1", Type: "response", Command: "subagent_models", Success: true, Data: RPCModelList{Enabled: &enabled, Models: []Model{}}},
		RPCResponse{ID: "history1", Type: "response", Command: "messages_list", Success: true, Data: RPCMessagesList{Messages: []Message{{ID: "tool-result", Role: RoleTool, Content: []ContentBlock{{Type: BlockText, Text: "done"}}, Timestamp: 1, ToolCallID: "call-1", ToolName: "read", ToolDisplay: &ToolDisplay{Started: true, StartMessage: "file.go", Progress: []string{"reading"}, Output: "done", DurationMS: 2}}}}},
		RPCResponse{ID: "i1", Type: "response", Command: "session_info", Success: true, Data: RPCSessionInfo{SessionID: "s", Name: "", Path: "", CWD: "/tmp", Provider: "fake", Model: "fake-1", Thinking: ThinkingOff, ThinkingLevels: []ThinkingLevel{ThinkingOff}, ReasoningSummary: ReasoningSummaryAuto, TextVerbosity: TextVerbosityLow, CollaborationMode: ModeDefault, Subagents: RPCSubagentLimits{}, PendingInputs: RPCPendingInputCounts{}}},
	}
	for _, value := range values {
		if err := output.Validate(jsonValue(t, value)); err != nil {
			t.Fatalf("%T does not conform: %v\n%#v", value, err, value)
		}
	}
}

func TestRPCSessionInfoControlFieldsRemainAdditive(t *testing.T) {
	schema := resolveRPCSchema(t, "session-info.schema.json")
	legacy := map[string]any{
		"session_id": "s", "name": "", "path": "", "cwd": "/tmp",
		"provider": "fake", "model": "fake-1", "thinking": "off",
		"thinking_levels": []any{"off"}, "collaboration_mode": "default",
		"subagents": map[string]any{
			"enabled": false, "max_concurrent_agents": 0, "max_concurrent_threads": 0,
			"max_agents_per_session": 0, "max_depth": 0, "durable": false,
			"allow_mutation": false,
		},
		"pending_inputs": map[string]any{"steering": 0, "follow_up": 0, "total": 0},
	}
	if err := schema.Validate(legacy); err != nil {
		t.Fatalf("v1 session_info without additive control fields does not conform: %v", err)
	}
}

func TestRPCPromptRequestWithContentConformsToRequestSchema(t *testing.T) {
	request := resolveRPCSchema(t, "request.schema.json")
	values := []any{
		RPCRequest{ID: "mm1", Type: "prompt", Message: "look", Mode: "plan", Content: []ContentBlock{{Type: BlockText, Text: "look"}, {Type: BlockImage, MIMEType: "image/png", Data: []byte{1, 2, 3}}}},
		RPCRequest{ID: "img1", Type: "prompt", Content: []ContentBlock{{Type: BlockImage, MIMEType: "image/jpeg", Data: []byte{9}}}},
		RPCRequest{ID: "t1", Type: "prompt", Message: "plain"},
	}
	for _, value := range values {
		if err := request.Validate(jsonValue(t, value)); err != nil {
			t.Fatalf("%T does not conform to request schema: %v\n%#v", value, err, value)
		}
	}
	invalid := []any{
		RPCRequest{ID: "x1", Type: "prompt", Content: []ContentBlock{{Type: BlockThinking, Text: "nope"}}},
		RPCRequest{ID: "x2", Type: "prompt", Content: []ContentBlock{{Type: BlockProviderData, Text: "opaque"}}},
	}
	for _, value := range invalid {
		if err := request.Validate(jsonValue(t, value)); err == nil {
			t.Fatalf("invalid prompt request conformed: %#v", value)
		}
	}
}

func TestAgentEventSchemaCoversKnownTypes(t *testing.T) {
	schemas := loadRPCSchemas(t)
	root := schemas["https://snow-core.dev/schemas/rpc/v1/agent-event.schema.json"]
	if root == nil || root.Properties["type"] == nil {
		t.Fatal("agent event type schema is missing")
	}
	got := make([]string, 0, len(root.Properties["type"].Enum))
	for _, value := range root.Properties["type"].Enum {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("non-string event enum: %#v", value)
		}
		got = append(got, name)
	}
	wantTypes := KnownAgentEventTypes()
	want := make([]string, len(wantTypes))
	for i, eventType := range wantTypes {
		want[i] = string(eventType)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event schema types = %v, want %v", got, want)
	}
}

func collectSchemaTypeValues(schema *jsonschema.Schema, values map[string]bool) {
	if schema == nil {
		return
	}
	if property := schema.Properties["type"]; property != nil {
		if property.Const != nil {
			if value, ok := (*property.Const).(string); ok {
				values[value] = true
			}
		}
		for _, raw := range property.Enum {
			if value, ok := raw.(string); ok {
				values[value] = true
			}
		}
	}
	for _, child := range schema.Defs {
		collectSchemaTypeValues(child, values)
	}
	for _, child := range schema.Properties {
		collectSchemaTypeValues(child, values)
	}
	for _, group := range [][]*jsonschema.Schema{schema.OneOf, schema.AnyOf, schema.AllOf} {
		for _, child := range group {
			collectSchemaTypeValues(child, values)
		}
	}
}

func TestRPCRequestSchemaCoversKnownCommands(t *testing.T) {
	schemas := loadRPCSchemas(t)
	typeValues := make(map[string]bool)
	collectSchemaTypeValues(schemas["https://snow-core.dev/schemas/rpc/v1/request.schema.json"], typeValues)
	// contentBlock defs discriminate text and image payloads, not commands.
	nonCommands := map[string]bool{"text": true, "image": true}
	gotCommands := make([]string, 0, len(typeValues))
	for command := range typeValues {
		if !nonCommands[command] {
			gotCommands = append(gotCommands, command)
		}
	}
	wantCommands := KnownRPCCommands()
	sort.Strings(gotCommands)
	sort.Strings(wantCommands)
	if !reflect.DeepEqual(gotCommands, wantCommands) {
		t.Fatalf("request schema commands = %v, want %v", gotCommands, wantCommands)
	}

	request := resolveRPCSchema(t, "request.schema.json")
	for _, command := range KnownRPCCommands() {
		value := RPCRequest{ID: "test", Type: command}
		switch command {
		case "prompt":
			value.Message = "hello"
		case "steer", "follow_up":
			value.Message = "next"
		case "set_model":
			value.Model = "fake-1"
		case "set_thinking":
			value.Thinking = "off"
		case "set_mode":
			value.Mode = "default"
		case "goal_create", "goal_set":
			value.Params = json.RawMessage(`{"objective":"ship"}`)
		case "goal_edit":
			value.Params = json.RawMessage(`{"objective":"ship safely"}`)
		case "session_rename":
			value.Params = json.RawMessage(`{"name":"renamed"}`)
		case "branch_select", "branch_delete":
			value.Params = json.RawMessage(`{"branch_id":"branch-1"}`)
		case "branch_rename":
			value.Params = json.RawMessage(`{"branch_id":"branch-1","name":"renamed"}`)
		case "set_reasoning_summary":
			value.ReasoningSummary = "auto"
		case "set_text_verbosity":
			value.TextVerbosity = "medium"
		case "subagent_followup", "subagent_send_message":
			value.Params = json.RawMessage(`{"target":"/root/child","message":"continue"}`)
		case "subagent_get", "subagent_interrupt":
			value.Params = json.RawMessage(`{"target":"/root/child"}`)
		case "subagent_spawn":
			value.Params = json.RawMessage(`{"name":"child","task":"inspect"}`)
		case "subagent_wait":
			value.Params = json.RawMessage(`{}`)
		case "user_input_reject":
			value.Params = json.RawMessage(`{"request_id":"ask-1"}`)
		case "user_input_reply":
			value.Params = json.RawMessage(`{"request_id":"ask-1","answers":[]}`)
		case "permission_reject":
			value.Params = json.RawMessage(`{"request_id":"perm-1"}`)
		case "permission_reply":
			value.Params = json.RawMessage(`{"request_id":"perm-1","decision":"allow"}`)
		}
		if err := request.Validate(jsonValue(t, value)); err != nil {
			t.Errorf("command %s is not covered: %v", command, err)
		}
	}
	invalidName := RPCRequest{ID: "bad", Type: "subagent_spawn", Params: json.RawMessage(`{"name":"has-hyphen","task":"inspect"}`)}
	if err := request.Validate(jsonValue(t, invalidName)); err == nil {
		t.Fatal("subagent name with hyphen unexpectedly conforms to request schema")
	}
}
