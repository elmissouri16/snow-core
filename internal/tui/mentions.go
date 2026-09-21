package tui

import (
	"bytes"
	"cmp"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/config"
)

const (
	// mentionFileLimit keeps a large repository from making the first '@'
	// keystroke expensive or filling the picker with generated files.
	mentionFileLimit          = 2000
	mentionResultLimit        = 100
	mentionContentLimit       = 256 * 1024
	mentionAttachmentLimit    = 8
	mentionTotalContentLimit  = 1024 * 1024
	mentionTruncationNotice   = "\n[content truncated by snow]\n"
	mentionAttachmentOmission = " [attachment omitted by snow: prompt attachment limit reached]"
	mentionAttachmentFailure  = " [attachment omitted by snow: file is unavailable or unsupported]"
)

var ignoredMentionDirs = map[string]bool{
	".git": true,
	".hg":  true,
	".svn": true,
}

// discoverMentionFiles returns bounded, cwd-relative files and directories that
// can be browsed with @ in the composer. It scans breadth-first so a large early
// subtree cannot hide later root siblings. Directories carry a trailing slash;
// paths use '/' on every platform so inserted references stay portable.
func discoverMentionFiles(cwd string) []string {
	root, _, err := openMentionRoot(cwd)
	if err != nil {
		return nil
	}
	defer root.Close()

	var entries []string
	directories := []string{"."}
	scanned := 0
	for len(directories) > 0 && scanned < mentionFileLimit && len(entries) < mentionFileLimit {
		directory := directories[0]
		directories = directories[1:]
		file, openErr := openMentionDirectory(root, directory)
		if openErr != nil {
			continue
		}
		children, readErr := file.ReadDir(mentionFileLimit - scanned)
		_ = file.Close()
		if readErr != nil && readErr != io.EOF {
			continue
		}
		scanned += len(children)
		slices.SortFunc(children, func(a, b fs.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })
		for _, child := range children {
			if child.Type()&fs.ModeSymlink != 0 {
				continue
			}
			name := child.Name()
			// The textarea stores text as runes, so invalid UTF-8 names cannot be
			// inserted without losing the exact bytes needed to open them.
			if !utf8.ValidString(name) {
				continue
			}
			path := name
			if directory != "." {
				path = directory + "/" + name
			}
			switch {
			case child.IsDir():
				if ignoredMentionDirs[name] || config.IsDefaultGeneratedDir(name) {
					continue
				}
				entries = append(entries, path+"/")
				directories = append(directories, path)
			case child.Type().IsRegular():
				entries = append(entries, path)
			}
			if len(entries) >= mentionFileLimit {
				break
			}
		}
	}
	slices.Sort(entries)
	return entries
}

func openMentionDirectory(root *os.Root, path string) (*os.File, error) {
	before, err := root.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, cmp.Or(err, fs.ErrInvalid)
	}
	file, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	opened, openErr := file.Stat()
	current, currentErr := root.Lstat(path)
	if openErr != nil || currentErr != nil || !opened.IsDir() || !os.SameFile(before, opened) || !os.SameFile(opened, current) {
		_ = file.Close()
		return nil, cmp.Or(openErr, currentErr, fs.ErrInvalid)
	}
	return file, nil
}

type mentionToken struct {
	query      string
	start, end int
}

// mentionQuery keeps the original end-of-draft helper for focused matching
// tests. Runtime completion uses mentionQueryAt so an earlier token can be
// edited without discarding the text after the caret.
func mentionQuery(text string) (query string, start int, ok bool) {
	token, ok := mentionQueryAt(text, len(text))
	return token.query, token.start, ok
}

