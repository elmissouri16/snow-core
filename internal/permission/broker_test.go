package permission

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type atomicInt struct{ v atomic.Int64 }

func (a *atomicInt) Add(n int64) { a.v.Add(n) }
func (a *atomicInt) Load() int64 { return a.v.Load() }

func TestBrokerDenyWithoutHandlerOrManual(t *testing.T) {
	svc := NewService(ModeAsk, DenyAll{})
	b := NewBroker(svc)
	var published atomicInt
	b.SetPublisher(func(protocol.AgentEvent) { published.Add(1) })
	d, err := b.Ask(context.Background(), Request{Tool: "bash", Risk: RiskExec})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if d != DecisionDeny {
		t.Fatalf("decision = %s, want deny", d)
	}
	if published.Load() != 0 {
		t.Fatalf("published %d events, want 0", published.Load())
	}
}

// eventCapture captures the single published permission_request event.
type eventCapture struct {
	ch chan protocol.PermissionRequest
}

func capturePublisher() (*eventCapture, func(protocol.AgentEvent)) {
	c := &eventCapture{ch: make(chan protocol.PermissionRequest, 4)}
	return c, func(e protocol.AgentEvent) {
		if e.Permission != nil {
			c.ch <- e.Permission.Request
		}
	}
}

func TestBrokerManualReplyAllowAndRemember(t *testing.T) {
	svc := NewService(ModeAsk, DenyAll{})
	b := NewBroker(svc)
	capture, pub := capturePublisher()
	b.SetPublisher(pub)
	b.EnableManual()

	type result struct {
		d   Decision
		err error
	}
	ch := make(chan result, 1)
	svc.SetAsker(b)
	go func() {
		d, err := svc.Authorize(context.Background(), Request{Tool: "bash", Args: []byte(`{"x":1}`), Risk: RiskExec})
		ch <- result{d, err}
	}()

	select {
	case req := <-capture.ch:
		if req.ID == "" {
			t.Fatal("published request has no id")
		}
		if err := b.Reply(req.ID, DecisionAllowSession); err != nil {
			t.Fatalf("Reply: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("Ask err: %v", res.err)
		}
		if res.d != DecisionAllowSession {
			t.Fatalf("decision = %s, want allow_session", res.d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for resolution")
	}
	// allow_session was remembered as an allow rule for tool|risk.
	if got, _ := svc.Authorize(context.Background(), Request{Tool: "bash", Risk: RiskExec}); got != DecisionAllow {
		t.Fatalf("remembered rule decision = %s, want allow", got)
	}
}

func TestBrokerRejectDenies(t *testing.T) {
	svc := NewService(ModeAsk, DenyAll{})
	b := NewBroker(svc)
	capture, pub := capturePublisher()
	b.SetPublisher(pub)
	b.EnableManual()

	ch := make(chan Decision, 1)
	go func() {
		d, _ := b.Ask(context.Background(), Request{Tool: "bash", Risk: RiskExec})
		ch <- d
	}()
	req := <-capture.ch
	if err := b.Reject(req.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	select {
	case d := <-ch:
		if d != DecisionDeny {
			t.Fatalf("decision = %s, want deny", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestBrokerStaleIDDoesNotResolvePending(t *testing.T) {
	svc := NewService(ModeAsk, DenyAll{})
	b := NewBroker(svc)
	capture, pub := capturePublisher()
	b.SetPublisher(pub)
	b.EnableManual()

	ch := make(chan Decision, 1)
	go func() {
		d, _ := b.Ask(context.Background(), Request{Tool: "bash", Risk: RiskExec})
		ch <- d
	}()
	req := <-capture.ch
	if err := b.Reply("perm-stale", DecisionAllow); err == nil {
		t.Fatal("expected error for stale id")
	}
	// pending still unresolved
	select {
	case d := <-ch:
		t.Fatalf("resolved prematurely with %s", d)
	case <-time.After(50 * time.Millisecond):
	}
	if err := b.Reply(req.ID, DecisionDeny); err != nil {
		t.Fatalf("Reply valid: %v", err)
	}
	select {
	case d := <-ch:
		if d != DecisionDeny {
			t.Fatalf("decision = %s, want deny", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestBrokerHandlerErrorDenies(t *testing.T) {
	svc := NewService(ModeAsk, DenyAll{})
	b := NewBroker(svc)
	var published atomicInt
	b.SetPublisher(func(protocol.AgentEvent) { published.Add(1) })
	b.SetHandler(func(ctx context.Context, req protocol.PermissionRequest) (protocol.PermissionResponse, error) {
		return protocol.PermissionResponse{}, errors.New("handler exploded")
	})
	d, err := b.Ask(context.Background(), Request{Tool: "bash", Risk: RiskExec})
	if err == nil {
		t.Fatal("expected Ask to surface handler error")
	}
	if d != DecisionDeny {
		t.Fatalf("decision = %s, want deny", d)
	}
	if published.Load() == 0 {
		t.Fatal("handler path must publish before invoking handler")
	}
}

func TestBrokerHandlerAllowAndRemember(t *testing.T) {
	svc := NewService(ModeAsk, DenyAll{})
	b := NewBroker(svc)
	b.SetPublisher(func(protocol.AgentEvent) {})
	b.SetHandler(func(ctx context.Context, req protocol.PermissionRequest) (protocol.PermissionResponse, error) {
		return protocol.PermissionResponse{RequestID: req.ID, Decision: protocol.PermissionAllowAlways}, nil
	})
	svc.SetAsker(b)
	d, err := svc.Authorize(context.Background(), Request{Tool: "bash", Risk: RiskExec})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if d != DecisionAllowAlways {
		t.Fatalf("decision = %s, want allow_always", d)
	}
	// allow_always collapsed to remembered allow session rule.
	if got, _ := svc.Authorize(context.Background(), Request{Tool: "bash", Risk: RiskExec}); got != DecisionAllow {
		t.Fatalf("remembered = %s, want allow", got)
	}
}

func TestBrokerCancelDeniesAndPublishesNext(t *testing.T) {
	svc := NewService(ModeAsk, DenyAll{})
	b := NewBroker(svc)
	capture, pub := capturePublisher()
	b.SetPublisher(pub)
	b.EnableManual()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := make(chan Decision, 1)
	go func() {
		d, _ := b.Ask(ctx, Request{Tool: "bash", Risk: RiskExec})
		ch <- d
	}()
	req := <-capture.ch
	cancel()
	select {
	case d := <-ch:
		if d != DecisionDeny {
			t.Fatalf("decision = %s, want deny on cancel", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout cancel")
	}
	_ = req
}

func TestBrokerActivatesRequestsFIFO(t *testing.T) {
	b := NewBroker(NewService(ModeAsk, DenyAll{}))
	capture, publish := capturePublisher()
	b.SetPublisher(publish)
	b.EnableManual()

	type result struct {
		tool     string
		decision Decision
	}
	results := make(chan result, 2)
	go func() {
		decision, _ := b.Ask(context.Background(), Request{Tool: "first", Risk: RiskExec})
		results <- result{tool: "first", decision: decision}
	}()
	first := <-capture.ch
	go func() {
		decision, _ := b.Ask(context.Background(), Request{Tool: "second", Risk: RiskExec})
		results <- result{tool: "second", decision: decision}
	}()
	select {
	case request := <-capture.ch:
		t.Fatalf("published queued request %q before first resolved", request.Tool)
	case <-time.After(50 * time.Millisecond):
	}
	if err := b.Reply(first.ID, DecisionAllow); err != nil {
		t.Fatal(err)
	}
	second := <-capture.ch
	if second.Tool != "second" {
		t.Fatalf("second published tool = %q", second.Tool)
	}
	if err := b.Reject(second.ID); err != nil {
		t.Fatal(err)
	}
	want := map[string]Decision{"first": DecisionAllow, "second": DecisionDeny}
	for range 2 {
		select {
		case got := <-results:
			if got.decision != want[got.tool] {
				t.Fatalf("%s decision = %s, want %s", got.tool, got.decision, want[got.tool])
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for decision")
		}
	}
}

func TestBrokerCloseReleasesActiveAndQueued(t *testing.T) {
	b := NewBroker(NewService(ModeAsk, DenyAll{}))
	capture, publish := capturePublisher()
	b.SetPublisher(publish)
	b.EnableManual()

	results := make(chan error, 2)
	go func() {
		_, err := b.Ask(context.Background(), Request{Tool: "first", Risk: RiskExec})
		results <- err
	}()
	<-capture.ch
	go func() {
		_, err := b.Ask(context.Background(), Request{Tool: "second", Risk: RiskExec})
		results <- err
	}()
	time.Sleep(20 * time.Millisecond)
	b.Close()
	for range 2 {
		select {
		case err := <-results:
			if err == nil || err.Error() != "permission: broker is closed" {
				t.Fatalf("close error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("close left a request blocked")
		}
	}
}
