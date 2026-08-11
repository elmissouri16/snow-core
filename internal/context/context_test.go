package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPreambleIsEmbeddedMarkdown(t *testing.T) {
	if !strings.Contains(DefaultPreamble, "You are snow") {
		t.Fatalf("embedded preamble = %q", DefaultPreamble)
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