func mentionQueryAt(text string, caret int) (mentionToken, bool) {
	if caret < 0 || caret > len(text) {
		return mentionToken{}, false
	}
	before := text[:caret]
	for search := len(before); search > 0; {
		index := strings.LastIndex(before[:search], "@\"")
		if index < 0 {
			break
		}
		if index == 0 || mentionSpaceBefore(before, index) {
			raw := before[index+1:]
			candidate := raw
			if !strings.HasSuffix(candidate, "\"") {
				candidate += "\""
			}
			if value, err := strconv.Unquote(candidate); err == nil {
				end := caret
				if !strings.HasSuffix(raw, "\"") {
					end = quotedMentionEnd(text, caret)
				}
				return mentionToken{query: filepath.ToSlash(value), start: index, end: end}, true
			}
		}
		search = index
	}

	start := 0
	if separator := strings.LastIndexFunc(before, unicode.IsSpace); separator >= 0 {
		_, size := utf8.DecodeRuneInString(before[separator:])
		start = separator + size
	}
	if start >= caret || text[start] != '@' {
		return mentionToken{}, false
	}
	query := text[start+1 : caret]
	if strings.IndexFunc(query, unicode.IsSpace) >= 0 || strings.Contains(query, "\"") {
		return mentionToken{}, false
	}
	end := caret
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if unicode.IsSpace(r) {
			break
		}
		end += size
	}
	return mentionToken{query: filepath.ToSlash(query), start: start, end: end}, true
}

func quotedMentionEnd(text string, caret int) int {
	escaped := false
	for end := caret; end < len(text); end++ {
		switch {
		case escaped:
			escaped = false
		case text[end] == '\\':
			escaped = true
		case text[end] == '"':
			return end + 1
		}
	}
	return caret
}

func mentionPathLess(a, b string) int {
	aDir, bDir := strings.HasSuffix(a, "/"), strings.HasSuffix(b, "/")
	if aDir != bDir {
		if aDir {
			return -1
		}
		return 1
	}
	return cmp.Compare(strings.ToLower(a), strings.ToLower(b))
}

func mentionName(path string) string {
	return filepath.Base(strings.TrimSuffix(path, "/"))
}

// matchMentionFiles treats a slash-containing query as directory browsing and
// otherwise retains repository-wide basename discovery. Exact/prefix groups
// remain ahead of substring matches, with directories first in each group.
func matchMentionFiles(files []string, query string) []string {
	query = strings.ToLower(filepath.ToSlash(query))
	if query == "" || strings.Contains(query, "/") {
		parent, filter := "", query
		if strings.HasSuffix(query, "/") {
			parent, filter = strings.TrimSuffix(query, "/"), ""
		} else if before, after, found := strings.CutLast(query, "/"); found {
			parent, filter = before, after
		}
		var prefixes, contains []string
		for _, path := range files {
			trimmed := strings.TrimSuffix(path, "/")
			dir := ""
			if before, _, found := strings.CutLast(trimmed, "/"); found {
				dir = strings.ToLower(before)
			}
			if dir != parent {
				continue
			}
			name := strings.ToLower(mentionName(path))
			if strings.HasPrefix(name, filter) {
				prefixes = append(prefixes, path)
			} else if strings.Contains(name, filter) {
				contains = append(contains, path)
			}
		}
		slices.SortFunc(prefixes, mentionPathLess)
		slices.SortFunc(contains, mentionPathLess)
		return limitMentionMatches(append(prefixes, contains...))
	}

	var pathMatches, baseMatches, contains []string
	for _, path := range files {
		lower := strings.ToLower(strings.TrimSuffix(path, "/"))
		base := strings.ToLower(mentionName(path))
		switch {
		case strings.HasPrefix(lower, query):
			pathMatches = append(pathMatches, path)
		case strings.HasPrefix(base, query):
			baseMatches = append(baseMatches, path)
		case utf8.RuneCountInString(query) >= 2 && strings.Contains(lower, query):
			contains = append(contains, path)
		}
	}
	for _, group := range [][]string{pathMatches, baseMatches, contains} {
		slices.SortFunc(group, mentionPathLess)
	}
	return limitMentionMatches(append(append(pathMatches, baseMatches...), contains...))
}

