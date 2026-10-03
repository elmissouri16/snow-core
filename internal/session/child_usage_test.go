package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSQLiteSessionValidationHonorsCanceledContext(t *testing.T) {
	cwd := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(cwd, "child.db"), cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := validateSQLiteSessionDB(store.db); err != nil {
		t.Fatalf("context-free validation changed: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := validateSQLiteSessionDBContext(ctx, store.db); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled validation error=%v", err)
	}
	if _, err := InspectChildUsage(ctx, store.Path(), store.ID()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled child inspection error=%v", err)
	}
}
