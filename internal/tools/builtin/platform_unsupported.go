//go:build !darwin && !linux

package builtin

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

var errUnsupportedPlatform = errors.New("snow requires macOS or Linux")

type managedProcess struct{}

func startManagedProcess(*exec.Cmd, bool) (*managedProcess, error) {
	return nil, errUnsupportedPlatform
}
func (*managedProcess) close()            {}
func (*managedProcess) forceKill()        {}
func (*managedProcess) wasCanceled() bool { return false }

func shellCommand(context.Context, string, []string, string) (*exec.Cmd, error) {
	return nil, errUnsupportedPlatform
}

func shellDescription() string {
	return "Shell execution is available on macOS and Linux."
}

func validatePlatformPath(string) error { return errUnsupportedPlatform }
func platformPathWithin(string) bool    { return false }

func renameReplace(string, string) error { return errUnsupportedPlatform }

func preserveRootedReplacementSecurity(*os.Root, string, *os.File) error {
	return errUnsupportedPlatform
}
