//go:build darwin || linux

package web

import (
	"errors"
	"os"
	"testing"
)

func TestRegistrySkillsMigrationPersistenceAndIdentity(t *testing.T) {
	r, p, manager := trustTestProject(t)
	// Upgrade a pre-preference registry without granting skills or trust.
	for _, statement := range []string{`DROP TRIGGER project_skills_reset`, `ALTER TABLE projects DROP COLUMN skills_enabled`} {
		if _, err := r.db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = registryTestOpen(t, manager)
	p, err := r.Lookup(t.Context(), p.ID)
	if err != nil || p.SkillsEnabled || p.Trusted {
		t.Fatalf("migration granted consent: %+v, %v", p, err)
	}
	if err := r.SetProjectSkills(t.Context(), p, true); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = registryTestOpen(t, manager)
	projects, err := r.List(t.Context())
	if err != nil || len(projects) != 1 || !projects[0].SkillsEnabled || projects[0].Trusted {
		t.Fatalf("preference not retained independently of trust: %+v, %v", projects, err)
	}
	if err := os.Rename(p.Path, p.Path+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.SetProjectSkills(t.Context(), p, true); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("changed directory accepted: %v", err)
	}
	if err := os.Remove(p.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(p.Path+"-moved", p.Path); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RestoreProject(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := r.Lookup(t.Context(), p.ID)
	if err != nil || restored.SkillsEnabled {
		t.Fatalf("archive/restore retained opt-in: %+v, %v", restored, err)
	}
}
