package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

// mdRenderer converts markdown assistant content to ANSI for the transcript.
// One renderer is reused across messages; renders are cached by (raw, width)
// so the streaming path re-renders only when a new delta actually changes the
// text. Width or theme changes recreate the renderer and drop the cache.
type mdRenderer struct {
	mu       sync.Mutex
	renderer *glamour.TermRenderer
	width    int
	lastRaw  string
	lastOut  string
	lastW    int
	style    *ansi.StyleConfig
	thinking bool
}

func newMarkdownRenderer() *mdRenderer {
	return &mdRenderer{style: markdownStyleForTheme(activeTUITheme, lipgloss.HasDarkBackground(), false)}
}

func newThinkingMarkdownRenderer() *mdRenderer {
	return &mdRenderer{
		style:    markdownStyleForTheme(activeTUITheme, lipgloss.HasDarkBackground(), true),
		thinking: true,
	}
}

func resolvedThemeColor(color lipgloss.TerminalColor, dark bool) string {
	switch color := color.(type) {
	case lipgloss.AdaptiveColor:
		if dark {
			return color.Dark
		}
		return color.Light
	case lipgloss.Color:
		return string(color)
	default:
		return ""
	}
}

func markdownStyleForTheme(theme tuiTheme, dark, thinking bool) *ansi.StyleConfig {
	style := styles.DarkStyleConfig
	foreground := resolvedThemeColor(theme.soft, dark)
	accent := resolvedThemeColor(theme.accent, dark)
	muted := resolvedThemeColor(theme.muted, dark)
	warning := resolvedThemeColor(theme.warn, dark)
	if thinking {
		foreground = muted
		warning = muted
	}

	primitives := []*ansi.StylePrimitive{
		&style.Document.StylePrimitive,
		&style.BlockQuote.StylePrimitive,
		&style.Paragraph.StylePrimitive,
		&style.List.StylePrimitive,
		&style.Heading.StylePrimitive,
		&style.H1.StylePrimitive,
		&style.H2.StylePrimitive,
		&style.H3.StylePrimitive,
		&style.H4.StylePrimitive,
		&style.H5.StylePrimitive,
		&style.H6.StylePrimitive,
		&style.Text,
		&style.Strikethrough,
		&style.Emph,
		&style.Strong,
		&style.HorizontalRule,
		&style.Item,
		&style.Enumeration,
		&style.Task.StylePrimitive,
		&style.Link,
		&style.LinkText,
		&style.Image,
		&style.ImageText,
		&style.Code.StylePrimitive,
		&style.CodeBlock.StylePrimitive,
		&style.Table.StylePrimitive,
		&style.DefinitionList.StylePrimitive,
		&style.DefinitionTerm,
		&style.DefinitionDescription,
		&style.HTMLBlock.StylePrimitive,
		&style.HTMLSpan.StylePrimitive,
	}
	for _, primitive := range primitives {
		primitive.Color = stringPointer(foreground)
		primitive.BackgroundColor = nil
	}

	style.BlockQuote.Color = stringPointer(muted)
	style.HorizontalRule.Color = stringPointer(muted)
	style.Link.Color = stringPointer(muted)
	style.LinkText.Color = stringPointer(accent)
	style.Image.Color = stringPointer(muted)
	style.ImageText.Color = stringPointer(accent)
	style.Item.Color = stringPointer(accent)
	style.Enumeration.Color = stringPointer(accent)
	style.Code.Color = stringPointer(warning)
	style.CodeBlock.Color = stringPointer(warning)
	style.CodeBlock.Theme = ""
	style.CodeBlock.Chroma = nil
	style.Table.Color = stringPointer(muted)
	for _, heading := range []*ansi.StyleBlock{&style.Heading, &style.H1, &style.H2, &style.H3, &style.H4, &style.H5, &style.H6} {
		heading.Color = stringPointer(accent)
	}

	zero := uint(0)
	style.Document.Margin = &zero
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	style.Paragraph.Margin = &zero
	return &style
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (r *mdRenderer) applyTheme(theme tuiTheme) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.style = markdownStyleForTheme(theme, lipgloss.HasDarkBackground(), r.thinking)
	r.renderer = nil
	r.width = 0
	r.lastRaw = ""
	r.lastOut = ""
	r.lastW = 0
	r.mu.Unlock()
}

// clearCache releases the last raw and rendered documents while retaining the
// width-specific renderer. Finalized transcript rows own their rendered output,
// so keeping these duplicate strings would only extend their lifetime.
func (r *mdRenderer) clearCache() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.lastRaw = ""
	r.lastOut = ""
	r.lastW = 0
	r.mu.Unlock()
}

func (m *Model) clearFinalizedMarkdownCaches() {
	if m == nil {
		return
	}
	m.md.clearCache()
	m.thinkingMD.clearCache()
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
		if r.style == nil {
			r.style = markdownStyleForTheme(activeTUITheme, lipgloss.HasDarkBackground(), r.thinking)
		}
		tr, err := glamour.NewTermRenderer(
			glamour.WithStyles(*r.style),
			glamour.WithWordWrap(width),
		)
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
