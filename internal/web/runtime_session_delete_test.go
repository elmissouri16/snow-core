package web

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRuntimeSessionDeleteOnlySessionRPCAndIdleGates(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "workflow")
	p := projects[0]
	snapshot, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !m.SessionDeleteSupported(p.ID, snapshot.InstanceID) {
		t.Fatal("missing capability")
	}
	r, err := m.runtime(p.ID, snapshot.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() error{
		"active":  func() error { return m.DeleteSession(t.Context(), p.ID, snapshot.InstanceID, snapshot.SessionID) },
		"stale":   func() error { return m.DeleteSession(t.Context(), p.ID, "stale", "saved") },
		"foreign": func() error { return m.DeleteSession(t.Context(), p.ID, snapshot.InstanceID, "foreign") },
		"invalid": func() error { return m.DeleteSession(t.Context(), p.ID, snapshot.InstanceID, "../saved") },
	} {
		t.Run(name, func(t *testing.T) {
			if run() == nil {
				t.Fatal("invalid deletion accepted")
			}
		})
	}
	r.control.Lock()
	err = m.DeleteSession(t.Context(), p.ID, snapshot.InstanceID, "saved")
	r.control.Unlock()
	if !errors.Is(err, ErrRuntimeBusy) {
		t.Fatal(err)
	}
	before, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteSession(t.Context(), p.ID, snapshot.InstanceID, "saved"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(after[len(before):])); got != "sessions_list\nsession_delete" {
		t.Fatalf("unexpected calls %q", got)
	}
	current, ok := m.Snapshot(p.ID)
	if !ok || current.InstanceID != snapshot.InstanceID || current.SessionID != snapshot.SessionID {
		t.Fatal("delete switched owner")
	}
	if err = m.Prompt(t.Context(), p.ID, snapshot.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteSession(t.Context(), p.ID, snapshot.InstanceID, "saved"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatal(err)
	}
}
func TestRuntimeSessionDeleteUncertainResponseNeverRetries(t *testing.T) {
	for _, mode := range []string{"workflow-delete-error", "workflow-delete-wrong"} {
		t.Run(mode, func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, mode)
			snap, err := m.Open(t.Context(), projects[0], "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			err = m.DeleteSession(t.Context(), projects[0].ID, snap.InstanceID, "saved")
			if !errors.Is(err, ErrSessionDeleteUncertain) {
				t.Fatal(err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(data), "session_delete") != 1 {
				t.Fatal(string(data))
			}
		})
	}
}
