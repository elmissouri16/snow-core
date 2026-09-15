// Package process explicitly starts and supervises a single Snow RPC worker.
// It uses executable/argument vectors, never a shell. Workers have the caller's
// OS privileges: this package is not a sandbox and does not supervise or kill
// descendants. Callers must select a trusted executable and worker configuration.
package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
)

// Options describes an explicitly requested worker. No RPC arguments, permission
// policy, session directory, plugin policy, or other runtime settings are added
// implicitly; the caller is responsible for all of these in Args and Env.
type Options struct {
	Executable string
	Args       []string
	Dir        string
	// Env replaces the environment when non-nil (including an empty slice).
	// Nil inherits the current environment, with exec.Cmd's usual PWD handling.
	Env []string
	RPC rpc.Options
	// ShutdownTimeout bounds the grace period after closing worker stdin.
	// Zero defaults to one second; negative values are invalid. The direct
	// child is killed and reaped if it does not exit within this period.
	ShutdownTimeout time.Duration
}

// Worker owns a process and its RPC client. Do not copy it or replace Client.
// Consume Client.Events continuously as required by the RPC client contract.
// Close releases all process and transport resources. Cancellation of the Start
// context, client termination, or child exit also initiates automatic cleanup.
// The zero value is not usable.
type Worker struct {
	Client *rpc.Client

	cmd          *exec.Cmd
	conn         *pipeConn
	exited       chan struct{}
	done         chan struct{}
	request      chan struct{}
	requestClose func()
	grace        time.Duration
	waitErr      error // published by closing exited
	closeErr     error // published by closing done
}

// Start launches a direct child and waits for its validated rpc_ready handshake.
// ctx governs the entire worker lifetime. RPC.HandshakeTimeout bounds readiness
// (default ten seconds). Failed startup closes all descriptors and kills/reaps
// the child after the bounded EOF grace period before returning. Unsupported
// pipe deadlines fail before spawning. Stderr goes directly to the null device:
// it is never buffered, returned, logged, or copied by a background goroutine.
func Start(ctx context.Context, opts Options) (*Worker, error) {
	if ctx == nil || opts.Executable == "" || opts.ShutdownTimeout < 0 {
		return nil, errors.New("rpc worker: context, executable, and valid shutdown timeout are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.ShutdownTimeout == 0 {
		opts.ShutdownTimeout = time.Second
	}
	conn, stdin, stdout, err := newPipes()
	if err != nil {
		return nil, fmt.Errorf("rpc worker: deadline-capable pipes: %w", err)
	}
	defer stdin.Close()
	defer stdout.Close()
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rpc worker: stderr sink: %w", err)
	}
	defer stderr.Close()

	cmd := exec.Command(opts.Executable, opts.Args...)
	cmd.Dir = opts.Dir
	cmd.Env = slices.Clone(opts.Env)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Start(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rpc worker: start: %w", err)
	}
	// Release the parent's copies immediately: retaining stdout's writer would
	// hide child exit from the RPC reader. Deferred closes cover earlier errors.
	_ = stdin.Close()
	_ = stdout.Close()
	w := &Worker{cmd: cmd, conn: conn, exited: make(chan struct{}), done: make(chan struct{}), request: make(chan struct{}), grace: opts.ShutdownTimeout}
	w.requestClose = sync.OnceFunc(func() { close(w.request) })
	go func() {
		w.waitErr = cmd.Wait()
		close(w.exited)
	}()
	client, err := rpc.New(ctx, conn, opts.RPC)
	if err != nil {
		_ = conn.Close() // New rejects invalid options without taking ownership.
		_ = w.stop()
		return nil, fmt.Errorf("rpc worker: handshake: %w", err)
	}
	w.Client = client
	go func() {
		select {
		case <-ctx.Done():
		case <-client.Done():
		case <-w.exited:
		case <-w.request:
		}
		w.closeErr = w.stop()
		// Preserve final frames already written by a gracefully exiting child.
		// A leaked descendant stdout handle must not make this drain unbounded.
		if err := conn.SetReadDeadline(time.Now().Add(w.grace)); err == nil {
			<-client.Done()
		}
		_ = client.Close() // joins the RPC reader and any in-progress write
		close(w.done)
	}()
	return w, nil
}

// stop is called by exactly one owner and joins the sole Wait goroutine.
func (w *Worker) stop() error {
	_ = w.conn.closeWrite() // allow the worker to process stdin EOF gracefully
	timer := time.NewTimer(w.grace)
	defer timer.Stop()
	select {
	case <-w.exited:
		return w.waitErr
	case <-timer.C:
		killErr := w.cmd.Process.Kill()
		<-w.exited
		if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return fmt.Errorf("rpc worker: kill: %w", killErr)
		}
		// A forced exit is expected after expiry of our shutdown grace period.
		return nil
	}
}

// Close sends stdin EOF, allows a bounded graceful exit, then kills if needed.
// It is concurrent-safe and idempotent, and joins process supervision and RPC
// I/O before returning. Draining final stdout frames after child exit is also
// bounded by ShutdownTimeout. A nonzero spontaneous/graceful exit is returned;
// an exit caused by our timeout Kill is not an error. It never returns stderr.
func (w *Worker) Close() error {
	w.requestClose()
	<-w.done
	return w.closeErr
}
