package permission

import (
	"context"
	"errors"
	"testing"
)

func fakeReq(tool string, risk Risk) Request {
	return Request{Tool: tool, Risk: risk}
}

func TestDenyModeAllowsRead(t *testing.T) {
	s := NewService(ModeDeny, nil)
	if d, _ := s.Authorize(context.Background(), fakeReq("read", RiskRead)); d != DecisionAllow {
		t.Fatalf("read should be allowed in deny mode, got %s", d)
	}
	if d, _ := s.Authorize(context.Background(), fakeReq("bash", RiskExec)); d != DecisionDeny {
		t.Fatalf("bash should be denied, got %s", d)
	}
}

func TestDelegateRiskPolicy(t *testing.T) {
	s := NewService(ModeDeny, nil)
	if s.CanExpose("spawn_agent", RiskDelegate) {
		t.Fatal("deny mode exposed delegation")
	}
	if d, _ := s.Authorize(context.Background(), fakeReq("spawn_agent", RiskDelegate)); d != DecisionDeny {
		t.Fatalf("decision=%s", d)
	}
	calls := 0
	s = NewService(ModeAsk, askerFunc(func(context.Context, Request) (Decision, error) { calls++; return DecisionAllow, nil }))
	if d, _ := s.Authorize(context.Background(), fakeReq("spawn_agent", RiskDelegate)); d != DecisionAllow || calls != 1 {
		t.Fatalf("ask=%s calls=%d", d, calls)
	}
}

func TestAllowModeAllowsEverything(t *testing.T) {
	s := NewService(ModeAllow, nil)
	if d, _ := s.Authorize(context.Background(), fakeReq("write", RiskWrite)); d != DecisionAllow {
		t.Fatalf("expected allow, got %s", d)
	}
}

func TestAskModeUsesAsker(t *testing.T) {
	calls := 0
	s := NewService(ModeAsk, askerFunc(func(ctx context.Context, r Request) (Decision, error) {
		calls++
		return DecisionAllow, nil
	}))
	d, _ := s.Authorize(context.Background(), fakeReq("write", RiskWrite))
	if d != DecisionAllow || calls != 1 {
		t.Fatalf("asker not used: d=%s calls=%d", d, calls)
	}
}

func TestAskModeReadAllowedWithoutAsker(t *testing.T) {
	s := NewService(ModeAsk, nil)
	d, err := s.Authorize(context.Background(), fakeReq("read", RiskRead))
	if err != nil || d != DecisionAllow {
		t.Fatalf("read should be allowed without asker: %s %v", d, err)
	}
}

func TestAskModeDenyByDefaultHeadless(t *testing.T) {
	s := NewService(ModeAsk, nil)
	d, err := s.Authorize(context.Background(), fakeReq("bash", RiskExec))
	if err != nil || d != DecisionDeny {
		t.Fatalf("headless ask mode should deny exec: %s %v", d, err)
	}
}

func TestRememberSessionRule(t *testing.T) {
	s := NewService(ModeAsk, nil)
	req := fakeReq("bash", RiskExec)
	s.Remember(req, DecisionAllow)
	d, _ := s.Authorize(context.Background(), req)
	if d != DecisionAllow {
		t.Fatalf("session rule should allow, got %s", d)
	}
	// Different tool still denied.
	d2, _ := s.Authorize(context.Background(), fakeReq("write", RiskWrite))
	if d2 != DecisionDeny {
		t.Fatalf("write should still deny, got %s", d2)
	}
}

func TestAskerErrorDenies(t *testing.T) {
	s := NewService(ModeAsk, askerFunc(func(ctx context.Context, r Request) (Decision, error) {
		return DecisionDeny, errors.New("user cancelled")
	}))
	d, err := s.Authorize(context.Background(), fakeReq("write", RiskWrite))
	if d != DecisionDeny || err == nil {
		t.Fatalf("expected deny + error, got %s %v", d, err)
	}
}

func TestInvalidAskerDecisionDenies(t *testing.T) {
	s := NewService(ModeAsk, askerFunc(func(context.Context, Request) (Decision, error) {
		return Decision("approved"), nil
	}))
	d, err := s.Authorize(context.Background(), fakeReq("write", RiskWrite))
	if d != DecisionDeny || err == nil {
		t.Fatalf("invalid decision should deny with an error, got %q, %v", d, err)
	}
}

func TestAllowSessionPersists(t *testing.T) {
	calls := 0
	s := NewService(ModeAsk, askerFunc(func(context.Context, Request) (Decision, error) {
		calls++
		return DecisionAllowSession, nil
	}))
	req := fakeReq("write", RiskWrite)
	if d, err := s.Authorize(context.Background(), req); err != nil || d != DecisionAllowSession {
		t.Fatalf("first decision = %q, %v", d, err)
	}
	if d, err := s.Authorize(context.Background(), req); err != nil || d != DecisionAllow || calls != 1 {
		t.Fatalf("remembered decision = %q, %v; calls=%d", d, err, calls)
	}
}

func TestAllowAlwaysPersists(t *testing.T) {
	calls := 0
	s := NewService(ModeAsk, askerFunc(func(ctx context.Context, r Request) (Decision, error) {
		calls++
		return DecisionAllowAlways, nil
	}))
	req := fakeReq("bash", RiskExec)
	d, _ := s.Authorize(context.Background(), req)
	if d != DecisionAllowAlways {
		t.Fatalf("expected allow_always, got %s", d)
	}
	// Second time: remembered, asker not called again.
	d2, _ := s.Authorize(context.Background(), req)
	if d2 != DecisionAllow || calls != 1 {
		t.Fatalf("allow_always should be remembered: d2=%s calls=%d", d2, calls)
	}
}

func TestModeAccessors(t *testing.T) {
	s := NewService(ModeAsk, nil)
	if s.Mode() != ModeAsk {
		t.Fatal("wrong initial mode")
	}
	s.SetMode(ModeDeny)
	if s.Mode() != ModeDeny {
		t.Fatal("SetMode failed")
	}
}

func TestStateRestoreAndChangeHandler(t *testing.T) {
	s := NewService(ModeAsk, nil)
	req := fakeReq("bash", RiskExec)
	var changes []State
	s.SetChangeHandler(func(state State) { changes = append(changes, state) })
	s.SetMode(ModeAllow)
	s.Remember(req, DecisionAllow)
	if len(changes) != 2 || changes[1].Mode != ModeAllow || changes[1].Rules[ruleKey(req)] != DecisionAllow {
		t.Fatalf("changes = %+v", changes)
	}

	restored := NewService(ModeDeny, nil)
	restored.RestoreState(changes[1])
	if restored.Mode() != ModeAllow {
		t.Fatalf("restored mode = %q", restored.Mode())
	}
	state := restored.State()
	if state.Rules[ruleKey(req)] != DecisionAllow {
		t.Fatalf("restored rules = %+v", state.Rules)
	}
}

type askerFunc func(ctx context.Context, r Request) (Decision, error)

func (f askerFunc) Ask(ctx context.Context, r Request) (Decision, error) { return f(ctx, r) }
