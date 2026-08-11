// Package context assembles the system prompt from the built-in preamble,
// optional user instructions, and project AGENTS.md files (cwd → parents),
// subject to a hard byte cap.
package context

import (
	"bytes"
	_ "embed"
	"fmt"
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
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
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
		data, err := os.ReadFile(f.Path)
		if err != nil || len(data) == 0 {
			continue
		}
		title := fmt.Sprintf("%s (depth %d)", filepath.Base(f.Path), f.Depth)
		if len(data) > remaining {
			data = data[:remaining]
			a.Truncated = true
		}
		a.Sections = append(a.Sections, Section{Title: title, Content: string(data)})
		remaining -= len(data)
		if remaining <= 0 {
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
