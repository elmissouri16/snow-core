package web

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type memoryRecoveryStore struct {
	mu          sync.Mutex
	hints       map[string]RecoveryHint
	failIntent  bool
	failOutcome bool
}

func (s *memoryRecoveryStore) LoadRecovery(_ context.Context, id string) (RecoveryHint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hint, found := s.hints[id]
	return hint, found, nil
}
func (s *memoryRecoveryStore) SaveRecovery(_ context.Context, id string, hint RecoveryHint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (s.failIntent && hint.State == RecoveryAdmissionUnknown) || (s.failOutcome && hint.State != RecoveryAdmissionUnknown && hint.State != RecoveryBound) {
		return ErrRegistryStorage
	}
	if s.hints == nil {
		s.hints = make(map[string]RecoveryHint)
	}
	s.hints[id] = hint
	return nil
}

func TestRecoveryIntentFailureNeverDispatches(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "")
	store := &memoryRecoveryStore{failIntent: true}
	m.recovery = store
	snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), projects[0].ID, snapshot.InstanceID, "private unsent prompt"); !errors.Is(err, ErrRecoveryStorage) {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "prompt") {
		t.Fatalf("dispatched after failed intent: %q", data)
	}
	after, _ := m.Snapshot(projects[0].ID)
	if after.Status != "idle" || len(after.Messages) != len(snapshot.Messages) || after.Recovery.State != RecoveryBound {
		t.Fatalf("failed intent changed live state: %+v", after)
	}
}

func TestRecoveryBeforeAckAndAdmittedEOF(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		state  RecoveryState
	}{{"exit", RecoveryAdmissionUnknown}, {"ack-exit", RecoveryAdmitted}} {
		t.Run(tc.prompt, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, "")
			m.recovery = &memoryRecoveryStore{}
			snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			err = m.Prompt(t.Context(), projects[0].ID, snapshot.InstanceID, tc.prompt)
			if tc.prompt == "exit" && !errors.Is(err, ErrRuntimeUnavailable) {
				t.Fatalf("before-ack error: %v", err)
			}
			if tc.prompt == "ack-exit" {
				if err != nil {
					t.Fatal(err)
				}
				releaseRecoveryEOF(t, m, projects[0].ID, snapshot.InstanceID)
			}
			failed := runtimeWait(t, m, projects[0].ID, func(s RuntimeSnapshot) bool { return s.Status == "failed" })
			if failed.Recovery.State != tc.state || failed.Permission != nil || failed.Input != nil {
				t.Fatalf("failure evidence: %+v", failed)
			}
			for _, activity := range failed.Activities {
				if activity.Status == "canceled" || activity.Status == "running" {
					t.Fatalf("unobserved cancellation: %+v", activity)
				}
			}
			hint, found, err := m.Recovery(t.Context(), projects[0].ID)
			if err != nil || !found || hint.State != tc.state {
				t.Fatalf("stored evidence: %+v %v %v", hint, found, err)
			}
		})
	}
}

func TestRecoveryCompletionBeforeAckPreservesEvidence(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		state  RecoveryState
	}{{"hello", RecoveryCompleted}, {"fail", RecoveryFailed}} {
		t.Run(tc.prompt, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, "")
			m.recovery = &memoryRecoveryStore{}
			snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Prompt(t.Context(), projects[0].ID, snapshot.InstanceID, tc.prompt); err != nil {
				t.Fatal(err)
			}
			done := runtimeWait(t, m, projects[0].ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
			if done.Recovery.State != tc.state {
				t.Fatalf("completion downgraded: %+v", done.Recovery)
			}
			r, err := m.runtime(projects[0].ID, snapshot.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			r.fail()
			failed, _ := m.Snapshot(projects[0].ID)
			if failed.Recovery.State != tc.state {
				t.Fatal("EOF lost completion evidence")
			}
			hint, found, err := m.Recovery(t.Context(), projects[0].ID)
			if err != nil || !found || hint.State != tc.state {
				t.Fatalf("stored completion: %+v %v %v", hint, found, err)
			}
		})
	}
}

