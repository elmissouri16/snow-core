package snowsdk

import (
	"context"
	"errors"
	"github.com/snow-core/snow/pkg/protocol"
	"testing"
)

func TestSDKGoalMethodsStopped(t *testing.T) {
	var nilSession *Session
	if _, err := nilSession.Goal(); !errors.Is(err, ErrStopped) {
		t.Fatalf("nil goal err=%v", err)
	}
	if err := nilSession.Abort(context.Background()); !errors.Is(err, ErrStopped) {
		t.Fatalf("nil abort err=%v", err)
	}
	s, err := Open(context.Background(), Options{Provider: "fake", NoSession: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err = s.CreateGoal("x", nil, false); !errors.Is(err, ErrStopped) {
		t.Fatalf("closed create err=%v", err)
	}
	if err = s.Abort(context.Background()); !errors.Is(err, ErrStopped) {
		t.Fatalf("closed abort err=%v", err)
	}
	// Observer/snapshot helpers are intentionally zero-value safe because they
	// cannot return lifecycle errors in their established signatures.
	nilSession.Subscribe(nil)()
	s.Subscribe(nil)()
	if got := nilSession.StateEvent(); got.Type != "" {
		t.Fatalf("nil state event=%+v", got)
	}
	if got := s.Model(); got.ID != "" {
		t.Fatalf("closed model=%+v", got)
	}
}

func TestSDKGoalLifecycleAndAbort(t *testing.T) {
	s, e := Open(context.Background(), Options{Provider: "fake", PermissionMode: "allow", CWD: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	g, e := s.CreateGoal("ship", nil, false)
	if e != nil {
		t.Fatal(e)
	}
	s.Abort(context.Background())
	g, e = s.PauseGoal()
	if e != nil {
		t.Fatal(e)
	}
	if g.Status != protocol.GoalPaused {
		t.Fatalf("goal=%+v", g)
	}
	if e = s.ClearGoal(); e != nil {
		t.Fatal(e)
	}
	g, e = s.Goal()
	if e != nil || g != nil {
		t.Fatalf("goal=%+v err=%v", g, e)
	}
}
