//go:build darwin || linux

package session

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockSessionShared(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_SH)
}

func tryLockSessionExclusive(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return errSessionInUse
	}
	return err
}

func unlockSessionFile(file *os.File) {
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
