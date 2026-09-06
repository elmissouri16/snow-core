package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func registerGoalWork(t *testing.T, a *Agent, run func() tools.ToolResult) {
	t.Helper()
	err := a.opts.Registry.(*tools.SimpleRegistry).Register(&testTool{
		schema:  protocol.ToolSchema{Name: "goal_work", Parameters: []byte(`{"type":"object"}`)},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult { return run() },
	})
	if err != nil {
		t.Fatal(err)
	}
}

func goalCall(id, name, args string) protocol.StreamEvent {
	return protocol.StreamEvent{Type: protocol.EvStreamToolCallDone, ToolCallID: id, ToolName: name, Arguments: []byte(args)}
}

func goalDone() protocol.StreamEvent {
	return protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}
}
func goalToolsDone() protocol.StreamEvent {
	return protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}
}
func goalTokens(n int) protocol.StreamEvent {
	return protocol.StreamEvent{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: n}}
}

func TestGoalBudgetBlocksCrossingBatchAndReportTools(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{goalTokens(1), goalCall("cross-1", "goal_work", `{}`), goalCall("cross-2", "goal_work", `{}`), goalToolsDone()},
		{goalTokens(10), goalCall("report", "goal_work", `{}`), goalToolsDone()},
	}}
	a, c, st := goalAgent(t, p)
	ran := 0
	registerGoalWork(t, a, func() tools.ToolResult { ran++; return tools.TextResult("worked") })
	if _, err := c.Create("bounded", new(int64(1)), false); err != nil {
		t.Fatal(err)
	}
	a.ContinueGoal()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := a.WaitGoal(ctx); err != nil {
		t.Fatal(err)
	}
	g, _ := c.Get()
	if ran != 0 || p.call != 2 || g.Status != protocol.GoalBudgetLimited || g.TokensUsed != 11 {
		t.Fatalf("ran=%d calls=%d goal=%+v", ran, p.call, g)
	}
	if len(p.requests[0].Tools) == 0 || len(p.requests[1].Tools) != 0 {
		t.Fatal("final report must expose no tools")
	}
	if !strings.Contains(p.requests[1].InternalContext[0].Text, "budget has been reached") {
		t.Fatal("missing report steering")
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]bool{}
	for _, m := range messages {
		if m.Role == protocol.RoleTool {
			if !m.IsError || !strings.Contains(m.Content[0].Text, "goal token budget") {
				t.Fatalf("unexpected result=%+v", m)
			}
			results[m.ToolCallID] = true
		}
	}
	for _, id := range []string{"cross-1", "cross-2", "report"} {
		if !results[id] {
			t.Fatalf("unpaired call %s", id)
		}
	}
	// A subsequent direct user request must not inherit the old budget gate.
	p.scripts = append(p.scripts, []protocol.StreamEvent{goalCall("user", "goal_work", `{}`), goalToolsDone()}, []protocol.StreamEvent{goalDone()})
	if err := a.Prompt(t.Context(), "new direct task"); err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Fatalf("new user work remained blocked: ran=%d", ran)
	}
}

func TestGoalBudgetReportDoesNotRetry(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{goalTokens(1), goalDone()},
		{{Type: protocol.EvStreamError, Err: &providerpkg.AdvisedError{Err: errors.New("temporary"), Advice: providerpkg.RetryAdvice{Kind: providerpkg.RetryTransient}}}},
	}}
	a, c, _ := goalAgent(t, p)
	a.opts.Retry.Goal = fastRetryProfile(3)
	if _, err := c.Create("bounded", new(int64(1)), false); err != nil {
		t.Fatal(err)
	}
	a.ContinueGoal()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := a.WaitGoal(ctx); err != nil {
		t.Fatal(err)
	}
	if p.call != 2 {
		t.Fatalf("report retried: calls=%d", p.call)
	}
}

func TestGoalBudgetPreservesQueuedFollowUp(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{goalCall("work", "goal_work", `{}`), goalToolsDone()},
		{goalTokens(1), goalDone()},
	}}
	a, c, _ := goalAgent(t, p)
	registerGoalWork(t, a, func() tools.ToolResult {
		if _, err := a.QueueInput(protocol.QueuedInputFollowUp, "keep this input"); err != nil {
			t.Error(err)
		}
		return tools.TextResult("worked")
	})
	if _, err := c.Create("bounded", new(int64(1)), false); err != nil {
		t.Fatal(err)
	}
	if err := a.internalTurn(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	pending := a.PendingInputs()
	if len(pending.Items) != 1 || pending.Items[0].Text != "keep this input" {
		t.Fatalf("pending=%+v", pending)
	}
}

func TestGoalCompactionBudgetBlocksReportTools(t *testing.T) {
	for _, chain := range []bool{false, true} {
		t.Run(fmt.Sprint(chain), func(t *testing.T) {
			p := &scriptedProvider{}
			a, c, st := goalAgent(t, p)
			compactionUsageHistory(t, a, st)
			ran := 0
			registerGoalWork(t, a, func() tools.ToolResult { ran++; return tools.TextResult("worked") })
			if _, err := c.Create("compact then finish", new(int64(150)), false); err != nil {
				t.Fatal(err)
			}
			first := []protocol.StreamEvent{goalTokens(95), goalDone()}
			if chain {
				first = []protocol.StreamEvent{goalTokens(95), goalCall("read", "get_goal", `{}`), goalToolsDone()}
			}
			p.scripts = [][]protocol.StreamEvent{
				first,
				{{Type: protocol.EvStreamTextDelta, Text: "checkpoint"}, goalTokens(100), goalDone()},
				{goalCall("report-work", "goal_work", `{}`), goalToolsDone()},
			}
			a.ContinueGoal()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := a.WaitGoal(ctx); err != nil {
				t.Fatal(err)
			}
			g, _ := c.Get()
			if ran != 0 || p.call != 3 || g.TokensUsed != 195 || g.Status != protocol.GoalBudgetLimited {
				t.Fatalf("ran=%d calls=%d goal=%+v", ran, p.call, g)
			}
			if len(p.requests[2].Tools) != 0 {
				t.Fatal("compaction budget report exposed tools")
			}
		})
	}
}
