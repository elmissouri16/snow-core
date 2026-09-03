package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchHonorsNestedGitignoreNegationHiddenAndBypass(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"logs", "src", ".hidden", "vendor", ".git"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{"logs/a.log": "needle", "logs/keep.log": "needle", "src/a.tmp": "needle", "src/keep.tmp": "needle", ".hidden/a.txt": "needle", "vendor/a.txt": "needle", ".git/a.txt": "needle"}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n!logs/keep.log\n"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "src", ".gitignore"), []byte("*.tmp\n!keep.tmp\n"), 0o600)
	host := stubHost{cwd: root, roots: []string{root}}
	g := NewGlob(NewPathGuard([]string{root}, root))
	result, _ := g.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "**/*"}), host)
	out := result.Content[0].Text
	for _, want := range []string{"logs/keep.log", "src/keep.tmp"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s: %q", want, out)
		}
	}
	for _, unwanted := range []string{"logs/a.log", "src/a.tmp", ".hidden/a.txt", "vendor/a.txt", ".git/a.txt"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("leaked %s: %q", unwanted, out)
		}
	}
	bypass, _ := g.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "**/*", "hidden": true, "include_ignored": true}), host)
	bypassOut := bypass.Content[0].Text
	for _, want := range []string{"logs/a.log", ".hidden/a.txt", "vendor/a.txt"} {
		if !strings.Contains(bypassOut, want) {
			t.Fatalf("bypass missing %s: %q", want, bypassOut)
		}
	}
	if strings.Contains(bypassOut, ".git/a.txt") {
		t.Fatalf("hard .git exclusion bypassed: %q", bypassOut)
	}
}

func TestSearchDirectoryOnlyDoesNotHideRegularFileAndSubtreeUsesAncestorRules(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("cache/\n*.tmp\n"), 0o600)
	_ = os.WriteFile(filepath.Join(sub, "cache"), []byte("keep"), 0o600)
	_ = os.WriteFile(filepath.Join(sub, "hidden.tmp"), []byte("hide"), 0o600)
	_ = os.WriteFile(filepath.Join(sub, "visible.txt"), []byte("show"), 0o600)
	result, _ := NewGlob(NewPathGuard([]string{root}, root)).Run(context.Background(), searchArgs(t, map[string]any{"pattern": "*", "path": "sub"}), stubHost{cwd: root, roots: []string{root}})
	out := result.Content[0].Text
	if !strings.Contains(out, "cache") || !strings.Contains(out, "visible.txt") || strings.Contains(out, "hidden.tmp") {
		t.Fatalf("%q", out)
	}
}

func TestSearchEscapesAnchorsAndForcedExcludeWins(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"!literal", "#literal", "hash#", "trail ", "root.txt", "sub/root.txt", "keep.txt", "drop.txt"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("\\!literal\n\\#literal\ntrail\\ \n/root.txt\n**/drop.txt\n!keep.txt\n"), 0o600)
	host := stubHost{cwd: root, roots: []string{root}}
	result, _ := NewGlob(NewPathGuard([]string{root}, root)).Run(context.Background(), searchArgs(t, map[string]any{"pattern": "**/*", "exclude": []string{"keep.txt"}}), host)
	out := result.Content[0].Text
	lines := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		lines[line] = true
	}
	for _, hidden := range []string{"!literal", "#literal", "trail ", "root.txt", "drop.txt", "keep.txt"} {
		if lines[hidden] {
			t.Fatalf("forced/ignore rule leaked %q in %q", hidden, out)
		}
	}
	if !strings.Contains(out, "sub/root.txt") || !strings.Contains(out, "hash#") {
		t.Fatalf("anchored/literal result=%q", out)
	}
}

func TestSearchExplicitHardRootsAndIgnoreFileTypes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, ".git", "config"), []byte("needle"), 0o600)
	host := stubHost{cwd: root, roots: []string{root}}
	grep := NewGrep(NewPathGuard([]string{root}, root))
	result, _ := grep.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "needle", "path": ".git/config", "hidden": true, "include_ignored": true}), host)
	if result.IsError || !strings.Contains(result.Content[0].Text, "no matches") {
		t.Fatalf("explicit .git file leaked: %+v", result)
	}
	result, _ = grep.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "needle", "path": ".git", "hidden": true, "include_ignored": true}), host)
	if result.IsError || !strings.Contains(result.Content[0].Text, "no matches") {
		t.Fatalf("explicit .git directory leaked: %+v", result)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	_ = os.WriteFile(outside, []byte("needle"), 0o600)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(outside, alias); err == nil {
		bad, _ := grep.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "needle", "path": "alias"}), host)
		if !bad.IsError {
			t.Fatal("explicit symlink root accepted")
		}
	}
	inside := filepath.Join(root, "inside")
	_ = os.MkdirAll(inside, 0o755)
	_ = os.WriteFile(filepath.Join(inside, "file.txt"), []byte("needle"), 0o600)
	aliasDir := filepath.Join(root, "alias-dir")
	if err := os.Symlink(inside, aliasDir); err == nil {
		bad, _ := grep.Run(context.Background(), searchArgs(t, map[string]any{"pattern": "needle", "path": filepath.Join("alias-dir", "file.txt")}), host)
		if !bad.IsError {
			t.Fatal("explicit root with symlink ancestor accepted")
		}
	}
	// A symlinked ignore file is ignored rather than followed outside the root.
	_ = os.Remove(filepath.Join(root, ".gitignore"))
	ignoreOutside := filepath.Join(t.TempDir(), "ignore")
	_ = os.WriteFile(ignoreOutside, []byte("visible.txt\n"), 0o600)
	if err := os.Symlink(ignoreOutside, filepath.Join(root, ".gitignore")); err == nil {
		_ = os.WriteFile(filepath.Join(root, "visible.txt"), []byte("x"), 0o600)
		listed, _ := NewGlob(NewPathGuard([]string{root}, root)).Run(context.Background(), searchArgs(t, map[string]any{"pattern": "*.txt"}), host)
		if !strings.Contains(listed.Content[0].Text, "visible.txt") {
			t.Fatalf("symlinked ignore file was followed: %+v", listed)
		}
	}
}

func TestSearchPerCallExclude(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o600)
	result, _ := NewGlob(NewPathGuard([]string{root}, root)).Run(context.Background(), searchArgs(t, map[string]any{"pattern": "*", "exclude": []string{"*.txt"}}), stubHost{cwd: root, roots: []string{root}})
	out := result.Content[0].Text
	if !strings.Contains(out, "a.go") || strings.Contains(out, "a.txt") {
		t.Fatalf("%q", out)
	}
}
