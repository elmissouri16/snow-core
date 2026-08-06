package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrite_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(context.Background(), argsFor(t, map[string]any{"path": "out.txt", "content": "hello"}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want hello", string(data))
	}
}

func TestWrite_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(context.Background(), argsFor(t, map[string]any{"path": filepath.Join("deep", "nested", "f.txt"), "content": "x"}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	if _, err := os.Stat(filepath.Join(dir, "deep", "nested", "f.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(context.Background(), argsFor(t, map[string]any{"path": file, "content": "new"}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "new" {
		t.Errorf("content = %q, want new", string(data))
	}
}

func TestWrite_MissingPath(t *testing.T) {
	dir := t.TempDir()
	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(context.Background(), argsFor(t, map[string]any{"content": "x"}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError {
		t.Fatal("expected error for missing path")
	}
}

func TestWrite_EscapeDenied(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(context.Background(), argsFor(t, map[string]any{"path": filepath.Join(outside, "x.txt"), "content": "x"}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError {
		t.Fatal("expected escape rejection")
	}
}

func TestWrite_ResultMentionsBytes(t *testing.T) {
	dir := t.TempDir()
	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(context.Background(), argsFor(t, map[string]any{"path": "f.txt", "content": strings.Repeat("y", 5)}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "5 bytes") {
		t.Errorf("result should mention byte count: %q", res.Content[0].Text)
	}
}
