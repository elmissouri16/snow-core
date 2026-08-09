package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// tuiKeyMap is the single source of truth for the bindings advertised by the
// TUI. Textarea navigation remains owned by Bubbles; these bindings are only
// consulted by Snow's outer model and picker handlers.
type tuiKeyMap struct {
	Submit         key.Binding
	Newline        key.Binding
	Paste          key.Binding
	Abort          key.Binding
	Quit           key.Binding
	Mode           key.Binding
	PageUp         key.Binding
	PageDown       key.Binding
	Top            key.Binding
	Bottom         key.Binding
	LineUp         key.Binding
	LineDown       key.Binding
	PickerUp       key.Binding
	PickerDown     key.Binding
	PickerPrev     key.Binding
	PickerNext     key.Binding
	PickerPageUp   key.Binding
	PickerPageDown key.Binding
	PickerTop      key.Binding
	PickerBottom   key.Binding
	Accept         key.Binding
	Close          key.Binding
}

var tuiKeys = tuiKeyMap{
	Submit:         key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
	Newline:        key.NewBinding(key.WithKeys("ctrl+j", "alt+enter"), key.WithHelp("ctrl+j", "newline")),
	Paste:          key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "paste")),
	Abort:          key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("ctrl+c/esc", "abort")),
	Quit:           key.NewBinding(key.WithKeys("ctrl+c", "ctrl+d"), key.WithHelp("ctrl+c/ctrl+d", "quit")),
	Mode:           key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "mode")),
	PageUp:         key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
	PageDown:       key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
	Top:            key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
	Bottom:         key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
	LineUp:         key.NewBinding(key.WithKeys("ctrl+up"), key.WithHelp("ctrl+↑", "line up")),
	LineDown:       key.NewBinding(key.WithKeys("ctrl+down"), key.WithHelp("ctrl+↓", "line down")),
	PickerUp:       key.NewBinding(key.WithKeys("up", "left", "k"), key.WithHelp("↑/←/k", "previous")),
	PickerDown:     key.NewBinding(key.WithKeys("down", "right", "j"), key.WithHelp("↓/→/j", "next")),
	PickerPrev:     key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous")),
	PickerNext:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
	PickerPageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
	PickerPageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
	PickerTop:      key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "first")),
	PickerBottom:   key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "last")),
	Accept:         key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Close:          key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
}

// ShortHelp implements bubbles/help.KeyMap for the always-visible footer.
func (k tuiKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Newline, k.Paste, k.Mode, k.PageUp, k.PageDown}
}

// FullHelp implements bubbles/help.KeyMap for the detailed /help output.
func (k tuiKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Submit, k.Newline, k.Paste, k.Mode},
		{k.PageUp, k.PageDown, k.Top, k.Bottom, k.LineUp, k.LineDown},
		{k.PickerUp, k.PickerDown, k.PickerNext, k.PickerPrev, k.Accept, k.Close},
	}
}

func keyMatches(msg tea.KeyMsg, binding key.Binding) bool {
	return key.Matches(msg, binding)
}

func normalizePickerKey(msg tea.KeyMsg) tea.KeyMsg {
	switch pickerKeyAction(msg) {
	case pickerUp:
		return tea.KeyMsg{Type: tea.KeyUp}
	case pickerDown:
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return msg
	}
}

// pickerAction normalizes navigation across all list-like overlays. j/k are
// deliberately handled here, never in the regular textarea path.
type pickerAction uint8

const (
	pickerNone pickerAction = iota
	pickerUp
	pickerDown
	pickerPrev
	pickerNext
	pickerPageUp
	pickerPageDown
	pickerTop
	pickerBottom
	pickerAccept
	pickerClose
)

func pickerKeyAction(msg tea.KeyMsg) pickerAction {
	switch {
	case keyMatches(msg, tuiKeys.PickerUp):
		return pickerUp
	case keyMatches(msg, tuiKeys.PickerDown):
		return pickerDown
	case keyMatches(msg, tuiKeys.PickerPrev):
		return pickerPrev
	case keyMatches(msg, tuiKeys.PickerNext):
		return pickerNext
	case keyMatches(msg, tuiKeys.PickerPageUp):
		return pickerPageUp
	case keyMatches(msg, tuiKeys.PickerPageDown):
		return pickerPageDown
	case keyMatches(msg, tuiKeys.PickerTop):
		return pickerTop
	case keyMatches(msg, tuiKeys.PickerBottom):
		return pickerBottom
	case keyMatches(msg, tuiKeys.Accept):
		return pickerAccept
	case keyMatches(msg, tuiKeys.Close):
		return pickerClose
	default:
		return pickerNone
	}
}

func movePicker(index, count int, action pickerAction, pageSize int) (int, bool) {
	if count <= 0 {
		return 0, action != pickerNone
	}
	if pageSize < 1 {
		pageSize = 1
	}
	switch action {
	case pickerUp, pickerPrev:
		return movePickerIndex(index, count, -1), true
	case pickerDown, pickerNext:
		return movePickerIndex(index, count, 1), true
	case pickerPageUp:
		return clampPickerIndex(index-pageSize, count), true
	case pickerPageDown:
		return clampPickerIndex(index+pageSize, count), true
	case pickerTop:
		return 0, true
	case pickerBottom:
		return count - 1, true
	default:
		return index, false
	}
}

func movePickerIndex(index, count, delta int) int {
	if count <= 0 {
		return 0
	}
	index = (index + delta) % count
	if index < 0 {
		index += count
	}
	return index
}

func clampPickerIndex(index, count int) int {
	if count <= 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= count {
		return count - 1
	}
	return index
}
