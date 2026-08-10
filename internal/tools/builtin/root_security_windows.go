//go:build windows

package builtin

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

var reOpenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

// preserveRootedReplacementSecurity copies the destination DACL through rooted
// file handles before rename. The temporary otherwise inherits its directory's
// ACL, which can be broader than an existing sensitive file's ACL.
func preserveRootedReplacementSecurity(root *os.Root, targetName string, temp *os.File) error {
	target, err := root.OpenFile(targetName, os.O_RDONLY, 0)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer target.Close()

	var descriptor *windows.SECURITY_DESCRIPTOR
	var securityErr error
	targetRaw, err := target.SyscallConn()
	if err != nil {
		return err
	}
	if err := targetRaw.Control(func(fd uintptr) {
		descriptor, securityErr = windows.GetSecurityInfo(
			windows.Handle(fd), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION,
		)
	}); err != nil {
		return err
	}
	if securityErr != nil {
		return securityErr
	}

	securityInformation := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED != 0 {
		securityInformation |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	} else {
		securityInformation |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	}

	tempRaw, err := temp.SyscallConn()
	if err != nil {
		return err
	}
	if err := tempRaw.Control(func(fd uintptr) {
		// os.OpenFile's generic read/write access does not include WRITE_DAC.
		// ReOpenFile obtains that right on the same already-rooted inode, avoiding
		// a name-based reopen and its path race.
		reopened, _, callErr := reOpenFile.Call(
			fd,
			uintptr(windows.WRITE_DAC),
			uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE),
			0,
		)
		if windows.Handle(reopened) == windows.InvalidHandle {
			if callErr != nil && callErr != syscall.Errno(0) {
				securityErr = callErr
			} else {
				securityErr = syscall.EINVAL
			}
			return
		}
		handle := windows.Handle(reopened)
		defer windows.CloseHandle(handle)
		securityErr = windows.SetKernelObjectSecurity(
			handle, securityInformation, descriptor,
		)
	}); err != nil {
		return err
	}
	return securityErr
}
