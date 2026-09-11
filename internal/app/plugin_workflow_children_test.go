package app

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPluginWorkflowRestrictionsInheritedAndRefreshedByChild(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	path := writeV2Fixture(t, cwd, "guard", `
 snow.registerCommand({name:"update",description:"update",uses:["workflow","tool_policy"],async run(input,ctx){await ctx.workflow.update(JSON.parse(input));return "updated"}});
 snow.registerCommand({name:"clear",description:"clear",uses:["tool_policy"],async run(_,ctx){await ctx.tools.clearRestriction();return "cleared"}});
 `, []string{"commands", "workflow", "tool_policy"}, nil)
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", Permission: "allow", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{path}, Subagents: new(true)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.ReadySubagents(); err != nil {
		t.Fatal(err)
	}
	runWorkflow(t, a, "guard:update", `{"set":{"profile":"first"},"toolRestriction":{"deny":["grep"]}}`)
	blocking := &blockingChildProvider{Provider: fake.NewRecorded(), started: make(chan struct{}), release: make(chan struct{})}
	a.runtimeSelection.mu.Lock()
	a.runtimeSelection.providers["fake"] = blocking
	a.runtimeSelection.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	child, err := a.SpawnSubagent(ctx, protocol.SpawnSubagentRequest{Name: "workflow_child", Task: "inspect", Role: "explorer", ForkTurns: "none"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocking.started:
	case <-ctx.Done():
		t.Fatal("child provider did not start", ctx.Err())
	}
	if !a.Subagents.HasActive() || a.Agent.IsRunning() {
		t.Fatal("test requires active child and idle root")
	}
	workflow := a.Session.(session.WorkflowStateStore)
	before, err := workflow.WorkflowState(ctx, "guard")
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []struct{ name, input string }{
		{"guard:update", `{"set":{"profile":"rejected"},"toolRestriction":{"deny":["glob"]}}`},
		{"guard:clear", ""},
	} {
		_, err := a.RunPluginCommand(ctx, command.name, command.input)
		if err == nil || !strings.Contains(err.Error(), "finish child work") {
			t.Fatalf("active-child restriction change: %v", err)
		}
		state, err := workflow.WorkflowState(ctx, "guard")
		if err != nil || state.TipID != before.TipID || state.BranchID != before.BranchID || string(state.Values["profile"]) != `"first"` || state.Restriction == nil || !slices.Equal(state.Restriction.Deny, []string{"grep"}) {
			t.Fatalf("rejected change appended metadata or altered state: %+v %v", state, err)
		}
	}
	close(blocking.release)
	if err := a.Subagents.WaitAll(ctx); err != nil {
		t.Fatal(err)
	}
	calls := blocking.RecordedCalls()
	if len(calls) != 1 {
		t.Fatalf("initial child requests: %d", len(calls))
	}
	assertWorkflowChildSchema(t, calls[0], "glob", "grep")
	if a.Subagents.HasActive() {
		t.Fatal("completed child still active")
	}
	runWorkflow(t, a, "guard:update", `{"set":{"profile":"second"},"toolRestriction":{"deny":["glob"]}}`)
	if err := a.FollowupSubagent(ctx, string(child.Agent.Path), "inspect again"); err != nil {
		t.Fatal(err)
	}
	if err := a.Subagents.WaitAll(ctx); err != nil {
		t.Fatal(err)
	}
	calls = blocking.RecordedCalls()
	if len(calls) != 2 {
		t.Fatalf("followup child requests: %d", len(calls))
	}
	assertWorkflowChildSchema(t, calls[1], "grep", "glob")
	state, err := workflow.WorkflowState(ctx, "guard")
	if err != nil || string(state.Values["profile"]) != `"second"` {
		t.Fatalf("idle update not persisted: %+v %v", state, err)
	}
}

func assertWorkflowChildSchema(t *testing.T, request protocol.ChatRequest, allowed, denied string) {
	t.Helper()
	var names []string
	for _, tool := range request.Tools {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, "read") || !slices.Contains(names, allowed) || slices.Contains(names, denied) {
		t.Fatalf("child schema did not apply root restriction: allow=%s deny=%s tools=%v", allowed, denied, names)
	}
}
