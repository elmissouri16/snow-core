package textarea_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	upstream "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	local "github.com/elmissouri16/snow-core/internal/tui/textarea"
)

// Keep the unmodified pinned component as an independent rendering/navigation
// oracle. This covers both Update's viewport preparation and View after scrolling.
func TestVisibleRowsMatchUpstream(t *testing.T) {
	for _, word := range []string{"word ", "café ", "日本語 ", "👩‍💻 ", "a\u0301 "} {
		for _, width := range []int{9, 40, 120} {
			for _, virtual := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/virtual=%v", word, width, virtual), func(t *testing.T) {
					m, want := local.New(), upstream.New()
					m.CharLimit, want.CharLimit = 0, 0
					m.MaxHeight, want.MaxHeight = 0, 0
					m.SetVirtualCursor(virtual)
					want.SetVirtualCursor(virtual)
					m.SetWidth(width)
					want.SetWidth(width)
					m.SetHeight(4)
					want.SetHeight(4)
					m.Focus()
					want.Focus()
					text := strings.Repeat(word, 250) + "\n" + strings.Repeat(word, 60) + "\nlast"
					m.SetValue(text)
					want.SetValue(text)
					check := func(step string) {
						t.Helper()
						if got, expected := m.View(), want.View(); got != expected {
							t.Fatalf("%s view mismatch\ngot: %q\nwant: %q", step, got, expected)
						}
						if m.Value() != want.Value() || m.Line() != want.Line() || m.Column() != want.Column() ||
							m.ScrollYOffset() != want.ScrollYOffset() || m.ScrollPercent() != want.ScrollPercent() ||
							m.SelectedText() != want.SelectedText() || !reflect.DeepEqual(m.Cursor(), want.Cursor()) {
							t.Fatalf("%s editor state differs", step)
						}
					}
					check("initial")
					keys := []tea.KeyPressMsg{
						{Code: tea.KeyUp}, {Code: tea.KeyUp, Mod: tea.ModShift}, {Code: tea.KeyUp, Mod: tea.ModShift},
						{Code: tea.KeyDown, Mod: tea.ModShift}, {Code: 'x', Text: "界"}, {Code: tea.KeyBackspace},
						{Code: tea.KeyHome, Mod: tea.ModCtrl}, {Code: tea.KeyDown}, {Code: tea.KeyPgDown},
						{Code: tea.KeyPgUp}, {Code: tea.KeyEnd, Mod: tea.ModCtrl}, {Code: 'g', Mod: tea.ModCtrl},
					}
					for i, key := range keys {
						m, _ = m.Update(key)
						want, _ = want.Update(key)
						check(fmt.Sprintf("key %d", i))
					}
					m.BeginSelection(3, 0)
					want.BeginSelection(3, 0)
					m.ExtendSelection(width-2, 3)
					want.ExtendSelection(width-2, 3)
					m.EndSelection()
					want.EndSelection()
					check("mouse selection")
					for _, w := range []int{17, 80, 5, width} {
						m.SetWidth(w)
						want.SetWidth(w)
						check("resize")
					}
					m.ClearSelection()
					want.ClearSelection()
					m.MoveToBegin()
					want.MoveToBegin()
					check("begin")
					for range 20 {
						m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
						want, _ = want.Update(tea.KeyPressMsg{Code: tea.KeyDown})
						check("scroll")
					}
					m.DynamicHeight, want.DynamicHeight = true, true
					m.MaxHeight, want.MaxHeight = 6, 6
					m.SetValue("short\ntext")
					want.SetValue("short\ntext")
					check("dynamic shrink")
					styles, wantStyles := m.Styles(), want.Styles()
					styles.Focused.Base = lipgloss.NewStyle().Padding(1).Border(lipgloss.NormalBorder())
					wantStyles.Focused.Base = styles.Focused.Base
					m.SetStyles(styles)
					want.SetStyles(wantStyles)
					m.SetWidth(width)
					want.SetWidth(width)
					check("style change")
					m.Blur()
					want.Blur()
					check("blur")
					m.Reset()
					want.Reset()
					m.Placeholder, want.Placeholder = "placeholder text", "placeholder text"
					check("placeholder")
				})
			}
		}
	}
}
