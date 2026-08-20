package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/internal/procgroup"
)

type runtimeProcess struct {
	id        string
	name      string
	sessionID string
	cwd       string
	command   string
	cmd       *exec.Cmd
	output    *outputRing
	done      chan struct{}

	mu         sync.Mutex
	status     string
	startedAt  int64
	finishedAt int64
	exitCode   *int
	signal     string
	reason     string
	ready      bool
	waitErr    error
	stopGate   chan struct{}
}

func newRuntimeProcess(id, name, cwd, command string, outputBytes int) *runtimeProcess {
	return &runtimeProcess{
		id: id, name: name, cwd: cwd, command: command,
		output: newOutputRing(outputBytes), done: make(chan struct{}), status: "running",
		stopGate: make(chan struct{}, 1),
	}
}

func (p *runtimeProcess) start() error {
	cmd := exec.Command("sh", "-c", p.command)
	cmd.Dir = p.cwd
	cmd.Stdout = p.output
	cmd.Stderr = p.output
	if err := procgroup.Configure(cmd); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	p.mu.Lock()
	p.cmd = cmd
	p.startedAt = time.Now().UnixMilli()
	p.mu.Unlock()
	go p.wait()
	return nil
}

func (p *runtimeProcess) wait() {
	err := p.cmd.Wait()
	p.mu.Lock()
	p.waitErr = err
	p.finishedAt = time.Now().UnixMilli()
	if p.reason == "" {
		p.reason = "natural"
	}
	if p.reason == "stop_requested" || p.reason == "shutdown" || p.reason == "readiness_failed" || p.reason == "start_cancelled" {
		p.status = "stopped"
	} else {
		p.status = "exited"
	}
	if p.cmd.ProcessState != nil {
		code := p.cmd.ProcessState.ExitCode()
		if code >= 0 {
			p.exitCode = &code
		}
		p.signal = procgroup.ExitSignal(p.cmd.ProcessState)
	}
	p.mu.Unlock()
	close(p.done)
}

func (p *runtimeProcess) isRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status == "running"
}

func (p *runtimeProcess) markReady() {
	p.mu.Lock()
	p.ready = true
	p.mu.Unlock()
}

func (p *runtimeProcess) state() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := State{
		ProcessID: p.id, Name: p.name, Status: p.status, StartedAt: p.startedAt,
		FinishedAt: p.finishedAt, Signal: p.signal, Reason: p.reason, Ready: p.ready,
	}
	if p.exitCode != nil {
		code := *p.exitCode
		state.ExitCode = &code
	}
	return state
}

func (p *runtimeProcess) stop(ctx context.Context, grace time.Duration, reason string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case p.stopGate <- struct{}{}:
		defer func() { <-p.stopGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-p.done:
		return nil
	default:
	}
	p.mu.Lock()
	if p.status != "running" {
		p.mu.Unlock()
		return nil
	}
	if p.reason == "" {
		p.reason = reason
	}
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return errors.New("managed process was not started")
	}
	terminateErr := procgroup.Terminate(cmd.Process)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	cancelErr := error(nil)
	select {
	case <-p.done:
		// The shell leader may exit before descendants finish graceful cleanup.
		// Return immediately only when the complete process group is gone.
		if !procgroup.Exists(cmd.Process) {
			return ignoreExpectedExitError(terminateErr)
		}
		select {
		case <-timer.C:
		case <-ctx.Done():
			cancelErr = ctx.Err()
		}
	case <-timer.C:
	case <-ctx.Done():
		cancelErr = ctx.Err()
	}
	killErr := procgroup.Kill(cmd.Process)
	select {
	case <-p.done:
		return errors.Join(cancelErr, ignoreExpectedExitError(terminateErr), ignoreExpectedExitError(killErr))
	case <-time.After(DefaultStopGrace):
		return errors.Join(cancelErr, ignoreExpectedExitError(terminateErr), ignoreExpectedExitError(killErr), fmt.Errorf("managed process %s did not exit after kill", p.id))
	}
}

func ignoreExpectedExitError(err error) error {
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