func TestRecoveryLateAcknowledgmentsCannotReviveTerminalRuntime(t *testing.T) {
	for _, status := range []string{"failed", "closing", "canceled-context", "early-completion"} {
		for _, success := range []bool{false, true} {
			t.Run(status+map[bool]string{false: "-negative", true: "-positive"}[success], func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				r := &liveRuntime{ctx: ctx, cancel: cancel, busy: true, snapshot: RuntimeSnapshot{Status: status, Recovery: RecoveryHint{SessionID: "saved", State: RecoveryAdmissionUnknown, UpdatedAt: time.Now()}}}
				if status == "canceled-context" {
					r.snapshot.Status = "running"
					cancel()
				}
				if status == "early-completion" {
					r.snapshot.Status = "idle"
					r.busy = false
					r.earlyCompletion = "7"
					r.completeRecoveryLocked(protocol.RPCPromptCompletedStatus)
				}
				before := r.snapshot.Recovery
				err := r.promptAcknowledged(protocol.RPCResponse{ID: "7", Success: success}, nil, false)
				if status != "early-completion" || !success {
					if !errors.Is(err, ErrRuntimeUnavailable) {
						t.Fatal(err)
					}
				}
				if success && status != "early-completion" {
					if r.snapshot.Recovery.State != RecoveryAdmitted {
						t.Fatalf("received admission evidence discarded: %+v", r.snapshot.Recovery)
					}
				} else if r.snapshot.Recovery != before {
					t.Fatalf("late ack overwrote evidence: %+v", r.snapshot.Recovery)
				}
				if status != "early-completion" && r.snapshot.Status == "idle" {
					t.Fatal("late ack revived runtime")
				}
			})
		}
	}
}

func TestRecoveryFailedWorkerCloseReopenIsIsolated(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "")
	m.recovery = &memoryRecoveryStore{}
	first, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Open(t.Context(), projects[1], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), projects[1].ID, second.InstanceID, "permission"); err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), projects[0].ID, first.InstanceID, "ack-exit"); err != nil {
		t.Fatal(err)
	}
	releaseRecoveryEOF(t, m, projects[0].ID, first.InstanceID)
	runtimeWait(t, m, projects[0].ID, func(s RuntimeSnapshot) bool { return s.Status == "failed" })
	before, _ := os.ReadFile(log)
	for range 3 {
		_, _ = m.Snapshot(projects[0].ID)
		_, _, _ = m.Recovery(t.Context(), projects[0].ID)
	}
	after, _ := os.ReadFile(log)
	if string(before) != string(after) {
		t.Fatal("read dispatched work")
	}
	if _, err := m.Open(t.Context(), projects[0], first.SessionID, "", ""); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("failed runtime reopened implicitly: %v", err)
	}
	if err := m.CloseProject(t.Context(), projects[0].ID, first.InstanceID); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Snapshot(projects[0].ID); ok {
		t.Fatal("closed runtime retained")
	}
	fresh, err := m.Open(t.Context(), projects[0], first.SessionID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.InstanceID == first.InstanceID || fresh.Permission != nil || fresh.Input != nil || fresh.Recovery.State != RecoveryAdmitted {
		t.Fatalf("reopen reused authority or lost evidence: %+v", fresh)
	}
	if err := m.ReplyPermission(t.Context(), projects[0].ID, first.InstanceID, "old-permission", protocol.PermissionAllow); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("stale permission accepted: %v", err)
	}
	other, _ := m.Snapshot(projects[1].ID)
	if other.InstanceID != second.InstanceID || other.Status != "permission" || other.Permission == nil {
		t.Fatalf("unrelated worker changed: %+v", other)
	}
}

func TestRecoveryOutcomeWriteFailureRetainsIntent(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "")
	m.recovery = &memoryRecoveryStore{failOutcome: true}
	snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), projects[0].ID, snapshot.InstanceID, "hello"); err != nil {
		t.Fatal(err)
	}
	runtimeWait(t, m, projects[0].ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	hint, found, err := m.Recovery(t.Context(), projects[0].ID)
	if err != nil || !found || hint.State != RecoveryAdmissionUnknown {
		t.Fatalf("intent missing: %+v %v %v", hint, found, err)
	}
}

