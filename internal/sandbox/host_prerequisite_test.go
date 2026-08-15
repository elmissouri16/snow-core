package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindExecutableInPathUsesFallbackDirectory(t *testing.T) {
	dir := t.TempDir()
	formatter := filepath.Join(dir, persistentDiskFormatter)
	if err := os.WriteFile(formatter, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, ok := findExecutableInPath(persistentDiskFormatter, "/missing", []string{dir})
	if !ok || got != formatter {
		t.Fatalf("formatter = %q, ok=%v", got, ok)
	}
}

func TestFindExecutableInPathRejectsNonExecutableFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, persistentDiskFormatter), []byte("not executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := findExecutableInPath(persistentDiskFormatter, "", []string{dir}); ok {
		t.Fatalf("accepted non-executable formatter %q", got)
	}
}

func TestSmolVMProcessEnvironmentAddsKegOnlyFormatter(t *testing.T) {
	dir := t.TempDir()
	formatter := filepath.Join(dir, persistentDiskFormatter)
	if err := os.WriteFile(formatter, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	environ := smolVMProcessEnvironmentFor("darwin", []string{"HOME=/tmp/home"}, []string{dir})
	if got := environmentPath(environ); got != dir {
		t.Fatalf("PATH = %q", got)
	}
}

func TestPersistentDiskPrerequisiteRequiresFormatterOnlyOnMacOS(t *testing.T) {
	if err := checkPersistentDiskPrerequisite("darwin", []string{"PATH=/missing"}, nil); err == nil || !strings.Contains(err.Error(), "brew install e2fsprogs") {
		t.Fatalf("macOS prerequisite error = %v", err)
	}
	if err := checkPersistentDiskPrerequisite("linux", []string{"PATH=/missing"}, nil); err != nil {
		t.Fatalf("Linux prerequisite error = %v", err)
	}
}

func TestPathContainsDirUsesPathElements(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	path := strings.Join([]string{"/usr/bin", dir, "/bin"}, string(os.PathListSeparator))
	if !pathContainsDir(path, dir) {
		t.Fatalf("pathContainsDir(%q, %q) = false", path, dir)
	}
	if pathContainsDir(path, dir+"-other") {
		t.Fatalf("pathContainsDir accepted partial match")
	}
}
