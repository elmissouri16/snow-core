package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFileStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	if _, ok := fs.Get("opencode-go"); ok {
		t.Fatal("expected no credential before Put")
	}

	cred := Credential{Type: CredentialAPIKey, Key: "oc-secret"}
	if err := fs.Put("opencode-go", cred); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok := fs.Get("opencode-go")
	if !ok {
		t.Fatal("expected credential after Put")
	}
	if got.Key != "oc-secret" {
		t.Fatalf("key = %q, want %q", got.Key, "oc-secret")
	}
	if got.Provider != "opencode-go" {
		t.Fatalf("provider = %q, want opencode-go", got.Provider)
	}

	if err := fs.Delete("opencode-go"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := fs.Get("opencode-go"); ok {
		t.Fatal("expected credential removed after Delete")
	}
}

func TestFileStoreOAuthRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	fs, _ := NewFileStore(path)

	cred := Credential{
		Type:    CredentialOAuth,
		Access:  "access-token",
		Refresh: "refresh-token",
		Expires: 1730000000,
		Extra:   map[string]any{"account_id": "acc-1"},
	}
	if err := fs.Put("chatgpt", cred); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok := fs.Get("chatgpt")
	if !ok {
		t.Fatal("expected oauth credential")
	}
	if got.Type != CredentialOAuth || got.Access != "access-token" || got.Refresh != "refresh-token" {
		t.Fatalf("oauth credential mismatch: %+v", got)
	}
	if got.Expires != 1730000000 {
		t.Fatalf("expires = %d, want 1730000000", got.Expires)
	}
	if got.Extra["account_id"] != "acc-1" {
		t.Fatalf("extra mismatch: %+v", got.Extra)
	}
}

func TestFileStorePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	fs1, _ := NewFileStore(path)
	if err := fs1.Put("opencode-go", Credential{Type: CredentialAPIKey, Key: "k1"}); err != nil {
		t.Fatal(err)
	}

	fs2, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := fs2.Get("opencode-go")
	if !ok || got.Key != "k1" {
		t.Fatalf("reloaded credential = %+v ok=%v", got, ok)
	}
}

func TestFileStorePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	fs, _ := NewFileStore(path)
	if err := fs.Put("opencode-go", Credential{Type: CredentialAPIKey, Key: "k"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 600", perm)
	}

	// Chmod intentionally and Put again: mode must be restored to 0600.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fs.Put("chatgpt", Credential{Type: CredentialOAuth, Access: "a"}); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode after re-Put = %o, want 600", perm)
	}
}

func TestFileStoreNoTempFilesLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	fs, _ := NewFileStore(path)
	for range 5 {
		if err := fs.Put("p", Credential{Type: CredentialAPIKey, Key: "k"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".auth-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestFileStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, _ := NewFileStore(path)
	if _, ok := fs.Get("p"); ok {
		t.Fatal("Get on corrupt file should not return a credential")
	}
	if err := fs.Put("p", Credential{Type: CredentialAPIKey, Key: "k"}); err == nil {
		t.Fatal("Put on corrupt file should error, not silently overwrite")
	}
	if err := fs.Delete("p"); err == nil {
		t.Fatal("Delete on corrupt file should error")
	}
}

func TestFileStoreEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fs, _ := NewFileStore(path)
	if _, ok := fs.Get("p"); ok {
		t.Fatal("empty file should yield no credential")
	}
	if err := fs.Put("p", Credential{Type: CredentialAPIKey, Key: "k"}); err != nil {
		t.Fatalf("Put on empty file should work: %v", err)
	}
}

func TestFileStorePath(t *testing.T) {
	fs, _ := NewFileStore("/tmp/x/auth.json")
	if fs.Path() != "/tmp/x/auth.json" {
		t.Fatalf("Path() = %q", fs.Path())
	}
}

func TestFileStoreEmptyPath(t *testing.T) {
	if _, err := NewFileStore(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

// TestConcurrentPutsNoLostUpdate: concurrent Put calls from many goroutines
// must not lose updates (the store serializes load-modify-save cycles).
func TestConcurrentPutsNoLostUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			provider := fmt.Sprintf("provider-%02d", i)
			cred := Credential{Type: CredentialAPIKey, Key: "key-" + provider}
			if err := fs.Put(provider, cred); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Put: %v", err)
	}

	for i := range n {
		provider := fmt.Sprintf("provider-%02d", i)
		got, ok := fs.Get(provider)
		if !ok {
			t.Fatalf("provider %q missing after concurrent puts (lost update)", provider)
		}
		if got.Key != "key-"+provider {
			t.Fatalf("provider %q key = %q, want %q", provider, got.Key, "key-"+provider)
		}
	}
}

func TestFileStoreReadCacheInvalidatesAndClones(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("p", Credential{Type: CredentialOAuth, Access: "one", Extra: map[string]any{"nested": map[string]any{"value": "original"}}}); err != nil {
		t.Fatal(err)
	}
	first, ok := store.Get("p")
	if !ok {
		t.Fatal("missing credential")
	}
	first.Extra["nested"].(map[string]any)["value"] = "mutated"
	second, _ := store.Get("p")
	if second.Extra["nested"].(map[string]any)["value"] != "original" {
		t.Fatal("caller mutation escaped cached credential clone")
	}

	data, err := json.MarshalIndent(map[string]persistCredential{
		"p": persistCredential(Credential{Type: CredentialOAuth, Access: "two"}),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	updated, ok := store.Get("p")
	if !ok || updated.Access != "two" {
		t.Fatalf("updated=%+v ok=%v", updated, ok)
	}
}

func TestFileStoreRefreshLockDoesNotBlockReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("p", Credential{Type: CredentialAPIKey, Key: "key"}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.WithRefreshLock("chatgpt", func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	readDone := make(chan struct{})
	go func() {
		_, _ = store.Get("p")
		close(readDone)
	}()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("provider refresh lock blocked unrelated credential read")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
