//go:build darwin || linux

package mcp

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func lockCacheFile(f *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("MCP cache lock deadline exceeded")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func unlockCacheFile(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
