package sandbox

import (
	"path/filepath"
	"strings"
)

func machineName(project string) string {
	base := strings.ToLower(filepath.Base(project))
	var b strings.Builder
	for _, r := range base {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	base = strings.Trim(b.String(), "-")
	if base == "" {
		base = "project"
	}
	if len(base) > 32 {
		base = base[:32]
	}
	return "snow-" + base + "-" + projectHash(project)
}
