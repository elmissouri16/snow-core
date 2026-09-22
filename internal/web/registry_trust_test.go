//go:build darwin || linux

package web

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"uuid"
)

func trustTestProject(t *testing.T) (*Registry, Project, string) {
	t.Helper()
	base := t.TempDir()
	manager := filepath.Join(base, "manager")
	r := registryTestOpen(t, manager)
	p, err := r.Add(t.Context(), "Project", registryTestDirectory(t, base, "project"))
	if err != nil {
		t.Fatal(err)
	}
	return r, p, manager
}

func assertProjectTrust(t *testing.T, r *Registry, id string, remembered, trusted bool) {
	t.Helper()
	p, err := r.Lookup(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.TrustRemembered != remembered || p.Trusted != trusted {
		t.Fatalf("lookup consent: remembered=%t trusted=%t", p.TrustRemembered, p.Trusted)
	}
	projects, err := r.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if p.ID == id {
			if p.TrustRemembered != remembered || p.Trusted != trusted {
				t.Fatalf("list consent: remembered=%t trusted=%t", p.TrustRemembered, p.Trusted)
			}
			return
		}
	}
	t.Fatal("project missing from list")
}

func TestRegistryTrustRevokedWhenRuntimeAuthorityExpands(t *testing.T) {
	r, p, manager := trustTestProject(t)
	if err := r.RememberProjectTrust(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.ExecContext(t.Context(), `DELETE FROM runtime_profile_consent`); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = registryTestOpen(t, manager)
	assertProjectTrust(t, r, p.ID, false, false)
	var version int
	if err := r.db.QueryRowContext(t.Context(), `SELECT version FROM runtime_profile_consent WHERE singleton=1`).Scan(&version); err != nil || version != runtimeProfileConsentVersion {
		t.Fatalf("runtime profile consent version = %d, err=%v", version, err)
	}
	// Simulate an older binary writing trust without per-row profile consent
	// while the database-level marker remains from a newer run.
	if _, err := r.db.ExecContext(t.Context(), `INSERT INTO project_trust(project_id,path,device,inode) VALUES(?,?,?,?)`, p.ID, p.Path, p.device, p.inode); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = registryTestOpen(t, manager)
	assertProjectTrust(t, r, p.ID, false, false)
}

func TestRegistryTrustPersistsExplicitly(t *testing.T) {
	r, p, manager := trustTestProject(t)
	if p.Trusted || p.TrustRemembered {
		t.Fatal("registration granted consent")
	}
	assertProjectTrust(t, r, p.ID, false, false)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = registryTestOpen(t, manager)
	assertProjectTrust(t, r, p.ID, false, false)
	for range 2 {
		if err := r.RememberProjectTrust(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	assertProjectTrust(t, r, p.ID, true, true)
	if err := r.RenameProject(t.Context(), p.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	assertProjectTrust(t, r, p.ID, true, true)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = registryTestOpen(t, manager)
	assertProjectTrust(t, r, p.ID, true, true)
	for range 2 {
		if err := r.RevokeProjectTrust(t.Context(), p.ID); err != nil {
			t.Fatal(err)
		}
	}
	assertProjectTrust(t, r, p.ID, false, false)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = registryTestOpen(t, manager)
	assertProjectTrust(t, r, p.ID, false, false)
	for _, name := range []string{"manager.db", "manager.lock"} {
		info, err := os.Stat(filepath.Join(manager, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatal("consent storage is not private")
		}
	}
}

func TestRegistryTrustMigrationHasNoDefault(t *testing.T) {
	r, p, manager := trustTestProject(t)
	// Model a prior manager database, preserving its registration identity.
	for _, statement := range []string{
		`DROP TRIGGER project_trust_active_insert`, `DROP TRIGGER project_trust_active_update`,
		`DROP TRIGGER project_trust_remove`, `DROP TRIGGER project_trust_delete`, `DROP TABLE project_trust`,
	} {
		if _, err := r.db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal("prepare old schema")
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = registryTestOpen(t, manager)
	assertProjectTrust(t, r, p.ID, false, false)
	if err := r.RememberProjectTrust(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	assertProjectTrust(t, r, p.ID, true, true)
}

func TestRegistryTrustRejectsChangedFilesystem(t *testing.T) {
	for _, replacement := range []string{"missing", "directory", "file", "symlink", "ancestor-symlink"} {
		t.Run(replacement, func(t *testing.T) {
			r, p, _ := trustTestProject(t)
			if err := r.RememberProjectTrust(t.Context(), p); err != nil {
				t.Fatal(err)
			}
			if replacement == "ancestor-symlink" {
				// Move the folder into a new parent, then alias the old parent. The
				// registry itself must remain at its pinned location.
				parent := registryTestDirectory(t, filepath.Dir(p.Path), "parent")
				if err := os.Rename(p.Path, filepath.Join(parent, "project")); err != nil {
					t.Fatal(err)
				}
				if err := r.Remove(t.Context(), p.ID); err != nil {
					t.Fatal(err)
				}
				var err error
				p, err = r.Add(t.Context(), "Project", filepath.Join(parent, "project"))
				if err != nil {
					t.Fatal(err)
				}
				if err := r.RememberProjectTrust(t.Context(), p); err != nil {
					t.Fatal(err)
				}
				moved := parent + "-old"
				if err := os.Rename(parent, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, parent); err != nil {
					t.Fatal(err)
				}
			} else {
				old := p.Path + "-old"
				if err := os.Rename(p.Path, old); err != nil {
					t.Fatal(err)
				}
				switch replacement {
				case "directory":
					if err := os.Mkdir(p.Path, 0o700); err != nil {
						t.Fatal(err)
					}
				case "file":
					if err := os.WriteFile(p.Path, []byte("not a directory"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink(old, p.Path); err != nil {
						t.Fatal(err)
					}
				}
			}
			assertProjectTrust(t, r, p.ID, true, false)
			if err := r.RememberProjectTrust(t.Context(), p); !errors.Is(err, ErrProjectInvalid) {
				t.Fatalf("stale identity: %v", err)
			}
			for range 2 {
				if err := r.RevokeProjectTrust(t.Context(), p.ID); err != nil {
					t.Fatal(err)
				}
			}
			assertProjectTrust(t, r, p.ID, false, false)
		})
	}
}

func TestRegistryTrustRejectsForgedExpected(t *testing.T) {
	r, p, _ := trustTestProject(t)
	for _, mutate := range []func(*Project){
		func(p *Project) { p.ID = uuid.New().String() },
		func(p *Project) { p.Path += "-other" },
		func(p *Project) { p.device += "0" },
		func(p *Project) { p.inode += "0" },
		func(p *Project) { p.Available = false },
		func(p *Project) { p.Archived = true },
		func(p *Project) { p.device = ""; p.inode = "" },
	} {
		forged := p
		mutate(&forged)
		if err := r.RememberProjectTrust(t.Context(), forged); !errors.Is(err, ErrProjectInvalid) && !errors.Is(err, ErrProjectNotFound) {
			t.Fatalf("forged consent accepted: %v", err)
		}
		assertProjectTrust(t, r, p.ID, false, false)
	}
}

func TestRegistryTrustArchiveRestoreAndReregisterReset(t *testing.T) {
	r, p, _ := trustTestProject(t)
	if err := r.RememberProjectTrust(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM project_trust`).Scan(&count); err != nil || count != 0 {
		t.Fatal("archive retained consent")
	}
	if err := r.RevokeProjectTrust(t.Context(), p.ID); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("archived revoke: %v", err)
	}
	restored, err := r.RestoreProject(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Trusted || restored.TrustRemembered {
		t.Fatal("restore granted consent")
	}
	assertProjectTrust(t, r, p.ID, false, false)
	if err := r.RememberProjectTrust(t.Context(), restored); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	next, err := r.Add(t.Context(), "New registration", p.Path)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == p.ID {
		t.Fatal("registration ID reused")
	}
	assertProjectTrust(t, r, next.ID, false, false)
	if err := r.RememberProjectTrust(t.Context(), p); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("stale registration: %v", err)
	}
}

func TestRegistryTrustStorageFailsClosed(t *testing.T) {
	for _, corruption := range []string{"missing-table", "identity", "orphan", "invalid-project", "unsafe-file", "closed", "canceled"} {
		t.Run(corruption, func(t *testing.T) {
			r, p, manager := trustTestProject(t)
			if err := r.RememberProjectTrust(t.Context(), p); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			want := ErrRegistryStorage
			switch corruption {
			case "missing-table":
				if _, err := r.db.ExecContext(ctx, `DROP TABLE project_trust`); err != nil {
					t.Fatal("drop trust table")
				}
			case "identity", "orphan":
				if _, err := r.db.ExecContext(ctx, `DROP TRIGGER project_trust_active_update`); err != nil {
					t.Fatal("prepare corrupt storage")
				}
				statement := `UPDATE project_trust SET inode='wrong'`
				if corruption == "orphan" {
					statement = `UPDATE project_trust SET project_id='` + uuid.New().String() + `'`
				}
				if _, err := r.db.ExecContext(ctx, statement); err != nil {
					t.Fatal("corrupt storage")
				}
			case "invalid-project":
				if _, err := r.db.ExecContext(ctx, `UPDATE projects SET name=char(10)`); err != nil {
					t.Fatal("corrupt project")
				}
			case "unsafe-file":
				want = ErrRegistryUnsafe
				if err := os.Chmod(filepath.Join(manager, "manager.db"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "closed":
				want = ErrRegistryClosed
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				want = context.Canceled
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			got, err := r.Lookup(ctx, p.ID)
			if !errors.Is(err, want) || got.Trusted || got.TrustRemembered {
				t.Fatalf("lookup failed open: %v", err)
			}
			projects, err := r.List(ctx)
			if !errors.Is(err, want) || len(projects) != 0 {
				t.Fatalf("list failed open: %v", err)
			}
			if err := r.RememberProjectTrust(ctx, p); !errors.Is(err, want) {
				t.Fatalf("remember failed open: %v", err)
			}
			if corruption == "identity" || corruption == "orphan" || corruption == "invalid-project" {
				return
			}
			if err := r.RevokeProjectTrust(ctx, p.ID); !errors.Is(err, want) {
				t.Fatalf("revoke failure: %v", err)
			}
		})
	}
}

func TestRegistryTrustTriggersAndBound(t *testing.T) {
	r, p, _ := trustTestProject(t)
	for _, identity := range []Project{{ID: uuid.New().String(), Path: p.Path, device: p.device, inode: p.inode}, {ID: p.ID, Path: p.Path, device: p.device, inode: "wrong"}} {
		if _, err := r.db.ExecContext(t.Context(), `INSERT INTO project_trust VALUES(?,?,?,?)`, identity.ID, identity.Path, identity.device, identity.inode); err == nil {
			t.Fatal("trigger accepted invalid identity")
		}
	}
	if err := r.RememberProjectTrust(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	for i := range MaxProjects - 1 {
		next, err := r.Add(t.Context(), "Project", registryTestDirectory(t, filepath.Dir(p.Path), fmt.Sprintf("p-%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		if err := r.RememberProjectTrust(t.Context(), next); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM project_trust`).Scan(&count); err != nil || count != MaxProjects {
		t.Fatal("incorrect trust bound")
	}
	if err := r.RememberProjectTrust(t.Context(), p); err != nil {
		t.Fatal("idempotent consent at capacity", err)
	}
	if _, err := r.db.ExecContext(t.Context(), `UPDATE projects SET inode='changed' WHERE id=?`, p.ID); err != nil {
		t.Fatal("change identity")
	}
	if err := r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM project_trust WHERE project_id=?`, p.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("identity rebind retained consent")
	}
	if err := r.RevokeProjectTrust(t.Context(), "invalid"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("invalid revoke: %v", err)
	}
	if err := r.RevokeProjectTrust(t.Context(), uuid.New().String()); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("unknown revoke: %v", err)
	}
}

func TestRegistryTrustDeletionIsTransactional(t *testing.T) {
	r, p, _ := trustTestProject(t)
	if err := r.RememberProjectTrust(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	tx, err := r.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), `UPDATE projects SET removed_at=1 WHERE id=?`, p.ID); err != nil {
		t.Fatal("archive project")
	}
	var count int
	if err := tx.QueryRowContext(t.Context(), `SELECT count(*) FROM project_trust WHERE project_id=?`, p.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("archive did not delete consent in transaction")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertProjectTrust(t, r, p.ID, true, true)
	if err := r.Remove(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.ExecContext(t.Context(), `INSERT INTO project_trust VALUES(?,?,?,?)`, p.ID, p.Path, p.device, p.inode); err == nil {
		t.Fatal("archived project accepted consent")
	}
	p, err = r.RestoreProject(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RememberProjectTrust(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.ExecContext(t.Context(), `DELETE FROM projects WHERE id=?`, p.ID); err != nil {
		t.Fatal("delete project")
	}
	if err := r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM project_trust`).Scan(&count); err != nil || count != 0 {
		t.Fatal("delete retained consent")
	}
}

func TestRegistryTrustRememberRevalidatesAfterGate(t *testing.T) {
	r, p, _ := trustTestProject(t)
	leave, err := r.enter(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- r.RememberProjectTrust(t.Context(), p) }()
	// Alter the registration while the waiting caller has only a stale snapshot.
	_, err = r.db.ExecContext(t.Context(), `UPDATE projects SET inode='changed' WHERE id=?`, p.ID)
	leave()
	if err != nil {
		t.Fatal("change registration")
	}
	if err := <-result; !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("stale snapshot accepted: %v", err)
	}
	assertProjectTrust(t, r, p.ID, false, false)
}
