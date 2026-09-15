package web

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRuntimePermissionPolicyTransitionsAndSessionBaseline(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "workflow-policy")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil || s.PermissionMode != "ask" {
		t.Fatalf("initial: %+v %v", s, err)
	}
	for _, mode := range []string{"deny", "allow", "ask", "allow"} {
		before := policyLog(t, log)
		if err := m.SetPermissionMode(t.Context(), p.ID, s.InstanceID, mode, mode == "allow"); err != nil {
			t.Fatal(err)
		}
		s, _ = m.Snapshot(p.ID)
		if s.PermissionMode != mode || s.Status != "idle" {
			t.Fatalf("policy: %+v", s)
		}
		if got := strings.TrimPrefix(policyLog(t, log), before); got != "permission_mode_set\npolicy:"+mode+"\nsession_info\n" {
			t.Fatalf("mutation RPCs: %q", got)
		}
	}
	original := s
	s, err = m.Switch(t.Context(), p.ID, original.InstanceID, "", false)
	if err != nil || s.PermissionMode != "ask" {
		t.Fatalf("new baseline: %+v %v", s, err)
	}
	if err := m.SetPermissionMode(t.Context(), p.ID, original.InstanceID, "deny", false); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("stale switch identity: %v", err)
	}
	s, err = m.Switch(t.Context(), p.ID, s.InstanceID, original.SessionID, false)
	if err != nil || s.PermissionMode != "allow" {
		t.Fatalf("saved policy: %+v %v", s, err)
	}
	before := policyLog(t, log)
	for range 3 {
		m.Snapshot(p.ID)
	}
	if policyLog(t, log) != before {
		t.Fatal("snapshot mutated policy")
	}
}

func TestRuntimePermissionPolicyValidatesRestoredPolicy(t *testing.T) {
	for _, mode := range []string{"ask", "deny", "allow", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, "workflow-policy")
			s, err := m.Open(t.Context(), projects[0], "saved-"+mode, "", "")
			if mode == "invalid" {
				if err == nil {
					t.Fatal("invalid restored policy trusted")
				}
			} else if err != nil || s.PermissionMode != mode {
				t.Fatalf("restored policy: %+v %v", s, err)
			}
			if strings.Contains(policyLog(t, log), "permission_mode_set") {
				t.Fatal("restore forced policy")
			}
		})
	}
}

func TestRuntimePermissionPolicyRejectsInvalidIdentityAndConfirmation(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "workflow-policy")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	before := policyLog(t, log)
	for _, tc := range []struct {
		project, instance, mode string
		confirm                 bool
	}{
		{p.ID, s.InstanceID, "", false}, {p.ID, s.InstanceID, "ALLOW", true}, {p.ID, s.InstanceID, "auto", false},
		{p.ID, s.InstanceID, "allow", false}, {p.ID, s.InstanceID, "ask", true}, {p.ID, s.InstanceID, "deny", true},
		{p.ID, "stale", "deny", false}, {p.ID, "", "deny", false}, {projects[1].ID, s.InstanceID, "deny", false},
	} {
		if err := m.SetPermissionMode(t.Context(), tc.project, tc.instance, tc.mode, tc.confirm); err == nil {
			t.Fatalf("invalid request admitted: %+v", tc)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := m.SetPermissionMode(ctx, p.ID, s.InstanceID, "deny", false); err == nil {
		t.Fatal("canceled request admitted")
	}
	if policyLog(t, log) != before {
		t.Fatal("invalid request reached worker")
	}
}

func TestRuntimePermissionPolicyRequiresIdleVerifiedAuthority(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "workflow-policy")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.runtime(p.ID, s.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	before := policyLog(t, log)
	for _, mutate := range []func(){
		func() { r.busy = true }, func() { r.transitioning = true },
		func() { r.snapshot.Status = "running" }, func() { r.snapshot.Status = "opening" },
		func() { r.snapshot.Status = "unknown" }, func() { r.snapshot.Status = "failed" },
		func() { r.snapshot.Status = "closing" }, func() { r.snapshot.Status = "switching" },
		func() { r.snapshot.Permission = &RuntimePermission{ID: "pending"} },
		func() { r.snapshot.Input = &protocol.UserInputRequest{ID: "pending"} },
		func() { r.snapshot.PermissionMode = "" }, func() { r.snapshot.SessionID = "" },
		func() { r.snapshot.Recovery.State = RecoveryAdmissionUnknown },
		func() { r.snapshot.Recovery.State = RecoveryAdmitted }, func() { r.snapshot.Recovery.State = "unknown" },
	} {
		r.mu.Lock()
		mutate()
		r.mu.Unlock()
		if err := m.SetPermissionMode(t.Context(), p.ID, s.InstanceID, "allow", true); err == nil {
			t.Fatal("non-idle policy accepted")
		}
		r.mu.Lock()
		r.snapshot = s.clone()
		r.busy = false
		r.transitioning = false
		r.mu.Unlock()
	}
	r.control.Lock()
	err = m.SetPermissionMode(t.Context(), p.ID, s.InstanceID, "deny", false)
	r.control.Unlock()
	if !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("operation gate: %v", err)
	}
	if policyLog(t, log) != before {
		t.Fatal("blocked control reached worker")
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPermissionMode(t.Context(), p.ID, s.InstanceID, "deny", false); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("busy turn: %v", err)
	}
}

