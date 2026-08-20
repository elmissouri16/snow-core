package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/tools"
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

func TestEditRejectsOversizedFileWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "large.txt")
	if err := os.WriteFile(file, []byte("123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEdit(NewPathGuard([]string{dir}, dir))
	e.MaxFileBytes = 8
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": file, "old_str": "1", "new_str": "x"}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError || !strings.Contains(res.Content[0].Text, "maximum editable size") {
		t.Fatalf("oversized edit result = %+v", res)
	}
	after, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("rejected oversized edit replaced the file")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "123456789" {
		t.Fatalf("rejected edit changed content to %q", data)
	}
}

func TestEditRejectsReplacementExpansionBeyondLimit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "expand.txt")
	if err := os.WriteFile(file, []byte("aaaaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEdit(NewPathGuard([]string{dir}, dir))
	e.MaxFileBytes = 10
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": file, "old_str": "a", "new_str": "bbb", "replace_all": true}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError || !strings.Contains(res.Content[0].Text, "replacement would exceed") {
		t.Fatalf("expanding edit result = %+v", res)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "aaaaa" {
		t.Fatalf("rejected expansion changed content to %q", data)
	}
}

func TestEditReplaceAllHonorsReplacementLimit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "matches.txt")
	if err := os.WriteFile(file, []byte("a a a a"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEdit(NewPathGuard([]string{dir}, dir))
	e.MaxReplacements = 3
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": file, "old_str": "a", "new_str": "b", "replace_all": true}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError || !strings.Contains(res.Content[0].Text, "maximum is 3") {
		t.Fatalf("replacement-limit result = %+v", res)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a a a a" {
		t.Fatalf("rejected replace_all changed content to %q", data)
	}
}

func TestEditReplaceAllAtReplacementLimitSucceeds(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "matches.txt")
	if err := os.WriteFile(file, []byte("a a a"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEdit(NewPathGuard([]string{dir}, dir))
	e.MaxReplacements = 3
	res, _ := e.Run(context.Background(), argsFor(t, map[string]any{"path": file, "old_str": "a", "new_str": "b", "replace_all": true}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("replacement-limit edit failed: %+v", res)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "b b b" {
		t.Fatalf("content = %q, want b b b", data)
	}
}

func TestBoundedEditReadLimitDoesNotOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got := boundedEditReadLimit(maxInt); got != maxInt {
		t.Fatalf("max-int read limit = %d, want %d", got, maxInt)
	}
	if got := boundedEditReadLimit(10); got != 11 {
		t.Fatalf("ordinary read limit = %d, want 11", got)
	}
}

func TestEditDiffBoundsInputMatchesAndOutput(t *testing.T) {
	tooLarge := strings.Repeat("a", maxDiffInputBytes+1)
	if got := editDiff(tooLarge, "b", "a", "b", false); got != "" {
		t.Fatalf("oversized diff preview was not omitted: %d bytes", len(got))
	}

	many := strings.Repeat("a", maxDiffEdits+1)
	if got := editDiff(many, strings.Repeat("b", len(many)), "a", "b", true); got != "" {
		t.Fatalf("high-match diff preview was not omitted: %d bytes", len(got))
	}

	before := "x" + strings.Repeat("a", 100_000)
	after := "y" + before[1:]
	got := editDiff(before, after, "x", "y", false)
	if len(got) > maxDiffPreviewBytes {
		t.Fatalf("diff preview = %d bytes, cap is %d", len(got), maxDiffPreviewBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("bounded diff preview is not valid UTF-8")
	}
	if !strings.HasSuffix(got, diffTruncationMarker) {
		t.Fatalf("bounded diff preview missing truncation marker: %q", got[len(got)-min(len(got), 80):])
	}
}
