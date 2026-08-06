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

type askerFunc func(ctx context.Context, r Request) (Decision, error)

func (f askerFunc) Ask(ctx context.Context, r Request) (Decision, error) { return f(ctx, r) }
