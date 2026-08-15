package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveStorePublishesPresecuredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sandboxes.json")
	state := storeFile{Version: storeVersion, Projects: map[string]Record{}}
	if err := saveStore(path, state); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("published state mode = %v", info.Mode())
	}
}
