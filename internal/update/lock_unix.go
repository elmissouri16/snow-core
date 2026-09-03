//go:build darwin || linux

package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func openUpdateLock(ctx context.Context, root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("update: inspect lock: %w", err)
	}
	flags := os.O_RDWR
	if errors.Is(err, os.ErrNotExist) {
		flags |= os.O_CREATE | os.O_EXCL
	} else if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("update: lock is not a regular non-symlink file")
	}
	file, err := root.OpenFile(name, flags, 0o600)
	if err != nil {
		return nil, fmt.Errorf("update: open lock: %w", err)
	}
	opened, statErr := file.Stat()
	if statErr != nil || !opened.Mode().IsRegular() || info != nil && !os.SameFile(info, opened) {
		file.Close()
		return nil, errors.New("update: lock identity changed")
	}
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			file.Close()
			return nil, fmt.Errorf("update: lock executable: %w", err)
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, fmt.Errorf("update: wait for executable lock: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	current, err := root.Lstat(name)
	if err != nil || !os.SameFile(opened, current) {
		file.Close()
		return nil, errors.New("update: lock identity changed")
	}
	return file, nil
}
