package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/tools"
)

const (
	// DefaultBashTimeout is the default command timeout.
	DefaultBashTimeout = 120 * time.Second
	// defaultProcessWaitDelay bounds pipe draining after a shell leader exits.
	defaultProcessWaitDelay = 2 * time.Second
)

// Bash is the shell command execution tool.
type Bash struct {
	// MaxOutputBytes caps combined stdout+stderr. Defaults to 262144.
	MaxOutputBytes int
	// Timeout caps execution. Defaults to 120s.
	Timeout time.Duration
	// CommandFactory optionally routes commands through an operator-owned
	// execution backend. active=false is the only case that permits the normal
	// host shell; backend errors fail closed.
	CommandFactory func(context.Context, string, []string, time.Duration) (cmd *exec.Cmd, gracefulCancel bool, active bool, err error)
	// SandboxActive is used only to describe the current tool boundary.
	SandboxActive func() bool
}

// NewBash returns a Bash tool with defaults.
func NewBash() *Bash {
	return &Bash{MaxOutputBytes: DefaultMaxOutputBytes, Timeout: DefaultBashTimeout}
}

// bashArgs is the JSON schema payload for bash.
type bashArgs struct {
	Command   string `json:"command"`
	TimeoutMS *int   `json:"timeout_ms"`
}

// Schema implements tools.Tool.
func (b *Bash) Schema() tools.ToolSchema {
	description := shellDescription()
	if b.SandboxActive != nil && b.SandboxActive() {
		description = "Run a non-interactive POSIX shell command inside the configured persistent smolvm Linux sandbox. The host project is mounted at the sandbox working directory."
	}
	return tools.ToolSchema{
		Name:        "bash",
		Description: description,
		Parameters: json.RawMessage(`{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": { "type": "string", "description": "Shell command to execute. Must be non-interactive." },
    "timeout_ms": { "type": "integer", "default": 120000, "description": "Timeout in milliseconds." }
  }
}`),
	}
}

// Run implements tools.Tool.
func (b *Bash) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var a bashArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.ErrorResult(fmt.Errorf("bash: invalid arguments: %w", err)), nil
	}
	if a.Command == "" {
		return tools.ErrorResult(fmt.Errorf("bash: command is required")), nil
	}
	emitProgress(host, "running command", false, false)
	defer emitProgress(host, "command finished", true, false)

	timeout := b.Timeout
	if timeout <= 0 {
		timeout = DefaultBashTimeout
	}
	// The model-supplied timeout_ms is bounded by the operator-configured cap
	// (b.Timeout) so a model cannot run commands for arbitrarily long.
	if a.TimeoutMS != nil && *a.TimeoutMS > 0 {
		// Compare before converting: int milliseconds may overflow Duration.
		// Values above the configured cap need no conversion at all.
		capMS := int64(timeout / time.Millisecond)
		requestedMS := int64(*a.TimeoutMS)
		if requestedMS < capMS {
			timeout = time.Duration(requestedMS) * time.Millisecond
		}
	}

	runCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	var hostEnv []string
	var hostCWD string
	if host != nil {
		hostEnv = host.Environ()
		hostCWD = host.CWD()
	}
	var cmd *exec.Cmd
	var err error
	gracefulCancel := false
	backendActive := false
	if b.CommandFactory != nil {
		cmd, gracefulCancel, backendActive, err = b.CommandFactory(runCtx, a.Command, hostEnv, timeout)
		if err != nil {
			return tools.ErrorResult(fmt.Errorf("bash: %w", err)), nil
		}
	}
	if !backendActive {
		cmd, err = shellCommand(runCtx, a.Command, hostEnv, hostCWD)
		if err != nil {
			return tools.ErrorResult(fmt.Errorf("bash: %w", err)), nil
		}
	}
	if cmd == nil {
		return tools.ErrorResult(errors.New("bash: execution backend returned no command")), nil
	}
	cmd.WaitDelay = boundedProcessWaitDelay(timeout)

	if host != nil {
		cmd.Dir = host.CWD()
		// A command factory may need a backend-specific environment (for
		// example smolvm's macOS disk formatter path). Preserve it; only apply
		// the host environment when the factory left Env unset.
		if cmd.Env == nil {
			if env := host.Environ(); env != nil {
				cmd.Env = env
			}
		}
	}

	cap := b.MaxOutputBytes
	if cap <= 0 {
		cap = DefaultMaxOutputBytes
	}
	// Buffer with a small headroom so we can append the truncation marker.
	limited := &limitedBuffer{cap: cap}
	cmd.Stdout = limited
	cmd.Stderr = limited

	managed, err := startManagedProcess(cmd, gracefulCancel)
	if err == nil {
		err = cmd.Wait()
	}
	if managed != nil {
		if gracefulCancel && managed.wasCanceled() {
			managed.forceKill()
		}
		managed.close()
	}

	output := sanitizeBoundedUTF8(limited.buf, cap)
	if limited.truncated {
		output += "\n... [output truncated]"
	}

	if err != nil {
		// A timeout or cancellation takes precedence over exit-code reporting.
		if runCtx.Err() != nil {
			if ctx.Err() != nil {
				return tools.ErrorResult(fmt.Errorf("bash: command cancelled: %w", ctx.Err())), nil
			}
			return tools.ErrorResult(fmt.Errorf("bash: command timed out after %s", timeout)), nil
		}
		if errors.Is(err, exec.ErrWaitDelay) {
			return tools.ErrorResult(fmt.Errorf("bash: command exited but descendant output remained open for %s", cmd.WaitDelay)), nil
		}
		// A non-zero exit code is normal tool feedback, not a tool error.
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			return tools.TextResult(fmt.Sprintf("exit code %d\n%s", code, output)), nil
		}
		return tools.ErrorResult(fmt.Errorf("bash: %w", err)), nil
	}

	return tools.TextResult(output), nil
}

// sanitizeBoundedUTF8 converts arbitrary process bytes into valid UTF-8 while
// keeping the returned text within the original byte budget.
func boundedProcessWaitDelay(timeout time.Duration) time.Duration {
	if timeout > 0 && timeout < defaultProcessWaitDelay {
		return timeout
	}
	return defaultProcessWaitDelay
}

func sanitizeBoundedUTF8(data []byte, maxBytes int) string {
	if maxBytes <= 0 || len(data) == 0 {
		return ""
	}
	value := string(data)
	if !utf8.Valid(data) {
		value = strings.ToValidUTF8(value, string(utf8.RuneError))
	}
	return truncateRunes(value, maxBytes)
}

// limitedBuffer caps writes at cap bytes, tracking truncation.
type limitedBuffer struct {
	cap       int
	buf       []byte
	truncated bool
}

// Write implements io.Writer.
func (l *limitedBuffer) Write(p []byte) (int, error) {
	remaining := l.cap - len(l.buf)
	if remaining > 0 {
		if len(p) > remaining {
			l.buf = append(l.buf, p[:remaining]...)
			l.truncated = true
			return len(p), nil // report full write; consumer sees truncated content
		}
		l.buf = append(l.buf, p...)
	} else if len(p) > 0 {
		l.truncated = true
	}
	return len(p), nil
}
