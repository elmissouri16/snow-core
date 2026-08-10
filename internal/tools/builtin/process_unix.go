//go:build !windows

package builtin

import (
	"os/exec"
	"syscall"
	"time"
)

type managedProcess struct{}

func startManagedProcess(cmd *exec.Cmd) (*managedProcess, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &managedProcess{}, nil
}

func (*managedProcess) close() {}
