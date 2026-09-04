package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
)

func TestSearchIgnoreCacheBoundsPreserveRules(t *testing.T) {
	for _, ruleCount := range []int{0, 60, maxSearchCachedRuleSlots + 1} {
		t.Run(fmt.Sprintf("rules-%d", ruleCount), func(t *testing.T) {
			root := t.TempDir()
			matcher := newSearchIgnoreMatcher(root, searchWalkOptions{Policy: config.DefaultSearchPolicy()}, nil)
			rules := make([]searchIgnoreRule, ruleCount)
			for i := range rules {
				rules[i], _ = parseSearchIgnoreRule("", fmt.Sprintf("*.extension-%d", i))
			}
			matcher.cache[filepath.Join(root, ".gitignore")] = rules
			for i := range maxSearchCachedDirectories + 2 {
				file := filepath.Join(root, fmt.Sprintf("dir-%d", i), "file.go")
				for range 2 {
					got := matcher.rulesFor(file, false)
					if len(got) != len(rules) {
						t.Fatalf("cache saturation lost rules: got %d, want %d", len(got), len(rules))
					}
					for j, rule := range got {
						if rule.pattern != rules[j].pattern {
							t.Fatal("cache changed rule precedence")
						}
					}
				}
			}
			retained := 0
			for _, rules := range matcher.directoryRules {
				retained += cap(rules)
			}
			if retained > maxSearchCachedRuleSlots || len(matcher.directoryRules) > maxSearchCachedDirectories {
				t.Fatalf("cache exceeded bounds: %d slots, %d directories", retained, len(matcher.directoryRules))
			}
			if ruleCount == 0 && len(matcher.directoryRules) != maxSearchCachedDirectories {
				t.Fatal("empty-rule directories did not exercise the directory limit")
			}
			if ruleCount == 60 && (retained == 0 || len(matcher.directoryRules) >= maxSearchCachedDirectories) {
				t.Fatal("rule-slot limit was not exercised before the directory limit")
			}
			if ruleCount > maxSearchCachedRuleSlots && len(matcher.directoryRules) != 0 {
				t.Fatal("oversized rule list was retained")
			}
		})
	}
}

func TestSearchIgnoreCacheIsScopedToOneSearch(t *testing.T) {
	root := t.TempDir()
	guard := NewPathGuard([]string{root}, root)
	defer guard.Close()
	opts := searchWalkOptions{Policy: config.DefaultSearchPolicy()}
	ignorePath := filepath.Join(root, ".gitignore")
	for _, pattern := range []string{"first.go", "second.go"} {
		if err := os.WriteFile(ignorePath, []byte(pattern+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		matcher := newSearchIgnoreMatcher(root, opts, guard)
		for range 2 {
			for _, name := range []string{"first.go", "second.go"} {
				if got := matcher.ignore(filepath.Join(root, name), false); got != (name == pattern) {
					t.Fatalf("new search used stale rule: pattern=%q file=%q ignored=%v", pattern, name, got)
				}
			}
		}
	}
}

func TestCompiledSearchIgnoreRules(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		dir, want     bool
	}{
		{"*.go", "src/main.go", false, true},
		{"[", "src/main.go", false, false},
		{"/root.txt", "sub/root.txt", false, false},
		{"/root.txt", "root.txt", false, true},
		{"cache/", "cache", false, false},
		{"cache/", "cache", true, true},
		{"cache/", "cache/file.txt", false, true},
		{"src/cache/", "src/cache/nested", true, true},
		{"src/**/generated/", "src/a/generated/file.txt", false, true},
		{`\#literal`, "#literal", false, true},
		{`\!literal`, "!literal", false, true},
		{"**/界.txt", "src/界.txt", false, true},
	} {
		rule, ok := parseSearchIgnoreRule("", tc.pattern)
		if !ok || rule.matches(tc.path, tc.dir) != tc.want {
			t.Errorf("pattern=%q path=%q dir=%v: want %v", tc.pattern, tc.path, tc.dir, tc.want)
		}
	}
}
