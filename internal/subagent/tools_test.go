package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestManagerToolsDriveLifecycle(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	root := rootAgent(t, st)
	defer root.Close()
	m := New(context.Background(), Limits{
		MaxConcurrentThreads: 2,
		MaxAgentsPerSession:  8,
		MaxDepth:             1,
		TaskTimeout:          time.Second,
		MinWait:              time.Millisecond,
		DefaultWait:          20 * time.Millisecond,
		MaxWait:              200 * time.Millisecond,
		DefaultRole:          "general",
		Roles:                map[string]Role{"general": {Name: "general"}},
	})
	var active, maxActive atomic.Int32
	factory := ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) {
		return &mockChild{delay: 10 * time.Millisecond, active: &active, max: &maxActive}, nil
	})
	if err := m.Bind(root, factory, root.Publish, st); err != nil {
		t.Fatal(err)
	}
	if err := m.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	caller := m.RootCaller()
	tool := func(name string) tools.Tool {
		for _, candidate := range Tools(m, caller) {
			if candidate.Schema().Name == name {
				return candidate
			}
		}
		t.Fatalf("missing tool %s", name)
		return nil
	}
	run := func(name string, args any) tools.ToolResult {
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		result, err := tool(name).Run(context.Background(), raw, nil)
		if err != nil {
			t.Fatalf("%s returned error: %v", name, err)
		}
		if result.IsError {
			t.Fatalf("%s returned tool error: %+v", name, result)
		}
		return result
	}

	spawn := run("spawn_agent", map[string]any{"name": "inspect", "task": "inspect", "role": "general", "fork_turns": "none"})
	if len(spawn.Content) != 1 || spawn.Content[0].Text == "" {
		t.Fatalf("spawn result=%+v", spawn)
	}
	spawned, err := m.Get(context.Background(), "inspect")
	if err != nil || spawned.Agent.Role != "general" {
		t.Fatalf("spawned state=%+v err=%v, want general role", spawned, err)
	}
	run("list_agents", map[string]any{})
	run("send_message", map[string]any{"target": "inspect", "message": "also check tests"})
	waitAll := run("wait_agent", map[string]any{"timeout_ms": 200, "until": "all"})
	var waited protocol.WaitSubagentsResult
	if len(waitAll.Content) != 1 || json.Unmarshal([]byte(waitAll.Content[0].Text), &waited) != nil || !waited.AllTerminal || waited.Running != 0 || waited.Queued != 0 || waited.Terminal != 1 {
		t.Fatalf("wait until all result=%+v decoded=%+v", waitAll, waited)
	}
	awaitToolState(t, m, "inspect", protocol.AgentCompleted)

	run("followup_task", map[string]any{"target": "inspect", "message": "follow up"})
	run("wait_agent", map[string]any{"timeout_ms": 200, "until": "all"})
	awaitToolState(t, m, "inspect", protocol.AgentCompleted)

	run("close_agent", map[string]any{"target": "inspect"})
	awaitToolState(t, m, "inspect", protocol.AgentClosed)
	run("resume_agent", map[string]any{"target": "inspect"})
	awaitToolState(t, m, "inspect", protocol.AgentNotLoaded)

	// A second task exercises interrupt_agent while its initial turn is active.
	run("spawn_agent", map[string]any{"name": "stop", "task": "long task", "fork_turns": "none"})
	awaitToolState(t, m, "stop", protocol.AgentRunning)
	previous := run("interrupt_agent", map[string]any{"target": "stop"})
	if len(previous.Content) != 1 || previous.Content[0].Text == "" {
		t.Fatalf("interrupt result=%+v", previous)
	}
	awaitToolState(t, m, "stop", protocol.AgentInterrupted)
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeStrictAcceptsRawCompatibilityEnvelope(t *testing.T) {
	var got protocol.SpawnSubagentRequest
	raw := json.RawMessage(`{"_raw":"{\"name\":\"demo-index\",\"task\":\"inspect\",\"fork_turns\":\"none\"}"}`)
	if err := decodeStrict(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo-index" || got.Task != "inspect" || got.ForkTurns != "none" {
		t.Fatalf("decoded request=%+v", got)
	}
}

