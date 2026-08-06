package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
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
	if runtime.GOOS == "windows" {
		t.Skip("permission bits not enforced on windows")
	}
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
	for i := 0; i < 5; i++ {
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

func TestResolveAPIKeyEnvFallback(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileStore(filepath.Join(dir, "auth.json"))
	t.Setenv("OPENCODE_API_KEY", "env-key")

	c, err := ResolveAPIKey(fs, "OPENCODE_API_KEY", "opencode-go")
	if err != nil {
		t.Fatalf("ResolveAPIKey: %v", err)
	}
	if c.Key != "env-key" || c.Type != CredentialAPIKey {
		t.Fatalf("resolved = %+v, want env-key", c)
	}
}

func TestResolveAPIKeyStorePriority(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileStore(filepath.Join(dir, "auth.json"))
	if err := fs.Put("opencode-go", Credential{Type: CredentialAPIKey, Key: "store-key"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_API_KEY", "env-key")

	c, err := ResolveAPIKey(fs, "OPENCODE_API_KEY", "opencode-go")
	if err != nil {
		t.Fatalf("ResolveAPIKey: %v", err)
	}
	if c.Key != "store-key" {
		t.Fatalf("resolved = %q, want store-key (store wins over env)", c.Key)
	}
}

func TestResolveAPIKeyIgnoresOAuthEntry(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileStore(filepath.Join(dir, "auth.json"))
	if err := fs.Put("chatgpt", Credential{Type: CredentialOAuth, Access: "a"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHATGPT_TEST_ENV", "")
	_, err := ResolveAPIKey(fs, "CHATGPT_TEST_ENV", "chatgpt")
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential for oauth entry without env", err)
	}
}

func TestResolveAPIKeyNoCredential(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileStore(filepath.Join(dir, "auth.json"))
	t.Setenv("OPENCODE_API_KEY", "")
	_, err := ResolveAPIKey(fs, "OPENCODE_API_KEY", "opencode-go")
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential", err)
	}
}

func TestResolveAPIKeyNilStore(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "env-key")
	c, err := ResolveAPIKey(nil, "OPENCODE_API_KEY", "opencode-go")
	if err != nil {
		t.Fatalf("ResolveAPIKey with nil store: %v", err)
	}
	if c.Key != "env-key" {
		t.Fatalf("key = %q, want env-key", c.Key)
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
	for i := 0; i < n; i++ {
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

	for i := 0; i < n; i++ {
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
