package tempfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepStaleRemovesOnlyOldMatchingRegularFiles(t *testing.T) {
	dir := t.TempDir()
	oldMatch := filepath.Join(dir, ".auth-old.tmp")
	freshMatch := filepath.Join(dir, ".auth-fresh.tmp")
	other := filepath.Join(dir, "keep.tmp")
	for _, path := range []string{oldMatch, freshMatch, other} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldMatch, old, old); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".auth-link.tmp")
	if err := os.Symlink(oldMatch, link); err != nil {
		t.Fatal(err)
	}

	SweepStale(dir, []string{".auth-"}, 24*time.Hour)
	if _, err := os.Stat(oldMatch); !os.IsNotExist(err) {
		t.Fatalf("old matching file remains: %v", err)
	}
	for _, path := range []string{freshMatch, other} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("kept file %q: %v", path, err)
		}
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("symlink should not be removed: %v", err)
	}
}