func TestRuntimePermissionPolicyMutationUncertaintyFailsClosed(t *testing.T) {
	for _, mode := range []string{"workflow-policy-exit", "workflow-policy-rejected", "workflow-policy-info-rejected", "workflow-policy-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, mode)
			p := projects[0]
			s, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := m.SetPermissionMode(t.Context(), p.ID, s.InstanceID, "allow", true); !errors.Is(err, ErrRuntimeUnavailable) {
				t.Fatalf("uncertain policy: %v", err)
			}
			failed, _ := m.Snapshot(p.ID)
			if failed.Status != "failed" || failed.PermissionMode != "ask" {
				t.Fatalf("unverified policy published: %+v", failed)
			}
			if err := m.SetPermissionMode(t.Context(), p.ID, s.InstanceID, "allow", true); err == nil {
				t.Fatal("failed mutation retried")
			}
			if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "retry"); err == nil {
				t.Fatal("prompt admitted after uncertain policy")
			}
			if strings.Count(policyLog(t, log), "permission_mode_set\n") != 1 {
				t.Fatal("mutation repeated")
			}
		})
	}
}

func TestRuntimePermissionPolicyBrowserDisconnectDoesNotRetryOrCancel(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "workflow-policy-browser-disconnect")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.SetPermissionMode(ctx, p.ID, s.InstanceID, "allow", true) }()
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(policyLog(t, log), "policy:allow\n") {
		if time.Now().After(deadline) {
			t.Fatal("mutation not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := os.WriteFile(log+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admitted mutation did not finish")
	}
	s, _ = m.Snapshot(p.ID)
	if s.Status != "idle" || s.PermissionMode != "allow" {
		t.Fatalf("policy after disconnect: %+v", s)
	}
	if strings.Count(policyLog(t, log), "permission_mode_set\n") != 1 {
		t.Fatal("disconnect repeated mutation")
	}
}

func TestRuntimePermissionPolicyPublicationDoesNotReviveTerminalRuntime(t *testing.T) {
	for _, terminal := range []string{"canceled", "eof", "closing"} {
		t.Run(terminal, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, "workflow-policy")
			p := projects[0]
			s, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			r, err := m.runtime(p.ID, s.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			r.control.Lock()
			defer r.control.Unlock()
			switch terminal {
			case "canceled":
				r.cancel()
			case "eof":
				_ = r.worker.Close()
				<-r.drained
			case "closing":
				r.mu.Lock()
				r.snapshot.Status = "closing"
				r.mu.Unlock()
			}
			if err := r.publishPermissionPolicy(protocol.RPCSessionInfo{SessionID: s.SessionID, PermissionMode: "allow"}); !errors.Is(err, ErrRuntimeUnavailable) {
				t.Fatalf("terminal publication: %v", err)
			}
			s, _ = m.Snapshot(p.ID)
			if s.PermissionMode != "ask" {
				t.Fatal("late policy published")
			}
		})
	}
}

func policyLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
