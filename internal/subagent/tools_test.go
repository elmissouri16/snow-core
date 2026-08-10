package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
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
		DefaultRole:          "default",
		Roles:                map[string]Role{"default": {Name: "default"}},
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

	spawn := run("spawn_agent", map[string]any{"task_name": "inspect", "message": "inspect", "agent_type": "general", "fork_turns": "none"})
	if len(spawn.Content) != 1 || spawn.Content[0].Text == "" {
		t.Fatalf("spawn result=%+v", spawn)
	}
	aliased, err := m.Get(context.Background(), "inspect")
	if err != nil || aliased.Agent.Role != "default" {
		t.Fatalf("general alias state=%+v err=%v, want canonical default role", aliased, err)
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

	// A second task exercises interrupt_agent while its initial turn is active.
	run("spawn_agent", map[string]any{"task_name": "stop", "message": "long task", "fork_turns": "none"})
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
	raw := json.RawMessage(`{"_raw":"{\"task_name\":\"demo-index\",\"message\":\"inspect\",\"fork_turns\":\"none\"}"}`)
	if err := decodeStrict(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.TaskName != "demo-index" || got.Message != "inspect" || got.ForkTurns != "none" {
		t.Fatalf("decoded request=%+v", got)
	}
}

func TestDecodeStrictRejectsInvalidRawCompatibilityEnvelope(t *testing.T) {
	tests := map[string]string{
		"malformed inner":    `{"_raw":"{"}`,
		"mixed fields":       `{"_raw":"{}","message":"mixed"}`,
		"unknown inner":      `{"_raw":"{\"task_name\":\"inspect\",\"message\":\"inspect\",\"extra\":true}"}`,
		"array inner":        `{"_raw":"[]"}`,
		"null inner":         `{"_raw":"null"}`,
		"non-string inner":   `{"_raw":{}}`,
		"trailing inner":     `{"_raw":"{\"task_name\":\"inspect\",\"message\":\"inspect\"} {}"}`,
		"trailing top-level": `{"task_name":"inspect","message":"inspect"} {}`,
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

func TestSpawnAgentToolCanonicalizesHyphenatedRawTaskName(t *testing.T) {
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
		DefaultRole:          "default",
		Roles:                map[string]Role{"default": {Name: "default"}},
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
	inner := `{"task_name":"demo-index","message":"inspect","fork_turns":"none"}`
	raw, err := json.Marshal(map[string]string{"_raw": inner})
	if err != nil {
		t.Fatal(err)
	}
	result, err := spawn.Run(context.Background(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, `"task_name":"/root/demo_index"`) {
		t.Fatalf("spawn result=%+v", result)
	}
	if state, err := m.Get(context.Background(), "demo_index"); err != nil || state.Agent.Path != "/root/demo_index" {
		t.Fatalf("canonical state=%+v err=%v", state, err)
	}

	collisionRaw := json.RawMessage(`{"task_name":"demo_index","message":"duplicate","fork_turns":"none"}`)
	collision, err := spawn.Run(context.Background(), collisionRaw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !collision.IsError {
		t.Fatalf("canonical collision unexpectedly succeeded: %+v", collision)
	}
	if _, err := m.Spawn(context.Background(), m.RootCaller(), protocol.SpawnSubagentRequest{TaskName: "sdk-hyphen", Message: "strict", ForkTurns: "none"}); err == nil || !strings.Contains(err.Error(), "invalid agent path segment") {
		t.Fatalf("direct manager hyphen error=%v", err)
	}
}

func TestSpawnAgentSchemaExplainsBuiltInRoles(t *testing.T) {
	schema := toolSchemas["spawn_agent"]
	for _, want := range []string{"canonical /root/", "hyphens normalize to underscores", "default role", "general is an accepted alias", "explorer", "worker", "permission-gated bash", "write/edit"} {
		if !strings.Contains(schema.Description, want) {
			t.Fatalf("spawn_agent description missing %q: %q", want, schema.Description)
		}
	}
	parameters := string(schema.Parameters)
	for _, want := range []string{`"maxLength":64`, `"pattern":"^[a-z][a-z0-9_-]{0,63}$"`, `"pattern":"^(none|all|[1-9][0-9]*)$"`, "positive integer string", `"description":"Optional role:`, "general is an accepted alias", "Omit to use the configured default role"} {
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
