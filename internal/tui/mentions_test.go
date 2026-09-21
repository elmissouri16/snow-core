package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/permission"
)

func TestDiscoverMentionFilesSkipsGeneratedAndSymlinkedPaths(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		"README.md",
		filepath.Join("internal", "tui", "tui.go"),
		filepath.Join("vendor", "ignored.bin"),
		filepath.Join(".git", "config"),
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := discoverMentionFiles(root)
	joined := strings.Join(files, "\n")
	for _, want := range []string{"README.md", "internal/tui/tui.go"} {
		if !strings.Contains(joined, filepath.ToSlash(want)) {
			t.Fatalf("mention files missing %q: %v", want, files)
		}
	}
	for _, unwanted := range []string{"vendor", ".git"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("mention files should skip %q: %v", unwanted, files)
		}
	}
}

func TestDiscoverMentionFilesSkipsNamesTheComposerCannotRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows filenames are Unicode strings")
	}
	root := t.TempDir()
	invalid := string([]byte{'b', 'a', 'd', '-', 0xff, '.', 't', 'x', 't'})
	if err := os.WriteFile(filepath.Join(root, invalid), []byte("hidden"), 0o644); err != nil {
		t.Skipf("filesystem does not accept invalid UTF-8 filenames: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "valid.txt"), []byte("visible"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := discoverMentionFiles(root)
	if !slices.Contains(files, "valid.txt") {
		t.Fatalf("valid sibling missing from discovery: %q", files)
	}
	for _, path := range files {
		if !utf8.ValidString(path) {
			t.Fatalf("discovery returned a path the composer cannot round-trip: %q", path)
		}
	}
}

func TestDiscoverMentionFilesKeepsRootSiblingsAheadOfLargeSubtrees(t *testing.T) {
	root := t.TempDir()
	large := filepath.Join(root, "a-large")
	if err := os.Mkdir(large, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range mentionFileLimit {
		name := filepath.Join(large, fmt.Sprintf("%04d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "z-root.txt"), []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := discoverMentionFiles(root)
	if !slices.Contains(files, "z-root.txt") {
		t.Fatalf("bounded discovery hid a later root sibling: %v", files[len(files)-min(10, len(files)):])
	}
}

func TestMentionQueryAndMatching(t *testing.T) {
	query, start, ok := mentionQuery("read this @internal/t")
	if !ok || query != "internal/t" || start != len("read this ") {
		t.Fatalf("mentionQuery = %q, %d, %v", query, start, ok)
	}
	files := []string{"README.md", "internal/tui/tui.go", "cmd/snow/main.go"}
	got := matchMentionFiles(files, "tui")
	if len(got) != 1 || got[0] != "internal/tui/tui.go" {
		t.Fatalf("basename match = %v, want internal/tui/tui.go", got)
	}
	if got, _ := replaceMentionToken("read @README", len("read "), len("read @README"), "README.md"); got != "read @README.md " {
		t.Fatalf("replaceMentionToken = %q, want completed file reference", got)
	}
}

func TestMentionFoldersQuotedPathsAndCaretLocalReplacement(t *testing.T) {
	files := []string{"README.md", "docs/", "docs/guide.md", "folder name/", "folder name/file name.txt", "internal/", "internal/tui/", "internal/tui/tui.go"}
	if got, want := matchMentionFiles(files, ""), []string{"docs/", "folder name/", "internal/", "README.md"}; !slices.Equal(got, want) {
		t.Fatalf("root matches = %v, want %v", got, want)
	}
	if got, want := matchMentionFiles(files, "internal/"), []string{"internal/tui/"}; !slices.Equal(got, want) {
		t.Fatalf("folder matches = %v, want %v", got, want)
	}

	text := "before @REA after"
	token, ok := mentionQueryAt(text, len("before @REA"))
	if !ok || token.query != "REA" || token.start != len("before ") || token.end != len("before @REA") {
		t.Fatalf("caret token = %+v, %v", token, ok)
	}
	next, caret := replaceMentionToken(text, token.start, token.end, "README.md")
	if want := "before @README.md  after"; next != want || caret != len("before @README.md ") {
		t.Fatalf("caret replacement = %q at %d, want %q at %d", next, caret, want, len("before @README.md "))
	}

	directory := mentionReference("folder name/", true)
	if directory != `@"folder name/` {
		t.Fatalf("quoted directory reference = %q", directory)
	}
	quoted, ok := mentionQueryAt(directory, len(directory))
	if !ok || quoted.query != "folder name/" {
		t.Fatalf("quoted directory query = %+v, %v", quoted, ok)
	}
	file := mentionReference("folder name/file name.txt", false)
	if file != `@"folder name/file name.txt"` {
		t.Fatalf("quoted file reference = %q", file)
	}
}

func TestMentionUnicodeWhitespaceBoundariesAndQuotedNames(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, separator := range []string{"\u00a0", "\u2003"} {
		text := "read" + separator + "@notes.md"
		token, ok := mentionQueryAt(text, len(text))
		if !ok || token.query != "notes.md" || token.start != len("read"+separator) {
			t.Errorf("separator %q token = %+v, %v", separator, token, ok)
		}
		if expanded := expandMentionPrompt(text, root); !strings.Contains(expanded, `<file name="notes.md">`) {
			t.Errorf("separator %q did not expand exact mention: %q", separator, expanded)
		}
	}
	for _, path := range []string{"folder\u00a0name/file.md", "folder\u2003name/file.md", "control\x1bname.md", `back\\slash.md`} {
		reference := mentionReference(path, false)
		if !strings.HasPrefix(reference, `@"`) {
			t.Errorf("unsafe path %q was not quoted: %q", path, reference)
			continue
		}
		parsed, end, ok := parsedMentionReference(reference, 0)
		if !ok || end != len(reference) || parsed != filepath.ToSlash(path) {
			t.Errorf("quoted path %q parsed as %q at %d, ok=%v", path, parsed, end, ok)
		}
	}
}

func TestMentionPickerSanitizesLabelsWithoutChangingInsertedPath(t *testing.T) {
	path := "界界\x1b]2;spoofed\a\nname.md"
	m := &Model{width: 16, mentionVisible: true, mentionMatches: []string{path}}
	rendered := m.renderMentionPicker()
	if strings.Contains(rendered, "\x1b]2;") || strings.ContainsAny(rendered, "\a\n") {
		t.Fatalf("picker retained terminal or row controls: %q", rendered)
	}
	if width := xansi.StringWidth(stripANSI(rendered)); width > m.width-2 {
		t.Fatalf("picker width = %d, want at most %d: %q", width, m.width-2, rendered)
	}
	replaced, _ := replaceMentionToken("@bad", 0, len("@bad"), path)
	parsed, _, ok := parsedMentionReference(strings.TrimSuffix(replaced, " "), 0)
	if !ok || parsed != path {
		t.Fatalf("inserted path = %q parsed as %q, ok=%v", replaced, parsed, ok)
	}
}

func TestComposerHighlightsFileAndFolderMentions(t *testing.T) {
	textStyle, mentionStyle := mentionHighlightTestStyles()
	const view = "open @internal/tui and @README.md but not user@example.com"
	rendered := highlightComposerMentions(view, textStyle, mentionStyle)
	if got := stripANSI(rendered); got != view {
		t.Fatalf("highlighting changed composer text: got %q, want %q", got, view)
	}
	for _, mention := range []string{"@internal/tui", "@README.md"} {
		if !strings.Contains(rendered, mentionStyle.Render(mention)) {
			t.Errorf("composer does not apply mention style to %q: %q", mention, rendered)
		}
	}
	if strings.Contains(rendered, mentionStyle.Render("@example.com")) {
		t.Fatalf("email-like text was styled as a path mention: %q", rendered)
	}
	if got := styleMention.GetForeground(); got != colorAccent {
		t.Fatalf("mention foreground = %v, want accent %v", got, colorAccent)
	}
}

func TestComposerMentionHighlightSurvivesCursorEscapeSequences(t *testing.T) {
	textStyle, mentionStyle := mentionHighlightTestStyles()
	const view = "read @fol\x1b[7md\x1b[0mer/file.go next"
	rendered := highlightComposerMentions(view, textStyle, mentionStyle)
	if got, want := stripANSI(rendered), stripANSI(view); got != want {
		t.Fatalf("highlighting changed composer text: got %q, want %q", got, want)
	}
	for _, part := range []string{"@fol", "d", "er/file.go"} {
		if !strings.Contains(rendered, mentionStyle.Render(part)) {
			t.Errorf("cursor-split mention part %q is not styled: %q", part, rendered)
		}
	}
}

func mentionHighlightTestStyles() (lipgloss.Style, lipgloss.Style) {
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#eeeeee"))
	mentionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#0088ff")).Bold(true)
	return textStyle, mentionStyle
}

func TestMentionMatchingPreservesCaseAndPriority(t *testing.T) {
	files := []string{"b/service.go", "SERVICE/z.go", "A/Service.go", "src/İnput.go", "src/Σervice.go"}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"SERVICE", []string{"SERVICE/z.go", "A/Service.go", "b/service.go"}},
		{"İ", []string{"src/İnput.go"}},
		{"σ", []string{"src/Σervice.go"}},
	} {
		if got := matchMentionFiles(files, tc.query); !slices.Equal(got, tc.want) {
			t.Errorf("query %q = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestExpandMentionPromptIncludesExactConfinedTextFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("important notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder name"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "folder name", "new file.md"), []byte("new content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("notes.md", filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	got := expandMentionPrompt(`summarize @notes.md and @"folder name/new file.md" but not @link.md`, root)
	for _, want := range []string{"<file name=\"notes.md\">", "important notes", `<file name="folder name/new file.md">`, "new content", "</file>", "@link.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expanded prompt missing %q: %q", want, got)
		}
	}
	if strings.Count(got, "<file name=") != 2 {
		t.Fatalf("expanded prompt followed a symlink or duplicated content: %q", got)
	}
}

func TestReadMentionFileRejectsNamespaceReplacementRaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not permit renaming these open handles")
	}
	t.Run("root", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "project")
		replacement := filepath.Join(parent, "replacement")
		for _, directory := range []string{root, replacement} {
			if err := os.Mkdir(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte(directory), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, err := readMentionFileWithHook(root, "notes.txt", func(stage mentionReadStage, _ string) {
			if stage != mentionReadRootOpened {
				return
			}
			if renameErr := os.Rename(root, filepath.Join(parent, "old-project")); renameErr != nil {
				t.Fatal(renameErr)
			}
			if renameErr := os.Rename(replacement, root); renameErr != nil {
				t.Fatal(renameErr)
			}
		})
		if err == nil {
			t.Fatal("read followed a replaced project root")
		}
	})

	t.Run("directory", func(t *testing.T) {
		root := t.TempDir()
		original := filepath.Join(root, "sub")
		replacement := filepath.Join(root, "replacement")
		for _, directory := range []string{original, replacement} {
			if err := os.Mkdir(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte(directory), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, err := readMentionFileWithHook(root, "sub/notes.txt", func(stage mentionReadStage, name string) {
			if stage != mentionReadDirectoryReady || name != "sub" {
				return
			}
			if renameErr := os.Rename(original, filepath.Join(root, "old-sub")); renameErr != nil {
				t.Fatal(renameErr)
			}
			if renameErr := os.Rename(replacement, original); renameErr != nil {
				t.Fatal(renameErr)
			}
		})
		if err == nil {
			t.Fatal("read followed a replaced child directory")
		}
	})

	t.Run("file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "notes.txt")
		replacement := filepath.Join(root, "replacement.txt")
		if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(replacement, []byte("replacement"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := readMentionFileWithHook(root, "notes.txt", func(stage mentionReadStage, _ string) {
			if stage != mentionReadFileRead {
				return
			}
			if renameErr := os.Rename(path, filepath.Join(root, "old-notes.txt")); renameErr != nil {
				t.Fatal(renameErr)
			}
			if renameErr := os.Rename(replacement, path); renameErr != nil {
				t.Fatal(renameErr)
			}
		})
		if err == nil {
			t.Fatal("read accepted a replaced leaf file")
		}
	})
}

func TestExpandMentionPromptHandlesEmptyUTF8BoundaryAndAttachmentLimits(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	boundaries := map[string]string{
		"boundary.txt":         strings.Repeat("a", mentionContentLimit-1) + "é",
		"partial-boundary.txt": strings.Repeat("a", mentionContentLimit) + "💠",
	}
	for name, content := range boundaries {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := range mentionAttachmentLimit + 1 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("small-%d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	empty := expandMentionPrompt("@empty.txt", root)
	if empty != "<file name=\"empty.txt\">\n</file>" {
		t.Fatalf("empty expansion = %q", empty)
	}
	for name := range boundaries {
		truncated := expandMentionPrompt("@"+name, root)
		if !utf8.ValidString(truncated) || !strings.Contains(truncated, "[content truncated by snow]") {
			t.Fatalf("UTF-8 boundary expansion for %s is invalid or unmarked: valid=%v suffix=%q", name, utf8.ValidString(truncated), truncated[len(truncated)-min(80, len(truncated)):])
		}
	}
	duplicate := expandMentionPrompt("@empty.txt and @empty.txt", root)
	if strings.Count(duplicate, "<file name=") != 1 || !strings.Contains(duplicate, "duplicate attachment omitted") {
		t.Fatalf("duplicate expansion was not bounded: %q", duplicate)
	}
	var prompt strings.Builder
	for i := range mentionAttachmentLimit + 1 {
		fmt.Fprintf(&prompt, "@small-%d.txt ", i)
	}
	bounded := expandMentionPrompt(prompt.String(), root)
	if strings.Count(bounded, "<file name=") != mentionAttachmentLimit || !strings.Contains(bounded, mentionAttachmentOmission) {
		t.Fatalf("attachment-count expansion was not bounded: files=%d output=%q", strings.Count(bounded, "<file name="), bounded)
	}
}

func TestExpandMentionPromptBoundsFailingReadAttempts(t *testing.T) {
	var prompt strings.Builder
	prompt.WriteString("@missing-0.txt @missing-0.txt ")
	for i := 1; i < mentionAttachmentLimit+2; i++ {
		fmt.Fprintf(&prompt, "@missing-%d.txt ", i)
	}

	reads := make(map[string]int)
	expanded := expandMentionPromptWithReader(prompt.String(), t.TempDir(), func(_ string, path string) ([]byte, error) {
		reads[path]++
		return nil, os.ErrNotExist
	})
	if len(reads) != mentionAttachmentLimit {
		t.Fatalf("failing read attempts = %d, want %d: %v", len(reads), mentionAttachmentLimit, reads)
	}
	for path, count := range reads {
		if count != 1 {
			t.Fatalf("failing path %q read %d times, want once", path, count)
		}
	}
	if got := strings.Count(expanded, mentionAttachmentFailure); got != mentionAttachmentLimit {
		t.Fatalf("failure markers = %d, want %d: %q", got, mentionAttachmentLimit, expanded)
	}
	if !strings.Contains(expanded, "duplicate attachment omitted") || !strings.Contains(expanded, mentionAttachmentOmission) {
		t.Fatalf("bounded failures lack duplicate or limit marker: %q", expanded)
	}
}

func TestModelMentionsUseCanonicalProjectRootForSymlinkLaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	testHome(t)
	parent := t.TempDir()
	project := filepath.Join(parent, "project")
	alias := filepath.Join(parent, "project-link")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "notes.md"), []byte("canonical notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, alias); err != nil {
		t.Skipf("cannot create project symlink: %v", err)
	}
	a, err := app.New(t.Context(), app.Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: alias,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	m := newModel(t.Context(), app.Options{})
	m.app = a

	m.editor.SetValue("read @not")
	if cmd := m.refreshInputCompletions(); cmd != nil {
		m.Update(cmd())
	}
	if !m.mentionVisible || !slices.Contains(m.mentionMatches, "notes.md") {
		t.Fatalf("symlink-launch picker = visible %v matches %v; cwd=%q root=%q", m.mentionVisible, m.mentionMatches, a.CWD(), a.ProjectInputRoot)
	}
	if expanded := m.expandedPrompt("@notes.md"); !strings.Contains(expanded, "canonical notes") {
		t.Fatalf("symlink-launch expansion did not use canonical root: %q", expanded)
	}
}

func TestModelMentionPickerInsertsFileReference(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := os.WriteFile(filepath.Join(m.app.CWD(), "notes.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}

	m.editor.SetValue("please read @no")
	if cmd := m.refreshInputCompletions(); cmd != nil {
		m.Update(cmd())
	}
	if !m.mentionVisible || len(m.mentionMatches) != 1 || m.mentionMatches[0] != "notes.md" {
		t.Fatalf("mention picker = visible %v, matches %v", m.mentionVisible, m.mentionMatches)
	}
	_, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.editor.Value(); got != "please read @notes.md " {
		t.Fatalf("editor after mention = %q", got)
	}
	if m.mentionVisible {
		t.Fatal("mention picker should close after insertion")
	}
}

func TestModelMentionPickerBrowsesFoldersAndPreservesSuffix(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	root := m.app.CWD()
	if err := os.MkdirAll(filepath.Join(root, "folder name"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "folder name", "notes file.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}

	m.editor.SetValue("please read @fol afterwards")
	m.editor.CursorUp()
	m.editor.SetCursorColumn(len([]rune("please read @fol")))
	if cmd := m.refreshInputCompletions(); cmd != nil {
		m.Update(cmd())
	}
	if !m.mentionVisible || !slices.Contains(m.mentionMatches, "folder name/") {
		t.Fatalf("folder picker = visible %v, matches %v", m.mentionVisible, m.mentionMatches)
	}
	m.mentionIndex = slices.Index(m.mentionMatches, "folder name/")
	_, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		m.Update(cmd())
	}
	if got := m.editor.Value(); got != `please read @"folder name/ afterwards` {
		t.Fatalf("editor after folder = %q", got)
	}
	if !m.mentionVisible || !slices.Contains(m.mentionMatches, "folder name/notes file.md") {
		t.Fatalf("nested picker = visible %v, matches %v", m.mentionVisible, m.mentionMatches)
	}
	m.mentionIndex = slices.Index(m.mentionMatches, "folder name/notes file.md")
	_, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.editor.Value(); got != `please read @"folder name/notes file.md"  afterwards` {
		t.Fatalf("editor after nested file = %q", got)
	}
}

func TestModelShowsPermissionModeInFooter(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()
	if got := stripANSI(m.renderFooter()); !strings.Contains(got, "permission: allow") {
		t.Fatalf("footer = %q, want current permission mode", got)
	}
	if got := m.permissionStatusStyle().GetForeground(); got != colorErr {
		t.Fatalf("allow foreground = %v, want red %v", got, colorErr)
	}
	m.app.Perm.SetMode(permission.ModeAsk)
	if got := m.permissionStatusStyle().GetForeground(); got != colorOk {
		t.Fatalf("ask foreground = %v, want green %v", got, colorOk)
	}
	m.app.Perm.SetMode(permission.ModeDeny)
	if got := stripANSI(m.renderFooter()); !strings.Contains(got, "permission: deny") {
		t.Fatalf("footer = %q, want updated permission mode", got)
	}
}
