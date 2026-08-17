//go:build darwin || linux

package config

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockConfigFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

func unlockConfigFile(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
