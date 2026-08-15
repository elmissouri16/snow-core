//go:build darwin || linux

package sandbox

import (
	"context"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const lifecycleOutputLimit = 64 << 10

type cappedOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	available := lifecycleOutputLimit - len(w.data)
	if available > 0 {
		take := min(available, len(p))
		w.data = append(w.data, p[:take]...)
	}
	if len(p) > available {
		w.truncated = true
	}
	return len(p), nil
}

func (w *cappedOutput) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := append([]byte(nil), w.data...)
	if w.truncated {
		out = append(out, []byte("\n... [lifecycle output truncated]")...)
	}
	return out
}

func boundedCombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return boundedCombinedOutputEnv(ctx, nil, name, args...)
}

func boundedCombinedOutputEnv(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = append([]string(nil), env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output := &cappedOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }
	var cancelOnce sync.Once
	var canceled atomic.Bool
	cmd.Cancel = func() error {
		canceled.Store(true)
		if cmd.Process == nil {
			return nil
		}
		pid := cmd.Process.Pid
		err := syscall.Kill(-pid, syscall.SIGINT)
		cancelOnce.Do(func() {
			go func() {
				timer := time.NewTimer(2 * time.Second)
				defer timer.Stop()
				select {
				case <-done:
				case <-timer.C:
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			}()
		})
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		closeDone()
		return output.bytes(), err
	}
	err := cmd.Wait()
	if canceled.Load() && cmd.Process != nil {
		// WaitDelay can return after killing only the launcher PID. Kill the
		// process group synchronously before suppressing the grace timer so an
		// ignored SIGINT cannot leave helper descendants behind.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	closeDone()
	return output.bytes(), err
}
