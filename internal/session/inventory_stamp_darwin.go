package session

import (
	"os"
	"syscall"
)

func inventoryChangeTime(info os.FileInfo) (int64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Ctimespec.Nano(), true
}
