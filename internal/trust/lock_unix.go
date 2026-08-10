//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package trust

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lockStoreFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("trust: open lock: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("trust: lock store: %w", err)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
