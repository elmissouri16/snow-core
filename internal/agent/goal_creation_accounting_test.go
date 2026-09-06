package agent

import (
	"fmt"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCreatedGoalAccountsFollowingRequestsAndCompletion(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(fmt.Sprint(limited), func(t *testing.T) {
			p := &scriptedProvider{}
			a, c, _ := goalAgent(t, p)
			ran := 0
			registerGoalWork(t, a, func() tools.ToolResult {
				ran++
				// Use an elapsed baseline without making the test sleep.
				a.mu.Lock()
				a.turnStarted = time.Now().Add(-2 * time.Second)
				a.mu.Unlock()
				g, err := c.Get()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := c.SetStatus(g.GoalID, protocol.GoalComplete, true); err != nil {
					t.Error(err)
				}
				return tools.TextResult("completed")
			})
			budget := ""
			if limited {
				budget = `,"token_budget":1`
			}
			p.scripts = [][]protocol.StreamEvent{
				{goalTokens(100), goalCall("create", "create_goal", `{"objective":"work"`+budget+`}`), goalToolsDone()},
				{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 10, Cost: &protocol.Cost{Currency: "USD", Total: 0.2}}}, goalCall("work", "goal_work", `{}`), goalToolsDone()},
				{goalTokens(10), goalDone()},
			}
			if err := a.Prompt(t.Context(), "create and finish a goal"); err != nil {
				t.Fatal(err)
			}
			if err := a.WaitGoal(t.Context()); err != nil {
				t.Fatal(err)
			}
			g, err := c.Get()
			if err != nil {
				t.Fatal(err)
			}
			if g.TokensUsed != 20 || len(g.EstimatedCosts) != 1 || g.EstimatedCosts[0].Total != 0.2 {
				t.Fatalf("goal=%+v", g)
			}
			if limited {
				if ran != 0 || g.Status != protocol.GoalBudgetLimited {
					t.Fatalf("ran=%d goal=%+v", ran, g)
				}
			} else if ran != 1 || g.Status != protocol.GoalComplete || g.SecondsUsed < 2 {
				t.Fatalf("ran=%d goal=%+v", ran, g)
			}
		})
	}
}

func TestCreatedGoalRebindsAfterSameTurnCompletion(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	first, err := c.Create("first", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	var previousTokens int64
	registerGoalWork(t, a, func() tools.ToolResult {
		old, err := c.Get()
		if err != nil {
			t.Fatal(err)
		}
		previousTokens = old.TokensUsed
		a.mu.Lock()
		a.turnStarted = time.Now().Add(-2 * time.Second)
		a.mu.Unlock()
		return tools.TextResult("inspected")
	})
	p.scripts = [][]protocol.StreamEvent{
		{goalTokens(5), goalCall("work", "goal_work", `{}`), goalCall("complete", "update_goal", fmt.Sprintf(`{"goal_id":%q,"status":"complete"}`, first.GoalID)), goalToolsDone()},
		{goalTokens(7), goalCall("create", "create_goal", `{"objective":"second","token_budget":1}`), goalToolsDone()},
		{goalTokens(10), goalDone()},
		{goalTokens(2), goalDone()},
	}
	if err := a.Prompt(t.Context(), "complete then create a new goal"); err != nil {
		t.Fatal(err)
	}
	if err := a.WaitGoal(t.Context()); err != nil {
		t.Fatal(err)
	}
	g, err := c.Get()
	if err != nil {
		t.Fatal(err)
	}
	if previousTokens != 5 || g.GoalID == first.GoalID || g.TokensUsed != 12 || g.SecondsUsed != 0 || g.Status != protocol.GoalBudgetLimited {
		t.Fatalf("previous tokens=%d new goal=%+v", previousTokens, g)
	}
}

func TestFailedGoalCreationKeepsUsageOwner(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	first, err := c.Create("first", new(int64(20)), false)
	if err != nil {
		t.Fatal(err)
	}
	p.scripts = [][]protocol.StreamEvent{
		{goalTokens(5), goalCall("create", "create_goal", `{"objective":"replacement"}`), goalToolsDone()},
		{goalTokens(15), goalDone()},
		{goalDone()},
	}
	if err := a.Prompt(t.Context(), "try another goal"); err != nil {
		t.Fatal(err)
	}
	if err := a.WaitGoal(t.Context()); err != nil {
		t.Fatal(err)
	}
	g, err := c.Get()
	if err != nil {
		t.Fatal(err)
	}
	if g.GoalID != first.GoalID || g.TokensUsed != 20 || g.Status != protocol.GoalBudgetLimited {
		t.Fatalf("goal=%+v", g)
	}
}
