package session

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
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
	got, cross, err := gs.AccountGoal("stale", 10, 1)
	if err != nil || cross || got.GoalID != "g1" || got.TokensUsed != 0 {
		t.Fatalf("stale account=%+v %v %v", got, cross, err)
	}
	got, cross, err = gs.AccountGoal("g1", 10, 2)
	if err != nil || !cross || got.Status != protocol.GoalBudgetLimited || got.TokensUsed != 10 {
		t.Fatalf("exact budget=%+v %v %v", got, cross, err)
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
	if _, _, err := gs.AccountGoal(forkGoal.GoalID, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := branches.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	main, _ := gs.Goal()
	if main.TokensUsed != 10 || main.Status != protocol.GoalBudgetLimited {
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
	for i := 0; i < 100; i++ {
		for _, store := range []*SQLiteStore{first, second} {
			wg.Add(1)
			go func(st *SQLiteStore) {
				defer wg.Done()
				_, _, err := st.AccountGoal(goal.GoalID, 1, 0)
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
		g, err := first.TransitionGoal(original.GoalID, protocol.GoalActive, protocol.GoalPaused, false)
		results <- result{g, err}
	}()
	go func() {
		g, err := second.TransitionGoal(original.GoalID, protocol.GoalActive, protocol.GoalBlocked, false)
		results <- result{g, err}
	}()
	successes := 0
	for i := 0; i < 2; i++ {
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
	if _, _, err := second.AccountGoal(current.GoalID, 5, 1); err != nil {
		t.Fatal(err)
	}
	revised, err := first.ReviseGoal(current.GoalID, "cas-revision", "revised objective")
	if err != nil {
		t.Fatal(err)
	}
	if revised.TokensUsed != 5 || revised.SecondsUsed != 1 || revised.Status != protocol.GoalActive {
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
}
