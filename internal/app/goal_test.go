package app

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSetSessionRebindsGoalControllerProjection(t *testing.T) {
	a, err := New(t.Context(), Options{Provider: "fake", Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	before := a.Goal.Binding()

	next := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	now := time.Now().UnixMilli()
	nextGoal := protocol.ThreadGoal{
		GoalID:    "goal-11111111111111111111111111111111",
		Objective: "complete after rebind",
		Status:    protocol.GoalActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := next.CreateGoal(nextGoal, false); err != nil {
		t.Fatal(err)
	}
	if err := next.SetGoalContinuationDeferred(true); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSession(next); err != nil {
		t.Fatal(err)
	}

	binding := a.Goal.Binding()
	if binding.Generation != before.Generation+1 || binding.SessionID != next.ID() || binding.BranchID != "main" {
		t.Fatalf("binding before=%+v after=%+v", before, binding)
	}
	current, err := a.GoalState()
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.GoalID != nextGoal.GoalID || current.SessionID != next.ID() || current.BranchID != "main" {
		t.Fatalf("goal after rebind=%+v", current)
	}
	completed, err := a.Goal.SetStatus(nextGoal.GoalID, protocol.GoalComplete, true)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != protocol.GoalComplete {
		t.Fatalf("completed goal=%+v", completed)
	}
}

func TestGoalRequiresSavedSessionAndCapabilities(t *testing.T) {
	a, e := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if _, e = a.CreateGoal("ship", nil, false); e == nil || !strings.Contains(e.Error(), "persisted session") {
		t.Fatalf("error=%v", e)
	}
	b, e := New(context.Background(), Options{Provider: "fake", Permission: "allow", CWD: t.TempDir(), Tools: []string{"read"}})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if _, e = b.CreateGoal("ship", nil, false); e == nil || !strings.Contains(e.Error(), "required capability") {
		t.Fatalf("error=%v", e)
	}
}
func TestGoalSubscriberReentrantPauseAndClose(t *testing.T) {
	a, e := New(context.Background(), Options{Provider: "fake", Permission: "allow", CWD: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	var once sync.Once
	unsubscribeGoal := a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvThreadGoalUpdated && ev.ThreadGoal != nil && ev.ThreadGoal.Goal != nil {
			g, err := a.GoalState()
			if err != nil {
				t.Error(err)
				return
			}
			if g.Status == protocol.GoalActive {
				once.Do(func() { _, _ = a.PauseGoal(); close(done) })
			}
		}
	})
	if _, e = a.CreateGoal("reentrant", nil, false); e != nil {
		t.Fatal(e)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscriber deadlocked")
	}
	g, _ := a.GoalState()
	if g.Status != protocol.GoalPaused {
		t.Fatalf("goal=%+v", g)
	}
	// Test Close reentrancy independently: no earlier subscriber should read
	// session state after this callback intentionally closes the app.
	unsubscribeGoal()
	closed := make(chan struct{})
	a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvModeChanged {
			_ = a.Close()
			close(closed)
		}
	})
	a.Agent.Publish(a.Agent.StateEvent())
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close callback deadlocked")
	}
}

