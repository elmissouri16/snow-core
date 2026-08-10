//go:build !windows

package builtin

import "syscall"

func makeTestFIFO(path string) (bool, error) { return true, syscall.Mkfifo(path, 0o600) }
