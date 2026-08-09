package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/snow-core/snow/internal/tools"
)

// DefaultBashTimeout is the default command timeout.
const DefaultBashTimeout = 120 * time.Second

// Bash is the shell command execution tool.
type Bash struct {
	// MaxOutputBytes caps combined stdout+stderr. Defaults to 262144.
	MaxOutputBytes int
	// Timeout caps execution. Defaults to 120s.
	Timeout time.Duration
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
	return tools.ToolSchema{
		Name:        "bash",
		Description: "Run a shell command in the working directory.",
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

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "cmd", "/c", a.Command)
	} else {
		cmd = exec.CommandContext(runCtx, "sh", "-c", a.Command)
	}

	// Kill the whole process group on cancel so children are reaped too.
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = processGroupAttr()
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return nil
			}
			// Negative pid targets the process group.
			return killProcessGroup(cmd.Process.Pid)
		}
		cmd.WaitDelay = 2 * time.Second
	}

	if host != nil {
		cmd.Dir = host.CWD()
		if env := host.Environ(); env != nil {
			cmd.Env = env
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

	err := cmd.Run()

	output := limited.String()
	if limited.truncated {
		output += "\n... [output truncated]"
	}

	if err != nil {
		// A timeout or cancellation takes precedence over exit-code reporting.
		if runCtx.Err() != nil {
			return tools.ErrorResult(fmt.Errorf("bash: command timed out or cancelled after %s", timeout)), nil
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

// String returns the buffered content.
func (l *limitedBuffer) String() string { return string(l.buf) }
