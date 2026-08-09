package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func searchArgs(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestGrepMatchesLinesAndGlobFilters(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"main.go":           "needle\nneedle\n",
		"pkg/nested/lib.go": "NEEDLE\nother\n",
		"pkg/readme.md":     "needle in markdown\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	g := NewGrep(NewPathGuard([]string{root}, root))
	result, _ := g.Run(context.Background(), searchArgs(t, map[string]any{
		"pattern":     "needle",
		"glob":        "**/*.go",
		"ignore_case": true,
	}), stubHost{cwd: root, roots: []string{root}})
	if result.IsError {
		t.Fatalf("grep failed: %s", result.Content[0].Text)
	}
	out := result.Content[0].Text
	for _, want := range []string{"main.go:1: needle", "main.go:2: needle", "pkg/nested/lib.go:1: NEEDLE"} {
		if !strings.Contains(out, want) {
			t.Fatalf("grep output missing %q: %q", want, out)
		}
	}
	if strings.Contains(out, "readme.md") {
		t.Fatalf("glob filter leaked markdown file: %q", out)
	}
}

func TestGrepMaxMatchesAndInvalidArguments(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\nx\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := NewGrep(NewPathGuard([]string{root}, root))
	result, _ := g.Run(context.Background(), searchArgs(t, map[string]any{
		"pattern":     "x",
		"max_matches": 2,
	}), stubHost{cwd: root, roots: []string{root}})
	if result.IsError || !strings.Contains(result.Content[0].Text, "max matches reached: 2") {
		t.Fatalf("max match result = %+v", result)
	}
	bad, _ := g.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "["}), stubHost{cwd: root, roots: []string{root}})
	if !bad.IsError || !strings.Contains(bad.Content[0].Text, "invalid regex") {
		t.Fatalf("invalid regex result = %+v", bad)
	}
}

func TestGlobRecursiveAndPathPatterns(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"root.go", "src/main.go", "src/nested/test.go", "src/nested/readme.md"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := NewGlob(NewPathGuard([]string{root}, root))

	result, _ := g.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "**/*.go"}), stubHost{cwd: root, roots: []string{root}})
	if result.IsError {
		t.Fatalf("glob failed: %s", result.Content[0].Text)
	}
	out := result.Content[0].Text
	for _, want := range []string{"root.go", "src/main.go", "src/nested/test.go"} {
		if !strings.Contains(out, want) {
			t.Fatalf("glob output missing %q: %q", want, out)
		}
	}
	if strings.Contains(out, "readme.md") {
		t.Fatalf("glob matched wrong extension: %q", out)
	}

	direct, _ := g.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "src/*.go"}), stubHost{cwd: root, roots: []string{root}})
	if direct.IsError || !strings.Contains(direct.Content[0].Text, "src/main.go") || strings.Contains(direct.Content[0].Text, "nested/test.go") {
		t.Fatalf("direct glob should match only src files: %+v", direct)
	}
}

func TestGlobBoundsAndEscape(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := NewGlob(NewPathGuard([]string{root}, root))
	bounded, _ := g.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "*.txt", "max_results": 2}), stubHost{cwd: root, roots: []string{root}})
	if bounded.IsError || !strings.Contains(bounded.Content[0].Text, "max results reached: 2") {
		t.Fatalf("bounded glob = %+v", bounded)
	}
	outside := t.TempDir()
	bad, _ := g.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "*", "path": outside}), stubHost{cwd: root, roots: []string{root}})
	if !bad.IsError {
		t.Fatal("glob path escape should be denied")
	}
}

func TestSearchToolsAcceptNilContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := stubHost{cwd: root, roots: []string{root}}
	grepResult, _ := NewGrep(NewPathGuard([]string{root}, root)).Run(nil, searchArgs(t, map[string]any{"pattern": "needle"}), host)
	if grepResult.IsError {
		t.Fatalf("nil-context grep failed: %+v", grepResult)
	}
	globResult, _ := NewGlob(NewPathGuard([]string{root}, root)).Run(nil, searchArgs(t, map[string]any{"pattern": "*"}), host)
	if globResult.IsError {
		t.Fatalf("nil-context glob failed: %+v", globResult)
	}
}

func TestSearchCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := NewGlob(NewPathGuard([]string{root}, root))
	result, _ := g.Run(ctx, searchArgs(t, map[string]any{"pattern": "*"}), stubHost{cwd: root, roots: []string{root}})
	if !result.IsError || !strings.Contains(result.Content[0].Text, "canceled") {
		t.Fatalf("cancelled glob = %+v", result)
	}
}
