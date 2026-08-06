package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEdit_UniqueReplace(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("foo bar foo baz"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEdit(NewPathGuard([]string{dir}, dir))
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": file, "old_str": "bar", "new_str": "BAR"}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "foo BAR foo baz" {
		t.Errorf("content = %q, want replacement", string(data))
	}
}

func TestEdit_AmbiguousOldStr(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("foo foo foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEdit(NewPathGuard([]string{dir}, dir))
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": file, "old_str": "foo", "new_str": "bar"}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(res.Content[0].Text, "appears 3 times") {
		t.Errorf("error should mention count: %q", res.Content[0].Text)
	}
}

func TestEdit_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("foo foo foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEdit(NewPathGuard([]string{dir}, dir))
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": file, "old_str": "foo", "new_str": "bar", "replace_all": true}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "bar bar bar" {
		t.Errorf("content = %q, want all replaced", string(data))
	}
}

func TestEdit_NotFound(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEdit(NewPathGuard([]string{dir}, dir))
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": file, "old_str": "zzz", "new_str": "yyy"}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError {
		t.Fatal("expected not-found error")
	}
}

func TestEdit_MissingFile(t *testing.T) {
	dir := t.TempDir()
	e := NewEdit(NewPathGuard([]string{dir}, dir))
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": filepath.Join(dir, "nope.txt"), "old_str": "a", "new_str": "b"}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError {
		t.Fatal("expected error for missing file")
	}
}

func TestEdit_EmptyOldStr(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEdit(NewPathGuard([]string{dir}, dir))
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": file, "old_str": "", "new_str": "b"}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError {
		t.Fatal("expected error for empty old_str")
	}
}
