//go:build darwin || linux

package procgroup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShutdownStopsCompleteProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("sh", "-c", `sleep 30 & child=$!; printf '%s' "$child" > "$1"; wait`, "sh", pidFile)
	if err := Configure(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	leaderDone := make(chan error, 1)
	go func() { leaderDone <- cmd.Wait() }()

	childPID := waitChildPID(t, pidFile)
	if err := Shutdown(cmd.Process, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case <-leaderDone:
	case <-time.After(time.Second):
		t.Fatal("process-group leader was not reaped")
	}
	waitProcessGone(t, childPID)
}

func waitChildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child pid was not published")
	return 0
}

func waitProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("child process %d survived process-group shutdown", pid)
}
