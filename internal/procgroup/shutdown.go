package procgroup

import (
	"errors"
	"fmt"
	"os"
	"time"
)

const shutdownPollInterval = 10 * time.Millisecond

// Shutdown asks a configured process group to terminate, escalates to kill,
// and returns only after the complete group is gone or both waits expire.
func Shutdown(process *os.Process, grace time.Duration) error {
	if process == nil || !Exists(process) {
		return nil
	}
	if grace <= 0 {
		grace = time.Second
	}
	terminateErr := Terminate(process)
	if waitGone(process, grace) {
		return ignoreProcessDone(terminateErr)
	}
	killErr := Kill(process)
	if waitGone(process, grace) {
		return errors.Join(ignoreProcessDone(terminateErr), ignoreProcessDone(killErr))
	}
	return errors.Join(
		ignoreProcessDone(terminateErr),
		ignoreProcessDone(killErr),
		fmt.Errorf("process group %d did not exit after kill", process.Pid),
	)
}

func waitGone(process *os.Process, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for Exists(process) {
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(shutdownPollInterval)
	}
	return true
}

func ignoreProcessDone(err error) error {
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
