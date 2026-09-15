//go:build darwin || linux

package auth

import (
	"golang.org/x/sys/unix"
	"os"
)

// Do not block on a FIFO raced into the path after the initial lstat. The caller
// verifies regular-file type and inode identity before reading any bytes.
func openHostStatusFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
