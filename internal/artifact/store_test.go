package artifact

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	if err := store.CopyText(context.Background(), "session-a", "session-b", ref.ID); err != nil {
		t.Fatal(err)
	}
	if copied, err := store.ReadText(context.Background(), "session-b", ref.ID); err != nil || copied != "full output" {
		t.Fatalf("copied=%q err=%v", copied, err)
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
	if err != nil || len(files) != 2 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	fileInfo, err := os.Stat(files[0])
	if err != nil || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file mode=%v err=%v", fileInfo.Mode().Perm(), err)
	}
}

func TestLocalStoreBoundsVerifiedArtifactCache(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < maxVerifiedArtifacts+100; i++ {
		if _, err := store.SaveText(context.Background(), "session", fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	store.verifiedMu.Lock()
	got := len(store.verified)
	store.verifiedMu.Unlock()
	if got > maxVerifiedArtifacts {
		t.Fatalf("verified cache entries=%d, want <=%d", got, maxVerifiedArtifacts)
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

func TestExistsValidatesSessionOwnershipWithoutReading(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ref, err := store.SaveText(context.Background(), "session-a", "key", "value")
	if err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(context.Background(), "session-a", ref.ID); err != nil || !exists {
		t.Fatalf("owned artifact exists=%v err=%v", exists, err)
	}
	if exists, err := store.Exists(context.Background(), "session-b", ref.ID); err != nil || exists {
		t.Fatalf("cross-session artifact exists=%v err=%v", exists, err)
	}
	ids, err := store.ListIDs(context.Background(), "session-a")
	if err != nil || len(ids) != 1 || ids[0] != ref.ID {
		t.Fatalf("owned artifact IDs=%v err=%v", ids, err)
	}
	if ids, err := store.ListIDs(context.Background(), "session-b"); err != nil || len(ids) != 0 {
		t.Fatalf("cross-session artifact IDs=%v err=%v", ids, err)
	}
}

func TestLocalStoreDeleteSessionRemovesOnlyOwnedNamespace(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owned, err := store.SaveText(context.Background(), "delete-session", "call", "secret")
	if err != nil {
		t.Fatal(err)
	}
	kept, err := store.SaveText(context.Background(), "keep-session", "call", "keep")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSession(context.Background(), "delete-session"); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(context.Background(), "delete-session", owned.ID); err != nil || exists {
		t.Fatalf("deleted artifact exists=%v err=%v", exists, err)
	}
	if exists, err := store.Exists(context.Background(), "keep-session", kept.ID); err != nil || !exists {
		t.Fatalf("unrelated artifact exists=%v err=%v", exists, err)
	}
}

func TestSaveTextConcurrentSameAddressPublishesCompleteContent(t *testing.T) {
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "artifacts"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const workers = 32
	text := strings.Repeat("concurrent-content\n", 1024)
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ref, saveErr := store.SaveText(context.Background(), "session", "same", text)
			if saveErr != nil {
				errs <- saveErr
				return
			}
			got, readErr := store.ReadText(context.Background(), "session", ref.ID)
			if readErr != nil {
				errs <- readErr
			} else if got != text {
				errs <- fmt.Errorf("partial content: got %d bytes", len(got))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestSaveTextRepairsCrashOrphanAndSameSizeTamper(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	store, err := NewLocalStore(root, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const sessionID, key, text = "session", "call", "expected"
	id := artifactID(sessionID, key, text)
	dir := filepath.Join(root, namespace(sessionID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".txt")
	if err := os.WriteFile(path, []byte("part"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveText(context.Background(), sessionID, key, text); err != nil {
		t.Fatalf("repair partial: %v", err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil { // same length
		t.Fatal(err)
	}
	if _, err := store.SaveText(context.Background(), sessionID, key, text); err != nil {
		t.Fatalf("repair same-size tamper: %v", err)
	}
	got, err := store.ReadText(context.Background(), sessionID, id)
	if err != nil || got != text {
		t.Fatalf("read=%q err=%v", got, err)
	}
}
