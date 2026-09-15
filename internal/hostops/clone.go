package hostops

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	CloneTimeout      = 10 * time.Minute
	OutputBudget      = 64 << 10
	HelperArgument    = "--host-clone-helper-only"
	helperReapTimeout = 8 * time.Second
)

// Clone starts no execution before Release. Cancel works before or after release;
// Done closes only after supervision/cleanup has returned. CleanupFailed means
// bounded cleanup could not confirm completion, never successful cancellation.
type Clone struct {
	release     chan struct{}
	releaseOnce sync.Once
	done        chan struct{}
	cancel      context.CancelFunc
	result      protocol.RPCProjectCompletion
}

func newClone(parent context.Context, options Options, parentIdentity protocol.HostDirectoryIdentity, start protocol.RPCProjectCloneStartParams, child *os.File) *Clone {
	ctx, cancel := context.WithTimeout(parent, CloneTimeout)
	c := &Clone{release: make(chan struct{}), done: make(chan struct{}), cancel: cancel,
		result: protocol.RPCProjectCompletion{OperationID: start.OperationID, Child: start.Child}}
	go func() {
		defer close(c.done)
		defer cancel()
		defer child.Close()
		select {
		case <-ctx.Done():
			c.result.Status = contextStatus(ctx)
			return
		case <-c.release:
		}
		if ctx.Err() != nil {
			c.result.Status = contextStatus(ctx)
			return
		}
		if !matchesPath(parentIdentity) || !matchesPath(start.Child) {
			c.result.Status = protocol.HostOperationFailed
			return
		}
		c.result.Status = launchHelper(ctx, options, start, child)
	}()
	return c
}

func (c *Clone) Release()              { c.releaseOnce.Do(func() { close(c.release) }) }
func (c *Clone) Cancel()               { c.cancel() }
func (c *Clone) Done() <-chan struct{} { return c.done }
func (c *Clone) Completion() (protocol.RPCProjectCompletion, bool) {
	select {
	case <-c.done:
		return c.result, true
	default:
		return protocol.RPCProjectCompletion{}, false
	}
}

func contextStatus(ctx context.Context) protocol.HostOperationStatus {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return protocol.HostOperationTimedOut
	}
	return protocol.HostOperationCanceled
}

func launchHelper(ctx context.Context, options Options, start protocol.RPCProjectCloneStartParams, child *os.File) protocol.HostOperationStatus {
	read, write, err := os.Pipe()
	if err != nil {
		return protocol.HostOperationFailed
	}
	defer read.Close()
	defer write.Close()
	deadline, _ := ctx.Deadline()
	payload, err := json.Marshal(helperPayload{GitExecutable: options.GitExecutable, Start: start, Deadline: deadline})
	if err != nil {
		return protocol.HostOperationFailed
	}
	// Do not use CommandContext: killing the helper immediately would destroy
	// its process-group reaper. Cancellation closes the worker-only life writer.
	cmd := exec.Command(options.HelperExecutable, HelperArgument)
	cmd.Env = baseEnvironment()
	cmd.ExtraFiles = []*os.File{child, read}
	cmd.Stdin = bytes.NewReader(payload)
	output := &limitedCapture{limit: 1024, cancel: func() { _ = write.Close() }}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return protocol.HostOperationFailed
	}
	_ = read.Close()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-wait:
	case <-ctx.Done():
		_ = write.Close()
		select {
		case waitErr = <-wait:
		case <-time.After(helperReapTimeout):
			// A broken trusted helper is not reported as safely canceled. Normal
			// helpers finish their TERM/KILL/reap before this outer bound.
			_ = cmd.Process.Kill()
			select {
			case <-wait:
			case <-time.After(time.Second):
			}
			return protocol.HostOperationCleanupFailed
		}
	}
	var result helperResult
	if waitErr != nil || output.exceeded() || json.Unmarshal(output.bytes(), &result, json.RejectUnknownMembers(true)) != nil || !validTerminalStatus(result.Status) {
		return protocol.HostOperationCleanupFailed
	}
	if result.Status == protocol.HostOperationCleanupFailed {
		return result.Status
	}
	if ctx.Err() != nil {
		return contextStatus(ctx)
	}
	return result.Status
}

func validTerminalStatus(status protocol.HostOperationStatus) bool {
	switch status {
	case protocol.HostOperationSucceeded, protocol.HostOperationFailed, protocol.HostOperationCanceled,
		protocol.HostOperationTimedOut, protocol.HostOperationOutputLimit, protocol.HostOperationCleanupFailed:
		return true
	}
	return false
}

// All output is drained, but only a bounded prefix is retained. The Git budget
// uses retain=false; no Git output is ever serialized or returned to callers.
type limitedCapture struct {
	mu      sync.Mutex
	limit   int
	count   int
	data    []byte
	discard bool
	cancel  func()
	over    bool
}

func (w *limitedCapture) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	remaining := max(0, w.limit-w.count)
	if !w.discard {
		w.data = append(w.data, p[:min(n, remaining)]...)
	}
	w.count += min(n, max(0, w.limit+1-w.count))
	if w.count > w.limit && !w.over {
		w.over = true
		w.cancel()
	}
	return n, nil
}
func (w *limitedCapture) exceeded() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.over }
func (w *limitedCapture) bytes() []byte  { w.mu.Lock(); defer w.mu.Unlock(); return bytes.Clone(w.data) }
