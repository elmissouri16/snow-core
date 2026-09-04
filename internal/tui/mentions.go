package tui

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/config"
)

const (
	// mentionFileLimit keeps a large repository from making the first '@'
	// keystroke expensive or filling the picker with generated files.
	mentionFileLimit    = 2000
	mentionResultLimit  = 100
	mentionContentLimit = 256 * 1024
)

var ignoredMentionDirs = map[string]bool{
	".git": true,
	".hg":  true,
	".svn": true,
}

// discoverMentionFiles returns regular, cwd-relative files that can be
// referenced with @ in the composer. Paths use '/' on every platform so the
// inserted prompt is portable and easy for the provider to read.
func discoverMentionFiles(cwd string) []string {
	var files []string
	_ = filepath.WalkDir(cwd, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path != cwd && (ignoredMentionDirs[entry.Name()] || config.IsDefaultGeneratedDir(entry.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		// Do not follow symlinks. Besides avoiding duplicate results, this
		// keeps completion from exposing paths outside the active cwd.
		if entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil || rel == "." {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		if len(files) >= mentionFileLimit {
			return fs.SkipAll
		}
		return nil
	})
	slices.Sort(files)
	return files
}

// mentionQuery returns the current @ token and its byte start in text. Snow
// intentionally completes the token at the end of the editor: this keeps the
// behavior predictable for multiline input without needing cursor geometry.
func mentionQuery(text string) (query string, start int, ok bool) {
	start = strings.LastIndexAny(text, " \t\n") + 1
	if start >= len(text) || text[start] != '@' {
		return "", 0, false
	}
	token := text[start:]
	if strings.ContainsAny(token, "\r\n") {
		return "", 0, false
	}
	return token[1:], start, true
}

// matchMentionFiles matches a typed @ query against relative paths and file
// basenames. Path-prefix matches are listed first, followed by basename
// matches, with each group in lexical order.
func matchMentionFiles(files []string, query string) []string {
	query = strings.ToLower(filepath.ToSlash(query))
	var pathMatches, baseMatches []string
	for _, path := range files {
		lower := strings.ToLower(path)
		if query == "" || strings.HasPrefix(lower, query) {
			pathMatches = append(pathMatches, path)
			continue
		}
		if strings.HasPrefix(filepath.Base(lower), query) {
			baseMatches = append(baseMatches, path)
		}
	}
	slices.Sort(pathMatches)
	slices.Sort(baseMatches)
	matches := append(pathMatches, baseMatches...)
	if len(matches) > mentionResultLimit {
		matches = matches[:mentionResultLimit]
	}
	return matches
}

// replaceMentionToken replaces the current @ token and adds a separator so
// the user can continue writing immediately after accepting a file.
func replaceMentionToken(text string, start int, path string) string {
	return text[:start] + "@" + path + " "
}

// expandMentionPrompt replaces selected @path tokens with bounded text-file
// contents before the provider call. The visible composer stays short while
// the provider and durable session receive the useful file context.
func expandMentionPrompt(text, cwd string, files []string) string {
	if len(files) == 0 {
		files = discoverMentionFiles(cwd)
	}
	known := make(map[string]bool, len(files))
	for _, path := range files {
		known[filepath.ToSlash(path)] = true
	}
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '@' || (i > 0 && !isMentionSpace(text[i-1])) {
			out.WriteByte(text[i])
			i++
			continue
		}
		j := i + 1
		for j < len(text) && !isMentionSpace(text[j]) {
			j++
		}
		path := filepath.ToSlash(text[i+1 : j])
		if !known[path] {
			out.WriteString(text[i:j])
			i = j
			continue
		}
		data, err := os.ReadFile(filepath.Join(cwd, filepath.FromSlash(path)))
		if err != nil || len(data) == 0 || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			out.WriteString(text[i:j])
			i = j
			continue
		}
		if len(data) > mentionContentLimit {
			data = append(data[:mentionContentLimit], []byte("\n[content truncated by snow]\n")...)
		}
		out.WriteString("<file name=\"")
		out.WriteString(path)
		out.WriteString("\">\n")
		out.Write(data)
		if data[len(data)-1] != '\n' {
			out.WriteByte('\n')
		}
		out.WriteString("</file>")
		i = j
	}
	return out.String()
}

func isMentionSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// renderMentionPicker renders the bounded file list directly above the
// composer. The @ prefix makes it clear that accepting an item inserts a
// reference rather than sending the prompt.
func (m *Model) renderMentionPicker() string {
	if m.mentionLoading {
		return styleHeaderDim.Render("  searching project files…")
	}
	if !m.mentionVisible || len(m.mentionMatches) == 0 {
		return ""
	}
	limit := 8
	if m.inlineInputOverlay() {
		limit = min(limit, m.availableOverlayHeight())
	}
	limit = max(1, limit)
	start := 0
	end := min(len(m.mentionMatches), limit)
	if m.mentionIndex >= end {
		start = m.mentionIndex - limit + 1
		end = start + limit
		if end > len(m.mentionMatches) {
			end = len(m.mentionMatches)
			start = end - limit
		}
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		line := "@" + m.mentionMatches[i]
		line = truncateRunes(line, max(8, m.width-4))
		if i == m.mentionIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		if i+1 < end {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
