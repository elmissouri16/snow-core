//go:build windows

package builtin

import "testing"

func TestWindowsPathAliasRejection(t *testing.T) {
	for _, value := range []string{`C:relative`, `C:\tmp\file.txt:secret`, `\\.\PhysicalDrive0`, `\\?\C:\tmp\file`, `\\?\UNC\server\share\file`, `\??\C:\tmp\file`, `//./PhysicalDrive0`, `//?/C:/tmp/file`, `//?/UNC/server/share/file`, `/??/C:/tmp/file`, `\\??/C:/tmp/file`, `C:\tmp\NUL.txt`, `C:\tmp\NUL.foo.bar`, `C:\tmp\CONIN$`, `C:\tmp\CONOUT$.txt`, `C:\tmp\COM¹.log`, `C:\tmp\LPT³`, `C:\tmp\trail.`, `\\?\GLOBALROOT\Device\HarddiskVolume1`} {
		if err := validatePlatformPath(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, value := range []string{`C:\work\file.txt`, `\\server\share\dir\file.txt`} {
		if err := validatePlatformPath(value); err != nil {
			t.Errorf("rejected %q: %v", value, err)
		}
	}
}
