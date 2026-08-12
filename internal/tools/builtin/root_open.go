package builtin

import (
	"fmt"
	"os"
)

// openRootedRegular opens first and validates the opened inode, rather than
// checking a path and opening it later. On platforms with FIFOs the nonblocking
// flag prevents a concurrent replacement from hanging the tool in open(2).
func openRooted(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	file, err := root.OpenFile(name, rootedReadOnlyFlags, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

// OpenRootedRegular opens a regular file through a pinned os.Root. It is
// exported for other built-in capability packages that need the same
// race-resistant confinement without broadening filesystem roots.
func OpenRootedRegular(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	file, info, err := openRooted(root, name)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("not a regular file")
	}
	return file, info, nil
}

func openRootedRegular(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	return OpenRootedRegular(root, name)
}
