//go:build darwin || linux

package builtin

import (
	"os/exec"
	"time"

	"github.com/elmissouri16/snow-core/internal/procgroup"
)

func startManagedProcess(cmd *exec.Cmd) error {
	if err := procgroup.Configure(cmd); err != nil {
		return err
	}
	cmd.Cancel = func() error {
		return procgroup.Kill(cmd.Process)
	}
	if cmd.WaitDelay <= 0 {
		cmd.WaitDelay = 2 * time.Second
	}
	return cmd.Start()
}