func TestRecoveryDefinitiveCancellationEvidence(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "")
	m.recovery = &memoryRecoveryStore{}
	snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), projects[0].ID, snapshot.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	if err := m.Abort(t.Context(), projects[0].ID, snapshot.InstanceID); err != nil {
		t.Fatal(err)
	}
	done := runtimeWait(t, m, projects[0].ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	if done.Recovery.State != RecoveryCanceled {
		t.Fatalf("missing definitive cancellation: %+v", done.Recovery)
	}
	r, err := m.runtime(projects[0].ID, snapshot.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	r.fail()
	hint, found, err := m.Recovery(t.Context(), projects[0].ID)
	if err != nil || !found || hint.State != RecoveryCanceled {
		t.Fatalf("cancellation evidence lost: %+v %v %v", hint, found, err)
	}
}

// releaseRecoveryEOF lets the fixture exit only after the test has actually
// received its admission response. No sleep or process scheduling assumption.
func releaseRecoveryEOF(t *testing.T, m *RuntimeManager, projectID, instanceID string) {
	t.Helper()
	r, err := m.runtime(projectID, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := r.worker.Client.Call(ctx, protocol.RPCRequest{Type: "fixture_recovery_exit"}); err == nil {
		t.Fatal("fixture did not terminate")
	}
	runtimeWait(t, m, projectID, func(s RuntimeSnapshot) bool { return s.Status == "failed" })
}

func TestPromptAcknowledgmentReceivedBeforeEOFProcessedAfterEOF(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "")
	m.recovery = &memoryRecoveryStore{}
	snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.runtime(projects[0].ID, snapshot.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	r.control.Lock()
	defer r.control.Unlock()
	if err := r.savePromptIntent(); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.busy = true
	r.snapshot.Status = "running"
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	response, err := r.worker.Client.Call(ctx, protocol.RPCRequest{Type: "prompt", Message: "ack-exit"})
	if err != nil || !response.Success {
		t.Fatalf("admission response: %+v %v", response, err)
	}
	// Exact interleaving: response received, event consumer observes actual EOF,
	// then the pending Prompt caller projects its already-correlated response.
	releaseRecoveryEOF(t, m, projects[0].ID, snapshot.InstanceID)
	before, _ := m.Snapshot(projects[0].ID)
	if before.Recovery.State != RecoveryAdmissionUnknown {
		t.Fatalf("unexpected pre-ack evidence: %+v", before.Recovery)
	}
	if err := r.promptAcknowledged(response, snapshot.Messages, snapshot.HistoryTruncated); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatal(err)
	}
	after, _ := m.Snapshot(projects[0].ID)
	if after.Status != "failed" || after.Permission != nil || after.Input != nil || after.Recovery.State != RecoveryAdmitted {
		t.Fatalf("received admission discarded or runtime revived: %+v", after)
	}
	hint, found, err := m.Recovery(t.Context(), projects[0].ID)
	if err != nil || !found || hint.State != RecoveryAdmitted {
		t.Fatalf("received admission not persisted: %+v %v %v", hint, found, err)
	}
}

func TestPromptAcknowledgmentAfterFailurePreservesDefinitiveEvidence(t *testing.T) {
	for _, state := range []RecoveryState{RecoveryCompleted, RecoveryFailed, RecoveryCanceled} {
		for _, status := range []string{"failed", "closing", "canceled-context"} {
			for _, success := range []bool{false, true} {
				t.Run(string(state)+"/"+status+map[bool]string{false: "/negative", true: "/positive"}[success], func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					r := &liveRuntime{ctx: ctx, cancel: cancel, earlyCompletion: "7", snapshot: RuntimeSnapshot{Status: status, Error: "Worker connection failed. Close and explicitly reopen this project.", Recovery: RecoveryHint{SessionID: "saved", State: state, UpdatedAt: time.Now().UTC()}}}
					if status == "canceled-context" {
						r.snapshot.Status = "idle"
						cancel()
					}
					before := r.snapshot
					if err := r.promptAcknowledged(protocol.RPCResponse{ID: "7", Success: success}, nil, false); !errors.Is(err, ErrRuntimeUnavailable) {
						t.Fatal(err)
					}
					if r.snapshot.Recovery != before.Recovery || r.snapshot.Status != before.Status || r.snapshot.Error != before.Error {
						t.Fatalf("late ack changed terminal state: %+v", r.snapshot)
					}
				})
			}
		}
	}
}

func TestPromptAcknowledgmentMismatchedAfterEOFKeepsEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := &liveRuntime{ctx: ctx, cancel: cancel, promptID: "7", snapshot: RuntimeSnapshot{Status: "failed", Recovery: RecoveryHint{SessionID: "saved", State: RecoveryAdmissionUnknown, UpdatedAt: time.Now().UTC()}}}
	before := r.snapshot.Recovery
	if err := r.promptAcknowledged(protocol.RPCResponse{ID: "8", Success: true}, nil, false); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatal(err)
	}
	if r.snapshot.Recovery != before || r.snapshot.Status != "failed" {
		t.Fatalf("uncorrelated ack changed evidence: %+v", r.snapshot)
	}
}
