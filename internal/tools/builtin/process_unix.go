//go:build !windows

package builtin

import "syscall"

// processGroupAttr puts the child in its own process group so cancelling the
// context kills the entire group (children included).
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the process group of the given pid.
func killProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
