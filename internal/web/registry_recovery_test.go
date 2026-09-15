//go:build darwin || linux

package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistryRecoveryReopenBoundedPrivateAndInert(t *testing.T) {
	base := t.TempDir()
	managerDir := filepath.Join(base, "manager")
	registry := registryTestOpen(t, managerDir)
	project, err := registry.Add(t.Context(), "project", registryTestDirectory(t, base, "project"))
	if err != nil {
		t.Fatal(err)
	}
	for range 120 {
		if err := registry.SaveRecovery(t.Context(), project.ID, RecoveryHint{SessionID: "session-only", State: RecoveryAdmissionUnknown, UpdatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := registry.db.QueryRow(`SELECT count(*) FROM recovery_hints`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unbounded hints: %d %v", count, err)
	}
	rows, err := registry.db.Query(`PRAGMA table_info(recovery_hints)`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var index, notnull, pk int
		var name, kind string
		var def any
		if err := rows.Scan(&index, &name, &kind, &notnull, &def, &pk); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "project_id,session_id,state,updated_at" {
		t.Fatalf("unexpected durable payload: %v", names)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	registry = registryTestOpen(t, managerDir)
	hint, found, err := registry.LoadRecovery(t.Context(), project.ID)
	if err != nil || !found || hint.SessionID != "session-only" || hint.State != RecoveryAdmissionUnknown {
		t.Fatalf("reopened evidence: %+v %v %v", hint, found, err)
	}
	info, err := os.Stat(filepath.Join(managerDir, "manager.db"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("db permissions: %v %v", info, err)
	}
	info, err = os.Stat(managerDir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("directory permissions: %v %v", info, err)
	}
	manager := NewRuntimeManager(t.Context(), "/does-not-exist", "", registry)
	defer manager.Close()
	for range 5 {
		if _, ok := manager.Snapshot(project.ID); ok {
			t.Fatal("cold manager activated worker")
		}
		read, ok, err := manager.Recovery(t.Context(), project.ID)
		if err != nil || !ok || read != hint {
			t.Fatalf("cold read: %+v %v %v", read, ok, err)
		}
	}
	manager.mu.Lock()
	workers := len(manager.workers)
	manager.mu.Unlock()
	if workers != 0 {
		t.Fatal("recovery read started worker")
	}
	if err := registry.Remove(t.Context(), project.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := registry.LoadRecovery(t.Context(), project.ID); err != nil || found {
		t.Fatalf("removed recovery survived: %v %v", found, err)
	}
	if err := registry.db.QueryRow(`SELECT count(*) FROM recovery_hints`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("removed row retained: %d %v", count, err)
	}
	if err := registry.SaveRecovery(t.Context(), project.ID, hint); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("removed hint resurrected: %v", err)
	}
}

func TestRegistryRecoveryValidationAndStorageGates(t *testing.T) {
	base := t.TempDir()
	registry := registryTestOpen(t, filepath.Join(base, "manager"))
	project, err := registry.Add(t.Context(), "project", registryTestDirectory(t, base, "project"))
	if err != nil {
		t.Fatal(err)
	}
	valid := RecoveryHint{SessionID: "saved-session", State: RecoveryAdmitted, UpdatedAt: time.Now().UTC()}
	for _, hint := range []RecoveryHint{{SessionID: "../secret", State: RecoveryAdmitted, UpdatedAt: time.Now()}, {SessionID: "saved", State: RecoveryState("SECRET-WORKER-ERROR"), UpdatedAt: time.Now()}, {SessionID: strings.Repeat("x", 129), State: RecoveryAdmitted, UpdatedAt: time.Now()}, {SessionID: "saved", State: RecoveryAdmitted}} {
		if err := registry.SaveRecovery(t.Context(), project.ID, hint); !errors.Is(err, ErrRegistryStorage) {
			t.Fatalf("invalid hint accepted: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := registry.SaveRecovery(ctx, project.ID, valid); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err := registry.LoadRecovery(ctx, project.ID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	registry.gate <- struct{}{}
	bounded, stop := context.WithTimeout(t.Context(), 10*time.Millisecond)
	if err := registry.SaveRecovery(bounded, project.ID, valid); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	stop()
	<-registry.gate
	if err := os.Chmod(filepath.Join(base, "manager", "manager.db"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := registry.SaveRecovery(t.Context(), project.ID, valid); !errors.Is(err, ErrRegistryUnsafe) {
		t.Fatalf("unsafe storage accepted: %v", err)
	}
	if _, _, err := registry.LoadRecovery(t.Context(), project.ID); !errors.Is(err, ErrRegistryUnsafe) {
		t.Fatalf("unsafe storage read: %v", err)
	}
}
