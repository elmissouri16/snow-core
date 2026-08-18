//go:build !darwin && !linux

package mcp

import (
	"os"
	"time"
)

func lockCacheFile(*os.File, time.Duration) error { return nil }
func unlockCacheFile(*os.File)                    {}
