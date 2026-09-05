package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func compactionUsageHistory(t *testing.T, a *Agent, st session.Store) {
	t.Helper()
	a.model.ContextWindow = 100
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "local", AutoThresholdPercent: 90}
	for i := range 6 {
		msg := protocol.NewUserMessage(fmt.Sprintf("usage-history-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
}
func usageEvent(tokens int, cost float64) protocol.StreamEvent {
	return protocol.StreamEvent{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 90, Output: tokens - 90, Total: tokens, Cost: &protocol.Cost{Currency: "USD", Total: cost}}}
}
func TestCompactionAccountsAutomaticGoalUsage(t *testing.T) {
	for _, chain := range []bool{false, true} {
		for _, limited := range []bool{false, true} {
			t.Run(fmt.Sprintf("chain_%v/budget_%v", chain, limited), func(t *testing.T) {
				p := &scriptedProvider{}
				a, c, st := goalAgent(t, p)
				compactionUsageHistory(t, a, st)
				var budget *int64
				if limited {
					budget = new(int64(150))
				}
				g, err := c.Create("compact then finish", budget, false)
				if err != nil {
					t.Fatal(err)
				}
				first := []protocol.StreamEvent{{Type: protocol.EvStreamTextDelta, Text: "working"}, usageEvent(95, 0.1), {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}
				if chain {
					first = []protocol.StreamEvent{usageEvent(95, 0.1), {Type: protocol.EvStreamToolCallDone, ToolCallID: "read-goal", ToolName: "get_goal", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}}
				}
				p.scripts = [][]protocol.StreamEvent{first, {
					{Type: protocol.EvStreamTextDelta, Text: "goal summary"},
					usageEvent(92, 0.05), usageEvent(100, 0.2), // snapshots, not additive deltas
					{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
				}}
				if limited {
					p.scripts = append(p.scripts, []protocol.StreamEvent{{Type: protocol.EvStreamTextDelta, Text: "Budget reached; saved progress."}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}, []protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}})
				} else {
					p.scripts = append(p.scripts, []protocol.StreamEvent{{Type: protocol.EvStreamToolCallDone, ToolCallID: "done", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + g.GoalID + `","status":"complete"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}}, []protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}})
				}
				a.ContinueGoal()
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				if err := a.WaitGoal(ctx); err != nil {
					t.Fatal(err)
				}
				got, err := c.Get()
				if err != nil {
					t.Fatal(err)
				}
				wantStatus := protocol.GoalComplete
				if limited {
					wantStatus = protocol.GoalBudgetLimited
				}
				if got.Status != wantStatus || got.TokensUsed != 195 {
					t.Fatalf("goal=%+v calls=%d", got, p.call)
				}
				usage, err := st.AggregateUsage()
				if err != nil || usage.Total != 195 {
					t.Fatalf("usage=%+v err=%v", usage, err)
				}
				if usage.Cost == nil || math.Abs(usage.Cost.Total-0.3) > 1e-9 || len(got.EstimatedCosts) != 1 || math.Abs(got.EstimatedCosts[0].Total-0.3) > 1e-9 {
					t.Fatalf("session cost=%+v goal costs=%+v", usage.Cost, got.EstimatedCosts)
				}
				if len(p.requests) < 3 || !strings.Contains(p.requests[1].System, "working-state checkpoint") {
					t.Fatal("summary was not requested")
				}
				if limited {
					for _, req := range p.requests[2:] {
						var text string
						for _, fragment := range req.InternalContext {
							text += fragment.Text
						}
						if !strings.Contains(strings.ToLower(text), "budget") {
							t.Fatal("normal work continued after summary crossed budget")
						}
					}
				}
			})
		}
	}
}

func TestManualCompactionAccountsFailedAttemptsWithoutChargingGoal(t *testing.T) {
	p := &scriptedProvider{}
	a, c, st := goalAgent(t, p)
	compactionUsageHistory(t, a, st)
	if _, err := c.Create("keep goal paused during manual compaction", nil, false); err != nil {
		t.Fatal(err)
	}
	a.opts.Retry.Normal = fastRetryProfile(3)
	a.opts.Retry.Goal = fastRetryProfile(3)
	p.scripts = [][]protocol.StreamEvent{
		{usageEvent(95, 0.1), {Type: protocol.EvStreamError, Err: &providerpkg.AdvisedError{Err: errors.New("retry summary"), Advice: providerpkg.RetryAdvice{Kind: providerpkg.RetryTransient}}}},
		{{Type: protocol.EvStreamTextDelta, Text: "summary"}, usageEvent(100, 0.2), {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}
	if _, err := a.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	usage, err := st.AggregateUsage()
	if err != nil || usage.Total != 195 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	goal, err := c.Get()
	if err != nil || goal.TokensUsed != 0 {
		t.Fatalf("manual summary charged goal=%+v err=%v", goal, err)
	}
	if p.call != 2 {
		t.Fatalf("calls=%d", p.call)
	}
}

type failCompactionUsageStore struct{ session.Store }

func (s failCompactionUsageStore) Append(entry session.Entry) error {
	if entry.Key == session.MetaProviderUsage {
		return errors.New("usage storage failed")
	}
	return s.Store.Append(entry)
}
func TestCompactionDoesNotHideAccountingFailureWithLocalFallback(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamTextDelta, Text: "summary"}, usageEvent(100, 0.2), {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, _, st := goalAgent(t, p)
	compactionUsageHistory(t, a, st)
	a.opts.Session = failCompactionUsageStore{st}
	var terminal bool
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvCompactionDone && event.IsError {
			terminal = true
		}
	})
	_, err := a.Compact(t.Context())
	if err := a.DrainEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !terminal {
		t.Fatal("failed accounting left compaction without a terminal event")
	}
	if _, ok := errors.AsType[*compactionAccountingError](err); !ok {
		t.Fatalf("accounting error masked: %v", err)
	}
}
