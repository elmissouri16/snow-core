//go:build windows

package builtin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func shellCommand(ctx context.Context, command string, opts WindowsShellOptions, env []string, cwd string) (*exec.Cmd, error) {
	kind := opts.Kind
	if kind == "" {
		kind = "powershell"
	}
	switch kind {
	case "cmd":
		return exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command), nil
	case "executable":
		if opts.Executable == "" || !filepath.IsAbs(opts.Executable) {
			return nil, errors.New("configured Windows shell executable must be absolute")
		}
		return exec.CommandContext(ctx, opts.Executable, command), nil
	case "powershell":
		name, err := lookPathWithEnv("pwsh.exe", env, cwd)
		if err != nil {
			name, err = lookPathWithEnv("powershell.exe", env, cwd)
		}
		if err != nil {
			return nil, errors.New("PowerShell not found (tried pwsh.exe and powershell.exe)")
		}
		script := "[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new();" + command
		return exec.CommandContext(ctx, name, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script), nil
	default:
		return nil, fmt.Errorf("unsupported Windows shell kind %q", kind)
	}
}

func lookPathWithEnv(name string, env []string, cwd string) (string, error) {
	if len(env) == 0 {
		return exec.LookPath(name)
	}
	pathValue := ""
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, "PATH") {
			pathValue = value
			break
		}
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if !filepath.IsAbs(dir) {
			if cwd == "" {
				continue
			}
			dir = filepath.Join(cwd, dir)
		}
		candidate, absErr := filepath.Abs(filepath.Join(dir, name))
		if absErr != nil {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found in configured PATH", name)
}

func shellDescription() string {
	return "Run a non-interactive PowerShell command in the working directory (tool name retained for compatibility)."
}
