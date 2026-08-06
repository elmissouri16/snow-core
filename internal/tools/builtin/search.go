package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snow-core/snow/internal/tools"
)

// Grep is a pure-Go content search tool (no external ripgrep dependency).
type Grep struct {
	guard *PathGuard
	// MaxOutputBytes caps combined result output.
	MaxOutputBytes int
	// MaxMatches bounds total matches returned.
	MaxMatches int
}

// NewGrep creates the grep tool.
func NewGrep(guard *PathGuard) *Grep {
	return &Grep{guard: guard, MaxOutputBytes: 256 * 1024, MaxMatches: 1000}
}

// Schema implements tools.Tool.
func (g *Grep) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "grep",
		Description: "Search file contents with a regular expression within allowed roots. Returns matching lines with file paths.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["pattern"],
			"properties": {
				"pattern": {"type": "string", "description": "Go regular expression (RE2) to search for"},
				"path": {"type": "string", "description": "Directory or file to search. Defaults to cwd."},
				"glob": {"type": "string", "description": "Filename glob filter, e.g. '*.go' or '**/*.md'. Empty matches all files."},
				"ignore_case": {"type": "boolean", "default": false},
				"max_matches": {"type": "integer", "description": "Cap on matches returned (default 1000)"}
			}
		}`),
	}
}

type grepArgs struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	Glob       string `json:"glob"`
	IgnoreCase bool   `json:"ignore_case"`
	MaxMatches int    `json:"max_matches"`
}

// Run implements tools.Tool.
func (g *Grep) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	var a grepArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.ErrorResult(fmt.Errorf("grep: invalid args: %w", err)), nil
	}
	if a.Pattern == "" {
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

	root := a.Path
	if root == "" {
		root = host.CWD()
	}
	guard := g.guard
	if host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
	}
	resolved, err := guard.Resolve(root)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("grep: %w", err)), nil
	}

	maxMatches := a.MaxMatches
	if maxMatches <= 0 {
		maxMatches = g.MaxMatches
	}
	out := &strings.Builder{}
	count := 0
	bytesBudget := g.MaxOutputBytes

	filepath.WalkDir(resolved, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != resolved && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isTextFile(path) {
			return nil
		}
		if a.Glob != "" {
			ok, gerr := filepath.Match(a.Glob, d.Name())
			if gerr != nil || !ok {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				count++
				rel, _ := filepath.Rel(resolved, path)
				entry := fmt.Sprintf("%s:%d: %s\n", rel, lineNumber(string(data), line), truncate(line, 300))
				if out.Len()+len(entry) > bytesBudget {
					out.WriteString("[output truncated]\n")
					return filepath.SkipAll
				}
				out.WriteString(entry)
				if count >= maxMatches {
					out.WriteString("[max matches reached]\n")
					return filepath.SkipAll
				}
			}
		}
		return nil
	})

	if out.Len() == 0 {
		return tools.TextResult(fmt.Sprintf("no matches for %q", a.Pattern)), nil
	}
	return tools.TextResult(out.String()), nil
}

// Glob is a fast file path matching tool.
type Glob struct {
	guard *PathGuard
}

// NewGlob creates the glob tool.
func NewGlob(guard *PathGuard) *Glob {
	return &Glob{guard: guard}
}

// Schema implements tools.Tool.
func (g *Glob) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "glob",
		Description: "List files matching a glob pattern within allowed roots. Use '**' for recursive matching.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["pattern"],
			"properties": {
				"pattern": {"type": "string", "description": "Glob pattern, e.g. '*.go' or '**/*_test.go'"},
				"path": {"type": "string", "description": "Root directory to search. Defaults to cwd."},
				"max_results": {"type": "integer", "description": "Cap on results (default 500)"}
			}
		}`),
	}
}

type globArgs struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	MaxResults int    `json:"max_results"`
}

// Run implements tools.Tool.
func (g *Glob) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	var a globArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.ErrorResult(fmt.Errorf("glob: invalid args: %w", err)), nil
	}
	if a.Pattern == "" {
		return tools.ErrorResult(fmt.Errorf("glob: pattern is required")), nil
	}
	root := a.Path
	if root == "" {
		root = host.CWD()
	}
	guard := g.guard
	if host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
	}
	resolved, err := guard.Resolve(root)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("glob: %w", err)), nil
	}
	maxResults := a.MaxResults
	if maxResults <= 0 {
		maxResults = 500
	}

	// Recursive patterns need a walk; simple patterns match within root.
	recursive := strings.Contains(a.Pattern, "**")
	out := &strings.Builder{}
	count := 0

	if recursive {
		base := strings.SplitN(a.Pattern, "**", 2)[0]
		base = filepath.Dir(a.Pattern)
		_ = base
		filepath.WalkDir(resolved, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(resolved, path)
			if matchRecursive(rel, a.Pattern) {
				out.WriteString(rel + "\n")
				count++
				if count >= maxResults {
					return filepath.SkipAll
				}
			}
			return nil
		})
	} else {
		filepath.WalkDir(resolved, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(resolved, path)
			if ok, _ := filepath.Match(a.Pattern, filepath.Base(rel)); ok {
				out.WriteString(rel + "\n")
				count++
				if count >= maxResults {
					return filepath.SkipAll
				}
			}
			return nil
		})
	}

	if count == 0 {
		return tools.TextResult(fmt.Sprintf("no files match %q", a.Pattern)), nil
	}
	return tools.TextResult(out.String()), nil
}

func matchRecursive(rel, pattern string) bool {
	// Convert ** glob into a regexp over path segments.
	parts := strings.Split(pattern, "**")
	var b strings.Builder
	b.WriteString("^")
	for i, p := range parts {
		if i > 0 {
			b.WriteString(".*")
		}
		if p == "" {
			continue
		}
		b.WriteString(regexp.QuoteMeta(strings.TrimPrefix(p, "/")))
		if i < len(parts)-1 {
			b.WriteString(".*")
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(rel)
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

func lineNumber(data, line string) int {
	idx := strings.Index(data, line)
	if idx < 0 {
		return 0
	}
	return strings.Count(data[:idx], "\n") + 1
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
