package builtin

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
)

func searchBenchmarkFixture(b *testing.B) (string, *PathGuard, []string) {
	b.Helper()
	root := b.TempDir()
	var rules strings.Builder
	for i := range 30 {
		fmt.Fprintf(&rules, "ignored-%d/\n*.extension-%d\n", i, i)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(rules.String()), 0600); err != nil {
		b.Fatal(err)
	}
	var paths []string
	for i := range 50 {
		dir := filepath.Join(root, "src", fmt.Sprintf("package-%d", i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			b.Fatal(err)
		}
		for j := range 40 {
			path := filepath.Join(dir, fmt.Sprintf("file-%d.go", j))
			if err := os.WriteFile(path, []byte("package sample\n"), 0600); err != nil {
				b.Fatal(err)
			}
			paths = append(paths, path)
		}
	}
	guard := NewPathGuard([]string{root}, root)
	b.Cleanup(func() { guard.Close() })
	return root, guard, paths
}

func BenchmarkSearchIgnore2000(b *testing.B) {
	root, guard, paths := searchBenchmarkFixture(b)
	opts := searchWalkOptions{Policy: config.DefaultSearchPolicy()}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		matcher := newSearchIgnoreMatcher(root, opts, guard)
		for _, path := range paths {
			if matcher.ignore(path, false) {
				b.Fatal(path)
			}
		}
	}
}

func BenchmarkGlob2000(b *testing.B) {
	root, guard, _ := searchBenchmarkFixture(b)
	tool := NewGlob(guard)
	tool.MaxResults = 3000
	host := stubHost{cwd: root, roots: []string{root}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := tool.Run(b.Context(), []byte(`{"pattern":"**/*.go"}`), host)
		if err != nil || result.IsError || strings.Count(result.Content[0].Text, "\n") != 2000 {
			b.Fatal(err, result)
		}
	}
}

func BenchmarkReadSearchLines100000(b *testing.B) {
	wire := strings.Repeat(strings.Repeat("a", 99)+"\n", 100000)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		reader := bufio.NewReaderSize(strings.NewReader(wire), 32*1024)
		for range 100000 {
			line, oversized, err := readBoundedSearchLine(b.Context(), reader, maxSearchLineBytes)
			if len(line) != 100 || oversized || err != nil {
				b.Fatal(len(line), oversized, err)
			}
		}
	}
}

func BenchmarkGrep10MB(b *testing.B) {
	root := b.TempDir()
	wire := strings.Repeat(strings.Repeat("a", 99)+"\n", 100000)
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte(wire), 0600); err != nil {
		b.Fatal(err)
	}
	guard := NewPathGuard([]string{root}, root)
	defer guard.Close()
	tool := NewGrep(guard)
	host := stubHost{cwd: root, roots: []string{root}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := tool.Run(b.Context(), []byte(`{"pattern":"absent-needle", "path":"input.txt"}`), host)
		if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "no matches") {
			b.Fatal(err, result)
		}
	}
}
