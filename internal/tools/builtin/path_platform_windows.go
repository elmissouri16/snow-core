//go:build windows

package builtin

import (
	"fmt"
	"path/filepath"
	"strings"
)

var reservedWindowsNames = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true, "COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true, "LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true, "COM¹": true, "COM²": true, "COM³": true, "LPT¹": true, "LPT²": true, "LPT³": true}

func validatePlatformPath(value string) error {
	upper := strings.ToUpper(strings.ReplaceAll(value, "/", `\`))
	if strings.HasPrefix(upper, `\\.\`) || strings.HasPrefix(upper, `\\?\`) || strings.HasPrefix(upper, `\??\`) || strings.HasPrefix(upper, `\\??\`) {
		return fmt.Errorf("Windows device namespace is not allowed")
	}
	if len(value) >= 2 && value[1] == ':' && !filepath.IsAbs(value) {
		return fmt.Errorf("drive-relative Windows paths are not allowed")
	}
	volume := filepath.VolumeName(value)
	rest := strings.TrimPrefix(value, volume)
	for _, part := range strings.FieldsFunc(rest, func(r rune) bool { return r == '\\' || r == '/' }) {
		if strings.Contains(part, ":") {
			return fmt.Errorf("Windows alternate data streams are not allowed")
		}
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return fmt.Errorf("Windows trailing-dot/space aliases are not allowed")
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if reservedWindowsNames[base] {
			return fmt.Errorf("reserved Windows device name %q is not allowed", part)
		}
	}
	return nil
}

func platformPathWithin(rel string) bool {
	rel = strings.ToLower(rel)
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