func TestDecodeStrictRejectsInvalidRawCompatibilityEnvelope(t *testing.T) {
	tests := map[string]string{
		"malformed inner":    `{"_raw":"{"}`,
		"mixed fields":       `{"_raw":"{}","task":"mixed"}`,
		"unknown inner":      `{"_raw":"{\"name\":\"inspect\",\"task\":\"inspect\",\"extra\":true}"}`,
		"array inner":        `{"_raw":"[]"}`,
		"null inner":         `{"_raw":"null"}`,
		"non-string inner":   `{"_raw":{}}`,
		"trailing inner":     `{"_raw":"{\"name\":\"inspect\",\"task\":\"inspect\"} {}"}`,
		"trailing top-level": `{"name":"inspect","task":"inspect"} {}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			var got protocol.SpawnSubagentRequest
			if err := decodeStrict(json.RawMessage(raw), &got); err == nil {
				t.Fatalf("decodeStrict(%s) unexpectedly succeeded: %+v", raw, got)
			}
		})
	}
}

func TestSpawnAgentToolCanonicalizesHyphenatedRawName(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	root := rootAgent(t, st)
	defer root.Close()
	m := New(context.Background(), Limits{
		MaxConcurrentThreads: 1,
		MaxAgentsPerSession:  4,
		MaxDepth:             1,
		TaskTimeout:          time.Second,
		MinWait:              time.Millisecond,
		DefaultWait:          20 * time.Millisecond,
		MaxWait:              200 * time.Millisecond,
		DefaultRole:          "general",
		Roles:                map[string]Role{"general": {Name: "general"}},
	})
	var active, maxActive atomic.Int32
	factory := ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) {
		return &mockChild{delay: 10 * time.Millisecond, active: &active, max: &maxActive}, nil
	})
	if err := m.Bind(root, factory, root.Publish, st); err != nil {
		t.Fatal(err)
	}
	if err := m.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := m.Close(context.Background()); err != nil {
			t.Errorf("close manager: %v", err)
		}
	}()

	var spawn tools.Tool
	for _, candidate := range Tools(m, m.RootCaller()) {
		if candidate.Schema().Name == "spawn_agent" {
			spawn = candidate
			break
		}
	}
	if spawn == nil {
		t.Fatal("missing spawn_agent tool")
	}
	inner := `{"name":"demo-index","task":"inspect","fork_turns":"none"}`
	raw, err := json.Marshal(map[string]string{"_raw": inner})
	if err != nil {
		t.Fatal(err)
	}
	result, err := spawn.Run(context.Background(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, `"name":"/root/demo_index"`) {
		t.Fatalf("spawn result=%+v", result)
	}
	if state, err := m.Get(context.Background(), "demo_index"); err != nil || state.Agent.Path != "/root/demo_index" {
		t.Fatalf("canonical state=%+v err=%v", state, err)
	}

	collisionRaw := json.RawMessage(`{"name":"demo_index","task":"duplicate","fork_turns":"none"}`)
	collision, err := spawn.Run(context.Background(), collisionRaw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !collision.IsError {
		t.Fatalf("canonical collision unexpectedly succeeded: %+v", collision)
	}
	if _, err := m.Spawn(context.Background(), m.RootCaller(), protocol.SpawnSubagentRequest{Name: "sdk-hyphen", Task: "strict", ForkTurns: "none"}); err == nil || !strings.Contains(err.Error(), "invalid agent path segment") {
		t.Fatalf("direct manager hyphen error=%v", err)
	}
}

func TestListSubagentModelsReturnsExactPairs(t *testing.T) {
	m := New(context.Background(), Limits{})
	m.SetModelCatalog(func() []protocol.Model {
		return []protocol.Model{{Provider: "chatgpt", ID: "gpt-x", DisplayName: "GPT X", SupportsTools: true}, {Provider: "opencode-go", ID: "deepseek-v3"}}
	})
	var catalog tools.Tool
	for _, candidate := range Tools(m, Caller{Path: protocol.RootAgentPath}) {
		if candidate.Schema().Name == "list_subagent_models" {
			catalog = candidate
			break
		}
	}
	if catalog == nil {
		t.Fatal("missing list_subagent_models tool")
	}
	result, err := catalog.Run(context.Background(), json.RawMessage(`{"provider":"opencode-go"}`), nil)
	if err != nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("catalog result=%+v err=%v", result, err)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, `"provider":"opencode-go"`) || !strings.Contains(text, `"model":"deepseek-v3"`) || !strings.Contains(text, `"thinking_levels":["off"]`) || strings.Contains(text, "gpt-x") {
		t.Fatalf("filtered catalog = %s", text)
	}
	missing, err := catalog.Run(context.Background(), json.RawMessage(`{"provider":"opencode"}`), nil)
	if err != nil || missing.IsError || len(missing.Content) != 1 || !strings.Contains(missing.Content[0].Text, `"available_providers"`) || !strings.Contains(missing.Content[0].Text, `no models found for exact provider`) {
		t.Fatalf("missing provider diagnostic=%+v err=%v", missing, err)
	}
}

func TestSpawnAgentSchemaExplainsBuiltInRoles(t *testing.T) {
	schema := toolSchemas["spawn_agent"]
	for _, want := range []string{"canonical /root/", "hyphens normalize to underscores", "list_subagent_models", "general role", "explorer", "implementer", "permission-gated bash", "write/edit"} {
		if !strings.Contains(schema.Description, want) {
			t.Fatalf("spawn_agent description missing %q: %q", want, schema.Description)
		}
	}
	parameters := string(schema.Parameters)
	for _, want := range []string{`"maxLength":64`, `"pattern":"^[a-z][a-z0-9_-]{0,63}$"`, `"pattern":"^(none|all|[1-9][0-9]*)$"`, "positive integer string", `"description":"Optional role:`, `"provider"`, `"required":["name","task"]`, "configured subagent default model", "Omit to use the configured default role"} {
		if !strings.Contains(parameters, want) {
			t.Fatalf("spawn_agent schema missing %q: %s", want, schema.Parameters)
		}
	}
	waitSchema := toolSchemas["wait_agent"]
	if !strings.Contains(waitSchema.Description, "until=all") || !strings.Contains(string(waitSchema.Parameters), `"enum":["activity","all"]`) {
		t.Fatalf("wait_agent schema lacks explicit all mode: %+v", waitSchema)
	}
}

func awaitToolState(t *testing.T, m *Manager, target string, want protocol.AgentStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, err := m.Get(context.Background(), target)
		if err == nil && state.Status == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	state, _ := m.Get(context.Background(), target)
	t.Fatalf("target %s status=%s want=%s", target, state.Status, want)
}
