package session

import (
	"database/sql"
	"math"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func exerciseGoals(t *testing.T, st Store) {
	t.Helper()
	gs := st.(ThreadGoalStore)
	budget := int64(10)
	now := time.Now().UnixMilli()
	g := protocol.ThreadGoal{GoalID: "g1", Objective: "ship safely", Status: protocol.GoalActive, TokenBudget: &budget, CreatedAt: now, UpdatedAt: now}
	tip := st.BranchTip()
	if err := gs.CreateGoal(g, false); err != nil {
		t.Fatal(err)
	}
	if st.BranchTip() != tip {
		t.Fatal("goal moved tip")
	}
	if err := gs.CreateGoal(protocol.ThreadGoal{GoalID: "g2", Objective: "replace", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}, false); err == nil {
		t.Fatal("unfinished replacement succeeded")
	}
	if err := gs.ClearGoal(""); err == nil {
		t.Fatal("wildcard clear deleted an existing goal")
	}
	if still, _ := gs.Goal(); still == nil || still.GoalID != "g1" {
		t.Fatalf("goal after wildcard clear=%+v", still)
	}
	paused := protocol.GoalPaused
	if _, err := gs.UpdateGoal("stale", nil, &paused, nil); err == nil {
		t.Fatal("stale update succeeded")
	}
	blockedStatus := protocol.GoalBlocked
	if _, err := gs.UpdateGoal("g1", nil, &blockedStatus, nil); err == nil {
		t.Fatal("reasonless blocked update succeeded")
	}
	atomic := st.(ThreadGoalAtomicStore)
	if _, err := atomic.TransitionGoal("g1", protocol.GoalActive, protocol.GoalBlocked, "  ", false); err == nil {
		t.Fatal("reasonless atomic blocked transition succeeded")
	}
	blocked, err := atomic.TransitionGoal("g1", protocol.GoalActive, protocol.GoalBlocked, "required service unavailable", false)
	if err != nil || blocked.BlockedReason != "required service unavailable" {
		t.Fatalf("blocked transition=%+v err=%v", blocked, err)
	}
	resumed, err := atomic.TransitionGoal("g1", protocol.GoalBlocked, protocol.GoalActive, "", true)
	if err != nil || resumed.BlockedReason != "" {
		t.Fatalf("resumed transition=%+v err=%v", resumed, err)
	}
	got, cross, err := gs.AccountGoal("stale", 10, 1, nil)
	if err != nil || cross || got.GoalID != "g1" || got.TokensUsed != 0 {
		t.Fatalf("stale account=%+v %v %v", got, cross, err)
	}
	got, cross, err = gs.AccountGoal("g1", 10, 2, &protocol.Cost{Currency: "usd", Input: 0.01, Output: 0.02, Total: 0.03})
	if err != nil || !cross || got.Status != protocol.GoalBudgetLimited || got.TokensUsed != 10 {
		t.Fatalf("exact budget=%+v %v %v", got, cross, err)
	}
	if len(got.EstimatedCosts) != 1 || got.EstimatedCosts[0].Currency != "USD" || got.EstimatedCosts[0].Total != 0.03 {
		t.Fatalf("estimated costs=%+v", got.EstimatedCosts)
	}
	branches := st.(BranchStore)
	fork, err := branches.ForkBranch("root")
	if err != nil {
		t.Fatal(err)
	}
	forkGoal, _ := gs.Goal()
	if forkGoal.GoalID == got.GoalID || forkGoal.BranchID != fork.ID {
		t.Fatalf("fork=%+v goal=%+v", fork, forkGoal)
	}
	status := protocol.GoalActive
	if _, err := gs.UpdateGoal(forkGoal.GoalID, nil, &status, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := gs.AccountGoal(forkGoal.GoalID, 1, 0, &protocol.Cost{Currency: "EUR", Total: 0.5}); err != nil {
		t.Fatal(err)
	}
	forkGoal, _ = gs.Goal()
	if len(forkGoal.EstimatedCosts) != 2 {
		t.Fatalf("fork did not preserve/add per-currency costs: %+v", forkGoal.EstimatedCosts)
	}
	if err := branches.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	main, _ := gs.Goal()
	if main.TokensUsed != 10 || main.Status != protocol.GoalBudgetLimited || len(main.EstimatedCosts) != 1 || main.EstimatedCosts[0].Total != 0.03 {
		t.Fatalf("main diverged=%+v", main)
	}
}
func TestMemoryGoals(t *testing.T) { exerciseGoals(t, NewMemoryStore(Options{})) }

func TestSQLiteGoalAccountingAtomicAcrossHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent-goal.db")
	first, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	now := time.Now().UnixMilli()
	goal := protocol.ThreadGoal{GoalID: "concurrent", Objective: "account atomically", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}
	if err := first.CreateGoal(goal, false); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for range 100 {
		for _, store := range []*SQLiteStore{first, second} {
			wg.Add(1)
			go func(st *SQLiteStore) {
				defer wg.Done()
				_, _, err := st.AccountGoal(goal.GoalID, 1, 0, &protocol.Cost{Currency: "USD", Total: 0.001})
				errs <- err
			}(store)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := first.Goal()
	if err != nil {
		t.Fatal(err)
	}
	if got.TokensUsed != 200 {
		t.Fatalf("lost concurrent accounting updates: got %d want 200", got.TokensUsed)
	}
	if len(got.EstimatedCosts) != 1 || math.Abs(got.EstimatedCosts[0].Total-0.2) > 1e-9 {
		t.Fatalf("lost concurrent estimated costs: %+v", got.EstimatedCosts)
	}
}

func TestGoalCostOverflowRejectedAtomically(t *testing.T) {
	factories := map[string]func(t *testing.T) Store{
		"memory": func(t *testing.T) Store { return NewMemoryStore(Options{}) },
		"sqlite": func(t *testing.T) Store {
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "overflow.db"), t.TempDir(), Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			return store
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			store := factory(t)
			goals := store.(ThreadGoalStore)
			now := time.Now().UnixMilli()
			goal := protocol.ThreadGoal{GoalID: "overflow", Objective: "check overflow", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}
			if err := goals.CreateGoal(goal, false); err != nil {
				t.Fatal(err)
			}
			if _, _, err := goals.AccountGoal(goal.GoalID, 0, 0, &protocol.Cost{Currency: "USD", Total: math.MaxFloat64}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := goals.AccountGoal(goal.GoalID, 1, 1, &protocol.Cost{Currency: "USD", Total: math.MaxFloat64}); err == nil {
				t.Fatal("cost overflow accepted")
			}
			got, err := goals.Goal()
			if err != nil {
				t.Fatal(err)
			}
			if got.TokensUsed != 0 || got.SecondsUsed != 0 || len(got.EstimatedCosts) != 1 || got.EstimatedCosts[0].Total != math.MaxFloat64 {
				t.Fatalf("overflow partially mutated goal: %+v", got)
			}
		})
	}
}

func TestSQLiteGoalReadsAreAtomicWithCostAccounting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atomic-read.db")
	reader, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	now := time.Now().UnixMilli()
	goal := protocol.ThreadGoal{GoalID: "atomic-read", Objective: "read atomically", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}
	if err := reader.CreateGoal(goal, false); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for range 100 {
			if _, _, err := writer.AccountGoal(goal.GoalID, 1, 0, &protocol.Cost{Currency: "USD", Total: 0.01}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			got, err := reader.Goal()
			if err != nil {
				t.Fatal(err)
			}
			if got.TokensUsed != 100 || len(got.EstimatedCosts) != 1 || math.Abs(got.EstimatedCosts[0].Total-1) > 1e-9 {
				t.Fatalf("final goal=%+v", got)
			}
			return
		default:
			got, err := reader.Goal()
			if err != nil {
				t.Fatal(err)
			}
			if got.TokensUsed == 0 && len(got.EstimatedCosts) == 0 {
				runtime.Gosched()
				continue
			}
			if len(got.EstimatedCosts) != 1 || math.Abs(got.EstimatedCosts[0].Total-float64(got.TokensUsed)*0.01) > 1e-9 {
				t.Fatalf("torn goal snapshot: %+v", got)
			}
			runtime.Gosched()
		}
	}
}

func TestSQLiteGoalTransitionsAndReplacementAreCompareAndSwap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal-cas.db")
	first, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	now := time.Now().UnixMilli()
	original := protocol.ThreadGoal{GoalID: "cas-original", Objective: "original", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}
	if err := first.CreateGoal(original, false); err != nil {
		t.Fatal(err)
	}

	type result struct {
		goal *protocol.ThreadGoal
		err  error
	}
	results := make(chan result, 2)
	go func() {
		g, err := first.TransitionGoal(original.GoalID, protocol.GoalActive, protocol.GoalPaused, "", false)
		results <- result{g, err}
	}()
	go func() {
		g, err := second.TransitionGoal(original.GoalID, protocol.GoalActive, protocol.GoalBlocked, "dependency unavailable", false)
		results <- result{g, err}
	}()
	successes := 0
	for range 2 {
		if r := <-results; r.err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("conditional transition successes=%d want 1", successes)
	}
	current, err := first.Goal()
	if err != nil {
		t.Fatal(err)
	}
	if current.Status == protocol.GoalBlocked && current.BlockedReason != "dependency unavailable" {
		t.Fatalf("blocked reason = %q", current.BlockedReason)
	}
	if _, _, err := second.AccountGoal(current.GoalID, 5, 1, nil); err != nil {
		t.Fatal(err)
	}
	revised, err := first.ReviseGoal(current.GoalID, "cas-revision", "revised objective")
	if err != nil {
		t.Fatal(err)
	}
	if revised.TokensUsed != 5 || revised.SecondsUsed != 1 || revised.Status != protocol.GoalActive || revised.BlockedReason != "" {
		t.Fatalf("revision lost accounting/state: %+v", revised)
	}
	if _, err := second.ReviseGoal(current.GoalID, "stale-revision", "stale"); err == nil {
		t.Fatal("stale revision overwrote newer goal")
	}
	replacement := *revised.Clone()
	replacement.GoalID = "cas-replacement"
	replacement.Objective = "replacement"
	if err := first.ReplaceGoal(revised.GoalID, replacement); err != nil {
		t.Fatal(err)
	}
	stale := replacement
	stale.GoalID = "stale-overwrite"
	if err := second.ReplaceGoal(revised.GoalID, stale); err == nil {
		t.Fatal("stale replacement overwrote newer goal")
	}
	got, _ := first.Goal()
	if got.GoalID != replacement.GoalID || got.Objective != replacement.Objective {
		t.Fatalf("goal=%+v", got)
	}
}

