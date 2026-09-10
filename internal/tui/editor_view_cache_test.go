package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/elmissouri16/snow-core/internal/app"
)

func TestEditorViewCacheTracksBufferCursorSelectionAndSize(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	m.width, m.height = 80, 24
	m.editor.SetValue("original 界 é")
	m.editor.CursorEnd()
	m.layout()
	first := m.cachedEditorView()
	if !m.editorViewCache.valid || first != m.cachedEditorView() {
		t.Fatal("stable editor view was not cached")
	}
	check := func(label string) {
		t.Helper()
		got := m.cachedEditorView()
		if want := m.editor.View(); got != want {
			t.Fatalf("stale %s view:\ngot %q\nwant %q", label, got, want)
		}
	}
	m.editor.SetValue("changed value")
	check("buffer")
	m.editor.CursorStart()
	check("cursor")
	m.editor.SelectAll()
	check("selection")
	m.editor.ClearSelection()
	m.editor.Blur()
	check("blur")
	m.editor.Focus()
	m.width = 40
	m.layout()
	check("resize")
	m.updateEditor(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.editorViewCache.valid {
		t.Fatal("component update did not invalidate cursor/blink rendering")
	}
	check("component update")
	m.refreshThemeStyles()
	if m.editorViewCache.valid {
		t.Fatal("theme refresh did not invalidate rendering")
	}
	check("theme")
}

func TestEditorViewCacheIsBounded(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	m.editor.SetValue(strings.Repeat("text ", maxEditorViewCacheBytes/5+1))
	_ = m.cachedEditorView()
	if m.editorViewCache.valid || m.editorViewCache.view != "" || m.editorViewCache.key.value != "" {
		t.Fatal("oversized composer retained in view cache")
	}
}
