package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestWrapTranscriptPreservesWidthAndTerminalState(t *testing.T) {
	for name, text := range map[string]string{
		"empty":       "",
		"paragraphs":  "first paragraph wraps at word boundaries\n\nsecond\n",
		"hard wrap":   strings.Repeat("abc", 20),
		"whitespace":  "tab\tindented\r\nnext  line  ",
		"graphemes":   "界界 e\u0301 👩‍💻 🇲🇦 text wraps",
		"color":       "\x1b[31mred text across many wrapped lines\x1b[0m plain",
		"RGB style":   "\x1b[1;38;2;1;2;3mcolor\nmore colored words\x1b[0m",
		"style edits": "\x1b[1;31mred\n\x1b[22;32mgreen\n\x1b[0mplain\n\x1b[4munderlined\x1b[0m",
		"links":       ansi.SetHyperlink("https://example.com", "id=example") + "linked words across rows\nmore" + ansi.ResetHyperlink(),
		"styled link": ansi.SetHyperlink("https://example.com", "") + "\x1b[32mlink and green across rows\x1b[0m" + ansi.ResetHyperlink(),
	} {
		t.Run(name, func(t *testing.T) {
			for _, width := range []int{1, 4, 12, 80} {
				want := lipgloss.NewStyle().Width(width).Render(text)
				if got := wrapTranscript(text, width); got != want {
					t.Errorf("width %d:\n got %q\nwant %q", width, got, want)
				}
			}
		})
	}
}

func TestRenderUserMessageBuildsFullWidthSurface(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	m.transcript.SetWidth(24)

	rendered := m.renderUserMessage("a long user prompt that wraps\nthen continues")
	if rendered == stripANSI(rendered) {
		t.Fatal("user message surface has no terminal styling")
	}
	rows := strings.Split(rendered, "\n")
	if len(rows) < 5 {
		t.Fatalf("user message rows=%d, want padded wrapped multiline content", len(rows))
	}
	if strings.TrimSpace(stripANSI(rows[0])) != "" || strings.TrimSpace(stripANSI(rows[len(rows)-1])) != "" {
		t.Fatalf("user message lacks blank container rows: first=%q last=%q", stripANSI(rows[0]), stripANSI(rows[len(rows)-1]))
	}
	for i, row := range rows {
		if width := lipgloss.Width(row); width != m.transcript.Width() {
			t.Errorf("row %d width=%d, want %d", i, width, m.transcript.Width())
		}
		plain := stripANSI(row)
		if !strings.HasPrefix(plain, " ") || !strings.HasSuffix(plain, " ") {
			t.Errorf("row %d lacks horizontal container padding: %q", i, plain)
		}
	}
	plain := stripANSI(rendered)
	for _, want := range []string{"› a long user prompt", "then continues"} {
		if !strings.Contains(plain, want) {
			t.Errorf("rendered user message missing %q: %q", want, plain)
		}
	}
}

func TestIsolateTranscriptRowsClosesOpenStyleAndLink(t *testing.T) {
	link := ansi.SetHyperlink("https://example.com", "id=test")
	got := isolateTranscriptRows(link + "\x1b[31mfirst\nsecond")
	want := link + "\x1b[31mfirst\x1b[m" + ansi.ResetHyperlink() + "\n" +
		link + "\x1b[31msecond\x1b[m" + ansi.ResetHyperlink()
	if got != want {
		t.Fatalf("rows do not own their styles:\n got %q\nwant %q", got, want)
	}
}
