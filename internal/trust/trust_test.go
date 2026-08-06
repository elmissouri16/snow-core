package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("/tmp/proj", LevelAllow); err != nil {
		t.Fatal(err)
	}
	s2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if lvl, ok := s2.Get("/tmp/proj"); !ok || lvl != LevelAllow {
		t.Fatalf("round trip = %q %v", lvl, ok)
	}
}

func TestStoreParentWalk(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("/tmp", LevelDeny); err != nil {
		t.Fatal(err)
	}
	// /tmp/a/b inherits the /tmp decision.
	if lvl, ok := s.Get("/tmp/a/b"); !ok || lvl != LevelDeny {
		t.Fatalf("parent walk = %q %v", lvl, ok)
	}
}

func TestStoreMissingReturnsNone(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("/some/unknown/path"); ok {
		t.Fatal("expected no decision")
	}
}

func TestHasSensitiveResources(t *testing.T) {
	dir := t.TempDir()
	if HasSensitiveResources(dir) {
		t.Fatal("empty dir should not be sensitive")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".snow"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".snow", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasSensitiveResources(dir) {
		t.Fatal(".snow/config.json should be sensitive")
	}
}

func TestFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	s, _ := New(path)
	if err := s.Set("/x", LevelAllow); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", fi.Mode().Perm())
	}
}
