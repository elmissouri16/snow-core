package tui

import (
	tea "charm.land/bubbletea/v2"
	legacygloss "github.com/charmbracelet/lipgloss"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/muesli/termenv"
	"strings"
	"testing"
)

func BenchmarkStableComposer(b *testing.B) {
	oldProfile, oldDark := legacygloss.ColorProfile(), legacygloss.HasDarkBackground()
	legacygloss.SetColorProfile(termenv.TrueColor)
	legacygloss.SetHasDarkBackground(true)
	defer legacygloss.SetColorProfile(oldProfile)
	defer legacygloss.SetHasDarkBackground(oldDark)
	for _, tc := range []struct{ name, word string }{
		{"ascii", "word "}, {"accents", "café "}, {"cjk", "日本語 "}, {"emoji", "👩‍💻 "},
	} {
		b.Run(tc.name, func(b *testing.B) {
			m := newModel(b.Context(), app.Options{})
			defer m.Close()
			m.width, m.height = 120, 40
			payload := strings.Repeat(tc.word, 8192/len(tc.word))
			m.editor.SetValue(payload)
			m.editor.CursorEnd()
			m.layout()
			b.ReportAllocs()
			for b.Loop() {
				_, _ = m.updateComposerEditor(tea.KeyPressMsg{Code: 'x', Text: "x"})
				m.layout()
				_ = m.View()
				_, _ = m.updateComposerEditor(tea.KeyPressMsg{Code: tea.KeyBackspace})
				m.layout()
				_ = m.View()
			}
		})
	}
}
