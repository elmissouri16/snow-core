package update

import (
	"errors"
	"runtime"
)

func inheritedExecutablePath() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return "/proc/self/fd/3", nil
	case "darwin":
		// Darwin does not provide fexecve(2), and executing /dev/fd/N is not
		// supported consistently. The caller keeps the staged inode open and
		// verifies pathname identity immediately before and after execution.
		return "", nil
	default:
		return "", errors.New("update: descriptor-based executable validation is unavailable on this platform")
	}
}
