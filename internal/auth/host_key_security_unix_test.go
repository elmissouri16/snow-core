//go:build darwin || linux

package auth

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestHostKeyRejectsFileRootAndLockSymlinksAndFIFOs(t *testing.T) {
	for _, kind := range []string{"file-symlink", "file-fifo", "lock-symlink", "lock-fifo", "root-symlink"} {
		t.Run(kind, func(t *testing.T) {
			path := hostAuthPath(t)
			outside := filepath.Join(filepath.Dir(path), "outside")
			writeHostAuth(t, outside, `{"opencode-go":{"type":"api_key","key":"outside-canary"}}`)
			switch kind {
			case "file-symlink":
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "file-fifo":
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			case "lock-symlink":
				if err := os.Symlink(outside, path+".lock"); err != nil {
					t.Fatal(err)
				}
			case "lock-fifo":
				if err := unix.Mkfifo(path+".lock", 0o600); err != nil {
					t.Fatal(err)
				}
			case "root-symlink":
				actual := filepath.Join(filepath.Dir(path), "actual")
				if err := os.Mkdir(actual, 0o700); err != nil {
					t.Fatal(err)
				}
				alias := filepath.Join(filepath.Dir(path), "alias")
				if err := os.Symlink(actual, alias); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(alias, "auth.json")
			}
			_, err := SetHostAPIKey(t.Context(), path, hostKeyRequest("opencode-go", "missing"))
			if !errors.Is(err, ErrHostKeyUnavailable) {
				t.Fatal("unsafe target accepted")
			}
			data, err := os.ReadFile(outside)
			if err != nil || string(data) != `{"opencode-go":{"type":"api_key","key":"outside-canary"}}` {
				t.Fatal("outside canary changed")
			}
		})
	}
}

func TestHostKeyPinnedRootSurvivesReplacement(t *testing.T) {
	path := hostAuthPath(t)
	parent := filepath.Join(filepath.Dir(path), "root")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(parent, "auth.json")
	writeHostAuth(t, path, `{}`)
	root, base, err := openHostKeyParent(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	snapshot, err := readHostKeySnapshot(t.Context(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parent, parent+"-moved"); err != nil {
		t.Fatal(err)
	}
	outside := parent + "-outside"
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	writeHostAuth(t, filepath.Join(outside, "auth.json"), `{"outside":{"key":"outside-canary"}}`)
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := writeHostKeyRoot(t.Context(), root, base, snapshot.info, []byte(`{"new":{"type":"api_key","key":"new-canary"}}`)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outside, "auth.json"))
	if err != nil || string(data) != `{"outside":{"key":"outside-canary"}}` {
		t.Fatal("pinned write followed replacement root")
	}
}

func TestHostKeyCrossProcessHelper(t *testing.T) {
	if os.Getenv("SNOW_HOST_KEY_CAS_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	_, err := SetHostAPIKey(t.Context(), os.Getenv("SNOW_HOST_KEY_CAS_PATH"), hostKeyRequest("opencode-go", "missing"))
	if err == nil {
		t.Log("HOST_KEY_SUCCESS")
	} else if errors.Is(err, ErrHostKeyConflict) {
		t.Log("HOST_KEY_CONFLICT")
	} else {
		t.Fatal(err)
	}
}

func TestHostKeyCrossProcessCAS(t *testing.T) {
	path := hostAuthPath(t)
	type result struct {
		output string
		err    error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHostKeyCrossProcessHelper$", "-test.v")
			command.Env = append(os.Environ(), "SNOW_HOST_KEY_CAS_HELPER=1", "SNOW_HOST_KEY_CAS_PATH="+path)
			data, err := command.CombinedOutput()
			results <- result{string(data), err}
		}()
	}
	successes, conflicts := 0, 0
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if strings.Contains(got.output, "HOST_KEY_SUCCESS") {
			successes++
		}
		if strings.Contains(got.output, "HOST_KEY_CONFLICT") {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestHostKeyLegacyProcessHelper(t *testing.T) {
	if os.Getenv("SNOW_HOST_KEY_LEGACY_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	store, err := NewFileStore(os.Getenv("SNOW_HOST_KEY_LEGACY_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("opencode-go", Credential{Type: CredentialAPIKey, Key: "stable-key-canary"}); err != nil {
		t.Fatal("legacy atomic write failed")
	}
}

func TestHostKeyExternalLegacySameContentInodeInvalidatesCAS(t *testing.T) {
	path := hostAuthPath(t)
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("opencode-go", Credential{Type: CredentialAPIKey, Key: "stable-key-canary"}); err != nil {
		t.Fatal("initial legacy write failed")
	}
	before := hostKeyInspect(t, path, "opencode-go")
	oldInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHostKeyLegacyProcessHelper$")
	command.Env = append(os.Environ(), "SNOW_HOST_KEY_LEGACY_HELPER=1", "SNOW_HOST_KEY_LEGACY_PATH="+path)
	if err := command.Run(); err != nil {
		t.Fatal("external legacy writer failed")
	}
	newInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(oldInfo, newInfo) {
		t.Fatal("legacy atomic writer reused inode")
	}
	// Same bytes, same size, restored mtime: only inode identity distinguishes
	// this independent legacy process's atomic replacement from the old file.
	if err := os.Chtimes(path, oldInfo.ModTime(), oldInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(original) != string(after) {
		t.Fatal("same-value legacy write changed bytes")
	}
	if hostKeyInspect(t, path, "opencode-go").Revision == before.Revision {
		t.Fatal("metadata revision omitted replacement inode")
	}
	if _, err := SetHostAPIKey(t.Context(), path, hostKeyRequest("opencode-go", before.Revision)); !errors.Is(err, ErrHostKeyConflict) {
		t.Fatal("external legacy writer did not invalidate CAS")
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(original) {
		t.Fatal("stale CAS changed external legacy result")
	}
}
