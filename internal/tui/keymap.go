package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// tuiKeyMap is the single source of truth for the bindings advertised by the
// TUI. Textarea navigation remains owned by Bubbles; these bindings are only
// consulted by Snow's outer model and picker handlers.
type tuiKeyMap struct {
	Submit          key.Binding
	FollowUp        key.Binding
	Newline         key.Binding
	Paste           key.Binding
	Abort           key.Binding
	Quit            key.Binding
	Mode            key.Binding
	ToggleWorktrees key.Binding
	PageUp          key.Binding
	PageDown        key.Binding
	Top             key.Binding
	Bottom          key.Binding
	LineUp          key.Binding
	LineDown        key.Binding
	PickerUp        key.Binding
	PickerDown      key.Binding
	PickerPrev      key.Binding
	PickerNext      key.Binding
	PickerPageUp    key.Binding
	PickerPageDown  key.Binding
	PickerTop       key.Binding
	PickerBottom    key.Binding
	Accept          key.Binding
	Close           key.Binding
	BranchFork      key.Binding
	BranchRename    key.Binding
	BranchDelete    key.Binding
	Confirm         key.Binding
}

var tuiKeys = tuiKeyMap{
	Submit:          key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
	FollowUp:        key.NewBinding(key.WithKeys("alt+enter"), key.WithHelp("alt+enter", "follow-up")),
	Newline:         key.NewBinding(key.WithKeys("ctrl+j", "alt+enter"), key.WithHelp("ctrl+j", "newline")),
	Paste:           key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "paste")),
	Abort:           key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("ctrl+c/esc", "abort")),
	Quit:            key.NewBinding(key.WithKeys("ctrl+c", "ctrl+d"), key.WithHelp("ctrl+c/ctrl+d", "quit")),
	Mode:            key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "mode")),
	ToggleWorktrees: key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "worktrees")),
	PageUp:          key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
	PageDown:        key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
	Top:             key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
	Bottom:          key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
	LineUp:          key.NewBinding(key.WithKeys("ctrl+up"), key.WithHelp("ctrl+↑", "line up")),
	LineDown:        key.NewBinding(key.WithKeys("ctrl+down"), key.WithHelp("ctrl+↓", "line down")),
	PickerUp:        key.NewBinding(key.WithKeys("up", "left", "k"), key.WithHelp("↑/←/k", "previous")),
	PickerDown:      key.NewBinding(key.WithKeys("down", "right", "j"), key.WithHelp("↓/→/j", "next")),
	PickerPrev:      key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous")),
	PickerNext:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
	PickerPageUp:    key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
	PickerPageDown:  key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
	PickerTop:       key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "first")),
	PickerBottom:    key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "last")),
	Accept:          key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Close:           key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	BranchFork:      key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fork")),
	BranchRename:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename")),
	BranchDelete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Confirm:         key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm")),
}

// ShortHelp implements bubbles/help.KeyMap for the always-visible footer.
func (k tuiKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Newline, k.Paste, k.Mode, k.PageUp, k.PageDown}
}

// FullHelp implements bubbles/help.KeyMap for the detailed /help output.
func (k tuiKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Submit, k.FollowUp, k.Newline, k.Paste, k.Abort, k.Quit, k.Mode, k.ToggleWorktrees},
		{k.PageUp, k.PageDown, k.Top, k.Bottom, k.LineUp, k.LineDown},
		{k.PickerUp, k.PickerDown, k.PickerNext, k.PickerPrev, k.Accept, k.Close},
		{k.BranchFork, k.BranchRename, k.BranchDelete, k.Confirm},
	}
}

