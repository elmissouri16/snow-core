package app

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
)

func TestAppSessionInventoryCreateOpenRenameDeleteByID(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("SNOW_HOME", filepath.Join(t.TempDir(), "home"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(t.TempDir(), "sessions"))
	a, err := New(t.Context(), Options{
		Provider: "fake", Permission: "allow", CWD: cwd,
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	originalID := a.Session.ID()
	if err := a.RenameSessionByID(originalID, "original"); err != nil {
		t.Fatal(err)
	}
	created, err := a.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.ID == originalID || a.Session.ID() != created.ID {
		t.Fatalf("created=%+v active=%q original=%q", created, a.Session.ID(), originalID)
	}
	createdID := created.ID
	if err := a.RenameSessionByID(createdID, "created"); err != nil {
		t.Fatal(err)
	}

	if err := a.RenameSessionByID(originalID, "inactive title"); err != nil {
		t.Fatal(err)
	}
	if a.Session.ID() != createdID {
		t.Fatalf("renaming inactive session switched active ID to %q", a.Session.ID())
	}
	infos, err := a.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("ListSessions returned %d entries: %+v", len(infos), infos)
	}
	var renamed bool
	for _, info := range infos {
		if info.ID == originalID && info.Name == "inactive title" {
			renamed = true
		}
	}
	if !renamed {
		t.Fatalf("renamed session missing from inventory: %+v", infos)
	}

	opened, err := a.OpenSession(originalID)
	if err != nil {
		t.Fatal(err)
	}
	if opened.ID != originalID || a.Session.ID() != originalID {
		t.Fatalf("opened=%+v active=%q", opened, a.Session.ID())
	}
	if err := a.DeleteSessionByID(originalID); err == nil {
		t.Fatal("DeleteSessionByID deleted the active session")
	}
	if err := a.DeleteSessionByID(createdID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.OpenSession(createdID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("OpenSession(deleted) error = %v, want ErrNotFound", err)
	}
}

func TestAppSessionInventoryRejectsForeignAndUnknownIDs(t *testing.T) {
	cwd := t.TempDir()
	foreignCWD := t.TempDir()
	t.Setenv("SNOW_HOME", filepath.Join(t.TempDir(), "home"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(t.TempDir(), "sessions"))
	a, err := New(t.Context(), Options{
		Provider: "fake", Permission: "allow", CWD: cwd,
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	index := session.NewFileIndex(session.DefaultSessionsRoot())
	foreign, err := index.Create(foreignCWD)
	if err != nil {
		t.Fatal(err)
	}
	foreignID := foreign.ID()
	if err := foreign.Close(); err != nil {
		t.Fatal(err)
	}
	for operation, err := range map[string]error{
		"open foreign":   func() error { _, err := a.OpenSession(foreignID); return err }(),
		"delete foreign": a.DeleteSessionByID(foreignID),
		"rename foreign": a.RenameSessionByID(foreignID, "wrong"),
		"open unknown":   func() error { _, err := a.OpenSession("missing"); return err }(),
	} {
		if !errors.Is(err, session.ErrNotFound) {
			t.Errorf("%s error = %v, want ErrNotFound", operation, err)
		}
	}
}

func TestAppRenameSessionByIDSupportsActiveEphemeralSession(t *testing.T) {
	a, err := New(t.Context(), Options{
		Provider: "fake", Permission: "allow", CWD: t.TempDir(), NoSession: true,
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	id := a.Session.ID()
	if err := a.RenameSessionByID(id, "ephemeral"); err != nil {
		t.Fatal(err)
	}
	if title, err := a.Agent.SessionTitle(); err != nil || title != "ephemeral" {
		t.Fatalf("title = %q, err = %v", title, err)
	}
}
