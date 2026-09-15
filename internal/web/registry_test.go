//go:build darwin || linux

package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"uuid"
)

func registryTestOpen(t *testing.T, path string) *Registry {
	t.Helper()
	r, err := OpenRegistry(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func registryTestDirectory(t *testing.T, parent, name string) string {
	t.Helper()
	path := filepath.Join(parent, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRegistryPersistsAndLocks(t *testing.T) {
	base := t.TempDir()
	manager := filepath.Join(base, "global", "manager")
	project := registryTestDirectory(t, base, "my project")
	r := registryTestOpen(t, manager)
	list, err := r.List(t.Context())
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("empty list: %+v, %v", list, err)
	}
	p, err := r.Add(t.Context(), "", project)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(p.ID); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "my project" || p.Path != canonical || !p.Available || p.State != "available" || p.Issue != "" {
		t.Fatalf("project: %+v", p)
	}
	if second, err := OpenRegistry(t.Context(), manager); !errors.Is(err, ErrRegistryBusy) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second instance: %v", err)
	}
	for path, want := range map[string]os.FileMode{manager: 0o700, filepath.Join(manager, "manager.db"): 0o600, filepath.Join(manager, "manager.lock"): 0o600} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("storage mode: %v, %v", info, err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = registryTestOpen(t, manager)
	got, err := r.Lookup(t.Context(), p.ID)
	if err != nil || got != p {
		t.Fatalf("reopen: %+v, %v", got, err)
	}
	list, err = r.List(t.Context())
	if err != nil || len(list) != 1 || list[0] != p {
		t.Fatalf("list: %+v, %v", list, err)
	}
}

func TestRegistryDuplicateCanonicalPaths(t *testing.T) {
	base := t.TempDir()
	r := registryTestOpen(t, filepath.Join(base, "manager"))
	project := registryTestDirectory(t, base, "project")
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(project, alias); err != nil {
		t.Fatal(err)
	}
	first, err := r.Add(t.Context(), "same display name", alias)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{project, alias, filepath.Join(project, "."), project + "/../project"} {
		if _, err := r.Add(t.Context(), "another name", path); !errors.Is(err, ErrProjectDuplicate) {
			t.Fatalf("duplicate accepted: %v", err)
		}
	}
	other := registryTestDirectory(t, base, "other")
	second, err := r.Add(t.Context(), first.Name, other)
	if err != nil || second.Name != first.Name || second.ID == first.ID {
		t.Fatalf("duplicate name rejected: %+v, %v", second, err)
	}
}

func TestRegistryMissingReplacementAndSymlinkRetarget(t *testing.T) {
	for _, replacement := range []string{"missing", "directory", "file", "symlink", "ancestor-symlink"} {
		t.Run(replacement, func(t *testing.T) {
			base := t.TempDir()
			manager := filepath.Join(base, "manager")
			r := registryTestOpen(t, manager)
			parent := registryTestDirectory(t, base, "parent")
			project := registryTestDirectory(t, parent, "project")
			p, err := r.Add(t.Context(), "", project)
			if err != nil {
				t.Fatal(err)
			}
			if replacement == "ancestor-symlink" {
				moved := filepath.Join(base, "moved-parent")
				if err := os.Rename(parent, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, parent); err != nil {
					t.Fatal(err)
				}
			} else {
				// Keep the old inode alive, avoiding filesystem inode-reuse noise.
				if err := os.Rename(project, filepath.Join(parent, "old")); err != nil {
					t.Fatal(err)
				}
				switch replacement {
				case "directory":
					registryTestDirectory(t, parent, "project")
				case "file":
					if err := os.WriteFile(project, []byte("replacement"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink(filepath.Join(parent, "old"), project); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			r = registryTestOpen(t, manager)
			got, err := r.Lookup(t.Context(), p.ID)
			if err != nil || got.Available || got.Issue == "" || got.Path != p.Path {
				t.Fatalf("unavailable: %+v, %v", got, err)
			}
			want := "changed"
			if replacement == "missing" {
				want = "missing"
			}
			if got.State != want {
				t.Fatalf("state: %q, want %q", got.State, want)
			}
			list, err := r.List(t.Context())
			if err != nil || len(list) != 1 || list[0].Available {
				t.Fatalf("list: %+v, %v", list, err)
			}
		})
	}
}

func TestRegistryRemoveOnlyMetadataAndReadd(t *testing.T) {
	base := t.TempDir()
	r := registryTestOpen(t, filepath.Join(base, "manager"))
	project := registryTestDirectory(t, base, "project")
	files := []string{"keep.txt", "session.db"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(project, name), []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := r.Add(t.Context(), "", project)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Lookup(t.Context(), p.ID); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("lookup removed: %v", err)
	}
	if err := r.Remove(t.Context(), p.ID); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
	list, err := r.List(t.Context())
	if err != nil || len(list) != 0 {
		t.Fatalf("removed list: %+v, %v", list, err)
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(project, name))
		if err != nil || string(data) != "unchanged" {
			t.Fatalf("file changed: %q, %v", data, err)
		}
	}
	var removed int
	if err := r.db.QueryRow(`SELECT count(*) FROM projects WHERE id=? AND removed_at IS NOT NULL`, p.ID).Scan(&removed); err != nil || removed != 1 {
		t.Fatalf("tombstone: %d, %v", removed, err)
	}
	if err := os.Rename(project, filepath.Join(base, "old")); err != nil {
		t.Fatal(err)
	}
	registryTestDirectory(t, base, "project")
	next, err := r.Add(t.Context(), "", project)
	if err != nil || next.ID == p.ID || !next.Available || next.inode == p.inode {
		t.Fatalf("readd: %+v, %v", next, err)
	}
}

func TestRegistryBoundsAndNoProjectCreation(t *testing.T) {
	base := t.TempDir()
	r := registryTestOpen(t, filepath.Join(base, "manager"))
	project := registryTestDirectory(t, base, "project")
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(base, "missing")
	for _, input := range []struct{ name, path string }{
		{"", ""}, {"", missing}, {"", file}, {strings.Repeat("n", 129), project}, {strings.Repeat(" ", 129), project},
		{"bad\nname", project}, {"bad\x00name", project}, {"\xff", project}, {"", strings.Repeat("x", 4097)}, {"", "bad\x00path"},
	} {
		if _, err := r.Add(t.Context(), input.name, input.path); !errors.Is(err, ErrProjectInvalid) {
			t.Fatalf("invalid input accepted: %v", err)
		}
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created project: %v", err)
	}
	if _, err := r.Add(t.Context(), strings.Repeat("n", 128), project); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Lookup(t.Context(), "../../../secret"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("unknown ID: %v", err)
	}
}

func TestRegistryLimitAndConcurrentAdds(t *testing.T) {
	base := t.TempDir()
	r := registryTestOpen(t, filepath.Join(base, "manager"))
	var first Project
	for i := range MaxProjects - 1 {
		p, err := r.Add(t.Context(), "", registryTestDirectory(t, base, fmt.Sprintf("p-%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = p
		}
	}
	paths := []string{registryTestDirectory(t, base, "last-a"), registryTestDirectory(t, base, "last-b")}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Go(func() { _, err := r.Add(t.Context(), "", path); results <- err })
	}
	wg.Wait()
	close(results)
	var added, limited int
	for err := range results {
		switch {
		case err == nil:
			added++
		case errors.Is(err, ErrProjectLimit):
			limited++
		default:
			t.Fatal(err)
		}
	}
	if added != 1 || limited != 1 {
		t.Fatalf("admission: %d added, %d limited", added, limited)
	}
	if err := r.Remove(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(t.Context(), "", first.Path); err != nil {
		t.Fatal(err)
	}
	list, err := r.List(t.Context())
	if err != nil || len(list) != MaxProjects {
		t.Fatalf("active count: %d, %v", len(list), err)
	}
}

func TestRegistryRejectsUnsafeStorage(t *testing.T) {
	for _, kind := range []string{"directory-mode", "directory-symlink", "writable-parent", "database-symlink", "database-hardlink", "database-directory", "database-fifo", "database-mode", "lock-symlink", "journal-symlink", "wal-symlink", "shm-symlink"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			manager := registryTestDirectory(t, base, "manager")
			target := filepath.Join(base, "untouched")
			if err := os.WriteFile(target, []byte("private original"), 0o600); err != nil {
				t.Fatal(err)
			}
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "directory-mode":
				must(os.Chmod(manager, 0o755))
			case "directory-symlink":
				must(os.Rename(manager, manager+"-real"))
				must(os.Symlink(manager+"-real", manager))
			case "writable-parent":
				must(os.Chmod(base, 0o777))
				t.Cleanup(func() { _ = os.Chmod(base, 0o700) })
			case "database-symlink":
				must(os.Symlink(target, filepath.Join(manager, "manager.db")))
			case "database-hardlink":
				must(os.Link(target, filepath.Join(manager, "manager.db")))
			case "database-directory":
				must(os.Mkdir(filepath.Join(manager, "manager.db"), 0o700))
			case "database-fifo":
				must(syscall.Mkfifo(filepath.Join(manager, "manager.db"), 0o600))
			case "database-mode":
				must(os.WriteFile(filepath.Join(manager, "manager.db"), nil, 0o644))
			case "lock-symlink":
				must(os.Symlink(target, filepath.Join(manager, "manager.lock")))
			case "journal-symlink":
				must(os.Symlink(target, filepath.Join(manager, "manager.db-journal")))
			case "wal-symlink":
				must(os.Symlink(target, filepath.Join(manager, "manager.db-wal")))
			case "shm-symlink":
				must(os.Symlink(target, filepath.Join(manager, "manager.db-shm")))
			}
			r, err := OpenRegistry(t.Context(), manager)
			if r != nil {
				_ = r.Close()
			}
			if !errors.Is(err, ErrRegistryUnsafe) {
				t.Fatalf("unsafe storage accepted: %v", err)
			}
			if strings.Contains(err.Error(), base) {
				t.Fatal("host path leaked")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "private original" {
				t.Fatalf("target modified: %q, %v", data, err)
			}
		})
	}
}

func TestRegistryDetectsStorageReplacement(t *testing.T) {
	for _, kind := range []string{"database", "directory", "lock"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			manager := filepath.Join(base, "manager")
			r := registryTestOpen(t, manager)
			path := manager
			if kind == "database" {
				path = filepath.Join(manager, "manager.db")
			}
			if kind == "lock" {
				path = filepath.Join(manager, "manager.lock")
			}
			if err := os.Rename(path, path+"-old"); err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := r.List(t.Context()); !errors.Is(err, ErrRegistryUnsafe) {
				t.Fatalf("replacement accepted: %v", err)
			}
		})
	}
}

func TestRegistryCancellationCloseAndFailedStartup(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	manager := filepath.Join(base, "cancelled")
	if _, err := OpenRegistry(ctx, manager); !errors.Is(err, context.Canceled) {
		t.Fatalf("open cancellation: %v", err)
	}
	if _, err := os.Stat(manager); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled creation: %v", err)
	}
	r := registryTestOpen(t, filepath.Join(base, "manager"))
	project := registryTestDirectory(t, base, "project")
	if _, err := r.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("list cancellation: %v", err)
	}
	if _, err := r.Add(ctx, "", project); !errors.Is(err, context.Canceled) {
		t.Fatalf("add cancellation: %v", err)
	}
	if _, err := r.Lookup(ctx, uuid.New().String()); !errors.Is(err, context.Canceled) {
		t.Fatalf("lookup cancellation: %v", err)
	}
	if err := r.Remove(ctx, uuid.New().String()); !errors.Is(err, context.Canceled) {
		t.Fatalf("remove cancellation: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.List(t.Context()); !errors.Is(err, ErrRegistryClosed) {
		t.Fatalf("closed list: %v", err)
	}
	broken := registryTestDirectory(t, base, "broken")
	if err := os.WriteFile(filepath.Join(broken, "manager.db"), []byte("invalid private database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRegistry(t.Context(), broken); !errors.Is(err, ErrRegistryStorage) {
		t.Fatalf("corrupt database: %v", err)
	}
	if err := os.Remove(filepath.Join(broken, "manager.db")); err != nil {
		t.Fatal(err)
	}
	registryTestOpen(t, broken) // Failure must not retain the manager lock.
}

func TestRegistrySchemaOwnershipAndTransactionalStartup(t *testing.T) {
	for _, scenario := range []string{"foreign", "future", "interrupted"} {
		t.Run(scenario, func(t *testing.T) {
			manager := registryTestDirectory(t, t.TempDir(), "manager")
			path := filepath.Join(manager, "manager.db")
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "foreign":
				_, err = db.Exec(`CREATE TABLE private_data(value TEXT); INSERT INTO private_data VALUES('retained')`)
			case "future":
				_, err = db.Exec(`PRAGMA application_id=1397641047; PRAGMA user_version=2`)
			case "interrupted":
				_, err = db.Exec(`BEGIN; CREATE TABLE projects(id TEXT); PRAGMA application_id=1397641047; PRAGMA user_version=1; ROLLBACK`)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			r, err := OpenRegistry(t.Context(), manager)
			if scenario == "interrupted" {
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				var version, app int
				if err := r.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
					t.Fatal(err)
				}
				if err := r.db.QueryRow(`PRAGMA application_id`).Scan(&app); err != nil {
					t.Fatal(err)
				}
				if version != 1 || app != registryApplicationID {
					t.Fatalf("schema identity: %d, %d", version, app)
				}
			} else {
				if r != nil {
					_ = r.Close()
				}
				if !errors.Is(err, ErrRegistryStorage) {
					t.Fatalf("foreign schema accepted: %v", err)
				}
			}
		})
	}
}

func TestRegistryInvalidCanonicalUTF8AndRecoveredIdentity(t *testing.T) {
	base := t.TempDir()
	r := registryTestOpen(t, filepath.Join(base, "manager"))
	t.Run("invalid canonical UTF-8", func(t *testing.T) {
		invalid := filepath.Join(base, "invalid-\xff")
		if err := os.Mkdir(invalid, 0o700); err != nil {
			if errors.Is(err, syscall.EILSEQ) {
				t.Skip("filesystem rejects non-UTF-8 filenames")
			}
			t.Fatal(err)
		}
		alias := filepath.Join(base, "valid-alias")
		if err := os.Symlink(invalid, alias); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Add(t.Context(), "valid name", alias); !errors.Is(err, ErrProjectInvalid) {
			t.Fatalf("invalid canonical UTF-8: %v", err)
		}
	})
	path := registryTestDirectory(t, base, "project")
	p, err := r.Add(t.Context(), "", path)
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(base, "moved")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	missing, err := r.Lookup(t.Context(), p.ID)
	if err != nil || missing.Available {
		t.Fatalf("missing: %+v, %v", missing, err)
	}
	if err := os.Rename(moved, path); err != nil {
		t.Fatal(err)
	}
	recovered, err := r.Lookup(t.Context(), p.ID)
	if err != nil || recovered != p {
		t.Fatalf("restored original identity: %+v, %v", recovered, err)
	}
}
