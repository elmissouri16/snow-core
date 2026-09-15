//go:build darwin || linux

package hostops

import (
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func supported() bool { return true }

func openChild(root *os.Root, leaf string) (*os.File, error) {
	return root.OpenFile(leaf, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
}

func statIdentity(info os.FileInfo, path string) (protocol.HostDirectoryIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() {
		return protocol.HostDirectoryIdentity{}, ErrIdentity
	}
	return protocol.HostDirectoryIdentity{Path: path, Device: strconv.FormatUint(uint64(stat.Dev), 10), Inode: strconv.FormatUint(uint64(stat.Ino), 10)}, nil
}

// validHelperDescriptors does not create os.File wrappers: a wrapper's finalizer
// alone could close an unrelated runtime fd even if no explicit Close is called.
// Fstat and F_GETFL inspect the whole pair before any ownership/flag mutation.
func validHelperDescriptors(childFD, lifeFD uintptr) bool {
	if childFD < 3 || lifeFD < 3 || childFD == lifeFD || childFD > uintptr(^uint(0)>>1) || lifeFD > uintptr(^uint(0)>>1) {
		return false
	}
	var child, life unix.Stat_t
	if unix.Fstat(int(childFD), &child) != nil || child.Mode&unix.S_IFMT != unix.S_IFDIR {
		return false
	}
	if unix.Fstat(int(lifeFD), &life) != nil || life.Mode&unix.S_IFMT != unix.S_IFIFO {
		return false
	}
	for _, fd := range []uintptr{childFD, lifeFD} {
		flags, err := unix.FcntlInt(fd, unix.F_GETFL, 0)
		if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
			return false
		}
	}
	return true
}

func enterHelperDirectory(child, life *os.File) error {
	// ExtraFiles are not automatically CLOEXEC inside the helper. Set both
	// before spawning Git: only the worker owns a liveness writer, and no Git
	// descendant should retain either private descriptor.
	unix.CloseOnExec(int(child.Fd()))
	unix.CloseOnExec(int(life.Fd()))
	flags, err := unix.FcntlInt(life.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		return ErrInvalid
	}
	info, err := life.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return ErrInvalid
	}
	return child.Chdir()
}
