//go:build darwin || linux

// Package procgroup centralizes Unix process-group lifecycle helpers used by
// synchronous shell commands and app-owned background processes.
package procgroup

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Configure starts cmd as the leader of a new process group.
func Configure(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

// Terminate asks the complete process group to exit.
func Terminate(process *os.Process) error {
	return signalGroup(process, syscall.SIGTERM)
}

// Kill forcefully stops the complete process group.
func Kill(process *os.Process) error {
	return signalGroup(process, syscall.SIGKILL)
}

func signalGroup(process *os.Process, signal syscall.Signal) error {
	if process == nil {
		return nil
	}
	err := syscall.Kill(-process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// Exists reports whether any process remains in the configured process group.
func Exists(process *os.Process) bool {
	if process == nil {
		return false
	}
	err := syscall.Kill(-process.Pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ExitSignal returns the Unix signal that terminated state, if any.
func ExitSignal(state *os.ProcessState) string {
	if state == nil {
		return ""
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return status.Signal().String()
}
