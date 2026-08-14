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
	return "Run a non-interactive POSIX shell command in the working directory."
}
