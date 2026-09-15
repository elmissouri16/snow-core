//go:build darwin || linux

package web

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrganizationPersistenceRestoreAndIdentityDecoration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "manager")
	r := registryTestOpen(t, dir)
	root := t.TempDir()
	sentinel := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("unchanged transcript sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := r.Add(t.Context(), "Original", root)
	if err != nil {
		t.Fatal(err)
	}
	page := CatalogSessions{Sessions: []SessionSummary{{ID: "durable-id", Name: "Original title"}}, HasMore: true, NextOffset: 25}
	if err := r.RenameProject(t.Context(), p.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := r.PinProject(t.Context(), p.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"pin", "archive"} {
		if err := r.saveSessionOrganization(t.Context(), p.ID, "durable-id", action, page); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Remove(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = registryTestOpen(t, dir)
	active, err := r.List(t.Context())
	if err != nil || len(active) != 0 {
		t.Fatalf("active %v %v", active, err)
	}
	archived, err := r.ListArchivedProjects(t.Context(), 0)
	if err != nil || len(archived.Projects) != 1 || archived.Projects[0].ID != p.ID || !archived.Projects[0].Pinned || !archived.Projects[0].Archived || archived.Projects[0].Name != "Renamed" {
		t.Fatalf("archive %+v %v", archived, err)
	}
	restored, err := r.RestoreProject(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != p.ID || restored.Path != p.Path || restored.device != p.device || restored.inode != p.inode {
		t.Fatal("restore discarded canonical identity")
	}
	decorated, err := r.DecorateProjects(t.Context(), []Project{restored})
	if err != nil || !decorated[0].Pinned || decorated[0].Archived || decorated[0].inode != p.inode {
		t.Fatalf("decoration %v %v", decorated, err)
	}
	sessions, err := r.DecorateSessions(t.Context(), p.ID, page)
	if err != nil || len(sessions) != 1 || sessions[0].SessionSummary != page.Sessions[0] || !sessions[0].Pinned || !sessions[0].Archived || !page.HasMore || page.NextOffset != 25 {
		t.Fatalf("sessions %v %v", sessions, err)
	}
	for _, action := range []string{"unpin", "restore"} {
		if err := r.saveSessionOrganization(t.Context(), p.ID, "durable-id", action, page); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := r.db.QueryRow(`SELECT count(*) FROM session_organization`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cleared flags retain bounded storage: %d %v", count, err)
	}
	contents, err := os.ReadFile(sentinel)
	if err != nil || string(contents) != "unchanged transcript sentinel" {
		t.Fatal("organization modified project files")
	}
}

func TestOrganizationRestoreRejectsStaleOrMaliciousIdentity(t *testing.T) {
	for _, kind := range []string{"missing", "replacement", "symlink", "malicious-stored-path"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			r := registryTestOpen(t, filepath.Join(t.TempDir(), "manager"))
			p, err := r.Add(t.Context(), "project", root)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Remove(t.Context(), p.ID); err != nil {
				t.Fatal(err)
			}
			if kind == "malicious-stored-path" {
				if _, err := r.db.Exec(`UPDATE projects SET path='../../outside' WHERE id=?`, p.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(root, root+"-original"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Remove(root); _ = os.Rename(root+"-original", root) })
				if kind == "replacement" {
					if err := os.Mkdir(root, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "symlink" {
					if err := os.Symlink(root+"-original", root); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := r.RestoreProject(t.Context(), p.ID); err == nil {
				t.Fatal("stale identity restored")
			}
			if active, err := r.List(t.Context()); err != nil || len(active) != 0 {
				t.Fatalf("failed restore activated registration: %v %v", active, err)
			}
		})
	}
}

func TestOrganizationRestoreUniquenessAndActiveCap(t *testing.T) {
	r := registryTestOpen(t, filepath.Join(t.TempDir(), "manager"))
	root := t.TempDir()
	p, err := r.Add(t.Context(), "first", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	newer, err := r.Add(t.Context(), "new ID", root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RestoreProject(t.Context(), p.ID); !errors.Is(err, ErrProjectDuplicate) {
		t.Fatalf("duplicate restored: %v", err)
	}
	if newer.ID == p.ID {
		t.Fatal("Add resurrected archived ID")
	}
	if err := r.Remove(t.Context(), newer.ID); err != nil {
		t.Fatal(err)
	}
	for i := range MaxProjects {
		if _, err := r.Add(t.Context(), fmt.Sprint(i), t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.RestoreProject(t.Context(), p.ID); !errors.Is(err, ErrProjectLimit) {
		t.Fatalf("restore cap: %v", err)
	}
	if _, err := r.db.Exec(`UPDATE projects SET removed_at=NULL WHERE id=?`, p.ID); err == nil {
		t.Fatal("SQL restore cap bypass")
	}
}

func TestOrganizationBoundedInventoryAndSessionMembership(t *testing.T) {
	r := registryTestOpen(t, filepath.Join(t.TempDir(), "manager"))
	root := t.TempDir()
	for range OrganizationPageSize + 2 {
		p, err := r.Add(t.Context(), "archived", root)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Remove(t.Context(), p.ID); err != nil {
			t.Fatal(err)
		}
	}
	page, err := r.ListArchivedProjects(t.Context(), 0)
	if err != nil || len(page.Projects) != OrganizationPageSize || !page.HasMore || page.NextOffset != 25 {
		t.Fatalf("page %+v %v", page, err)
	}
	next, err := r.ListArchivedProjects(t.Context(), page.NextOffset)
	if err != nil || len(next.Projects) != 2 || next.HasMore {
		t.Fatalf("next %+v %v", next, err)
	}
	for _, offset := range []int{-1, MaxOrganizationOffset + 1} {
		if _, err := r.ListArchivedProjects(t.Context(), offset); err == nil {
			t.Fatal("unbounded archive offset")
		}
	}
	p, err := r.Add(t.Context(), "sessions", root)
	if err != nil {
		t.Fatal(err)
	}
	good := CatalogSessions{Sessions: []SessionSummary{{ID: "saved"}}}
	badPages := []CatalogSessions{{}, {Sessions: []SessionSummary{{ID: "other"}}}, {Sessions: []SessionSummary{{ID: "saved"}, {ID: "saved"}}}, {Sessions: make([]SessionSummary, 26)}}
	for _, page := range badPages {
		if err := r.saveSessionOrganization(t.Context(), p.ID, "saved", "pin", page); err == nil {
			t.Fatal("nonmember or unbounded catalog accepted")
		}
	}
	for _, name := range []string{"", strings.Repeat("a", 129), "con\ntrol"} {
		if err := r.RenameProject(t.Context(), p.ID, name); err == nil {
			t.Fatalf("invalid name accepted %q", name)
		}
	}
	if err := r.saveSessionOrganization(t.Context(), p.ID, "saved", "pin", good); err != nil {
		t.Fatal(err)
	}
	// Populate only manager flags, never session files, to exercise storage caps.
	_, err = r.db.Exec(`WITH RECURSIVE ids(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM ids WHERE i<999)
 INSERT INTO session_organization SELECT ?, 's-' || i,1,0 FROM ids`, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	over := CatalogSessions{Sessions: []SessionSummary{{ID: "one-too-many"}}}
	if err := r.saveSessionOrganization(t.Context(), p.ID, "one-too-many", "pin", over); !errors.Is(err, ErrOrganizationLimit) {
		t.Fatalf("project metadata cap %v", err)
	}
	if err := r.saveSessionOrganization(t.Context(), p.ID, "saved", "archive", good); err != nil {
		t.Fatalf("updating existing row at cap: %v", err)
	}
	// Ten projects each capped at 1000 reach the independent global bound.
	for i := range 9 {
		other, err := r.Add(t.Context(), fmt.Sprint(i), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		_, err = r.db.Exec(`WITH RECURSIVE ids(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM ids WHERE i<1000)
   INSERT INTO session_organization SELECT ?, 's-' || i,1,0 FROM ids`, other.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	last, err := r.Add(t.Context(), "global cap", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.saveSessionOrganization(t.Context(), last.ID, "saved", "pin", good); !errors.Is(err, ErrOrganizationLimit) {
		t.Fatalf("global metadata cap %v", err)
	}
}
