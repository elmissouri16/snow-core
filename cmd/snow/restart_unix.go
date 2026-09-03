//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/elmissouri16/snow-core/internal/tui"
)

const restartGuardEnv = "SNOW_RESTARTED_AFTER_UPDATE"

func restartAfterUpdate(request tui.RunResult, initialSessionPath string, resumeCommand bool) error {
	if os.Getenv(restartGuardEnv) != "" {
		return errors.New("restart: automatic restart loop prevented; launch Snow again manually")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("restart: resolve executable: %w", err)
	}
	args := restartExecArguments(os.Args, initialSessionPath, request.SessionPath, resumeCommand)
	env := append(slices.Clone(os.Environ()), restartGuardEnv+"=1")
	if err := syscall.Exec(executable, args, env); err != nil {
		return fmt.Errorf("restart: execute updated Snow: %w", err)
	}
	return nil
}

func restartExecArguments(original []string, initialSessionPath, activeSessionPath string, resumeCommand bool) []string {
	args := slices.Clone(original)
	if len(args) == 0 {
		args = []string{"snow"}
	}
	if activeSessionPath == "" {
		return args
	}

	canonical := make([]string, 0, len(args)+2)
	canonical = append(canonical, args[0])
	removedResumePath := false
	seenResumeCommand := !resumeCommand
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if resumeCommand && arg == "resume" {
			seenResumeCommand = true
		}
		switch {
		case arg == "--no-session" || strings.HasPrefix(arg, "--no-session="):
			continue
		case arg == "--session":
			if i+1 < len(args) {
				i++
			}
			continue
		case strings.HasPrefix(arg, "--session="):
			continue
		case resumeCommand && seenResumeCommand && !removedResumePath && initialSessionPath != "" && arg == initialSessionPath:
			removedResumePath = true
			continue
		default:
			canonical = append(canonical, arg)
		}
	}
	return append(canonical, "--session", activeSessionPath)
}
