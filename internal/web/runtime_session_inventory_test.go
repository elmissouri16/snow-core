package web

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRuntimeSessionInventoryOnlySessionsAndControlGates(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "workflow")
	p := projects[0]
	snapshot, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := m.SessionInventory(t.Context(), p.ID, snapshot.InstanceID)
	if err != nil || !inventory.Available || len(inventory.Sessions) != 2 || inventory.ProjectID != p.ID || inventory.InstanceID != snapshot.InstanceID {
		t.Fatalf("%+v %v", inventory, err)
	}
	after, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(after[len(before):])); got != "sessions_list" {
		t.Fatalf("inventory invoked non-session RPC: %q", got)
	}
	r, err := m.runtime(p.ID, snapshot.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	inventory.Sessions[0].Name = "mutated"
	if r.choices.Sessions[0].Name == "mutated" {
		t.Fatal("inventory aliases runtime")
	}
	r.control.Lock()
	_, err = m.SessionInventory(t.Context(), p.ID, snapshot.InstanceID)
	r.control.Unlock()
	if !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("control gate: %v", err)
	}
	if _, err = m.SessionInventory(t.Context(), p.ID, "stale"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("stale owner: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = m.SessionInventory(ctx, p.ID, snapshot.InstanceID); err == nil {
		t.Fatal("canceled read admitted")
	}
	if err = m.Prompt(t.Context(), p.ID, snapshot.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.SessionInventory(t.Context(), p.ID, snapshot.InstanceID); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("busy read: %v", err)
	}
}

func TestRuntimeSessionInventoryBoundedAndUnsupported(t *testing.T) {
	for _, mode := range []string{"workflow-large", ""} {
		t.Run(mode, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, mode)
			snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			inventory, err := m.SessionInventory(t.Context(), projects[0].ID, snapshot.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "workflow-large" && (!inventory.Available || !inventory.Truncated || len(inventory.Sessions) != 100) {
				t.Fatalf("%+v", inventory)
			}
			if mode == "" && (inventory.Available || len(inventory.Sessions) != 0) {
				t.Fatalf("%+v", inventory)
			}
		})
	}
}
