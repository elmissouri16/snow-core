//go:build windows

package builtin

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var replaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

func renameReplace(from, to string) error {
	fromp, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	top, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	if _, err := os.Stat(to); err == nil {
		// ReplaceFile preserves the replaced file's ACLs and other metadata while
		// atomically installing the same-directory temporary file.
		r1, _, callErr := replaceFileW.Call(
			uintptr(unsafePointer(top)),
			uintptr(unsafePointer(fromp)),
			0,
			1, // REPLACEFILE_WRITE_THROUGH
			0,
			0,
		)
		if r1 == 0 {
			if callErr != nil && callErr != syscall.Errno(0) {
				return callErr
			}
			return syscall.EINVAL
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	// A destination that does not exist is a create, not a replacement.
	return windows.MoveFileEx(fromp, top, windows.MOVEFILE_WRITE_THROUGH)
}

// unsafePointer is isolated here so the Windows-only syscall glue stays small.
func unsafePointer[T any](p *T) unsafe.Pointer { return unsafe.Pointer(p) }
