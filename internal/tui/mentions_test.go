package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/permission"
)

func TestDiscoverMentionFilesSkipsGeneratedAndSymlinkedPaths(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		"README.md",
		filepath.Join("internal", "tui", "tui.go"),
		filepath.Join("node_modules", "ignored.js"),
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
	for _, unwanted := range []string{"node_modules", ".git"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("mention files should skip %q: %v", unwanted, files)
		}
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
	if replaceMentionToken("read @README", len("read "), "README.md") != "read @README.md " {
		t.Fatal("replaceMentionToken should replace the current token")
	}
}

func TestExpandMentionPromptIncludesTextFileContents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("important notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := expandMentionPrompt("summarize @notes.md", root, []string{"notes.md"})
	for _, want := range []string{"<file name=\"notes.md\">", "important notes", "</file>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expanded prompt missing %q: %q", want, got)
		}
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
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.editor.Value(); got != "please read @notes.md " {
		t.Fatalf("editor after mention = %q", got)
	}
	if m.mentionVisible {
		t.Fatal("mention picker should close after insertion")
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
