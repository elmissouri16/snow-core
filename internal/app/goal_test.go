package app

import (
	"context"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

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
func TestGoalSubscriberReentrantCompleteAndClose(t *testing.T) {
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
				once.Do(func() { _, _ = a.SetGoalStatus(protocol.GoalComplete); close(done) })
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
	if g.Status != protocol.GoalComplete {
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
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
