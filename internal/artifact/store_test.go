package artifact

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStoreScopesArtifactsAndRejectsTraversal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	store, err := NewLocalStore(root, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ref, err := store.SaveText(context.Background(), "session-a", "call", "full output")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadText(context.Background(), "session-a", ref.ID)
	if err != nil || got != "full output" {
		t.Fatalf("read=%q err=%v", got, err)
	}
	if _, err := store.ReadText(context.Background(), "session-b", ref.ID); err == nil {
		t.Fatal("cross-session read succeeded")
	}
	for _, id := range []string{"../x", "/tmp/x", "artifact-nope"} {
		if _, err := store.ReadText(context.Background(), "session-a", id); err == nil {
			t.Fatalf("invalid ID %q accepted", id)
		}
	}
	info, err := os.Stat(root)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("root mode=%v err=%v", info.Mode().Perm(), err)
	}
	files, err := filepath.Glob(filepath.Join(root, "session-*", "*.txt"))
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	fileInfo, err := os.Stat(files[0])
	if err != nil || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file mode=%v err=%v", fileInfo.Mode().Perm(), err)
	}
}

func TestLocalStoreRejectsSymlinkNamespace(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, namespace("session"))); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := store.SaveText(context.Background(), "session", "call", "secret"); err == nil {
		t.Fatal("symlink namespace accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, artifactID("session", "call", "secret")+".txt")); !os.IsNotExist(err) {
		t.Fatalf("outside artifact created: %v", err)
	}
}

func TestLocalStoreIsIdempotentAndBounded(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 32)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := store.SaveText(context.Background(), "session", "call", "same")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveText(context.Background(), "session", "call", "same")
	if err != nil || first != second {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
	if _, err := store.SaveText(context.Background(), "session", "large", strings.Repeat("x", 33)); err == nil {
		t.Fatal("oversized artifact accepted")
	}
}
