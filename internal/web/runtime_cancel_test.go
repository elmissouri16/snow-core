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

func cancelFixtureRelease(t *testing.T, log, suffix string) {
	t.Helper()
	if err := os.WriteFile(log+suffix, nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func cancelFixtureWaitLog(t *testing.T, log, suffix string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.HasSuffix(policyLog(t, log), suffix) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("worker log never ended in %q: %q", suffix, policyLog(t, log))
}

func TestRuntimeTurnCancelDuringPromptAdmission(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "cancel-gated")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.CancelToken != "" || s.CancelRequested {
		t.Fatalf("idle cancellation: %+v", s)
	}
	result := make(chan error, 1)
	go func() { result <- m.Prompt(t.Context(), p.ID, s.InstanceID, "cancel-delayed") }()
	s = runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "running" })
	if s.CancelToken == "" {
		t.Fatal("running published without token")
	}
	cancelFixtureWaitLog(t, log, "prompt\n")
	if err := m.Abort(t.Context(), p.ID, s.InstanceID); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("legacy admission gate: %v", err)
	}
	requestCtx, disconnect := context.WithCancel(t.Context())
	if err := m.CancelTurn(requestCtx, p.ID, s.InstanceID, s.CancelToken); err != nil {
		t.Fatal(err)
	}
	disconnect()
	for range 10 {
		if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
			t.Fatal(err)
		}
	}
	pending, _ := m.Snapshot(p.ID)
	if pending.Status != "running" || !pending.CancelRequested || pending.CancelToken != s.CancelToken {
		t.Fatalf("pending: %+v", pending)
	}
	if strings.Count(policyLog(t, log), "abort\n") != 1 {
		t.Fatal("abort dispatched before admission acknowledgment")
	}
	select {
	case err := <-result:
		t.Fatalf("prompt returned before ack: %v", err)
	default:
	}
	cancelFixtureRelease(t, log, ".ack")
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	cancelFixtureWaitLog(t, log, "prompt\nabort\n")
	r, _ := m.runtime(p.ID, s.InstanceID)
	r.cancelTasks.Wait() // abort ack is not definitive completion
	pending, _ = m.Snapshot(p.ID)
	if pending.Status != "running" || !pending.CancelRequested {
		t.Fatalf("abort ack pretended idle: %+v", pending)
	}
	if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
		t.Fatal(err)
	}
	if strings.Count(policyLog(t, log), "abort\n") != 2 {
		t.Fatal("duplicate retried abort")
	}
	cancelFixtureRelease(t, log, ".complete")
	done := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && !s.CancelRequested })
	if done.CancelToken != "" || done.CancelRequested {
		t.Fatalf("completed cancellation retained: %+v", done)
	}
	if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err == nil {
		t.Fatal("completed token accepted")
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Snapshot(p.ID)
	if next.CancelToken == "" || next.CancelToken == s.CancelToken || next.CancelRequested {
		t.Fatalf("next turn: %+v", next)
	}
	// Even a descheduled old task cannot cancel a newly admitted prompt.
	m.dispatchTurnCancel(r, p.ID, s.InstanceID, s.SessionID, s.CancelToken)
	if strings.Count(policyLog(t, log), "abort\n") != 2 {
		t.Fatal("old task aborted new turn")
	}
}

func TestRuntimeTurnCancelCompletionAndRejectionBeforeAck(t *testing.T) {
	for _, message := range []string{"cancel-complete", "cancel-reject"} {
		t.Run(message, func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, "")
			p := projects[0]
			initial, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { result <- m.Prompt(t.Context(), p.ID, initial.InstanceID, message) }()
			s := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "running" })
			if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
				t.Fatal(err)
			}
			cancelFixtureRelease(t, log, ".ack")
			err = <-result
			if message == "cancel-reject" && !errors.Is(err, ErrRuntimeInvalid) || message == "cancel-complete" && err != nil {
				t.Fatalf("ack: %v", err)
			}
			done := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && !s.CancelRequested })
			r, _ := m.runtime(p.ID, s.InstanceID)
			r.cancelTasks.Wait()
			if done.CancelRequested || done.CancelToken != "" {
				t.Fatal("idle retained cancellation")
			}
			if strings.Count(policyLog(t, log), "abort\n") != 1 {
				t.Fatal("completed/rejected turn dispatched abort")
			}
		})
	}
}

