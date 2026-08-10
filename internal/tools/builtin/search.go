package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/tools"
)

const (
	defaultSearchOutputBytes = 256 * 1024
	defaultGrepMaxMatches    = 1000
	defaultGlobMaxResults    = 500
	searchLinePreviewBytes   = 300
)

// Grep is a pure-Go content search tool (no external ripgrep dependency).
type Grep struct {
	guard *PathGuard
	// MaxOutputBytes caps combined result output.
	MaxOutputBytes int
	// MaxMatches bounds total matches returned.
	MaxMatches int
	Policy     config.EffectiveSearchPolicy
}

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
		Description: "Search text files with a regular expression within allowed roots. Returns matching lines with paths and line numbers.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["pattern"],
			"properties": {
				"pattern": {"type": "string", "description": "Go regular expression (RE2) to search for"},
				"path": {"type": "string", "description": "File or directory to search. Defaults to cwd."},
				"glob": {"type": "string", "description": "Filename/path glob filter, for example '*.go' or '**/*.md'. Empty matches all files."},
				"ignore_case": {"type": "boolean", "default": false},
				"max_matches": {"type": "integer", "description": "Maximum matching lines to return (default 1000)"},
				"hidden": {"type": "boolean", "description": "Include hidden files/directories for this call"},
				"include_ignored": {"type": "boolean", "description": "Bypass soft ignore rules (never .git or symlinks)"},
				"exclude": {"type": "array", "items":{"type":"string"}, "description":"Additional path globs to exclude"}
			}
		}`),
	}
}

type grepArgs struct {
	Pattern        string   `json:"pattern"`
	Path           string   `json:"path"`
	Glob           string   `json:"glob"`
	IgnoreCase     bool     `json:"ignore_case"`
	MaxMatches     int      `json:"max_matches"`
	Hidden         *bool    `json:"hidden"`
	IncludeIgnored bool     `json:"include_ignored"`
	Exclude        []string `json:"exclude"`
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

	root, _, rootIsFile, err := resolveSearchRoot(a.Path, host, g.guard, "grep")
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
	walkErr := walkSearchFiles(ctx, root, searchWalkOptions{PolicyRoot: searchPolicyRoot(root, host), Policy: g.Policy, Hidden: a.Hidden, IncludeIgnored: a.IncludeIgnored, Exclude: a.Exclude}, func(path string) error {
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
		if !isTextFile(path) {
			return nil
		}

		file, openErr := os.Open(path)
		if openErr != nil {
			return nil // unreadable files do not abort a repository search
		}
		defer file.Close()

		reader := bufio.NewReaderSize(file, 32*1024)
		lineNo := 0
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			line, readErr := reader.ReadString('\n')
			if len(line) > 0 {
				lineNo++
				line = strings.TrimSuffix(line, "\n")
				if re.MatchString(line) {
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

// Glob is a bounded file path matching tool.
type Glob struct {
	guard *PathGuard
	// MaxOutputBytes caps the returned path list.
	MaxOutputBytes int
	// MaxResults bounds the number of returned paths.
	MaxResults int
	Policy     config.EffectiveSearchPolicy
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
		Description: "List regular files matching a path glob within allowed roots. Use ** to match zero or more directories.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["pattern"],
			"properties": {
				"pattern": {"type": "string", "description": "Glob pattern, for example '*.go', 'src/*.go', or '**/*_test.go'"},
				"path": {"type": "string", "description": "File or directory to search. Defaults to cwd."},
				"max_results": {"type": "integer", "description": "Maximum paths to return (default 500)"},
				"hidden": {"type": "boolean", "description": "Include hidden files/directories for this call"},
				"include_ignored": {"type": "boolean", "description": "Bypass soft ignore rules (never .git or symlinks)"},
				"exclude": {"type": "array", "items":{"type":"string"}, "description":"Additional path globs to exclude"}
			}
		}`),
	}
}

type globArgs struct {
	Pattern        string   `json:"pattern"`
	Path           string   `json:"path"`
	MaxResults     int      `json:"max_results"`
	Hidden         *bool    `json:"hidden"`
	IncludeIgnored bool     `json:"include_ignored"`
	Exclude        []string `json:"exclude"`
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
	matcher, err := compileGlob(a.Pattern)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("glob: invalid pattern: %w", err)), nil
	}

	root, _, rootIsFile, err := resolveSearchRoot(a.Path, host, g.guard, "glob")
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
	walkErr := walkSearchFiles(ctx, root, searchWalkOptions{PolicyRoot: searchPolicyRoot(root, host), Policy: g.Policy, Hidden: a.Hidden, IncludeIgnored: a.IncludeIgnored, Exclude: a.Exclude}, func(path string) error {
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
	if host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
		if root == "" {
			root = host.CWD()
		}
	} else if root == "" {
		return "", nil, false, fmt.Errorf("%s: path is required when no host is provided", name)
	}
	if guard == nil {
		return "", nil, false, fmt.Errorf("%s: no path guard configured", name)
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
	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, false, fmt.Errorf("%s: %w", name, err)
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return "", nil, false, fmt.Errorf("%s: %q is not a file or directory", name, path)
	}
	return resolved, guard, info.Mode().IsRegular(), nil
}

// walkSearchFiles visits regular, non-symlink files below root while applying
// hard exclusions and one invocation-scoped hierarchical ignore matcher.
func walkSearchFiles(ctx context.Context, root string, opts searchWalkOptions, fn func(path string) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hardSearchExcluded(root, opts.PolicyRoot) {
			return nil
		}
		matcher := newSearchIgnoreMatcher(root, opts)
		if matcher.ignore(root, false) {
			return nil
		}
		return fn(root)
	}
	if hardSearchExcluded(root, opts.PolicyRoot) {
		return nil
	}
	matcher := newSearchIgnoreMatcher(root, opts)
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path != root && entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path != root && strings.EqualFold(entry.Name(), ".git") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path != root && matcher.ignore(path, true) {
				return filepath.SkipDir
			}
			return nil
		}
		entryInfo, err := entry.Info()
		if err != nil || !entryInfo.Mode().IsRegular() || matcher.ignore(path, false) {
			return nil
		}
		return fn(path)
	})
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

// globMatcher is a compiled path matcher. Compiling once per tool call is
// important when a repository contains thousands of files.
type globMatcher struct {
	patternParts []string
	basename     bool
}

// validateGlob validates all ordinary path segments using path.Match. A **
// segment is handled by globMatcher and matches zero or more path components.
func validateGlob(pattern string) error {
	_, err := compileGlob(pattern)
	return err
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

// matchRecursive is retained for compatibility with the original package
// helper and treats ** segments as recursive path components.
func matchRecursive(rel, pattern string) bool {
	matched, err := matchGlobPath(rel, pattern)
	return err == nil && matched
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

func isTextFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	for _, c := range buf[:n] {
		if c == 0 {
			return false
		}
	}
	return true
}

// lineNumber remains available to package tests and callers that need the
// first occurrence of a line in an in-memory string.
func lineNumber(data, line string) int {
	idx := strings.Index(data, line)
	if idx < 0 {
		return 0
	}
	return strings.Count(data[:idx], "\n") + 1
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
