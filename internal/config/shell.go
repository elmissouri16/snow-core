package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateShellProtectedPaths validates additive, global-only shell policy.
func ValidateShellProtectedPaths(paths []string) error {
	if len(paths) > 128 {
		return fmt.Errorf("config: shell_protected_paths must contain at most 128 entries")
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsRune(path, 0) {
			return fmt.Errorf("config: shell_protected_paths entries must be absolute paths of at most 4096 bytes")
		}
	}
	return nil
}
