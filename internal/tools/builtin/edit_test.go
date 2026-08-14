package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/tools"
)

func TestEdit_UniqueReplace(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("foo bar foo baz"), 0o600); err != nil {
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
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("edited file mode = %v", info.Mode().Perm())
	}
	details, ok := res.Details.(tools.DiffDetails)
	if !ok {
		t.Fatalf("details = %T, want tools.DiffDetails", res.Details)
	}
	for _, want := range []string{"-1 foo bar foo baz", "+1 foo BAR foo baz"} {
		if !strings.Contains(details.Diff, want) {
			t.Errorf("diff %q missing %q", details.Diff, want)
		}
	}
}

func TestEdit_IdenticalReplacementDoesNotReplaceFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "same.txt")
	if err := os.WriteFile(file, []byte("keep this text"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}

	e := NewEdit(NewPathGuard([]string{dir}, dir))
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": file, "old_str": "this", "new_str": "this"}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "No changes needed") {
		t.Fatalf("result = %q, want no-change message", res.Content[0].Text)
	}
	after, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("identical edit replaced the file inode")
	}
	if res.Details != nil {
		t.Fatalf("details = %#v, want nil for identical edit", res.Details)
	}
}

func TestEditDiffUsesContextAndMarkers(t *testing.T) {
	before := "line 1\nline 2\nline 3\nline 4\nold line\nline 6\nline 7\nline 8\nline 9\nline 10"
	after := strings.Replace(before, "old line", "new line", 1)
	got := editDiff(before, after, "old line", "new line", false)
	for _, want := range []string{"...", " 2 line 2", "-5 old line", "+5 new line", " 8 line 8"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff %q missing %q", got, want)
		}
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

func TestEditAcceptsNilContext(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := NewEdit(NewPathGuard([]string{dir}, dir)).Run(nil, argsFor(t, map[string]any{"path": file, "old_str": "before", "new_str": "after"}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("nil-context edit failed: %+v", res)
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
