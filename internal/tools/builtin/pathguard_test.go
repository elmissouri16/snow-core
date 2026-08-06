package builtin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// resolveForTest mirrors the guard's symlink resolution for expectations.
func resolveForTest(t *testing.T, p string) string {
	t.Helper()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	// Closest existing ancestor fallback (same logic as evalWithAncestors).
	if _, err := os.Lstat(p); err != nil {
		dir := filepath.Dir(p)
		base := filepath.Base(p)
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, base)
		}
	}
	return filepath.Clean(p)
}

func TestNewPathGuard_ResolveRelative(t *testing.T) {
	dir := t.TempDir()
	g := NewPathGuard([]string{dir}, dir)

	got, err := g.Resolve("file.txt")
	if err != nil {
		t.Fatalf("Resolve(relative) error: %v", err)
	}
	want := resolveForTest(t, filepath.Join(dir, "file.txt"))
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

func TestNewPathGuard_ResolveNested(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	g := NewPathGuard([]string{dir}, dir)

	got, err := g.Resolve(filepath.Join("a", "b", "c.txt"))
	if err != nil {
		t.Fatalf("Resolve nested error: %v", err)
	}
	if !strings.HasSuffix(got, string(filepath.Separator)+"a"+string(filepath.Separator)+"b"+string(filepath.Separator)+"c.txt") {
		t.Errorf("Resolve = %q, want suffix a/b/c.txt", got)
	}
}

func TestNewPathGuard_RejectsOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	g := NewPathGuard([]string{dir}, dir)

	cases := []string{
		filepath.Join(dir, "..", "escaped.txt"),
		filepath.Join(dir, "..", filepath.Base(outside), "x.txt"),
		outside, // absolute path outside root
	}
	for _, c := range cases {
		if _, err := g.Resolve(c); err == nil {
			t.Errorf("Resolve(%q) expected error", c)
		}
	}
}

func TestNewPathGuard_RejectsEmpty(t *testing.T) {
	g := NewPathGuard([]string{t.TempDir()}, t.TempDir())
	if _, err := g.Resolve(""); err == nil {
		t.Error("Resolve(empty) expected error")
	}
	if _, err := g.Resolve("   "); err == nil {
		t.Error("Resolve(whitespace) expected error")
	}
}

func TestNewPathGuard_PrefixComponentBoundary(t *testing.T) {
	// /root/foo2 must NOT be inside /root/foo.
	dir := t.TempDir()
	root := filepath.Join(dir, "foo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+"2", 0o755); err != nil {
		t.Fatal(err)
	}
	g := NewPathGuard([]string{root}, dir)

	if _, err := g.Resolve(root + "2" + string(filepath.Separator) + "x"); err == nil {
		t.Error("prefix sibling accepted, want rejection")
	}
	if _, err := g.Resolve(filepath.Join(root, "x")); err != nil {
		t.Errorf("inside root rejected: %v", err)
	}
}

func TestNewPathGuard_SymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests skipped on windows")
	}
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	g := NewPathGuard([]string{dir}, dir)
	if _, err := g.Resolve(filepath.Join("link", "secret.txt")); err == nil {
		t.Error("symlink escape accepted, want rejection")
	}
}

func TestNewPathGuard_SymlinkInsideRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests skipped on windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	g := NewPathGuard([]string{dir}, dir)
	got, err := g.Resolve(filepath.Join("alias", "f.txt"))
	if err != nil {
		t.Fatalf("symlink inside root rejected: %v", err)
	}
	want := resolveForTest(t, filepath.Join(real, "f.txt"))
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

func TestNewPathGuard_SymlinkedParentForNewFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests skipped on windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	g := NewPathGuard([]string{dir}, dir)
	// Target file does not exist yet; guard must resolve via the symlinked parent.
	got, err := g.Resolve(filepath.Join("alias", "new.txt"))
	if err != nil {
		t.Fatalf("Resolve through symlinked parent for new file: %v", err)
	}
	want := resolveForTest(t, filepath.Join(real, "new.txt"))
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

func TestNewPathGuard_Unicode(t *testing.T) {
	dir := t.TempDir()
	name := "héllo wörld 日本語.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := NewPathGuard([]string{dir}, dir)
	got, err := g.Resolve(name)
	if err != nil {
		t.Fatalf("unicode path rejected: %v", err)
	}
	if filepath.Base(got) != name {
		t.Errorf("Resolve base = %q, want %q", filepath.Base(got), name)
	}
}

func TestNewPathGuard_EmptyRootsDeniesAll(t *testing.T) {
	dir := t.TempDir()
	g := NewPathGuard(nil, dir)
	if _, err := g.Resolve(filepath.Join(dir, "x.txt")); err == nil {
		t.Error("empty roots allowed a path, want rejection")
	}
}
