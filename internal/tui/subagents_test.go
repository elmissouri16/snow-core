package tui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSubagentEventsStayOutOfRootBuffers(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	ref := &protocol.AgentRef{ThreadID: "c", ParentThreadID: "root", Path: "/root/c", ParentPath: "/root", Role: "explorer", Depth: 1}
	m.assistantBuf.WriteString("root")
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Agent: ref, Text: "child"})
	if m.assistantBuf.String() != "root" {
		t.Fatalf("root buffer=%q", m.assistantBuf.String())
	}
	activity := m.subagentFleetActivity["c"]
	if len(activity) != 1 || !strings.Contains(activity[0], "response  child") {
		t.Fatalf("activity=%q", activity)
	}
	before := len(m.lines)
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvSessionUpdated, Agent: ref})
	if len(m.lines) != before {
		t.Fatal("child session update changed root transcript")
	}
}

func TestPermissionBrokerSerializesAttributedChildren(t *testing.T) {
	a := newTUIAsker(newAgentEventMailbox())
	events := make(chan protocol.AgentEvent, 2)
	a.SetPublisher(func(ev protocol.AgentEvent) { events <- ev })
	decisions := make(chan permission.Decision, 2)
	for _, path := range []protocol.AgentPath{"/root/a", "/root/b"} {
		path := path
		go func() {
			d, _ := a.Ask(context.Background(), permission.Request{Tool: "edit", Risk: permission.RiskWrite, Agent: &protocol.AgentRef{ThreadID: string(path), Path: path, ParentThreadID: "root", ParentPath: "/root", Depth: 1}})
			decisions <- d
		}()
		time.Sleep(time.Millisecond)
	}
	first := <-events
	if first.Agent == nil || first.Agent.Path != "/root/a" {
		t.Fatalf("first=%+v", first.Agent)
	}
	if err := a.Respond(permission.DecisionAllow); err != nil {
		t.Fatal(err)
	}
	if d := <-decisions; d != permission.DecisionAllow {
		t.Fatalf("first decision=%s", d)
	}
	second := <-events
	if second.Agent == nil || second.Agent.Path != "/root/b" {
		t.Fatalf("second=%+v", second.Agent)
	}
	if err := a.Respond(permission.DecisionDeny); err != nil {
		t.Fatal(err)
	}
	if d := <-decisions; d != permission.DecisionDeny {
		t.Fatalf("second decision=%s", d)
	}
}

func TestSubagentToolRenderingIsCompact(t *testing.T) {
	waitJSON, err := json.Marshal(protocol.WaitSubagentsResult{Running: 2, Queued: 1, Terminal: 3, AllTerminal: false})
	if err != nil {
		t.Fatal(err)
	}
	wait, handled := renderSubagentToolSummary("wait_agent", string(waitJSON))
	plainWait := stripANSI(wait)
	if !handled || !strings.Contains(plainWait, "2 running · 1 queued · 3 finished · activity received") || strings.Contains(plainWait, "{") {
		t.Fatalf("wait summary handled=%v output=%q", handled, plainWait)
	}
	list, handled := renderSubagentToolSummary("list_agents", `{"running":0,"queued":0,"terminal":3,"concurrent_limit":4,"agent_limit":32}`)
	plainList := stripANSI(list)
	if !handled || !strings.Contains(plainList, "0 running · 0 queued · 3 finished") || !strings.Contains(plainList, "capacity 0/4 · open 3/32") {
		t.Fatalf("list summary handled=%v output=%q", handled, plainList)
	}
	if output, handled := renderSubagentToolSummary("spawn_agent", `{"status":"queued"}`); !handled || output != "" {
		t.Fatalf("spawn output was not suppressed: handled=%v output=%q", handled, output)
	}
}

