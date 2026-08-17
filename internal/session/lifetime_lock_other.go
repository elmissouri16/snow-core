//go:build !darwin && !linux

package session

import "os"

// Snow currently supports macOS and Linux. Keep unsupported targets buildable;
// callers still retain pinned-root and identity validation there, but cannot
// provide the cross-process lifetime lease.
func lockSessionShared(*os.File) error       { return nil }
func tryLockSessionExclusive(*os.File) error { return nil }
func unlockSessionFile(*os.File)             {}
