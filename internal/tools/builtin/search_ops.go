package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/tools"
)

// NewGrep creates the grep tool.
func NewGrep(guard *PathGuard) *Grep {
	return &Grep{
		guard:          guard,
		MaxOutputBytes: defaultSearchOutputBytes,
		MaxMatches:     defaultGrepMaxMatches,
		Policy:         config.DefaultSearchPolicy(),
	}
}

// Schema implements tools.Tool.
func (g *Grep) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "grep",
		Description: "Search one file or recursively search regular text files within allowed roots using a Go RE2 expression. Returns bounded path, line number, and matching text; respects ignore rules by default and never follows symlinks.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["pattern"],
			"properties": {
				"pattern": {"type": "string", "maxLength": 4096, "description": "Go regular expression (RE2) to search for"},
				"path": {"type": "string", "description": "File or directory to search. Defaults to cwd."},
				"glob": {"type": "string", "description": "Filename/path glob filter, for example '*.go' or '**/*.md'. Empty matches all files."},
				"ignore_case": {"type": "boolean", "default": false},
				"max_matches": {"type": "integer", "description": "Per-call match cap. Omitted or non-positive uses the configured cap, which defaults to 1000; this can lower but not raise that cap."},
				"hidden": {"type": "boolean", "description": "Include hidden files/directories for this call"},
				"include_ignored": {"type": "boolean", "description": "Bypass soft ignore rules (never .git or symlinks)"},
				"exclude": {"type": "array", "items":{"type":"string"}, "description":"Additional path globs to exclude"}
			}
		}`),
	}
}

// Run implements tools.Tool.
func (g *Grep) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	ctx = nonNilContext(ctx)
	var a grepArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.ErrorResult(fmt.Errorf("grep: invalid args: %w", err)), nil
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return tools.ErrorResult(fmt.Errorf("grep: pattern is required")), nil
	}
	if len(a.Pattern) > maxSearchPatternBytes {
		return tools.ErrorResult(fmt.Errorf("grep: pattern exceeds %d byte limit", maxSearchPatternBytes)), nil
	}
	if len(a.Glob) > maxSearchGlobBytes {
		return tools.ErrorResult(fmt.Errorf("grep: glob exceeds %d byte limit", maxSearchGlobBytes)), nil
	}

	pattern := a.Pattern
	if a.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("grep: invalid regex: %w", err)), nil
	}
	var filter *globMatcher
	if a.Glob != "" {
		compiled, err := compileGlob(a.Glob)
		if err != nil {
			return tools.ErrorResult(fmt.Errorf("grep: invalid glob: %w", err)), nil
		}
		filter = &compiled
	}

	searchGuard := g.guard
	if searchGuard == nil && host != nil {
		searchGuard = NewPathGuard(host.Roots(), host.CWD())
		defer searchGuard.Close()
	}
	root, guard, rootIsFile, err := resolveSearchRoot(a.Path, host, searchGuard, "grep")
	if err != nil {
		return tools.ErrorResult(err), nil
	}
	emitProgress(host, "searching text files", false, false)
	defer emitProgress(host, "grep finished", true, false)
	maxMatches := g.MaxMatches
	if maxMatches <= 0 {
		maxMatches = defaultGrepMaxMatches
	}
	if a.MaxMatches > 0 && a.MaxMatches < maxMatches {
		maxMatches = a.MaxMatches
	}
	maxOutput := g.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = defaultSearchOutputBytes
	}

	var out strings.Builder
	matches := 0
	truncated := false
	walkErr := walkSearchFiles(ctx, root, guard, searchWalkOptions{PolicyRoot: searchPolicyRoot(root, host), Policy: g.Policy, Hidden: a.Hidden, IncludeIgnored: a.IncludeIgnored, Exclude: a.Exclude}, func(path string, file *os.File) error {
		if filter != nil {
			rel := relativeSearchPath(root, path, rootIsFile)
			ok, matchErr := filter.Match(rel)
			if matchErr != nil {
				return matchErr
			}
			if !ok {
				return nil
			}
		}
		if !isTextReader(file) {
			return nil
		}
		if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
			return nil
		}

		reader := bufio.NewReaderSize(file, 32*1024)
		lineNo := 0
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			line, oversized, readErr := readBoundedSearchLine(ctx, reader, maxSearchLineBytes)
			if len(line) > 0 || oversized {
				lineNo++
				if oversized {
					entry := fmt.Sprintf("%s:%d: [skipped line larger than %d bytes]\n", relativeSearchPath(root, path, rootIsFile), lineNo, maxSearchLineBytes)
					if out.Len()+len(entry) > maxOutput {
						truncated = true
						return filepath.SkipAll
					}
					out.WriteString(entry)
				} else if re.MatchString(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")) {
					line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
					matches++
					entry := fmt.Sprintf("%s:%d: %s\n", relativeSearchPath(root, path, rootIsFile), lineNo, truncate(line, searchLinePreviewBytes))
					if out.Len()+len(entry) > maxOutput {
						truncated = true
						return filepath.SkipAll
					}
					out.WriteString(entry)
					if matches >= maxMatches {
						truncated = true
						return filepath.SkipAll
					}
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					break
				}
				return nil // a partially unreadable file is still searchable up to this point
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return tools.ErrorResult(fmt.Errorf("grep: %w", walkErr)), nil
	}
	if out.Len() == 0 {
		if matches > 0 && truncated {
			return tools.TextResult("[output truncated before a matching line could be rendered]"), nil
		}
		return tools.TextResult(fmt.Sprintf("no matches for %q", a.Pattern)), nil
	}
	if truncated {
		if matches >= maxMatches {
			out.WriteString(fmt.Sprintf("[max matches reached: %d]\n", maxMatches))
		} else {
			out.WriteString("[output truncated]\n")
		}
	}
	return tools.TextResult(out.String()), nil
}

// NewGlob creates the glob tool.
func NewGlob(guard *PathGuard) *Glob {
	return &Glob{
		guard:          guard,
		MaxOutputBytes: defaultSearchOutputBytes,
		MaxResults:     defaultGlobMaxResults,
		Policy:         config.DefaultSearchPolicy(),
	}
}

// Schema implements tools.Tool.
func (g *Glob) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "glob",
		Description: "List bounded regular-file paths matching a glob within allowed roots. Patterns are relative to path, ** matches zero or more directories, ignore rules apply by default, and symlinks are never followed.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["pattern"],
			"properties": {
				"pattern": {"type": "string", "description": "Glob pattern, for example '*.go', 'src/*.go', or '**/*_test.go'"},
				"path": {"type": "string", "description": "File or directory to search. Defaults to cwd."},
				"max_results": {"type": "integer", "description": "Per-call result cap. Omitted or non-positive uses the configured cap, which defaults to 500; this can lower but not raise that cap."},
				"hidden": {"type": "boolean", "description": "Include hidden files/directories for this call"},
				"include_ignored": {"type": "boolean", "description": "Bypass soft ignore rules (never .git or symlinks)"},
				"exclude": {"type": "array", "items":{"type":"string"}, "description":"Additional path globs to exclude"}
			}
		}`),
	}
}