func TestSQLiteV7MigrationBackfillsUnambiguousGoalCosts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal-cost-migration.db")
	st, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Second).UnixMilli()
	goal := protocol.ThreadGoal{GoalID: "legacy-cost", Objective: "migrate cost", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}
	if err := st.CreateGoal(goal, false); err != nil {
		t.Fatal(err)
	}
	usage := &protocol.Usage{Input: 90, Output: 10, Total: 100, Cost: &protocol.Cost{Currency: "USD", Input: 0.01, Output: 0.02, Total: 0.03}}
	message := protocol.NewAssistantMessage("priced", st.BranchTip(), "opencode-go", "model", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, usage)
	if err := st.Append(Entry{Type: EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AccountGoal(goal.GoalID, 100, 1, usage.Cost); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE thread_goal_costs; UPDATE session_meta SET version=7`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	reopened, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Goal()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.EstimatedCosts) != 1 || got.EstimatedCosts[0].Total != 0.03 {
		t.Fatalf("migrated estimated costs=%+v", got.EstimatedCosts)
	}
}

func TestSQLiteGoalBlockedReasonV10Migration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal-v9.db")
	st, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	goal := protocol.ThreadGoal{GoalID: "legacy-blocked", Objective: "wait for dependency", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}
	if err := st.CreateGoal(goal, false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TransitionGoal(goal.GoalID, protocol.GoalActive, protocol.GoalBlocked, "not retained by v9", false); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE thread_goals DROP COLUMN blocked_reason`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE session_meta SET version=9`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Goal()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != protocol.GoalBlocked || got.BlockedReason != "" || reopened.Header().Version != SessionVersion {
		t.Fatalf("migrated goal=%+v version=%d", got, reopened.Header().Version)
	}
}

func TestSQLiteGoalsReopenForkAndV3Migration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal.db")
	st, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	exerciseGoals(t, st)
	msg := protocol.NewUserMessage("persist", "", "keep")
	if err := st.Append(Entry{Type: EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db, _ := sql.Open("sqlite", sqliteDSN(path))
	if _, err := db.Exec(`UPDATE session_meta SET version=3`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	re, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	if re.Header().Version != SessionVersion {
		t.Fatalf("version=%d", re.Header().Version)
	}
	g, err := re.Goal()
	if err != nil || g == nil || g.GoalID != "g1" {
		t.Fatalf("reopen=%+v %v", g, err)
	}
	if len(g.EstimatedCosts) != 1 || g.EstimatedCosts[0].Currency != "USD" || g.EstimatedCosts[0].Total != 0.03 {
		t.Fatalf("reopened estimated costs=%+v", g.EstimatedCosts)
	}
}
