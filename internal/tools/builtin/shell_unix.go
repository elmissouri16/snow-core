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
	return "Run one bounded, non-interactive one-shot command through POSIX sh in the working directory and return combined stdout/stderr. Runs with Snow's OS privileges and is not confined to file-tool roots. Use process_start for development servers, watchers, workers, and other long-running commands; never background them with &, nohup, or disown."
}
