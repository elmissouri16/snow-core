package web

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func compactionTestManager(t *testing.T, mode string) (*RuntimeManager, Project, string, RuntimeSnapshot) {
	t.Helper()
	m, projects, log := runtimeTestManager(t, mode)
	m.env = slices.DeleteFunc(m.env, func(s string) bool { return strings.HasPrefix(s, "SNOW_WEB_RUNTIME_TEST_CHILD=") })
	m.env = append(m.env, "SNOW_WEB_COMPACTION_TEST_CHILD=1")
	p := projects[0]
	before, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return m, p, log, before
}
func compactionInput(s RuntimeSnapshot) RuntimeCompactionInput {
	return RuntimeCompactionInput{SessionID: s.SessionID, BranchID: s.Goal.BranchID, ExpectedTipID: s.Goal.TipID, ExpectedRevision: s.Revision}
}

func TestCompactionRuntimeTerminalOrderingAndRefresh(t *testing.T) {
	for _, order := range []string{"early-", "late-"} {
		for _, status := range []string{"completed", "noop", "fallback", "canceled", "failed"} {
			t.Run(order+status, func(t *testing.T) {
				m, p, log, before := compactionTestManager(t, order+status)
				receipt, err := m.StartCompaction(t.Context(), p.ID, before.InstanceID, compactionInput(before))
				if err != nil {
					t.Fatal(err)
				}
				after := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
				if receipt.CompactionACK == nil || after.CompactionACK != nil || after.Compaction.State != status || after.CancelToken != "" || len(after.Messages) != 1 || after.Messages[0].Text != "original user" || after.Telemetry.TotalTokens != 40 || after.Telemetry.ContextTokens != 40 {
					t.Fatalf("terminal not authoritatively refreshed: %+v", after)
				}
				if status == "failed" && after.Error == "" {
					t.Fatal("failed state lost")
				}
				if status == "fallback" && !after.Compaction.UsedFallback {
					t.Fatal("fallback distinction lost")
				}
				if strings.Count(policyLog(t, log), "compaction_start\n") != 1 {
					t.Fatal("replayed compaction")
				}
			})
		}
	}
}

func TestCompactionRuntimeStopBeforeACKAndDisconnectNoReplay(t *testing.T) {
	m, p, log, before := compactionTestManager(t, "gated")
	caller, disconnect := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := m.StartCompaction(caller, p.ID, before.InstanceID, compactionInput(before))
		result <- err
	}()
	pending := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Compaction != nil && s.Compaction.State == "pending" })
	cancelFixtureWaitLog(t, log, "compaction_start\n")
	if err := m.CancelTurn(t.Context(), p.ID, before.InstanceID, pending.CancelToken); err != nil {
		t.Fatal(err)
	}
	disconnect()
	if strings.Count(policyLog(t, log), "abort\n") != 1 {
		t.Fatal("Stop bypassed pending admission gate")
	}
	cancelFixtureRelease(t, log, ".ack")
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	cancelFixtureWaitLog(t, log, "abort\n")
	waiting := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool {
		return s.Compaction != nil && s.Compaction.ProgressDone && s.CancelRequested
	})
	if waiting.Status != "running" {
		t.Fatal("native progress claimed completion")
	}
	cancelFixtureRelease(t, log, ".abort")
	after := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && !s.CancelRequested })
	if after.Compaction.State != "canceled" || after.CancelToken != "" {
		t.Fatal("Stop did not own entire manual operation")
	}
	if strings.Count(policyLog(t, log), "compaction_start\n") != 1 {
		t.Fatal("HTTP ACK loss retried work")
	}
}

func TestCompactionRuntimeRefreshKeepsAdmissionReserved(t *testing.T) {
	m, p, log, before := compactionTestManager(t, "refresh-gated")
	result := make(chan error, 1)
	go func() {
		_, err := m.StartCompaction(t.Context(), p.ID, before.InstanceID, compactionInput(before))
		result <- err
	}()
	// Wait for the blocked authoritative refresh itself, not the transient
	// admission-log tail that the worker may already have advanced beyond.
	cancelFixtureWaitLog(t, log, "compaction_start\nmessages_page\n")
	current := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Compaction != nil && s.Compaction.ProgressDone })
	if current.Status != "running" {
		t.Fatal("idle before authoritative reads")
	}
	if err := m.Prompt(t.Context(), p.ID, before.InstanceID, "must not run"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("prompt during compaction: %v", err)
	}
	if _, err := m.StartCompaction(t.Context(), p.ID, before.InstanceID, compactionInput(before)); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("duplicate compaction: %v", err)
	}
	cancelFixtureRelease(t, log, ".refresh")
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
}

func TestCompactionRuntimeUnknownACKFailsClosed(t *testing.T) {
	m, p, log, before := compactionTestManager(t, "lost-rpc-ack")
	if _, err := m.StartCompaction(t.Context(), p.ID, before.InstanceID, compactionInput(before)); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("unknown ACK: %v", err)
	}
	after := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "failed" })
	if after.Compaction.State != "uncertain" {
		t.Fatal("unknown ACK claimed terminal outcome")
	}
	for range 3 {
		m.Snapshot(p.ID)
	}
	if strings.Count(policyLog(t, log), "compaction_start\n") != 1 {
		t.Fatal("unknown RPC ACK replayed work")
	}
}
