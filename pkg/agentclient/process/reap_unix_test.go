//go:build darwin || linux

package process

import (
	"errors"
	"syscall"
	"testing"
)

func assertReaped(t *testing.T, pid int) {
	t.Helper()
	var status syscall.WaitStatus
	got, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	if !errors.Is(err, syscall.ECHILD) {
		t.Fatalf("failed startup child not already reaped: Wait4 = %d, %v", got, err)
	}
}
