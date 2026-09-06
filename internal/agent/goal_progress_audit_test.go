package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestGoalRepeatedResponsesAndStatusReadsPause(t *testing.T) {
	for _, statusRead := range []bool{false, true} {
		t.Run(fmt.Sprint(statusRead), func(t *testing.T) {
			p := &scriptedProvider{}
			a, c, _ := goalAgent(t, p)
			for i := range 3 {
				if statusRead {
					p.scripts = append(p.scripts, []protocol.StreamEvent{goalCall(fmt.Sprint(i), "get_goal", `{}`), goalToolsDone()})
				}
				// Different chunks and whitespace must produce the same fingerprint.
				first, second := "Still", " waiting for credentials."
				if i == 1 {
					first, second = "St", "ill   waiting for credentials."
				}
				p.scripts = append(p.scripts, []protocol.StreamEvent{
					{Type: protocol.EvStreamTextDelta, Text: first},
					{Type: protocol.EvStreamTextDelta, Text: second}, goalDone(),
				})
			}
			if _, err := c.Create("deploy", nil, false); err != nil {
				t.Fatal(err)
			}
			a.ContinueGoal()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := a.WaitGoal(ctx); err != nil {
				t.Fatal(err)
			}
			g, _ := c.Get()
			deferred, _ := c.Deferred()
			want := 3
			if statusRead {
				want = 6
			}
			if g.Status != protocol.GoalPaused || !deferred || p.call != want {
				t.Fatalf("goal=%+v deferred=%v calls=%d", g, deferred, p.call)
			}
			if _, err := c.SetStatus(g.GoalID, protocol.GoalActive, false); err != nil {
				t.Fatal(err)
			}
			a.ResetGoalAudit()
			if err := a.internalTurn(t.Context(), false); err != nil {
				t.Fatal(err)
			}
			g, _ = c.Get()
			if g.Status != protocol.GoalActive {
				t.Fatal("resume did not reset audit")
			}
		})
	}
}

func TestGoalStatusOnlyTurnsPause(t *testing.T) {
	p := &scriptedProvider{}
	for i := range 3 {
		p.scripts = append(p.scripts, []protocol.StreamEvent{goalCall(fmt.Sprint(i), "get_goal", `{}`), goalToolsDone()}, []protocol.StreamEvent{goalDone()})
	}
	a, c, _ := goalAgent(t, p)
	if _, err := c.Create("work", nil, false); err != nil {
		t.Fatal(err)
	}
	a.ContinueGoal()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := a.WaitGoal(ctx); err != nil {
		t.Fatal(err)
	}
	g, _ := c.Get()
	if g.Status != protocol.GoalPaused || p.call != 6 {
		t.Fatalf("goal=%+v calls=%d", g, p.call)
	}
}

func TestGoalDistinctResponsesAndSuccessfulWorkDoNotPause(t *testing.T) {
	for _, work := range []bool{false, true} {
		t.Run(fmt.Sprint(work), func(t *testing.T) {
			p := &scriptedProvider{}
			a, c, _ := goalAgent(t, p)
			registerGoalWork(t, a, func() tools.ToolResult { return tools.TextResult("verified") })
			for i := range 5 {
				text := fmt.Sprintf("Verified requirement %d", i)
				if work {
					text = "Verified another requirement"
					p.scripts = append(p.scripts, []protocol.StreamEvent{goalCall(fmt.Sprint(i), "goal_work", `{}`), goalToolsDone()})
				}
				p.scripts = append(p.scripts, []protocol.StreamEvent{{Type: protocol.EvStreamTextDelta, Text: text}, goalDone()})
			}
			if _, err := c.Create("verify", nil, false); err != nil {
				t.Fatal(err)
			}
			for range 5 {
				if err := a.internalTurn(t.Context(), false); err != nil {
					t.Fatal(err)
				}
			}
			g, _ := c.Get()
			if g.Status != protocol.GoalActive {
				t.Fatalf("productive goal=%+v", g)
			}
		})
	}
}
