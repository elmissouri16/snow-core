//go:build !darwin && !linux

package procgroup

import (
	"errors"
	"os"
	"os/exec"
)

var errUnsupported = errors.New("process groups require macOS or Linux")

func Configure(*exec.Cmd) error          { return errUnsupported }
func Terminate(*os.Process) error        { return errUnsupported }
func Kill(*os.Process) error             { return errUnsupported }
func Exists(*os.Process) bool            { return false }
func ExitSignal(*os.ProcessState) string { return "" }
