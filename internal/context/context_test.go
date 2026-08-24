package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPreambleIsEmbeddedMarkdown(t *testing.T) {
	preamble := strings.Join(strings.Fields(DefaultPreamble), " ")
	for _, want := range []string{
		"You are snow",
		"Prefer read / grep / glob before bash",
		"Respect permission denials",
		"Explain briefly when done",
	} {
		if !strings.Contains(preamble, want) {
			t.Fatalf("embedded preamble missing %q: %q", want, DefaultPreamble)
		}
	}
}

func TestFindAgentsWalksParents(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// AGENTS.md in root only.
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLoader(0, false)
	files := l.FindAgents(sub)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != filepath.Join(root, "AGENTS.md") {
		t.Fatalf("wrong file: %s", files[0].Path)
	}
	if files[0].Depth != 2 {
		t.Fatalf("wrong depth: %d", files[0].Depth)
	}
}

func TestFindAgentsNearestFirst(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte("sub"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(0, false)
	files := l.FindAgents(sub)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Depth != 0 {
		t.Fatalf("nearest first, got depth %d", files[0].Depth)
	}
}

func TestAssembleIncludesPreambleAndAgents(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("use tabs"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(10000, false)
	a := l.Assemble(dir, "", "")
	rendered := a.Render()
	if !strings.Contains(rendered, "You are snow") {
		t.Fatal("preamble missing")
	}
	if !strings.Contains(rendered, "use tabs") {
		t.Fatal("AGENTS.md missing")
	}
	if !strings.Contains(rendered, "AGENTS.md") {
		t.Fatal("section title missing")
	}
}

func TestAssembleCapTruncates(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", 5000)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(1000, false)
	a := l.Assemble(dir, "", "")
	if !a.Truncated {
		t.Fatal("expected truncation flag")
	}
	if a.TotalSize > 1000 {
		t.Fatalf("total size %d exceeds cap", a.TotalSize)
	}
	if !strings.Contains(a.Render(), "truncated") {
		t.Fatal("expected truncation notice")
	}
}

func TestAssembleRejectsSymlinkedAgentsFile(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secretPath, []byte("provider-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretPath, filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	l := NewLoader(10000, false)
	if files := l.FindAgents(dir); len(files) != 0 {
		t.Fatalf("symlinked AGENTS.md discovered: %+v", files)
	}
	if rendered := l.Assemble(dir, "", "").Render(); strings.Contains(rendered, "provider-secret") {
		t.Fatal("symlink target leaked into project context")
	}
}

func TestReadProjectFileRevalidatesReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := NewLoader(10000, false).FindAgents(dir)
	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	secretPath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secretPath, []byte("provider-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretPath, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if data, _, err := readProjectFile(files[0].Path, 10000); err == nil {
		t.Fatalf("replacement read succeeded: %q", data)
	}
}

func TestReadProjectFileBoundsSparseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("begin"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 256<<20); err != nil {
		t.Skipf("sparse files unavailable: %v", err)
	}

	data, truncated, err := readProjectFile(path, 32)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(data) != 32 || !strings.HasPrefix(string(data), "begin") {
		t.Fatalf("bounded read = len %d, truncated %v, prefix %q", len(data), truncated, data[:min(len(data), 5)])
	}
}

func TestCLAUDEOffByDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude stuff"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(10000, false)
	files := l.FindAgents(dir)
	if len(files) != 0 {
		t.Fatal("CLAUDE.md should be excluded by default")
	}

	l2 := NewLoader(10000, true)
	files = l2.FindAgents(dir)
	if len(files) != 1 {
		t.Fatal("CLAUDE.md should be included when enabled")
	}
}

func TestUserInstructionsIncluded(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(10000, false)
	a := l.Assemble(dir, "", "always write tests")
	if !strings.Contains(a.Render(), "always write tests") {
		t.Fatal("user instructions missing")
	}
}
