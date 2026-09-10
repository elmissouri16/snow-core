package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFrameLayoutMatchesStyledLayout(t *testing.T) {
	fixtures := []string{
		"", "plain", "first\n\nlast\n", "a   \nb  ", "tab\tvalue\r\nnext",
		strings.Repeat("words wrap here ", 12),
		"\x1b[31mred words wrap across several lines\x1b[0m",
		"\x1b[1;38;2;11;22;33mbold\nnext\x1b[0m",
		ansi.SetHyperlink("https://example.com", "id=test") + "link\nmore words" + ansi.ResetHyperlink(),
		"日本語 é 👩‍💻 🇲🇦",
	}
	for i, text := range fixtures {
		for _, width := range []int{4, 12, 40, 120} {
			for _, height := range []int{1, 3, 12} {
				for _, bottom := range []bool{false, true} {
					t.Run(fmt.Sprintf("%d/%dx%d/bottom-%t", i, width, height, bottom), func(t *testing.T) {
						style := lipgloss.NewStyle().Width(width).Height(height).MaxWidth(width).MaxHeight(height)
						if bottom {
							style = style.AlignVertical(lipgloss.Bottom)
						}
						want := style.Render(text)
						if got := fitFrameAligned(text, width, height, bottom); got != want {
							t.Fatalf("got %q\nwant %q", got, want)
						}
					})
				}
			}
		}
	}
}

func TestFrameLayoutBoundsWideClustersAndOpenStyles(t *testing.T) {
	for _, text := range []string{"界", "👩‍💻", "1️⃣", "\x1b[31mred\nmore", ansi.SetHyperlink("https://example.com", "") + "link"} {
		for _, width := range []int{1, 2, 4} {
			frame := fitFrame(text, width, 2)
			lines := strings.Split(frame, "\n")
			if len(lines) != 2 {
				t.Fatalf("wrong row count: %q", frame)
			}
			for _, line := range lines {
				if cells := ansi.StringWidth(line); cells != width {
					t.Fatalf("row width=%d want=%d: %q", cells, width, line)
				}
			}
		}
	}
}

func TestLineCellWidthMatchesRenderer(t *testing.T) {
	for _, text := range []string{"", "ASCII text  ", "日本語", "é", "1️⃣", "👩‍💻 🇲🇦", "\ttext\r", "\x1b[31mred\x1b[m", "\x9b31mred\x9b0m", ansi.SetHyperlink("https://example.com", "") + "link" + ansi.ResetHyperlink()} {
		if got, want := lineCellWidth(text), ansi.StringWidth(text); got != want {
			t.Errorf("%q: width=%d want=%d", text, got, want)
		}
	}
}
