//go:build unix

package builtin

import (
	"os"
	"syscall"
)

const rootedReadOnlyFlags = os.O_RDONLY | syscall.O_NONBLOCK
