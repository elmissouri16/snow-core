package config

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func managerTestPath(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("SNOW_HOME", dir)
	return filepath.Join(dir, "config.json")
}
func managerTestWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestManagerAtomicFailurePreservesOriginal(t *testing.T) {
	path := managerTestPath(t)
	managerTestWrite(t, path, `{"thinking":"high"}`)
	root, base, err := openManagerParent(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_, before, err := readManagerRoot(t.Context(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	// A concurrent non-cooperating replacement must not be overwritten.
	replacement := filepath.Join(filepath.Dir(path), "replacement")
	managerTestWrite(t, replacement, `{"thinking":"low"}`)
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := writeManagerRoot(t.Context(), root, base, before, []byte(`{"thinking":"medium"}`)); !errors.Is(err, ErrManagerUnavailable) {
		t.Fatal("replaced inode overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != `{"thinking":"low"}` {
		t.Fatal("target changed on failure")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary file leaked")
	}
}

func TestManagerPinnedRootDoesNotFollowReplacement(t *testing.T) {
	path := managerTestPath(t)
	folder := filepath.Join(filepath.Dir(path), "root")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(folder, "config.json")
	managerTestWrite(t, path, `{"thinking":"high"}`)
	root, base, err := openManagerParent(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_, before, err := readManagerRoot(t.Context(), root, base)
	if err != nil {
		t.Fatal(err)
	}
	moved := folder + "-moved"
	if err := os.Rename(folder, moved); err != nil {
		t.Fatal(err)
	}
	outside := folder + "-outside"
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	managerTestWrite(t, filepath.Join(outside, "config.json"), `{"secret":"outside-canary"}`)
	if err := os.Symlink(outside, folder); err != nil {
		t.Fatal(err)
	}
	if err := writeManagerRoot(t.Context(), root, base, before, []byte(`{"thinking":"low"}`)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outside, "config.json"))
	if err != nil || string(data) != `{"secret":"outside-canary"}` {
		t.Fatal("pinned write escaped into replacement")
	}
	if _, _, err := openManagerParent(path, false); !errors.Is(err, ErrManagerUnavailable) {
		t.Fatal("subsequent read followed root symlink")
	}
}

func TestManagerProjectSelectionLimitAndUnknownFields(t *testing.T) {
	path := managerTestPath(t)
	document := managerObject{}
	selections := managerObject{}
	for i := range MaxProjectSelections {
		managerPut(selections, fmt.Sprintf("/project/%d", i), managerObject{})
	}
	managerPut(document, "project_selections", selections)
	managerPut(document, "unknown", "canary")
	root, base, err := openManagerParent(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	// Encoding goes through the same raw-map writer used by production.
	managerPut(document, "thinking", "off")
	data := []byte(marshalManagerTest(t, document))
	if err := writeManagerRoot(t.Context(), root, base, nil, data); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Dir(path)
	current, err := ReadManagerDefaults(t.Context(), path, protocol.HostDefaultsRequest{Scope: "project", CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateManagerDefaults(t.Context(), path, protocol.HostDefaultsUpdateRequest{Scope: "project", CWD: cwd, Revision: current.Revision, Project: &protocol.HostProjectDefaultsPatch{Thinking: &protocol.HostStringOperation{Op: "set", Value: new("high")}}}); !errors.Is(err, ErrManagerInvalid) {
		t.Fatal("project entry limit not enforced")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(data) {
		t.Fatal("limit failure rewrote file")
	}
}

func TestManagerCancellationWhileWaitingForSharedLock(t *testing.T) {
	path := managerTestPath(t)
	current, err := ReadManagerDefaults(t.Context(), path, protocol.HostDefaultsRequest{Scope: "global"})
	if err != nil {
		t.Fatal(err)
	}
	root, base, err := openManagerParent(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := openPinnedKeybindingLock(root, base+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lockConfigFile(lock); err != nil {
		t.Fatal(err)
	}
	defer unlockConfigFile(lock)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = UpdateManagerDefaults(ctx, path, protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{Thinking: &protocol.HostStringOperation{Op: "set", Value: new("high")}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("lock wait ignored deadline")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled write created configuration")
	}
}

func marshalManagerTest(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
