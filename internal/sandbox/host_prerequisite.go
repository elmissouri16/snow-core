package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const persistentDiskFormatter = "mkfs.ext4"

var homebrewE2FSProgsDirs = []string{
	"/opt/homebrew/opt/e2fsprogs/sbin",
	"/usr/local/opt/e2fsprogs/sbin",
}

// persistentDiskPrerequisiteChecker is implemented by the production launcher.
// Test launchers intentionally omit it because they do not boot real disks.
type persistentDiskPrerequisiteChecker interface {
	checkPersistentDiskPrerequisite() error
}

func (osLauncher) checkPersistentDiskPrerequisite() error {
	return checkPersistentDiskPrerequisite(runtime.GOOS, os.Environ(), homebrewE2FSProgsDirs)
}

func checkPersistentDiskPrerequisite(goos string, environ, fallbackDirs []string) error {
	if goos != "darwin" {
		return nil
	}
	if _, ok := findExecutableInPath(persistentDiskFormatter, environmentPath(environ), fallbackDirs); ok {
		return nil
	}
	return errors.New(`sandbox: persistent smolvm disks on macOS require mkfs.ext4; install it with "brew install e2fsprogs" (or put its sbin directory on PATH) and retry`)
}

// smolVMProcessEnvironment makes Homebrew's keg-only e2fsprogs binaries visible
// to smolvm without changing Snow's process environment. smolvm uses mkfs.ext4
// to create persistent storage and overlay files; without it, the first boot can
// appear successful while a later boot sees an empty image store.
func smolVMProcessEnvironment() []string {
	return smolVMProcessEnvironmentFor(runtime.GOOS, os.Environ(), homebrewE2FSProgsDirs)
}

func smolVMProcessEnvironmentFor(goos string, environ, fallbackDirs []string) []string {
	if goos != "darwin" {
		return append([]string(nil), environ...)
	}
	currentPath := environmentPath(environ)
	formatter, ok := findExecutableInPath(persistentDiskFormatter, currentPath, fallbackDirs)
	if !ok {
		return environ
	}
	dir := filepath.Dir(formatter)
	if pathContainsDir(currentPath, dir) {
		return environ
	}
	updatedPath := dir
	if currentPath != "" {
		updatedPath += string(os.PathListSeparator) + currentPath
	}
	return replaceEnvironmentValue(environ, "PATH", updatedPath)
}

func findExecutableInPath(name, currentPath string, fallbackDirs []string) (string, bool) {
	dirs := append([]string(nil), filepath.SplitList(currentPath)...)
	dirs = append(dirs, fallbackDirs...)
	seen := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, true
		}
	}
	return "", false
}

func environmentPath(environ []string) string {
	for _, entry := range environ {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			return value
		}
	}
	return ""
}

func pathContainsDir(value, target string) bool {
	target = filepath.Clean(target)
	for _, dir := range filepath.SplitList(value) {
		if dir != "" && filepath.Clean(dir) == target {
			return true
		}
	}
	return false
}

func replaceEnvironmentValue(environ []string, name, value string) []string {
	prefix := name + "="
	out := append([]string(nil), environ...)
	for i, entry := range out {
		if strings.HasPrefix(entry, prefix) {
			out[i] = prefix + value
			return out
		}
	}
	return append(out, prefix+value)
}
