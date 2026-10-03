//go:build !darwin && !linux

package session

import "os"

// Without reliable change-time metadata, inspecting SQLite is safer than reuse.
func inventoryChangeTime(os.FileInfo) (int64, bool) { return 0, false }
