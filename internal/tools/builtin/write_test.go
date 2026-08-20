package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/tools"
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
	details, ok := res.Details.(tools.DiffDetails)
	if !ok {
		t.Fatalf("details = %T, want tools.DiffDetails", res.Details)
	}
	if !strings.Contains(details.Diff, "-1 old") || !strings.Contains(details.Diff, "+1 new") {
		t.Fatalf("write diff = %q", details.Diff)
	}
}

func TestWrite_IdenticalContentDoesNotReplaceFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "same.txt")
	if err := os.WriteFile(file, []byte("unchanged"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}

	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(context.Background(), argsFor(t, map[string]any{"path": file, "content": "unchanged"}), stubHost{cwd: dir, roots: []string{dir}})
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
		t.Fatal("identical write replaced the file inode")
	}
	if res.Details != nil {
		t.Fatalf("details = %#v, want nil for identical write", res.Details)
	}
}

func TestWrite_LargeOverwriteSkipsPreview(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "large.txt")
	if err := os.WriteFile(file, []byte(strings.Repeat("x", maxDiffInputBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(context.Background(), argsFor(t, map[string]any{"path": file, "content": "small"}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	if res.Details != nil {
		t.Fatalf("details = %#v, want nil when existing file exceeds preview limit", res.Details)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "small" {
		t.Fatalf("content = %q, want small", data)
	}
}

func TestWrite_LargeIdenticalContentDoesNotReplaceFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "large-same.txt")
	content := strings.Repeat("x", maxDiffInputBytes+1)
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}

	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(context.Background(), argsFor(t, map[string]any{"path": file, "content": content}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	after, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("identical large write replaced the file inode")
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

func TestWrite_PreservesExistingMode(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "mode.txt")
	if err := os.WriteFile(file, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(context.Background(), argsFor(t, map[string]any{"path": file, "content": "new"}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestWrite_CancelLeavesExistingFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "cancel.txt")
	if err := os.WriteFile(file, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := NewWrite(NewPathGuard([]string{dir}, dir))
	res, _ := w.Run(ctx, argsFor(t, map[string]any{"path": file, "content": "new"}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError {
		t.Fatal("cancelled write should fail")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("cancelled write changed destination to %q", data)
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