// Run implements tools.Tool.
func (g *Glob) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	ctx = nonNilContext(ctx)
	var a globArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.ErrorResult(fmt.Errorf("glob: invalid args: %w", err)), nil
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return tools.ErrorResult(fmt.Errorf("glob: pattern is required")), nil
	}
	if len(a.Pattern) > maxSearchGlobBytes {
		return tools.ErrorResult(fmt.Errorf("glob: pattern exceeds %d byte limit", maxSearchGlobBytes)), nil
	}
	matcher, err := compileGlob(a.Pattern)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("glob: invalid pattern: %w", err)), nil
	}

	searchGuard := g.guard
	if searchGuard == nil && host != nil {
		searchGuard = NewPathGuard(host.Roots(), host.CWD())
		defer searchGuard.Close()
	}
	root, guard, rootIsFile, err := resolveSearchRoot(a.Path, host, searchGuard, "glob")
	if err != nil {
		return tools.ErrorResult(err), nil
	}
	emitProgress(host, "matching file paths", false, false)
	defer emitProgress(host, "glob finished", true, false)
	maxResults := g.MaxResults
	if maxResults <= 0 {
		maxResults = defaultGlobMaxResults
	}
	if a.MaxResults > 0 && a.MaxResults < maxResults {
		maxResults = a.MaxResults
	}
	maxOutput := g.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = defaultSearchOutputBytes
	}

	paths := make([]string, 0, minInt(maxResults, 128))
	outputBytes := 0
	truncated := false
	walkErr := walkSearchFiles(ctx, root, guard, searchWalkOptions{PolicyRoot: searchPolicyRoot(root, host), Policy: g.Policy, Hidden: a.Hidden, IncludeIgnored: a.IncludeIgnored, Exclude: a.Exclude}, func(path string, _ *os.File) error {
		rel := relativeSearchPath(root, path, rootIsFile)
		matched, matchErr := matcher.Match(rel)
		if matchErr != nil {
			return matchErr
		}
		if !matched {
			return nil
		}
		candidate := rel + "\n"
		if outputBytes+len(candidate) > maxOutput {
			truncated = true
			return filepath.SkipAll
		}
		paths = append(paths, rel)
		outputBytes += len(candidate)
		if len(paths) >= maxResults {
			truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return tools.ErrorResult(fmt.Errorf("glob: %w", walkErr)), nil
	}
	if len(paths) == 0 {
		return tools.TextResult(fmt.Sprintf("no files match %q", a.Pattern)), nil
	}
	sort.Strings(paths)
	var out strings.Builder
	for _, path := range paths {
		out.WriteString(path)
		out.WriteByte('\n')
	}
	if truncated {
		if len(paths) >= maxResults {
			out.WriteString(fmt.Sprintf("[max results reached: %d]\n", maxResults))
		} else {
			out.WriteString("[output truncated]\n")
		}
	}
	return tools.TextResult(out.String()), nil
}

// resolveSearchRoot resolves the user path against the host's roots. A nil
// host is supported for direct tests/callers, but then the tool must have a
// configured guard and an explicit path.
func resolveSearchRoot(path string, host tools.ToolHost, fallback *PathGuard, name string) (string, *PathGuard, bool, error) {
	guard := fallback
	root := path
	if guard == nil && host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
	}
	if guard == nil {
		if root == "" && host == nil {
			return "", nil, false, fmt.Errorf("%s: path is required when no host is provided", name)
		}
		return "", nil, false, fmt.Errorf("%s: no path guard configured", name)
	}
	if root == "" {
		root = guard.CWD()
	}
	inputPath := root
	if !filepath.IsAbs(inputPath) {
		inputPath = filepath.Join(guard.CWD(), inputPath)
	} else if host != nil && filepath.Clean(inputPath) == filepath.Clean(host.CWD()) {
		// The host cwd may use an OS-level alias such as macOS /var -> /private/var.
		// The guard already canonicalized that trusted root; inspect only user path
		// components beneath it.
		inputPath = guard.CWD()
	}
	if hasSymlinkComponent(inputPath, guard) {
		return "", nil, false, fmt.Errorf("%s: explicit roots containing symlinks are not searchable", name)
	}
	resolved, err := guard.Resolve(root)
	if err != nil {
		return "", nil, false, fmt.Errorf("%s: %w", name, err)
	}
	rooted, err := guard.rooted(resolved)
	if err != nil {
		return "", nil, false, fmt.Errorf("%s: %w", name, err)
	}
	info, err := rooted.root.Stat(rooted.name)
	if err != nil {
		return "", nil, false, fmt.Errorf("%s: %w", name, err)
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return "", nil, false, fmt.Errorf("%s: %q is not a file or directory", name, path)
	}
	return resolved, guard, info.Mode().IsRegular(), nil
}

