//go:build darwin || linux

package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
	"golang.org/x/sys/unix"
)

func TestManagerCrossProcessHelper(t *testing.T) {
	if os.Getenv("SNOW_MANAGER_CAS_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	_, err := UpdateManagerDefaults(t.Context(), os.Getenv("SNOW_MANAGER_CAS_PATH"), protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: os.Getenv("SNOW_MANAGER_CAS_REVISION"), Global: &protocol.HostGlobalDefaultsPatch{Thinking: &protocol.HostStringOperation{Op: "set", Value: new("high")}}})
	if err == nil {
		t.Log("CAS_SUCCESS")
	} else if errors.Is(err, ErrManagerConflict) {
		t.Log("CAS_CONFLICT")
	} else {
		t.Fatal(err)
	}
}

func TestManagerCrossProcessCASUsesSharedLock(t *testing.T) {
	path := managerTestPath(t)
	current, err := ReadManagerDefaults(t.Context(), path, protocol.HostDefaultsRequest{Scope: "global"})
	if err != nil {
		t.Fatal(err)
	}
	outputs := make(chan string, 2)
	failures := make(chan error, 2)
	for range 2 {
		go func() {
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestManagerCrossProcessHelper$", "-test.v")
			command.Env = append(os.Environ(), "SNOW_MANAGER_CAS_HELPER=1", "SNOW_MANAGER_CAS_PATH="+path, "SNOW_MANAGER_CAS_REVISION="+current.Revision)
			data, err := command.CombinedOutput()
			outputs <- string(data)
			failures <- err
		}()
	}
	success, conflict := 0, 0
	for range 2 {
		data := <-outputs
		if strings.Contains(data, "CAS_SUCCESS") {
			success++
		}
		if strings.Contains(data, "CAS_CONFLICT") {
			conflict++
		}
	}
	for range 2 {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("cross-process results: success=%d conflict=%d", success, conflict)
	}
}

func TestManagerRefusesNonregularAndLockSymlink(t *testing.T) {
	path := managerTestPath(t)
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManagerDefaults(t.Context(), path, protocol.HostDefaultsRequest{Scope: "global"}); !errors.Is(err, ErrManagerUnavailable) {
		t.Fatal("FIFO accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	current, err := ReadManagerDefaults(t.Context(), path, protocol.HostDefaultsRequest{Scope: "global"})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(path), "outside-lock")
	managerTestWrite(t, outside, "lock-canary")
	if err := os.Symlink(outside, path+".lock"); err != nil {
		t.Fatal(err)
	}
	_, err = UpdateManagerDefaults(t.Context(), path, protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: current.Revision, Global: &protocol.HostGlobalDefaultsPatch{Thinking: &protocol.HostStringOperation{Op: "set", Value: new("high")}}})
	if !errors.Is(err, ErrManagerUnavailable) {
		t.Fatal("lock symlink accepted")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "lock-canary" {
		t.Fatal("symlink target modified")
	}
}