func TestSuccessfulSpawnUsesSingleLifecycleRow(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	ref := &protocol.AgentRef{ThreadID: "child", ParentThreadID: "root", Path: "/root/count", ParentPath: "/root", Role: "general", Depth: 1}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "spawn_agent"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvSubagentStarted, Agent: ref})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolName: "spawn_agent", ToolOutput: `{"status":"queued"}`})
	if len(m.lines) != 1 || !strings.Contains(stripANSI(m.lines[0]), "agent /root/count started (general)") {
		t.Fatalf("spawn transcript rows=%d lines=%q", len(m.lines), m.lines)
	}
}

func TestAgentCommandRegistered(t *testing.T) {
	if spec, ok := commandByExact("/agent"); !ok || spec.name != "/agent" || !strings.Contains(spec.argHint, "concurrency") {
		t.Fatal("/agent missing concurrency guidance")
	}
}

func TestAgentDisplayIncludesCapacityStateAndTranscriptMetadata(t *testing.T) {
	now := time.UnixMilli(10_000)
	state := protocol.SubagentState{
		Agent:  protocol.AgentRef{ThreadID: "child-1", ParentThreadID: "root-1", Path: "/root/build", ParentPath: "/root", Role: "implementer", Depth: 1},
		Status: protocol.AgentErrored, Provider: "fake", Model: "m", Thinking: protocol.ThinkingHigh,
		CreatedAt: 1_000, StartedAt: 2_000, FinishedAt: 8_000, Error: "command failed", Generation: 4,
		Usage: &protocol.Usage{Input: 10, Output: 5, Total: 15},
	}
	list := protocol.SubagentList{Agents: []protocol.SubagentState{state}, Running: 3, Queued: 7, Terminal: 2, ConcurrentLimit: 10, AgentLimit: 32}
	title := subagentInfoTitle(list)
	for _, want := range []string{"3 running", "7 queued", "concurrency 3/10", "spawned 12/32"} {
		if !strings.Contains(title, want) {
			t.Fatalf("title missing %q: %q", want, title)
		}
	}
	items, targets := subagentInfoItems(list, false, now)
	if len(items) != 1 || len(targets) != 1 || targets[0] != "/root/build" {
		t.Fatalf("items=%+v targets=%+v", items, targets)
	}
	for _, want := range []string{"fake/m", "thinking high", "6s", "15 tokens", "command failed", "memory-only"} {
		if !strings.Contains(items[0].Detail, want) {
			t.Fatalf("detail missing %q: %q", want, items[0].Detail)
		}
	}
	messages := []protocol.Message{{Role: protocol.RoleAssistant, StopReason: protocol.StopToolUse, Content: []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"sleep 1"}`)}}}, {Role: protocol.RoleTool, ToolName: "bash", IsError: true, Content: []protocol.ContentBlock{protocol.NewTextBlock("exit code 1")}}}
	if got := historicalChildMessageCount([]protocol.Message{{Role: protocol.RoleUser}, {Role: protocol.RoleAgent}, {Role: protocol.RoleAgent}}); got != 2 {
		t.Fatalf("historical child message count = %d", got)
	}
	detail := renderSubagentInspection(state, messages, nil, false, now)
	for _, want := range []string{"thread: child-1", "parent: /root (root-1)", "history: memory-only", "transcript: 2 messages", `call bash {"command":"sleep 1"}`, "tool_result bash [error]: exit code 1"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("inspection missing %q:\n%s", want, detail)
		}
	}
}

func TestAgentConcurrencyCommandPersistsArbitraryLimit(t *testing.T) {
	home := testHome(t)
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	_, _ = m.runCommand("/agent concurrency 10")
	if m.app.Cfg.Subagents.MaxConcurrentThreads != 10 || m.app.Cfg.Subagents.MaxAgentsPerSession < 10 {
		t.Fatalf("runtime config = %+v", m.app.Cfg.Subagents)
	}
	cfg, err := config.Load(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Subagents.MaxConcurrentThreads != 10 || cfg.Subagents.MaxAgentsPerSession < 10 {
		t.Fatalf("persisted config = %+v", cfg.Subagents)
	}
	if len(m.lines) == 0 || !strings.Contains(stripANSI(m.lines[len(m.lines)-1]), "restart Snow") {
		t.Fatalf("command output = %+v", m.lines)
	}
}