// walkSearchFiles visits regular, non-symlink files below root while applying
// hard exclusions and one invocation-scoped hierarchical ignore matcher. Every
// enumeration and open is relative to the pinned os.Root; ambient WalkDir would
// reintroduce an ancestor-swap race before the per-file confinement check.
func walkSearchFiles(ctx context.Context, root string, guard *PathGuard, opts searchWalkOptions, fn func(path string, file *os.File) error) error {
	rootedRoot, err := guard.rooted(root)
	if err != nil {
		return err
	}
	opened, info, err := openRooted(rootedRoot.root, rootedRoot.name)
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		defer opened.Close()
		if err := ctx.Err(); err != nil {
			return err
		}
		if hardSearchExcluded(root, opts.PolicyRoot) {
			return nil
		}
		matcher := newSearchIgnoreMatcher(root, opts, guard)
		if matcher.ignore(root, false) {
			return nil
		}
		return fn(root, opened)
	}
	if !info.IsDir() {
		_ = opened.Close()
		return fmt.Errorf("%q is not a file or directory", root)
	}
	_ = opened.Close()
	if hardSearchExcluded(root, opts.PolicyRoot) {
		return nil
	}
	matcher := newSearchIgnoreMatcher(root, opts, guard)
	var walk func(string, string) error
	walk = func(absDir, rootedName string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir, dirInfo, err := openRooted(rootedRoot.root, rootedName)
		if err != nil || !dirInfo.IsDir() {
			if dir != nil {
				_ = dir.Close()
			}
			return nil
		}
		entries, err := dir.ReadDir(maxSearchDirectoryEntries + 1)
		_ = dir.Close()
		if err != nil && err != io.EOF {
			return nil
		}
		if len(entries) > maxSearchDirectoryEntries {
			return fmt.Errorf("directory %q exceeds %d entry search limit", absDir, maxSearchDirectoryEntries)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := entry.Name()
			childAbs := filepath.Join(absDir, name)
			childRooted := filepath.Join(rootedName, name)
			linkInfo, err := rootedRoot.root.Lstat(childRooted)
			if err != nil || linkInfo.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if strings.EqualFold(name, ".git") && linkInfo.IsDir() {
				continue
			}
			if linkInfo.IsDir() {
				if !matcher.ignore(childAbs, true) {
					if err := walk(childAbs, childRooted); err != nil {
						return err
					}
				}
				continue
			}
			file, openedInfo, err := openRootedRegular(rootedRoot.root, childRooted)
			if err != nil {
				continue
			}
			if openedInfo.Mode().IsRegular() && !matcher.ignore(childAbs, false) {
				visitErr := fn(childAbs, file)
				_ = file.Close()
				if visitErr != nil {
					return visitErr
				}
				continue
			}
			_ = file.Close()
		}
		return nil
	}
	return walk(root, rootedRoot.name)
}

