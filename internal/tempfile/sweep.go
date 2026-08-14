// Package tempfile provides conservative cleanup for crash-orphaned atomic-write files.
package tempfile

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SweepStale removes old regular, non-symlink files in dir whose base name
// starts with one of prefixes. It is intentionally non-recursive and
// best-effort so cleanup can never prevent application startup.
func SweepStale(dir string, prefixes []string, maxAge time.Duration) {
	if dir == "" || len(prefixes) == 0 || maxAge <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		matched := false
		for _, prefix := range prefixes {
			if prefix != "" && strings.HasPrefix(entry.Name(), prefix) {
				matched = true
				break
			}
		}
		if !matched || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(path)
	}
}
