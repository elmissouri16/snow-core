package auth

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func hostKeyInspect(t *testing.T, path, id string) protocol.HostAPIKeyStatusResponse {
	t.Helper()
	response, err := InspectHostAPIKey(t.Context(), path, id)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
func hostKeyRequest(id, revision string) protocol.HostAPIKeySetRequest {
	return protocol.HostAPIKeySetRequest{ProviderID: id, ExpectedRevision: revision, Secret: "submitted-secret-canary", ConfirmReplace: true}
}

func TestHostKeyPreservesUnknownProfilesAndExtra(t *testing.T) {
	path := hostAuthPath(t)
	original := `{"chatgpt":{"type":"oauth","access":"access-canary","refresh":"refresh-canary","expires":1,"extra":{"future":{"nested":"extra-canary"}}},"opencode-go":{"type":"api_key","key":"old-key-canary","extra":{"nested":[1,"extra-canary"]},"unknown":{"data":"unknown-canary"}},"unknown-provider":{"type":"api_key","key":"other-canary","future":"preserved"}}`
	writeHostAuth(t, path, original)
	before := hostKeyInspect(t, path, "opencode-go")
	response, err := SetHostAPIKey(t.Context(), path, hostKeyRequest("opencode-go", before.Revision))
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	if json.Unmarshal([]byte(original), &a) != nil || json.Unmarshal(after, &b) != nil {
		t.Fatal("invalid test document")
	}
	for _, id := range []string{"chatgpt", "unknown-provider"} {
		if !reflect.DeepEqual(a[id], b[id]) {
			t.Fatal("unrelated profile changed")
		}
	}
	old := a["opencode-go"].(map[string]any)
	next := b["opencode-go"].(map[string]any)
	if next["type"] != "api_key" || next["key"] != "submitted-secret-canary" || !reflect.DeepEqual(old["extra"], next["extra"]) || !reflect.DeepEqual(old["unknown"], next["unknown"]) {
		t.Fatal("target unknown fields lost")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("auth mode not 0600")
	}
	lockInfo, err := os.Stat(path + ".lock")
	if err != nil || lockInfo.Mode().Perm() != 0o600 {
		t.Fatal("shared lock mode not 0600")
	}
	encoded, err := json.Marshal(response)
	if err != nil || bytes.Contains(encoded, []byte("canary")) {
		t.Fatal("write-only result leaked a canary")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 2 {
		t.Fatal("temporary file leaked")
	}
}

func TestHostKeyMetadataRevisionDoesNotHashContents(t *testing.T) {
	path := hostAuthPath(t)
	first := `{"opencode-go":{"type":"api_key","key":"aaa"}}`
	second := `{"opencode-go":{"type":"api_key","key":"bbb"}}`
	writeHostAuth(t, path, first)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before := hostKeyInspect(t, path, "opencode-go")
	// Same inode, size and restored mtime: only secret contents changed. This is
	// deliberately outside the atomic writer contract and proves no secret hash.
	writeHostAuth(t, path, second)
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	after := hostKeyInspect(t, path, "opencode-go")
	if before.Revision != after.Revision {
		t.Fatal("revision includes secret bytes")
	}
	if hostKeyInspect(t, path, "opencode-go").Revision != after.Revision {
		t.Fatal("read/atime changed revision")
	}
}

func TestHostKeyLegacyAtomicWriterInvalidatesCAS(t *testing.T) {
	path := hostAuthPath(t)
	writeHostAuth(t, path, `{"opencode-go":{"type":"api_key","key":"old-canary"}}`)
	before := hostKeyInspect(t, path, "opencode-go")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("other", Credential{Type: CredentialAPIKey, Key: "legacy-canary"}); err != nil {
		t.Fatal("legacy mutation failed")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetHostAPIKey(t.Context(), path, hostKeyRequest("opencode-go", before.Revision)); !errors.Is(err, ErrHostKeyConflict) {
		t.Fatal("legacy writer did not invalidate CAS")
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, unchanged) {
		t.Fatal("stale CAS overwrote auth")
	}
}

func TestHostKeyMalformedOversizeAndSecretValidation(t *testing.T) {
	path := hostAuthPath(t)
	for _, secret := range []string{"", " ", strings.Repeat("x", MaxHostAPIKeyBytes+1), "line\nfeed", "carriage\rreturn", "tab\tkey", "nul\x00key", string([]byte{0xff})} {
		request := hostKeyRequest("opencode-go", "missing")
		request.Secret = secret
		if _, err := SetHostAPIKey(t.Context(), path, request); !errors.Is(err, ErrHostKeyInvalid) {
			t.Fatal("invalid secret accepted")
		}
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid input created lock")
	}
	for _, data := range []string{"", `null`, `{"parse":"canary",`, strings.Repeat(" ", MaxHostAuthFileBytes+1), `{"other":{"expires":"bad-canary"}}`, `{"other":null}`, `{"opencode-go":{},"opencode-go":{}}`} {
		writeHostAuth(t, path, data)
		if _, err := InspectHostAPIKey(t.Context(), path, "opencode-go"); !errors.Is(err, ErrHostKeyUnavailable) {
			t.Fatal("corrupt auth inspection accepted")
		}
		if _, err := SetHostAPIKey(t.Context(), path, hostKeyRequest("opencode-go", "missing")); !errors.Is(err, ErrHostKeyUnavailable) {
			t.Fatal("corrupt auth overwritten")
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != data {
			t.Fatal("corrupt auth changed")
		}
	}
}

func TestHostKeyConcurrentCASOnlyOneWins(t *testing.T) {
	path := hostAuthPath(t)
	before := hostKeyInspect(t, path, "opencode-go")
	errorsFound := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := SetHostAPIKey(t.Context(), path, hostKeyRequest("opencode-go", before.Revision))
			errorsFound <- err
		})
	}
	wg.Wait()
	close(errorsFound)
	successes, conflicts := 0, 0
	for err := range errorsFound {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrHostKeyConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 7 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestHostKeyCanceledCommitAndAtomicFailurePreserveFile(t *testing.T) {
	path := hostAuthPath(t)
	writeHostAuth(t, path, `{"opencode-go":{"type":"api_key","key":"old-canary"}}`)
	root, base, err := openHostKeyParent(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, err := readHostKeySnapshot(t.Context(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := writeHostKeyRoot(ctx, root, base, before.info, []byte(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled commit accepted")
	}
	replacement := path + ".replacement"
	writeHostAuth(t, replacement, `{"other":{"key":"replacement-canary"}}`)
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if _, err := writeHostKeyRoot(t.Context(), root, base, before.info, []byte(`{}`)); !errors.Is(err, ErrHostKeyUnavailable) {
		t.Fatal("uncoordinated inode replaced")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != `{"other":{"key":"replacement-canary"}}` {
		t.Fatal("failure changed replacement")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("failed atomic write leaked temporary file")
	}
}

func TestHostKeyLegacyLockWaitCancellation(t *testing.T) {
	path := hostAuthPath(t)
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- store.withExclusiveLock(func() error { close(locked); <-release; return nil }) }()
	select {
	case <-locked:
	case err := <-done:
		t.Fatal(err)
	case <-t.Context().Done():
		t.Fatal("legacy lock never acquired")
	}
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err = SetHostAPIKey(ctx, path, hostKeyRequest("opencode-go", "missing"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("new writer bypassed legacy shared lock")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled lock waiter wrote auth")
	}
}

func TestHostKeyEncodedLimitPreservesOriginalAndMaxUTF8Key(t *testing.T) {
	path := hostAuthPath(t)
	original := `{"future-profile":{"unknown":"` + strings.Repeat("x", MaxHostAuthFileBytes-256) + `"}}`
	writeHostAuth(t, path, original)
	inspected := hostKeyInspect(t, path, "opencode-go")
	request := hostKeyRequest("opencode-go", inspected.Revision)
	request.Secret = strings.Repeat("é", MaxHostAPIKeyBytes/2)
	if _, err := SetHostAPIKey(t.Context(), path, request); !errors.Is(err, ErrHostKeyUnavailable) {
		t.Fatal("encoded size limit bypassed")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatal("size failure changed original")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	request.ExpectedRevision = "missing"
	if _, err := SetHostAPIKey(t.Context(), path, request); err != nil {
		t.Fatal("valid bounded UTF-8 key rejected")
	}
}
