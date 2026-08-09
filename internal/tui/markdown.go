package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	xansi "github.com/charmbracelet/x/ansi"
)

// mdRenderer converts markdown assistant content to ANSI for the transcript.
// One renderer is reused across messages; renders are cached by (raw, width)
// so the streaming path re-renders only when a new delta actually changes the
// text. Width changes (terminal resize) recreate the renderer and drop the
// cache.
type mdRenderer struct {
	mu       sync.Mutex
	renderer *glamour.TermRenderer
	width    int
	lastRaw  string
	lastOut  string
	lastW    int
	style    *ansi.StyleConfig
}

func newMarkdownRenderer() *mdRenderer { return &mdRenderer{} }

func newThinkingMarkdownRenderer() *mdRenderer {
	style := styles.DarkStyleConfig
	muted := "245"
	zero := uint(0)
	style.Document.Color = nil
	style.Document.Margin = &zero
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	style.Paragraph.Color = &muted
	style.Paragraph.Margin = &zero
	style.Text.Color = &muted
	style.Heading.Color = &muted
	for _, heading := range []*ansi.StyleBlock{&style.H1, &style.H2, &style.H3, &style.H4, &style.H5, &style.H6} {
		heading.Color = &muted
		heading.BackgroundColor = nil
		heading.Margin = &zero
	}
	return &mdRenderer{style: &style}
}

// render converts markdown to ANSI, word-wrapped at width. On any failure it
// degrades to the raw text so the transcript never goes blank.
func (r *mdRenderer) render(md string, width int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if width < 10 {
		width = 10
	}
	if r.renderer == nil || r.width != width {
		var tr *glamour.TermRenderer
		var err error
		if r.style != nil {
			tr, err = glamour.NewTermRenderer(
				glamour.WithStyles(*r.style),
				glamour.WithWordWrap(width),
			)
		} else {
			tr, err = glamour.NewTermRenderer(
				glamour.WithStandardStyle("dark"),
				glamour.WithWordWrap(width),
			)
		}
		if err != nil {
			return md
		}
		r.renderer = tr
		r.width = width
		r.lastRaw, r.lastOut, r.lastW = "", "", 0
	}
	if md == r.lastRaw && r.lastW == width {
		return r.lastOut
	}
	out, err := r.renderer.Render(md)
	if err != nil {
		return md
	}
	out = strings.TrimRight(out, "\n")
	if r.style != nil {
		out = trimANSIRight(out)
	}
	r.lastRaw, r.lastOut, r.lastW = md, out, width
	return out
}

func trimANSIRight(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		visible := strings.TrimRight(xansi.Strip(line), " \t\r")
		if visible == "" {
			lines[i] = ""
			continue
		}
		lines[i] = xansi.Truncate(line, xansi.StringWidth(visible), "")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
