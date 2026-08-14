//go:build unix

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// These tests change the process-global umask and must never use t.Parallel.
func TestWriteNewFileRespectsUmask(t *testing.T) {
	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)

	dir := t.TempDir()
	guard := NewPathGuard([]string{dir}, dir)
	t.Cleanup(func() { _ = guard.Close() })
	result, _ := NewWrite(guard).Run(
		context.Background(),
		argsFor(t, map[string]any{"path": "new.txt", "content": "secret"}),
		stubHost{cwd: dir, roots: []string{dir}},
	)
	if result.IsError {
		t.Fatalf("write failed: %s", result.Content[0].Text)
	}
	info, err := os.Stat(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("new file mode = %o, want 600 with umask 077", got)
	}
}

func TestWriteReplacementPreservesModeDespiteUmask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatal(err)
	}

	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)

	guard := NewPathGuard([]string{dir}, dir)
	t.Cleanup(func() { _ = guard.Close() })
	result, _ := NewWrite(guard).Run(
		context.Background(),
		argsFor(t, map[string]any{"path": path, "content": "new"}),
		stubHost{cwd: dir, roots: []string{dir}},
	)
	if result.IsError {
		t.Fatalf("write failed: %s", result.Content[0].Text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o664 {
		t.Fatalf("replacement mode = %o, want preserved 664", got)
	}
}
