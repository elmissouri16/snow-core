package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/elmissouri16/snow-core/internal/tui/textarea"
)

const maxEditorViewCacheBytes = 32 << 10

type editorViewKey struct {
	value, prompt, placeholder      string
	width, height, row, col, offset int
	focused, selected               bool
	start, end                      textarea.Position
}

type editorViewCache struct {
	key   editorViewKey
	view  string
	valid bool
}

// Buffer/navigation changes are in the key. Cursor blink and style changes
// invalidate explicitly at the component update and theme boundaries. Thus
// transcript streaming or selection can reuse the untouched composer view.
func (m *Model) cachedEditorView() string {
	start, end, selected := m.editor.Selection()
	key := editorViewKey{
		value: m.editor.Value(), prompt: m.editor.Prompt, placeholder: m.editor.Placeholder,
		width: m.editor.Width(), height: m.editor.Height(), row: m.editor.Line(), col: m.editor.Column(),
		offset: m.editor.ScrollYOffset(), focused: m.editor.Focused(), selected: selected, start: start, end: end,
	}
	if m.editorViewCache.valid && m.editorViewCache.key == key {
		return m.editorViewCache.view
	}
	view := m.editor.View()
	if len(key.value)+len(view) <= maxEditorViewCacheBytes {
		m.editorViewCache = editorViewCache{key: key, view: view, valid: true}
	} else {
		m.editorViewCache = editorViewCache{}
	}
	return view
}

func (m *Model) updateEditor(msg tea.Msg) tea.Cmd {
	m.editorViewCache = editorViewCache{}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return cmd
}