func limitMentionMatches(matches []string) []string {
	if len(matches) > mentionResultLimit {
		return matches[:mentionResultLimit]
	}
	return matches
}

func mentionReference(path string, directory bool) string {
	path = filepath.ToSlash(path)
	quoted := strings.IndexFunc(path, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == '\\' || r == '"'
	}) >= 0
	if !quoted {
		return "@" + path
	}
	reference := "@" + strconv.Quote(path)
	if directory {
		reference = strings.TrimSuffix(reference, "\"")
	}
	return reference
}

// replaceMentionToken replaces only the token around the caret. Directories
// stay open for drill-down; files add a separator for continued prose.
func replaceMentionToken(text string, start, end int, path string) (string, int) {
	directory := strings.HasSuffix(path, "/")
	reference := mentionReference(path, directory)
	if !directory {
		reference += " "
	}
	return text[:start] + reference + text[end:], start + len(reference)
}

// expandMentionPrompt resolves each exact submitted path through a confined
// os.Root. Suggestion caches are presentation only and never attachment
// authority, so newly-created files and entries beyond the picker cap work.
func expandMentionPrompt(text, cwd string) string {
	return expandMentionPromptWithReader(text, cwd, readMentionFile)
}

func expandMentionPromptWithReader(text, cwd string, readFile func(string, string) ([]byte, error)) string {
	var out strings.Builder
	seen := make(map[string]struct{})
	attempts, contentBytes := 0, 0
	for i := 0; i < len(text); {
		if text[i] != '@' || (i > 0 && !mentionSpaceBefore(text, i)) {
			out.WriteByte(text[i])
			i++
			continue
		}
		path, end, ok := parsedMentionReference(text, i)
		if !ok || strings.HasSuffix(path, "/") {
			out.WriteByte(text[i])
			i++
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			out.WriteString(text[i:end])
			out.WriteString(" [duplicate attachment omitted by snow]")
			i = end
			continue
		}
		seen[path] = struct{}{}
		if attempts >= mentionAttachmentLimit || contentBytes >= mentionTotalContentLimit {
			out.WriteString(text[i:end])
			out.WriteString(mentionAttachmentOmission)
			i = end
			continue
		}
		attempts++
		data, err := readFile(cwd, path)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			out.WriteString(text[i:end])
			out.WriteString(mentionAttachmentFailure)
			i = end
			continue
		}
		limit := min(mentionContentLimit, mentionTotalContentLimit-contentBytes)
		truncated := len(data) > limit
		if truncated {
			data = truncateMentionContent(data, limit)
		}
		if !utf8.Valid(data) {
			out.WriteString(text[i:end])
			out.WriteString(mentionAttachmentFailure)
			i = end
			continue
		}
		contentBytes += len(data)
		out.WriteString("<file name=")
		out.WriteString(strconv.Quote(path))
		out.WriteString(">\n")
		out.Write(data)
		if truncated {
			out.WriteString(mentionTruncationNotice)
		} else if len(data) > 0 && data[len(data)-1] != '\n' {
			out.WriteByte('\n')
		}
		out.WriteString("</file>")
		i = end
	}
	return out.String()
}

func truncateMentionContent(data []byte, limit int) []byte {
	limit = min(max(limit, 0), len(data))
	for limit > 0 && limit < len(data) && !utf8.RuneStart(data[limit]) {
		limit--
	}
	return data[:limit]
}

func parsedMentionReference(text string, start int) (string, int, bool) {
	if start+1 >= len(text) {
		return "", start + 1, false
	}
	if text[start+1] == '"' {
		escaped := false
		for end := start + 2; end < len(text); end++ {
			switch {
			case escaped:
				escaped = false
			case text[end] == '\\':
				escaped = true
			case text[end] == '"':
				value, err := strconv.Unquote(text[start+1 : end+1])
				return filepath.ToSlash(value), end + 1, err == nil
			}
		}
		return "", len(text), false
	}
	end := start + 1
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if unicode.IsSpace(r) {
			break
		}
		end += size
	}
	return filepath.ToSlash(text[start+1 : end]), end, end > start+1
}

