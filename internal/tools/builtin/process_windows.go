//go:build windows

package builtin

import "syscall"

// processGroupAttr is a no-op on Windows; CommandContext already kills the
// direct child, though grandchildren may survive.
func processGroupAttr() *syscall.SysProcAttr {
	return nil
}
