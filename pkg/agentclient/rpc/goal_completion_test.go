package rpc

import (
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestGoalCompletionIsTypedAndIndependentOfPromptCompletion(t *testing.T) {
	for _, status := range []string{"finished", "canceled", "failed"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, Options{})
			want := protocol.RPCGoalRunCompleted{Type: protocol.RPCTypeGoalRunCompleted, RequestID: "request", GoalRunID: "run", GoalID: "goal", Status: status, GoalStatus: protocol.GoalPaused}
			writeJSON(t, f.peer, want)
			got := receive(t, f.client.Events())
			if got.GoalRunCompleted == nil || *got.GoalRunCompleted != want || got.AgentEvent != nil || got.PromptCompleted != nil {
				t.Fatalf("untyped or conflated goal terminal: %+v", got)
			}
		})
	}
}

func TestGoalCompletionMalformedBindingsFailClosed(t *testing.T) {
	for _, frame := range []string{
		`{"type":"goal_run_completed","goal_run_id":"run","goal_id":"goal","status":"finished"}`,
		`{"type":"goal_run_completed","request_id":"request","goal_id":"goal","status":"finished"}`,
		`{"type":"goal_run_completed","request_id":"request","goal_run_id":"run","status":"finished"}`,
		`{"type":"goal_run_completed","request_id":"request","goal_run_id":"run","goal_id":"goal","status":"completed"}`,
		`{"type":"goal_run_completed","request_id":"request","request_id":"foreign","goal_run_id":"run","goal_id":"goal","status":"finished"}`,
	} {
		t.Run(frame, func(t *testing.T) {
			f := newFixture(t, Options{})
			_, _ = f.peer.Write([]byte(frame + "\n"))
			awaitFailure(t, f.client, ErrMalformedFrame)
		})
	}
}

func TestGoalRunEventRetainsExplicitCorrelation(t *testing.T) {
	f := newFixture(t, Options{})
	for _, turn := range []string{"turn-one", "turn-two"} {
		writeJSON(t, f.peer, protocol.AgentEvent{Type: protocol.EvTextDelta, GoalRunID: "whole-run", TurnID: turn, Text: "public"})
		e := receive(t, f.client.Events())
		if e.AgentEvent == nil || e.AgentEvent.GoalRunID != "whole-run" || e.AgentEvent.TurnID != turn || e.GoalRunCompleted != nil {
			t.Fatalf("correlation lost: %+v", e)
		}
	}
}
