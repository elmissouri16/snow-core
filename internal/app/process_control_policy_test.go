package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/permission"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type processControlTestPolicy struct {
	request permission.Request
	denied  bool
	fail    bool
	called  int
}

func (p *processControlTestPolicy) Evaluate(_ context.Context, req permission.Request) (permission.PolicyDecision, error) {
	p.called++
	p.request = req
	if p.fail {
		return permission.PolicyDecision{}, errors.New("PRIVATE_POLICY_FAILURE")
	}
	return permission.PolicyDecision{Denied: p.denied, Reason: "PRIVATE_POLICY_REASON"}, nil
}
func (p *processControlTestPolicy) EvaluateInvocationPolicy(ctx context.Context, req permission.Request) (permission.PolicyDecision, error) {
	return p.Evaluate(ctx, req)
}

func TestProcessControlPolicyAdapterFailsClosed(t *testing.T) {
	req := processControlStopRequest(protocol.RPCProcessControlStopParams{SessionID: "session", ProcessID: "proc_" + strings.Repeat("a", 32)})
	if req.Tool != "process_stop" || req.Risk != permission.RiskExec || !req.Rememberable {
		t.Fatalf("wrong stop classification: %+v", req)
	}
	var args map[string]any
	if json.Unmarshal(req.Args, &args) != nil || len(args) != 2 || args["grace_ms"] != float64(2000) || args["process_id"] == nil {
		t.Fatalf("wrong builtin arguments: %s", req.Args)
	}
	for _, value := range []any{nil, struct{}{}, &processControlTestPolicy{denied: true}, &processControlTestPolicy{fail: true}} {
		err := authorizeProcessControlPolicy(t.Context(), value, req)
		if !errors.Is(err, ErrProcessControlDenied) || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("must deny unavailable/hard policy without private output: %v", err)
		}
	}
	p := &processControlTestPolicy{}
	if err := authorizeProcessControlPolicy(t.Context(), p, req); err != nil || p.called != 1 {
		t.Fatalf("allow policy=%v calls=%d", err, p.called)
	}
}

func TestProcessControlConfiguredHardPolicyCannotBeOverridden(t *testing.T) {
	a := newProcessTestApp(t, []string{"read"})
	t.Cleanup(func() { _ = a.Close() })
	configured := &processControlTestPolicy{denied: true}
	custom, err := agent.New(agent.Options{Provider: a.Provider, Registry: a.Registry, Session: a.Session, Permission: a.Perm, Model: a.Model, InvocationPolicy: configured})
	if err != nil {
		t.Fatal(err)
	}
	a.Agent.Close()
	a.Agent = custom
	state, err := a.ProcessManager.Start(t.Context(), managedprocess.StartRequest{Command: "sleep 30", Name: "policy-owned"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := protocol.RPCProcessControlStopParams{SessionID: a.Session.ID(), ProcessID: state.ProcessID, GraceMS: 50}
	// Current allow must not override hard deny or hard-policy evaluation errors.
	for _, fail := range []bool{false, true} {
		configured.fail = fail
		before := configured.called
		_, err := a.ProcessControlStop(t.Context(), p)
		if !errors.Is(err, ErrProcessControlDenied) || configured.called != before+1 {
			t.Fatalf("configured policy was not consulted: calls=%d err=%v", configured.called, err)
		}
		current, err := a.ProcessManager.Status(state.ProcessID)
		if err != nil || current.Status != "running" {
			t.Fatalf("denied Stop had an effect: %+v %v", current, err)
		}
	}
	configured.denied, configured.fail = false, false
	result, err := a.ProcessControlStop(t.Context(), p)
	if err != nil || result.Process.Status == "running" {
		t.Fatalf("allowed Stop=%+v %v", result, err)
	}
}
