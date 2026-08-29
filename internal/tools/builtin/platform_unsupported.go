//go:build !darwin && !linux

package builtin

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

var errUnsupportedPlatform = errors.New("snow requires macOS or Linux")

func startManagedProcess(*exec.Cmd) error { return errUnsupportedPlatform }

func shellCommand(context.Context, string, []string, string) (*exec.Cmd, error) {
	return nil, errUnsupportedPlatform
}

func shellDescription() string {
	return "Unavailable on this platform: bounded non-interactive POSIX shell execution requires macOS or Linux."
}

func validatePlatformPath(string) error { return errUnsupportedPlatform }
func platformPathWithin(string) bool    { return false }

func renameReplace(string, string) error { return errUnsupportedPlatform }

func preserveRootedReplacementSecurity(*os.Root, string, *os.File) error {
	return errUnsupportedPlatform
}
