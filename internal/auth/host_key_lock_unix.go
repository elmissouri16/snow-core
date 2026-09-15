//go:build darwin || linux

package auth

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func hostKeyRevision(info os.FileInfo) (string, error) {
	if info == nil {
		return HostAuthMissingRevision, nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ErrHostKeyUnavailable
	}
	// Metadata only. Do not add credential bytes, their digests, paths or atime.
	// Reading the file may change atime but must never invalidate this revision.
	metadata := make([]byte, 32)
	binary.BigEndian.PutUint64(metadata[0:8], uint64(stat.Dev))
	binary.BigEndian.PutUint64(metadata[8:16], uint64(stat.Ino))
	binary.BigEndian.PutUint64(metadata[16:24], uint64(info.Size()))
	binary.BigEndian.PutUint64(metadata[24:32], uint64(info.ModTime().UnixNano()))
	digest := sha256.Sum256(metadata)
	return hex.EncodeToString(digest[:]), nil
}

func openHostKeyLock(root *os.Root, name string) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, ErrHostKeyUnavailable
	}
	if err == nil && !before.Mode().IsRegular() {
		return nil, ErrHostKeyUnavailable
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|unix.O_NONBLOCK, 0o600)
	if errors.Is(err, os.ErrExist) {
		file, err = root.OpenFile(name, os.O_RDWR|unix.O_NONBLOCK, 0)
	}
	if err != nil {
		return nil, ErrHostKeyUnavailable
	}
	if verifyHostKeyFile(root, name, file) != nil {
		file.Close()
		return nil, ErrHostKeyUnavailable
	}
	if file.Chmod(0o600) != nil {
		file.Close()
		return nil, ErrHostKeyUnavailable
	}
	return file, nil
}

func lockHostKeyFile(ctx context.Context, file *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return ErrHostKeyUnavailable
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