func applyKeybindingOverrides(base tuiKeyMap, overrides map[string][]string) (tuiKeyMap, error) {
	targets := map[string]*key.Binding{
		"submit": &base.Submit, "follow_up": &base.FollowUp, "newline": &base.Newline, "paste": &base.Paste, "abort": &base.Abort, "quit": &base.Quit, "toggle_mode": &base.Mode, "toggle_worktrees": &base.ToggleWorktrees,
		"page_up": &base.PageUp, "page_down": &base.PageDown, "top": &base.Top, "bottom": &base.Bottom, "line_up": &base.LineUp, "line_down": &base.LineDown,
		"picker_up": &base.PickerUp, "picker_down": &base.PickerDown, "picker_previous": &base.PickerPrev, "picker_next": &base.PickerNext,
		"picker_page_up": &base.PickerPageUp, "picker_page_down": &base.PickerPageDown, "picker_top": &base.PickerTop, "picker_bottom": &base.PickerBottom,
		"accept": &base.Accept, "close": &base.Close, "branch_fork": &base.BranchFork, "branch_rename": &base.BranchRename, "branch_delete": &base.BranchDelete, "confirm": &base.Confirm,
	}
	var names []string
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		target, ok := targets[name]
		if !ok {
			return base, fmt.Errorf("unknown keybinding action %q", name)
		}
		keys := overrides[name]
		if len(keys) == 0 {
			return base, fmt.Errorf("action %q cannot be empty", name)
		}
		seen := map[string]bool{}
		clean := make([]string, 0, len(keys))
		for _, value := range keys {
			value = strings.ToLower(strings.TrimSpace(value))
			if !validKeyName(value) {
				return base, fmt.Errorf("invalid key %q for %s", value, name)
			}
			if !seen[value] {
				seen[value] = true
				clean = append(clean, value)
			}
		}
		help := target.Help()
		*target = key.NewBinding(key.WithKeys(clean...), key.WithHelp(strings.Join(clean, "/"), help.Desc))
	}
	// Install non-removable emergency keys before collision validation so an
	// override cannot bind Escape to accept (or otherwise shadow modal close).
	base.Abort = ensureBindingKey(base.Abort, "ctrl+c")
	base.Abort = ensureBindingKey(base.Abort, "esc")
	base.Quit = ensureBindingKey(base.Quit, "ctrl+c")
	base.Close = ensureBindingKey(base.Close, "esc")
	if err := validateBindingCollisions(map[string]key.Binding{"submit": base.Submit, "newline": base.Newline, "paste": base.Paste, "toggle_mode": base.Mode, "toggle_worktrees": base.ToggleWorktrees, "abort": base.Abort}); err != nil {
		return base, err
	}
	// Busy submit/steer and follow-up share a context; newline is deliberately
	// excluded because alt+enter is a newline only while idle.
	if err := validateBindingCollisions(map[string]key.Binding{"submit": base.Submit, "follow_up": base.FollowUp, "abort": base.Abort}); err != nil {
		return base, err
	}
	if err := validateBindingCollisions(map[string]key.Binding{"picker_up": base.PickerUp, "picker_down": base.PickerDown, "picker_previous": base.PickerPrev, "picker_next": base.PickerNext, "picker_page_up": base.PickerPageUp, "picker_page_down": base.PickerPageDown, "picker_top": base.PickerTop, "picker_bottom": base.PickerBottom, "accept": base.Accept, "close": base.Close, "branch_fork": base.BranchFork, "branch_rename": base.BranchRename, "branch_delete": base.BranchDelete, "confirm": base.Confirm}); err != nil {
		return base, err
	}
	for _, mandatory := range []struct {
		name    string
		binding key.Binding
	}{{"submit", base.Submit}, {"abort", base.Abort}, {"quit", base.Quit}, {"accept", base.Accept}, {"close", base.Close}} {
		if !mandatory.binding.Enabled() {
			return base, fmt.Errorf("mandatory action %s is disabled", mandatory.name)
		}
	}
	return base, nil
}

func validateBindingCollisions(bindings map[string]key.Binding) error {
	seen := map[string]string{}
	var names []string
	for name := range bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, value := range bindings[name].Keys() {
			if old := seen[value]; old != "" {
				return fmt.Errorf("key %q collides between %s and %s", value, old, name)
			}
			seen[value] = name
		}
	}
	return nil
}

func validKeyName(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	known := map[string]bool{"enter": true, "esc": true, "tab": true, "shift+tab": true, "up": true, "down": true, "left": true, "right": true, "home": true, "end": true, "pgup": true, "pgdown": true, "backspace": true, "delete": true}
	if known[value] {
		return true
	}
	if strings.HasPrefix(value, "ctrl+") || strings.HasPrefix(value, "alt+") {
		return len([]rune(strings.TrimPrefix(strings.TrimPrefix(value, "ctrl+"), "alt+"))) == 1 || strings.HasSuffix(value, "enter") || strings.HasSuffix(value, "up") || strings.HasSuffix(value, "down")
	}
	return len([]rune(value)) == 1
}

func ensureBindingKey(binding key.Binding, value string) key.Binding {
	for _, existing := range binding.Keys() {
		if existing == value {
			return binding
		}
	}
	keys := append(binding.Keys(), value)
	help := binding.Help()
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help.Key, help.Desc))
}

func keyMatches(msg tea.KeyMsg, binding key.Binding) bool {
	return key.Matches(msg, binding)
}

func normalizePickerKeyWithMap(msg tea.KeyMsg, keys tuiKeyMap) tea.KeyMsg {
	switch pickerKeyActionWithMap(msg, keys) {
	case pickerUp:
		return tea.KeyMsg{Type: tea.KeyUp}
	case pickerDown:
		return tea.KeyMsg{Type: tea.KeyDown}
	case pickerPrev:
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case pickerNext:
		return tea.KeyMsg{Type: tea.KeyTab}
	case pickerPageUp:
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case pickerPageDown:
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case pickerTop:
		return tea.KeyMsg{Type: tea.KeyHome}
	case pickerBottom:
		return tea.KeyMsg{Type: tea.KeyEnd}
	case pickerAccept:
		return tea.KeyMsg{Type: tea.KeyEnter}
	case pickerClose:
		return tea.KeyMsg{Type: tea.KeyEsc}
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

func pickerKeyAction(msg tea.KeyMsg) pickerAction { return pickerKeyActionWithMap(msg, tuiKeys) }

func pickerKeyActionWithMap(msg tea.KeyMsg, keys tuiKeyMap) pickerAction {
	switch {
	case keyMatches(msg, keys.PickerUp):
		return pickerUp
	case keyMatches(msg, keys.PickerDown):
		return pickerDown
	case keyMatches(msg, keys.PickerPrev):
		return pickerPrev
	case keyMatches(msg, keys.PickerNext):
		return pickerNext
	case keyMatches(msg, keys.PickerPageUp):
		return pickerPageUp
	case keyMatches(msg, keys.PickerPageDown):
		return pickerPageDown
	case keyMatches(msg, keys.PickerTop):
		return pickerTop
	case keyMatches(msg, keys.PickerBottom):
		return pickerBottom
	case keyMatches(msg, keys.Accept):
		return pickerAccept
	case keyMatches(msg, keys.Close):
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
