package agent

import (
	"errors"
	"testing"
	"time"

	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestFailedToolResultDoesNotCountAsTurnProgress(t *testing.T) {
	a := &Agent{}
	a.recordToolOutcome(tools.ErrorResult(session.ErrGoalConflict))
	a.mu.RLock()
	progress := a.turnProgress
	a.mu.RUnlock()
	if progress {
		t.Fatal("failed tool result counted as turn progress")
	}

	a.recordToolOutcome(tools.TextResult("inspected"))
	a.mu.RLock()
	progress = a.turnProgress
	a.mu.RUnlock()
	if !progress {
		t.Fatal("successful tool result did not count as turn progress")
	}
}

func TestStaleTurnAccountingDoesNotDeferReplacementGoal(t *testing.T) {
	a, controller, _ := goalAgent(t, &scriptedProvider{})
	admitted, err := controller.Create("original", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.turnOrigin = "goal"
	a.turnMode = protocol.ModeDefault
	a.goalAtTurn = admitted.Clone()
	a.turnStarted = time.Now()
	a.mu.Unlock()
	replacement, err := controller.Create("replacement", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	continuing, err := a.finalizeGoalTurn(nil, false)
	if !errors.Is(err, session.ErrGoalConflict) {
		t.Fatalf("finalize error=%v, want goal conflict", err)
	}
	if continuing {
		t.Fatal("stale admitted turn continued replacement goal")
	}
	current, err := controller.Get()
	if err != nil {
		t.Fatal(err)
	}
	if current.GoalID != replacement.GoalID || current.Status != protocol.GoalActive {
		t.Fatalf("replacement changed by stale accounting: %+v", current)
	}
	deferred, err := controller.Deferred()
	if err != nil {
		t.Fatal(err)
	}
	if deferred {
		t.Fatal("stale accounting deferred the replacement goal")
	}
}

func TestRepeatedGoalConflictsPauseAutomaticContinuation(t *testing.T) {
	a, controller, store := goalAgent(t, &scriptedProvider{})
	goal, err := controller.Create("finish safely", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	details := goalpkg.ConflictDetails{
		Conflict: session.GoalConflictError{
			Kind:              "goal_id",
			ExpectedGoalID:    "goal-00000000000000000000000000000000",
			CurrentGoalID:     goal.GoalID,
			CurrentStatus:     protocol.GoalActive,
			SessionID:         store.ID(),
			BranchID:          "main",
			BindingGeneration: controller.Binding().Generation,
		},
		Binding: controller.Binding(),
	}

	for turn := 1; turn <= 3; turn++ {
		a.mu.Lock()
		a.turnOrigin = "goal"
		a.turnMode = protocol.ModeDefault
		a.goalAtTurn = goal.Clone()
		a.turnStarted = time.Now()
		a.turnProgress = true // Explanatory text must not bypass the conflict breaker.
		copy := details
		a.turnGoalConflict = &copy
		a.mu.Unlock()

		continuing, err := a.finalizeGoalTurn(nil, false)
		if err != nil {
			t.Fatalf("turn %d: %v", turn, err)
		}
		if turn < 3 && !continuing {
			t.Fatalf("turn %d stopped before the bounded retry threshold", turn)
		}
		if turn == 3 && continuing {
			t.Fatal("third repeated conflict remained eligible for continuation")
		}
	}

	got, err := controller.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != protocol.GoalPaused {
		t.Fatalf("goal status = %q, want %q", got.Status, protocol.GoalPaused)
	}
	deferred, err := controller.Deferred()
	if err != nil {
		t.Fatal(err)
	}
	if !deferred {
		t.Fatal("conflict-paused goal remained eligible for automatic continuation")
	}
}