func hasSymlinkComponent(path string, guard *PathGuard) bool {
	path = filepath.Clean(path)
	stop := ""
	roots := append(guard.Roots(), guard.CWD())
	for _, root := range roots {
		if within(root, path) && (stop == "" || len(root) > len(stop)) {
			stop = root
		}
	}
	if stop == "" {
		info, err := os.Lstat(path)
		return err == nil && info.Mode()&os.ModeSymlink != 0
	}
	for path != stop {
		info, err := os.Lstat(path)
		if err != nil {
			return false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
	return false
}

func hardSearchExcluded(path, policyRoot string) bool {
	if policyRoot == "" {
		policyRoot = filepath.Dir(path)
	}
	rel, err := filepath.Rel(policyRoot, path)
	if err != nil {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.EqualFold(part, ".git") {
			return true
		}
	}
	return false
}

func searchPolicyRoot(searchRoot string, host tools.ToolHost) string {
	if host == nil || host.CWD() == "" {
		return searchRoot
	}
	cwd, err := filepath.Abs(host.CWD())
	if err != nil {
		return searchRoot
	}
	if resolved, err := evalWithAncestors(cwd); err == nil {
		cwd = resolved
	}
	if within(cwd, searchRoot) {
		return cwd
	}
	return searchRoot
}

func relativeSearchPath(root, path string, rootIsFile bool) string {
	if rootIsFile {
		return filepath.ToSlash(filepath.Base(root))
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return filepath.ToSlash(filepath.Base(path))
	}
	return filepath.ToSlash(rel)
}

func compileGlob(pattern string) (globMatcher, error) {
	pattern = normalizeGlob(pattern)
	if pattern == "" {
		return globMatcher{}, fmt.Errorf("pattern is empty")
	}
	parts := strings.Split(pattern, "/")
	for _, segment := range parts {
		if segment == "**" {
			continue
		}
		if _, err := pathpkg.Match(segment, ""); err != nil {
			return globMatcher{}, err
		}
	}
	return globMatcher{patternParts: parts, basename: len(parts) == 1}, nil
}

// matchGlobPath supports ordinary shell-style path.Match segments plus a
// recursive ** segment. A pattern without a slash is matched against the
// basename at any search depth, which makes '*.go' useful for repository-wide
// searches. It remains as a helper for package tests and small callers;
// production tools use the compiled matcher directly.
func matchGlobPath(rel, pattern string) (bool, error) {
	matcher, err := compileGlob(pattern)
	if err != nil {
		return false, err
	}
	return matcher.Match(rel)
}

func (m globMatcher) Match(rel string) (bool, error) {
	rel = normalizeGlob(rel)
	if m.basename {
		return pathpkg.Match(m.patternParts[0], pathpkg.Base(rel))
	}
	pathParts := []string{}
	if rel != "" {
		pathParts = strings.Split(rel, "/")
	}
	memo := make(map[[2]int]bool)
	seen := make(map[[2]int]bool)
	var match func(int, int) bool
	match = func(pi, ri int) bool {
		key := [2]int{pi, ri}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true
		var ok bool
		switch {
		case pi == len(m.patternParts):
			ok = ri == len(pathParts)
		case m.patternParts[pi] == "**":
			ok = match(pi+1, ri) || (ri < len(pathParts) && match(pi, ri+1))
		case ri < len(pathParts):
			matched, err := pathpkg.Match(m.patternParts[pi], pathParts[ri])
			ok = err == nil && matched && match(pi+1, ri+1)
		}
		memo[key] = ok
		return ok
	}
	return match(0, 0), nil
}

func normalizeGlob(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "./")
	return strings.Trim(value, "/")
}

func readBoundedSearchLine(ctx context.Context, reader *bufio.Reader, maxBytes int) (string, bool, error) {
	var line []byte
	oversized := false
	for {
		if err := ctx.Err(); err != nil {
			return "", oversized, err
		}
		part, err := reader.ReadSlice('\n')
		if !oversized {
			if len(line)+len(part) > maxBytes {
				oversized = true
				line = nil
			} else {
				line = append(line, part...)
			}
		}
		if err == nil {
			return string(line), oversized, nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return string(line), oversized, err
	}
}

func isTextReader(f io.Reader) bool {
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	for _, c := range buf[:n] {
		if c == 0 {
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	prefix := s[:n]
	for len(prefix) > 0 && !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + "…"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