type mentionReadStage string

const (
	mentionReadRootOpened     mentionReadStage = "root-opened"
	mentionReadDirectoryReady mentionReadStage = "directory-ready"
	mentionReadFileRead       mentionReadStage = "file-read"
)

func readMentionFile(cwd, path string) ([]byte, error) {
	return readMentionFileWithHook(cwd, path, nil)
}

func readMentionFileWithHook(cwd, path string, hook func(mentionReadStage, string)) ([]byte, error) {
	if path == "" || filepath.IsAbs(filepath.FromSlash(path)) {
		return nil, fs.ErrInvalid
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, fs.ErrInvalid
		}
	}
	root, absoluteRoot, err := openMentionRoot(cwd)
	if err != nil {
		return nil, err
	}
	if hook != nil {
		hook(mentionReadRootOpened, ".")
	}
	roots := []*os.Root{root}
	defer func() {
		for _, opened := range slices.Backward(roots) {
			_ = opened.Close()
		}
	}()
	current := root
	for _, part := range parts[:len(parts)-1] {
		before, statErr := current.Lstat(part)
		if statErr != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
			return nil, cmp.Or(statErr, fs.ErrInvalid)
		}
		if hook != nil {
			hook(mentionReadDirectoryReady, part)
		}
		next, openErr := current.OpenRoot(part)
		if openErr != nil {
			return nil, openErr
		}
		opened, openedErr := next.Stat(".")
		after, afterErr := current.Lstat(part)
		if openedErr != nil || afterErr != nil || !opened.IsDir() || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
			_ = next.Close()
			return nil, cmp.Or(openedErr, afterErr, fs.ErrInvalid)
		}
		roots = append(roots, next)
		current = next
	}
	name := parts[len(parts)-1]
	before, err := current.Lstat(name)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, cmp.Or(err, fs.ErrInvalid)
	}
	file, err := current.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !sameMentionFile(before, opened) {
		return nil, cmp.Or(err, fs.ErrInvalid)
	}
	data, err := io.ReadAll(io.LimitReader(file, mentionContentLimit+1))
	if err != nil {
		return nil, err
	}
	if hook != nil {
		hook(mentionReadFileRead, name)
	}
	openedAfter, openedErr := file.Stat()
	currentFile, currentErr := current.Lstat(name)
	currentRoot, rootErr := os.Lstat(absoluteRoot)
	pinnedRoot, pinnedErr := root.Stat(".")
	if openedErr != nil || currentErr != nil || rootErr != nil || pinnedErr != nil ||
		!sameMentionFile(before, openedAfter) || !sameMentionFile(openedAfter, currentFile) ||
		currentRoot.Mode()&os.ModeSymlink != 0 || !currentRoot.IsDir() || !os.SameFile(pinnedRoot, currentRoot) {
		return nil, cmp.Or(openedErr, currentErr, rootErr, pinnedErr, fs.ErrInvalid)
	}
	return data, nil
}

func openMentionRoot(cwd string) (*os.Root, string, error) {
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return nil, "", err
	}
	before, err := os.Lstat(absolute)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, "", cmp.Or(err, fs.ErrInvalid)
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, "", err
	}
	opened, openedErr := root.Stat(".")
	current, currentErr := os.Lstat(absolute)
	if openedErr != nil || currentErr != nil || !opened.IsDir() || !os.SameFile(before, opened) || !os.SameFile(opened, current) {
		_ = root.Close()
		return nil, "", cmp.Or(openedErr, currentErr, fs.ErrInvalid)
	}
	return root, absolute, nil
}

func sameMentionFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.Mode().IsRegular() && b.Mode().IsRegular() &&
		os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func mentionSpaceBefore(text string, index int) bool {
	if index <= 0 || index > len(text) {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return unicode.IsSpace(r)
}

func mentionSpaceSequence(value string) bool {
	r, size := utf8.DecodeRuneInString(value)
	return size == len(value) && unicode.IsSpace(r)
}

func composerCaretByteOffset(text string, row, column int) int {
	if row < 0 || column < 0 {
		return len(text)
	}
	offset := 0
	for currentRow, line := range strings.Split(text, "\n") {
		if currentRow == row {
			runes := []rune(line)
			column = min(column, len(runes))
			return offset + len(string(runes[:column]))
		}
		offset += len(line) + 1
	}
	return len(text)
}

func setComposerCaret(editor interface {
	Line() int
	CursorUp()
	SetCursorColumn(int)
}, text string, offset int) {
	before := text[:min(max(offset, 0), len(text))]
	row := strings.Count(before, "\n")
	column := utf8.RuneCountInString(before[strings.LastIndex(before, "\n")+1:])
	for editor.Line() > row {
		editor.CursorUp()
	}
	editor.SetCursorColumn(column)
}

// highlightComposerMentions gives unquoted and quoted @path tokens the theme
// accent while preserving textarea cursor and viewport escape sequences.
func highlightComposerMentions(view string, textStyle, mentionStyle lipgloss.Style) string {
	if !strings.Contains(view, "@") {
		return view
	}

	var out, segment strings.Builder
	out.Grow(len(view))
	segment.Grow(len(view))
	state := byte(0)
	atTokenStart := true
	inMention := false
	mentionQuoted := false
	mentionEscaped := false
	afterMentionAt := false
	segmentIsMention := false

	flush := func() {
		if segment.Len() == 0 {
			return
		}
		style := textStyle
		if segmentIsMention {
			style = mentionStyle
		}
		out.WriteString(style.Render(segment.String()))
		segment.Reset()
	}

	for len(view) > 0 {
		sequence, width, n, nextState := xansi.DecodeSequence(view, state, nil)
		if n <= 0 {
			flush()
			out.WriteString(view)
			break
		}
		view = view[n:]
		state = nextState

		if width == 0 {
			flush()
			out.WriteString(sequence)
			if mentionSpaceSequence(sequence) {
				atTokenStart = true
				inMention = false
			}
			continue
		}

		separator := mentionSpaceSequence(sequence)
		highlight := inMention
		if atTokenStart && sequence == "@" {
			inMention = true
			afterMentionAt = true
			highlight = true
		} else if inMention {
			highlight = true
			if afterMentionAt {
				mentionQuoted = sequence == "\""
				afterMentionAt = false
			} else if mentionQuoted {
				switch {
				case mentionEscaped:
					mentionEscaped = false
				case sequence == "\\":
					mentionEscaped = true
				case sequence == "\"":
					inMention = false
					mentionQuoted = false
				}
			}
		}
		if separator && !mentionQuoted {
			inMention = false
			afterMentionAt = false
			atTokenStart = true
			highlight = false
		} else {
			atTokenStart = false
		}

		if segment.Len() > 0 && highlight != segmentIsMention {
			flush()
		}
		segmentIsMention = highlight
		segment.WriteString(sequence)
	}
	flush()
	return out.String()
}

// renderMentionPicker renders the bounded file list directly above the
// composer. The @ prefix makes it clear that accepting an item inserts a
// reference rather than sending the prompt.
func (m *Model) renderMentionPicker() string {
	if !m.mentionVisible {
		return ""
	}
	if m.mentionLoading {
		return styleHeaderDim.Render("  searching project files and folders…")
	}
	if len(m.mentionMatches) == 0 {
		return styleHeaderDim.Render("  no matching files or folders")
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
		line := sanitizeTerminalLine("@" + m.mentionMatches[i])
		line = truncateDisplayText(line, max(1, m.width-4))
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
