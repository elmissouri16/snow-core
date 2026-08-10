package builtin

import (
	"bufio"
	"io"
	"path/filepath"
	"strings"

	"github.com/snow-core/snow/internal/config"
)

type searchWalkOptions struct {
	PolicyRoot     string
	Policy         config.EffectiveSearchPolicy
	Hidden         *bool
	IncludeIgnored bool
	Exclude        []string
}

type searchIgnoreRule struct {
	base     string
	pattern  string
	negate   bool
	dirOnly  bool
	anchored bool
	hasSlash bool
}

type searchIgnoreMatcher struct {
	root           string
	guard          *PathGuard
	policy         config.EffectiveSearchPolicy
	hidden         bool
	includeIgnored bool
	policyExtra    []searchIgnoreRule
	forced         []searchIgnoreRule
	cache          map[string][]searchIgnoreRule
}

func newSearchIgnoreMatcher(root string, opts searchWalkOptions, guard *PathGuard) *searchIgnoreMatcher {
	if opts.PolicyRoot != "" {
		root = opts.PolicyRoot
	}
	hidden := opts.Policy.Hidden
	if opts.Hidden != nil {
		hidden = *opts.Hidden
	}
	m := &searchIgnoreMatcher{root: root, guard: guard, policy: opts.Policy, hidden: hidden, includeIgnored: opts.IncludeIgnored, cache: map[string][]searchIgnoreRule{}}
	for _, value := range opts.Policy.Exclude {
		if rule, ok := parseSearchIgnoreRule("", value); ok {
			m.policyExtra = append(m.policyExtra, rule)
		}
	}
	for _, value := range opts.Exclude {
		if rule, ok := parseSearchIgnoreRule("", value); ok {
			rule.negate = false // per-call excludes are always additive exclusions
			m.forced = append(m.forced, rule)
		}
	}
	return m
}

func (m *searchIgnoreMatcher) ignore(path string, isDir bool) bool {
	rel, err := filepath.Rel(m.root, path)
	if err != nil {
		return true
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return false
	}
	parts := strings.Split(rel, "/")
	if !m.hidden {
		for _, part := range parts {
			if strings.HasPrefix(part, ".") && part != "." && part != ".." {
				return true
			}
		}
	}
	if m.includeIgnored {
		for _, rule := range m.forced {
			if rule.matches(rel, isDir) {
				return true
			}
		}
		return false
	}
	if isDir {
		for _, name := range m.policy.GeneratedDirs {
			if parts[len(parts)-1] == name {
				return true
			}
		}
	}
	ignored := false
	for _, rule := range m.policyExtra {
		if rule.matches(rel, isDir) {
			ignored = !rule.negate
		}
	}
	for _, rule := range m.rulesFor(path, isDir) {
		if rule.matches(rel, isDir) {
			ignored = !rule.negate
		}
	}
	// Per-call excludes are final and cannot be undone by repository negations.
	for _, rule := range m.forced {
		if rule.matches(rel, isDir) {
			return true
		}
	}
	return ignored
}

func (m *searchIgnoreMatcher) rulesFor(path string, isDir bool) []searchIgnoreRule {
	dir := path
	if !isDir {
		dir = filepath.Dir(path)
	}
	var dirs []string
	for {
		dirs = append(dirs, dir)
		if dir == m.root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || !within(m.root, parent) {
			break
		}
		dir = parent
	}
	var rules []searchIgnoreRule
	for i := len(dirs) - 1; i >= 0; i-- {
		base, _ := filepath.Rel(m.root, dirs[i])
		base = filepath.ToSlash(base)
		if base == "." {
			base = ""
		}
		if m.policy.RespectGitignore {
			rules = append(rules, m.loadIgnoreFile(filepath.Join(dirs[i], ".gitignore"), base)...)
		}
		if m.policy.RespectIgnore {
			rules = append(rules, m.loadIgnoreFile(filepath.Join(dirs[i], ".ignore"), base)...)
		}
	}
	return rules
}

func (m *searchIgnoreMatcher) loadIgnoreFile(path, base string) []searchIgnoreRule {
	if cached, ok := m.cache[path]; ok {
		return cached
	}
	m.cache[path] = nil
	if m.guard == nil {
		return nil
	}
	rooted, err := m.guard.rooted(path)
	if err != nil {
		return nil
	}
	file, opened, err := openRootedRegular(rooted.root, rooted.name)
	if err != nil || opened.Size() > 256*1024 {
		if file != nil {
			_ = file.Close()
		}
		return nil
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 256*1024+1))
	if err != nil || len(data) > 256*1024 {
		return nil
	}
	var rules []searchIgnoreRule
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	buf := make([]byte, 4096)
	scanner.Buffer(buf, 256*1024)
	for scanner.Scan() && len(rules) < 10_000 {
		if rule, ok := parseSearchIgnoreRule(base, scanner.Text()); ok {
			rules = append(rules, rule)
		}
	}
	if scanner.Err() != nil {
		return nil
	}
	m.cache[path] = rules
	return rules
}

func parseSearchIgnoreRule(base, line string) (searchIgnoreRule, bool) {
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		return searchIgnoreRule{}, false
	}
	escapedLeading := strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`)
	if escapedLeading {
		line = line[1:]
	} else if strings.HasPrefix(line, "#") {
		return searchIgnoreRule{}, false
	}
	rule := searchIgnoreRule{base: strings.Trim(base, "/")}
	if !escapedLeading && strings.HasPrefix(line, "!") {
		rule.negate = true
		line = strings.TrimPrefix(line, "!")
	}
	for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, `\ `) {
		line = strings.TrimSuffix(line, " ")
	}
	// Protect escaped trailing spaces from normalizeGlob's whitespace trim.
	const escapedSpace = "\x00"
	line = strings.ReplaceAll(line, `\ `, escapedSpace)
	if strings.HasSuffix(line, "/") {
		rule.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}
	if strings.HasPrefix(line, "/") {
		rule.anchored = true
		line = strings.TrimPrefix(line, "/")
	}
	line = normalizeGlob(line)
	line = strings.ReplaceAll(line, escapedSpace, " ")
	if line == "" {
		return searchIgnoreRule{}, false
	}
	rule.pattern = line
	rule.hasSlash = strings.Contains(line, "/")
	return rule, true
}

func (r searchIgnoreRule) matches(rootRel string, isDir bool) bool {
	candidate := rootRel
	if r.base != "" {
		prefix := r.base + "/"
		if candidate == r.base {
			candidate = ""
		} else if strings.HasPrefix(candidate, prefix) {
			candidate = strings.TrimPrefix(candidate, prefix)
		} else {
			return false
		}
	}
	if candidate == "" {
		return false
	}
	if r.dirOnly && !isDir {
		parts := strings.Split(candidate, "/")
		for i := 1; i < len(parts); i++ {
			prefix := strings.Join(parts[:i], "/")
			if ok, _ := matchGlobPath(prefix, r.pattern); ok {
				return true
			}
		}
		return false
	}
	match := false
	if r.anchored && !r.hasSlash {
		if !strings.Contains(candidate, "/") {
			match, _ = matchGlobPath(candidate, r.pattern)
		}
	} else if !r.hasSlash {
		for _, part := range strings.Split(candidate, "/") {
			if ok, _ := matchGlobPath(part, r.pattern); ok {
				match = true
				break
			}
		}
	} else {
		match, _ = matchGlobPath(candidate, r.pattern)
		if !match && r.dirOnly {
			match, _ = matchGlobPath(candidate, r.pattern+"/**")
		}
	}
	return match
}
