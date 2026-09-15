//go:build darwin || linux

package web

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"
)

func TestOperationStorePersistBoundariesNeverReplay(t *testing.T) {
	for _, stage := range []string{"admitted", "creating", "running", "awaiting_registration"} {
		t.Run(stage, func(t *testing.T) {
			m, b, parent := newOperationTestManager(t)
			req := selectOperation(t, m, parent, "faulted", "clone")
			trigger := `CREATE TRIGGER fault_operation BEFORE UPDATE ON manager_operations WHEN NEW.state='` + stage + `' BEGIN SELECT RAISE(ABORT,'injected'); END;`
			if stage == "admitted" {
				trigger = `CREATE TRIGGER fault_operation BEFORE INSERT ON manager_operations BEGIN SELECT RAISE(ABORT,'injected'); END;`
			}
			if _, err := m.registry.db.ExecContext(t.Context(), trigger); err != nil {
				t.Fatal(err)
			}
			op, err := m.Admit(t.Context(), "browser-one", req)
			if stage == "admitted" {
				if err == nil || b.opened.Load() != 0 {
					t.Fatalf("admission failure dispatched %+v %v", op, err)
				}
				if _, err := m.registry.db.ExecContext(t.Context(), `DROP TRIGGER fault_operation`); err != nil {
					t.Fatal(err)
				}
				if _, err := m.Admit(t.Context(), "browser-one", req); !errors.Is(err, ErrOperationConflict) {
					t.Fatalf("failed grant replay: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if stage == "awaiting_registration" {
				awaitOperation(t, m, op.ID, "running")
				b.terminal <- ProjectOperationTerminal{"succeeded", "observed"}
			}
			settled := awaitOperation(t, m, op.ID, "needs_review")
			if settled.ProjectID != "" || b.closed.Load() != 1 {
				t.Fatalf("unsafe terminal %+v", settled)
			}
			if stage == "creating" && b.prepared.Load() != 0 {
				t.Fatal("prepare before creating commit")
			}
			if stage == "running" && b.started.Load() != 0 {
				t.Fatal("clone before child identity commit")
			}
			if stage == "awaiting_registration" {
				projects, err := m.registry.List(t.Context())
				if err != nil || len(projects) != 0 {
					t.Fatalf("registered before outcome commit %+v %v", projects, err)
				}
			}
			_, err = m.Admit(t.Context(), "browser-one", req)
			if err != nil || b.opened.Load() != 1 {
				t.Fatalf("durable duplicate replayed %v", err)
			}
		})
	}
}
func TestOperationStoreRegistrationAtomicity(t *testing.T) {
	m, b, parent := newOperationTestManager(t)
	if _, err := m.registry.db.ExecContext(t.Context(), `CREATE TRIGGER fault_completion BEFORE UPDATE ON manager_operations WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'injected'); END;`); err != nil {
		t.Fatal(err)
	}
	req := selectOperation(t, m, parent, "retained", "create")
	op, err := m.Admit(t.Context(), "browser-one", req)
	if err != nil {
		t.Fatal(err)
	}
	awaitOperation(t, m, op.ID, "running")
	b.terminal <- ProjectOperationTerminal{"succeeded", "observed"}
	retained := awaitOperation(t, m, op.ID, "awaiting_registration")
	assertOperationRegistryEmpty(t, m.registry)
	if _, err := m.Register(t.Context(), retained.ID, retained.Revision, false); err == nil {
		t.Fatal("injected completion failure unexpectedly registered")
	}
	retained, err = m.Get(t.Context(), op.ID)
	if err != nil || retained.State != "awaiting_registration" || retained.Error != "registration_failed" {
		t.Fatalf("lost retryable registration %+v %v", retained, err)
	}
	projects, err := m.registry.List(t.Context())
	if err != nil || len(projects) != 0 {
		t.Fatalf("partial atomic insert %+v %v", projects, err)
	}
	if _, err := os.Stat(filepath.Join(parent, "retained")); err != nil {
		t.Fatal("registration failure deleted destination", err)
	}
	if _, err := m.registry.db.ExecContext(t.Context(), `DROP TRIGGER fault_completion`); err != nil {
		t.Fatal(err)
	}
	registered, err := m.Register(t.Context(), op.ID, retained.Revision, false)
	if err != nil || registered.State != "succeeded" || registered.ProjectID == "" {
		t.Fatalf("explicit register %+v %v", registered, err)
	}
	if b.opened.Load() != 1 || b.started.Load() != 1 {
		t.Fatal("registration reexecuted work")
	}
}
func TestOperationStoreCapacityAndPublicPageBounds(t *testing.T) {
	m, b, parent := newOperationTestManager(t)
	identity, err := openDirectoryIdentity(parent)
	if err != nil {
		t.Fatal(err)
	}
	// Large logical host paths stress byte-bounded projection without file reads.
	identity.Path = "/" + strings.Repeat("p", 3900)
	now := time.Now().UnixMilli()
	for i := range 128 {
		op := ProjectOperation{ID: uuid.New().String(), Kind: "create", Name: "retained", State: "interrupted", Revision: 1, CreatedAt: now + int64(i), UpdatedAt: now + int64(i), Parent: identity, Outcome: "unknown", Fingerprint: strings.Repeat("a", 64)}
		if err := m.registry.operationInsert(t.Context(), op); err != nil {
			t.Fatal(err)
		}
	}
	page, err := m.List(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Operations) == 0 || len(page.Operations) > 32 || !page.HasMore || page.NextOffset != len(page.Operations) {
		t.Fatalf("page %+v", page)
	}
	total := 0
	for _, op := range page.Operations {
		total += operationPublicSize(op)
		if op.Fingerprint != "" {
			t.Fatal("private fingerprint projected")
		}
	}
	if total > 64<<10 {
		t.Fatalf("oversized inventory %d", total)
	}
	req := selectOperation(t, m, parent, "not-created", "create")
	if _, err := m.Admit(t.Context(), "browser-one", req); !errors.Is(err, ErrOperationLimit) {
		t.Fatalf("retained limit %v", err)
	}
	if b.opened.Load() != 0 {
		t.Fatal("capacity rejection dispatched worker")
	}
	if err := m.Dismiss(t.Context(), page.Operations[0].ID, page.Operations[0].Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Admit(t.Context(), "browser-one", req); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("capacity-consumed grant reused %v", err)
	}
}