func TestRuntimeTurnCancelAttentionAndValidation(t *testing.T) {
	for _, state := range []string{"hold", "permission", "input"} {
		t.Run(state, func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, "cancel-gated")
			p := projects[0]
			initial, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Prompt(t.Context(), p.ID, initial.InstanceID, state); err != nil {
				t.Fatal(err)
			}
			status := state
			if status == "hold" {
				status = "running"
			}
			s := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == status })
			before := policyLog(t, log)
			for _, ids := range [][3]string{{p.ID, "", s.CancelToken}, {p.ID, "stale", s.CancelToken}, {p.ID, s.InstanceID, ""}, {p.ID, s.InstanceID, "old-turn"}, {projects[1].ID, s.InstanceID, s.CancelToken}} {
				if err := m.CancelTurn(t.Context(), ids[0], ids[1], ids[2]); err == nil {
					t.Fatal("stale authority accepted")
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := m.CancelTurn(ctx, p.ID, s.InstanceID, s.CancelToken); err == nil {
				t.Fatal("canceled caller admitted")
			}
			current, _ := m.Snapshot(p.ID)
			if current.CancelRequested || before != policyLog(t, log) {
				t.Fatal("invalid request mutated worker")
			}
			if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
				t.Fatal(err)
			}
			r, _ := m.runtime(p.ID, s.InstanceID)
			r.cancelTasks.Wait()
			pending, _ := m.Snapshot(p.ID)
			if pending.Status != status || !pending.CancelRequested || (pending.Permission == nil) != (s.Permission == nil) || (pending.Input == nil) != (s.Input == nil) {
				t.Fatalf("cancel discarded authoritative attention: %+v", pending)
			}
			cancelFixtureRelease(t, log, ".complete")
			runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
		})
	}
}

func TestRuntimeTurnCancelRejectsUnknownScope(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	r, _ := m.runtime(p.ID, s.InstanceID)
	original, _ := m.Snapshot(p.ID)
	before := policyLog(t, log)
	for _, mutate := range []func(){
		func() { r.busy = false }, func() { r.transitioning = true }, func() { r.snapshot.SessionID = "" }, func() { r.snapshot.CancelToken = "" },
		func() { r.snapshot.Status = "unknown" }, func() { r.snapshot.Status = "idle" }, func() { r.snapshot.Status = "opening" }, func() { r.snapshot.Status = "switching" }, func() { r.snapshot.Status = "closing" }, func() { r.snapshot.Status = "failed" },
	} {
		r.mu.Lock()
		mutate()
		r.mu.Unlock()
		if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, original.CancelToken); err == nil {
			t.Fatal("unknown/nonbusy scope accepted")
		}
		r.mu.Lock()
		if r.snapshot.CancelRequested {
			t.Fatal("invalid cancellation latched")
		}
		r.snapshot = original.clone()
		r.busy = true
		r.transitioning = false
		r.mu.Unlock()
	}
	if before != policyLog(t, log) {
		t.Fatal("invalid scope reached RPC")
	}
}

func TestRuntimeTurnCancelTransportFailureNoRetry(t *testing.T) {
	for _, mode := range []string{"cancel-abort-exit", "cancel-abort-reject", "cancel-prompt-exit"} {
		t.Run(mode, func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, mode)
			p := projects[0]
			s, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cancel-prompt-exit" {
				result := make(chan error, 1)
				go func() { result <- m.Prompt(t.Context(), p.ID, s.InstanceID, "cancel-exit") }()
				s = runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "running" })
				if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
					t.Fatal(err)
				}
				cancelFixtureRelease(t, log, ".ack")
				if err := <-result; !errors.Is(err, ErrRuntimeUnavailable) {
					t.Fatalf("EOF ack: %v", err)
				}
			} else {
				if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
					t.Fatal(err)
				}
				s, _ = m.Snapshot(p.ID)
				if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
					t.Fatal(err)
				}
			}
			runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "failed" })
			r, _ := m.runtime(p.ID, s.InstanceID)
			r.cancelTasks.Wait()
			before := policyLog(t, log)
			if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err == nil {
				t.Fatal("failed turn accepted retry")
			}
			if before != policyLog(t, log) {
				t.Fatal("failed mutation retried")
			}
			want := 2
			if mode == "cancel-prompt-exit" {
				want = 1
			}
			if strings.Count(before, "abort\n") != want {
				t.Fatalf("abort count: %q", before)
			}
		})
	}
}

