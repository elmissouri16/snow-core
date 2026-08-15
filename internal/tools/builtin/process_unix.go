//go:build darwin || linux

package builtin

import (
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type managedProcess struct {
	done       chan struct{}
	closeOnce  sync.Once
	cancelOnce sync.Once
	canceled   atomic.Bool
	pid        int
}

func startManagedProcess(cmd *exec.Cmd, gracefulCancel bool) (*managedProcess, error) {
	managed := &managedProcess{done: make(chan struct{})}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		managed.canceled.Store(true)
		if cmd.Process == nil {
			return nil
		}
		if !gracefulCancel {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		// smolvm handles SIGINT by cancelling the in-guest exec while leaving
		// the persistent machine usable. If it or a helper ignores SIGINT, kill
		// the complete process group after the same bounded drain grace instead
		// of relying on os/exec's launcher-PID-only WaitDelay backstop.
		pid := cmd.Process.Pid
		err := syscall.Kill(-pid, syscall.SIGINT)
		managed.cancelOnce.Do(func() {
			go func() {
				timer := time.NewTimer(2 * time.Second)
				defer timer.Stop()
				select {
				case <-managed.done:
				case <-timer.C:
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			}()
		})
		return err
	}
	if cmd.WaitDelay <= 0 {
		cmd.WaitDelay = 2 * time.Second
	}
	if err := cmd.Start(); err != nil {
		managed.close()
		return nil, err
	}
	managed.pid = cmd.Process.Pid
	return managed, nil
}

func (m *managedProcess) wasCanceled() bool {
	return m != nil && m.canceled.Load()
}

func (m *managedProcess) forceKill() {
	if m == nil || m.pid <= 0 {
		return
	}
	_ = syscall.Kill(-m.pid, syscall.SIGKILL)
}

func (m *managedProcess) close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() { close(m.done) })
}
