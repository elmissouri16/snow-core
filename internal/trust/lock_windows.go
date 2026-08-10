//go:build windows

package trust

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func lockStoreFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("trust: open lock: %w", err)
	}
	overlapped := new(windows.Overlapped)
	handle := windows.Handle(f.Fd())
	if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("trust: lock store: %w", err)
	}
	return func() {
		_ = windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
		_ = f.Close()
	}, nil
}