func TestRuntimeTurnCancelShutdownWhileAdmissionBlocked(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "")
	p := projects[0]
	initial, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- m.Prompt(t.Context(), p.ID, initial.InstanceID, "cancel-delayed") }()
	s := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "running" })
	if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown deadlocked with cancellation")
	}
	if err := <-result; !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("shutdown ack: %v", err)
	}
	if strings.Count(policyLog(t, log), "abort\n") != 1 {
		t.Fatal("shutdown dispatched pending abort")
	}
}

func TestRuntimeTurnCancelCompletionWhileControlHeld(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Snapshot(p.ID)
	r, _ := m.runtime(p.ID, s.InstanceID)
	r.control.Lock()
	if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
		r.control.Unlock()
		t.Fatal(err)
	}
	response, err := r.worker.Client.Call(t.Context(), protocol.RPCRequest{Type: "fixture_complete"})
	if err != nil || !response.Success {
		r.control.Unlock()
		t.Fatalf("completion gate: %v", err)
	}
	runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	r.control.Unlock()
	r.cancelTasks.Wait()
	if strings.Count(policyLog(t, log), "abort\n") != 1 {
		t.Fatal("finished turn canceled after control release")
	}
}

func TestRuntimeTurnCancelRevalidatesSessionAndInstanceAfterControl(t *testing.T) {
	for _, rotate := range []string{"session", "instance", "token", "context"} {
		t.Run(rotate, func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, "")
			p := projects[0]
			s, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
				t.Fatal(err)
			}
			s, _ = m.Snapshot(p.ID)
			r, _ := m.runtime(p.ID, s.InstanceID)
			r.control.Lock()
			if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
				r.control.Unlock()
				t.Fatal(err)
			}
			r.mu.Lock()
			switch rotate {
			case "session":
				r.snapshot.SessionID = "other-session"
			case "instance":
				r.instanceID = "replacement"
				r.snapshot.InstanceID = r.instanceID
			case "token":
				r.snapshot.CancelToken = "next-turn"
				r.snapshot.CancelRequested = false
			case "context":
				r.cancel()
			}
			r.mu.Unlock()
			r.control.Unlock()
			r.cancelTasks.Wait()
			if strings.Count(policyLog(t, log), "abort\n") != 1 {
				t.Fatal("stale queued task dispatched abort")
			}
		})
	}
}

func TestRuntimeTurnCancelStopJoinsTaskWhileControlHeld(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Snapshot(p.ID)
	r, _ := m.runtime(p.ID, s.InstanceID)
	r.control.Lock()
	if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
		r.control.Unlock()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { r.stop(); r.control.Unlock(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop waited for cancellation that waited for stop's control lock")
	}
}

func TestRuntimeTurnCancelReadyRequiresCompletionAndControlRelease(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "cancel-complete-before-abort-ack")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Snapshot(p.ID)
	if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
		t.Fatal(err)
	}
	// Fixture emits definitive completion but withholds the abort acknowledgment.
	completed := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	if !completed.CancelRequested || completed.CancelToken != s.CancelToken {
		t.Fatalf("completion advertised ready before abort ack: %+v", completed)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "must-not-send"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("prompt during retirement: %v", err)
	}
	if strings.Count(policyLog(t, log), "prompt\n") != 1 {
		t.Fatal("premature prompt dispatched")
	}
	cancelFixtureRelease(t, log, ".abort-ack")
	ready := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && !s.CancelRequested })
	if ready.CancelToken != "" || ready.Revision <= completed.Revision {
		t.Fatalf("retirement did not publish fresh ready state: %+v", ready)
	}
	// No sleeps, retries, or private task joins: the public ready state itself must
	// guarantee the cancellation dispatcher has released the mutation gate.
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
		t.Fatalf("public ready still rejects prompt: %v", err)
	}
	next, _ := m.Snapshot(p.ID)
	if next.CancelToken == "" || next.CancelToken == s.CancelToken || next.CancelRequested {
		t.Fatalf("next turn: %+v", next)
	}
}

func TestRuntimeTurnCancelPendingLatchRejectsPromptAfterGateRelease(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := m.runtime(p.ID, s.InstanceID)
	// Model the tiny window after Unlock and before scoped retirement takes mu.
	r.mu.Lock()
	r.snapshot.CancelRequested = true
	r.snapshot.CancelToken = "retiring"
	r.cancelTaskToken = "retiring"
	r.mu.Unlock()
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "must-not-send"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("pending latch admitted prompt: %v", err)
	}
	r.retireTurnCancel(s.InstanceID, s.SessionID, "retiring")
	ready, _ := m.Snapshot(p.ID)
	if ready.CancelRequested || ready.CancelToken != "" {
		t.Fatalf("latch not retired: %+v", ready)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
}
