//go:build darwin || linux

package builtin

import (
	"context"
	"os/exec"
)

func shellCommand(ctx context.Context, command string, _ []string, _ string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, "sh", "-c", command), nil
}

func shellDescription() string {
	return "Run a bounded, non-interactive one-shot POSIX shell command in the working directory. Use process_start instead for development servers, watchers, background workers, and other long-running commands; never background them with &, nohup, or disown."
}
