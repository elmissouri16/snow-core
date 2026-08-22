// Package context assembles the system prompt from the built-in preamble,
// optional user instructions, and project AGENTS.md files (cwd → parents),
// subject to a hard byte cap.
package context

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DefaultPreamble is the built-in system preamble. Keeping the source in
// system.md makes it easy to edit while go:embed preserves the single binary.
//
//go:embed system.md
var DefaultPreamble string

// Loader assembles system context for a working directory.
type Loader struct {
	// CapBytes bounds the total injected project context.
	CapBytes int
	// IncludeCLAUDE enables CLAUDE.md compatibility loading (default off).
	IncludeCLAUDE bool
}

// NewLoader creates a loader with default caps.
func NewLoader(capBytes int, includeCLAUDE bool) *Loader {
	if capBytes <= 0 {
		capBytes = 100 * 1024
	}
	return &Loader{CapBytes: capBytes, IncludeCLAUDE: includeCLAUDE}
}

// ProjectFile is a discovered project context file.
type ProjectFile struct {
	Path  string
	Depth int // 0 = cwd, 1 = parent, ...
}

// FindAgents walks cwd toward the filesystem root and returns AGENTS.md
// (and optional CLAUDE.md) files, nearest first. Missing dirs are ignored.
func (l *Loader) FindAgents(cwd string) []ProjectFile {
	var files []ProjectFile
	seen := make(map[string]bool)
	dir := cwd
	depth := 0
	for {
		if depth > 64 {
			break
		}
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			if name == "CLAUDE.md" && !l.IncludeCLAUDE {
				continue
			}
			p := filepath.Join(dir, name)
			if seen[p] {
				continue
			}
			seen[p] = true
			if isRegularProjectFile(dir, name) {
				files = append(files, ProjectFile{Path: p, Depth: depth})
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
		depth++
	}
	return files
}

func isRegularProjectFile(dir, name string) bool {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer root.Close()
	info, err := root.Lstat(name)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular()
}

func readProjectFile(path string, limit int) ([]byte, bool, error) {
	if limit <= 0 {
		return nil, false, nil
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, false, err
	}
	defer root.Close()

	name := filepath.Base(path)
	before, err := root.Lstat(name)
	if err != nil {
		return nil, false, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, false, errors.New("project instruction must be a regular non-symlink file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, false, errors.New("project instruction changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, false, err
	}
	truncated := len(data) > limit
	if truncated {
		data = data[:limit]
	}
	return data, truncated, nil
}

// Assembly is the result of context assembly.
type Assembly struct {
	Preamble  string
	Sections  []Section
	TotalSize int
	Truncated bool
}

// Section is one named context contribution.
type Section struct {
	Title   string
	Content string
}

// Assemble builds the final system prompt.
func (l *Loader) Assemble(cwd, preamble, userInstructions string) Assembly {
	if preamble == "" {
		preamble = DefaultPreamble
	}
	a := Assembly{Preamble: preamble}
	if userInstructions != "" {
		a.Sections = append(a.Sections, Section{Title: "User instructions", Content: userInstructions})
	}
	files := l.FindAgents(cwd)
	remaining := l.CapBytes
	for _, f := range files {
		if remaining <= 0 {
			break
		}
		data, truncated, err := readProjectFile(f.Path, remaining)
		if err != nil || len(data) == 0 {
			continue
		}
		title := fmt.Sprintf("%s (depth %d)", filepath.Base(f.Path), f.Depth)
		a.Sections = append(a.Sections, Section{Title: title, Content: string(data)})
		remaining -= len(data)
		if truncated {
			a.Truncated = true
			break
		}
	}
	for _, s := range a.Sections {
		a.TotalSize += len(s.Content)
	}
	return a
}

// Render serializes the assembly into the final system prompt text.
func (a Assembly) Render() string {
	var b bytes.Buffer
	b.WriteString(a.Preamble)
	if !strings.HasSuffix(a.Preamble, "\n") {
		b.WriteByte('\n')
	}
	for _, s := range a.Sections {
		b.WriteString("\n--- ")
		b.WriteString(s.Title)
		b.WriteString(" ---\n")
		b.WriteString(s.Content)
		b.WriteByte('\n')
	}
	if a.Truncated {
		b.WriteString("\n[context truncated to fit budget]\n")
	}
	return b.String()
}