func TestPersistedDeferralHonoredUntilReady(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.db")
	st, err := session.NewSQLiteStore(path, t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if err := st.CreateGoal(protocol.ThreadGoal{GoalID: "resume", Objective: "resume work", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}, false); err != nil {
		t.Fatal(err)
	}
	_ = st.SetGoalContinuationDeferred(true)
	msg := protocol.NewUserMessage("keep", "", "keep")
	_ = st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg})
	_ = st.Close()
	a, err := New(context.Background(), Options{Provider: "fake", Permission: "allow", SessionPath: path, CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	time.Sleep(20 * time.Millisecond)
	if a.Agent.IsRunning() {
		t.Fatal("goal started before surface readiness")
	}
	if err := a.ReadyGoal(); err != nil {
		t.Fatal(err)
	}
	if a.Agent.IsRunning() {
		t.Fatal("deferred goal started")
	}
	g, _ := a.GoalState()
	if g.Status != protocol.GoalActive {
		t.Fatalf("goal=%+v", g)
	}
	resumed, err := a.ResumeGoal()
	if err != nil {
		t.Fatal(err)
	}
	if resumed.GoalID != g.GoalID {
		t.Fatalf("resumed goal=%+v", resumed)
	}
	deferred, err := a.Goal.Deferred()
	if err != nil || deferred {
		t.Fatalf("deferred=%v err=%v", deferred, err)
	}
	a.Agent.Abort()
}

func TestManualCompactDeferralSurvivesReadyGoal(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(t.TempDir(), "compact-ready.db")
	st, err := session.NewSQLiteStore(path, cwd, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if err := st.CreateGoal(protocol.ThreadGoal{GoalID: "compact-ready", Objective: "do not resume after compact", Status: protocol.GoalActive, CreatedAt: now, UpdatedAt: now}, false); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	a, err := New(context.Background(), Options{Provider: "fake", Permission: "allow", SessionPath: path, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.Agent.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	deferred, err := a.Goal.Deferred()
	if err != nil || !deferred {
		t.Fatalf("manual compact deferred=%v err=%v", deferred, err)
	}
	if err := a.ReadyGoal(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if a.Agent.IsRunning() {
		t.Fatal("ready restarted goal after manual compact")
	}
	goal, err := a.GoalState()
	if err != nil || goal == nil || goal.Status != protocol.GoalActive {
		t.Fatalf("goal=%+v err=%v", goal, err)
	}
}

func TestResumeGoalChecksCapabilitiesBeforeTransition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restricted-resume.db")
	st, err := session.NewSQLiteStore(path, t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if err := st.CreateGoal(protocol.ThreadGoal{GoalID: "paused", Objective: "resume safely", Status: protocol.GoalPaused, CreatedAt: now, UpdatedAt: now}, false); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	a, err := New(context.Background(), Options{Provider: "fake", Permission: "allow", SessionPath: path, CWD: t.TempDir(), Tools: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.ResumeGoal(); err == nil || !strings.Contains(err.Error(), "required capability") {
		t.Fatalf("resume error=%v", err)
	}
	goal, err := a.GoalState()
	if err != nil || goal.Status != protocol.GoalPaused {
		t.Fatalf("goal=%+v err=%v", goal, err)
	}
}

func TestFailedGoalMutationRestartsPreviousAutomaticGoal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*App) error
	}{
		{name: "edit", mutate: func(a *App) error { _, err := a.EditGoal(" "); return err }},
		{name: "replace", mutate: func(a *App) error {
			invalid := int64(-1)
			_, err := a.CreateGoal("replacement", &invalid, true)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := New(context.Background(), Options{Provider: "fake", Permission: "allow", CWD: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			original, err := a.CreateGoal("keep working", nil, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.mutate(a); err == nil {
				t.Fatal("invalid mutation succeeded")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			if err := a.Agent.WaitGoal(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := a.GoalState()
			if err != nil {
				t.Fatal(err)
			}
			if got.GoalID != original.GoalID || got.Status != protocol.GoalPaused {
				t.Fatalf("previous goal was not restarted: %+v", got)
			}
		})
	}
}

func TestAppGoalLifecycle(t *testing.T) {
	a, e := New(context.Background(), Options{Provider: "fake", Permission: "allow", CWD: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	g, e := a.CreateGoal("ship", nil, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.PauseGoal(); e != nil {
		t.Fatal(e)
	}
	got, _ := a.GoalState()
	if got.GoalID != g.GoalID || got.Status != protocol.GoalPaused {
		t.Fatalf("goal=%+v", got)
	}
	if e := a.ClearGoal(); e != nil {
		t.Fatal(e)
	}
	got, _ = a.GoalState()
	if got != nil {
		t.Fatalf("goal=%+v", got)
	}
}
