//go:build !darwin && !linux

package process

import "testing"

func assertReaped(t *testing.T, pid int) {
	t.Helper()
	t.Log("Wait4 reap assertion is supported on macOS and Linux only")
}
