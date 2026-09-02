package session

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func goalConflictStores(t *testing.T) map[string]func(*testing.T) Store {
	t.Helper()
	return map[string]func(*testing.T) Store{
		"memory": func(*testing.T) Store { return NewMemoryStore(Options{}) },
		"sqlite": func(t *testing.T) Store {
			st, err := NewSQLiteStore(filepath.Join(t.TempDir(), "goal.db"), t.TempDir(), Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			return st
		},
	}
}

func TestGoalMutationsReturnCurrentTypedConflict(t *testing.T) {
	for name, open := range goalConflictStores(t) {
		t.Run(name, func(t *testing.T) {
			st := open(t)
			goals := st.(ThreadGoalStore)
			atomic := st.(ThreadGoalAtomicStore)
			now := time.Now().UnixMilli()
			current := protocol.ThreadGoal{GoalID: "goal-11111111111111111111111111111111", Objective: "current", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}
			if err := goals.CreateGoal(current, false); err != nil {
				t.Fatal(err)
			}
			_, err := atomic.TransitionGoal("goal-22222222222222222222222222222222", protocol.GoalActive, protocol.GoalComplete, "", false)
			if !errors.Is(err, ErrGoalConflict) {
				t.Fatalf("error = %v, want ErrGoalConflict", err)
			}
			conflict, ok := errors.AsType[*GoalConflictError](err)
			if !ok {
				t.Fatalf("error type = %T", err)
			}
			if conflict.Kind != "goal_state" || conflict.ExpectedGoalID != "goal-22222222222222222222222222222222" || conflict.CurrentGoalID != current.GoalID || conflict.CurrentStatus != protocol.GoalActive || conflict.SessionID != st.ID() || conflict.BranchID != "main" {
				t.Fatalf("conflict = %+v", conflict)
			}
		})
	}
}

func TestGoalAccountingReturnsTypedConflictWithoutChangingUsage(t *testing.T) {
	for name, open := range goalConflictStores(t) {
		t.Run(name, func(t *testing.T) {
			st := open(t)
			goals := st.(ThreadGoalStore)
			now := time.Now().UnixMilli()
			current := protocol.ThreadGoal{GoalID: "goal-11111111111111111111111111111111", Objective: "current", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}
			if err := goals.CreateGoal(current, false); err != nil {
				t.Fatal(err)
			}
			returned, crossed, err := goals.AccountGoal("goal-22222222222222222222222222222222", 7, 3, nil)
			if !errors.Is(err, ErrGoalConflict) {
				t.Fatalf("error = %v, want ErrGoalConflict", err)
			}
			conflict, ok := errors.AsType[*GoalConflictError](err)
			if !ok || conflict.CurrentGoalID != current.GoalID || conflict.ExpectedGoalID == current.GoalID {
				t.Fatalf("conflict = %+v, ok=%v", conflict, ok)
			}
			if crossed || returned == nil || returned.GoalID != current.GoalID || returned.TokensUsed != 0 || returned.SecondsUsed != 0 {
				t.Fatalf("returned=%+v crossed=%v", returned, crossed)
			}
			persisted, readErr := goals.Goal()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if persisted.TokensUsed != 0 || persisted.SecondsUsed != 0 {
				t.Fatalf("usage changed after stale accounting: %+v", persisted)
			}
		})
	}
}
