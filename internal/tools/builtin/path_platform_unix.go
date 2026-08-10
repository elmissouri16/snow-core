//go:build !windows

package builtin

import (
	"path/filepath"
	"strings"
)

func validatePlatformPath(string) error { return nil }
func platformPathWithin(rel string) bool {
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
