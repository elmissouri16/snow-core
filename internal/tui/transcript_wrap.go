package tui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// wrapTranscript applies the width-only Lip Gloss layout contract without its
// v2 WrapWriter's per-byte interface writes. Each row owns its SGR/link state so
// viewport slices and selection overlays cannot lose or leak terminal styling.
func wrapTranscript(text string, width int) string {
	text = strings.ReplaceAll(text, "\t", "    ")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = isolateTranscriptRows(ansi.Wrap(text, width, ""))
	// A grapheme wider than the requested width must remain intact. Match the
	// widest resulting row as well as the requested width when padding.
	for line := range strings.SplitSeq(text, "\n") {
		width = max(width, ansi.StringWidth(line))
	}
	spaces := strings.Repeat(" ", width)
	var out strings.Builder
	out.Grow(len(text))
	first := true
	for line := range strings.SplitSeq(text, "\n") {
		if !first {
			out.WriteByte('\n')
		}
		first = false
		out.WriteString(line)
		out.WriteString(spaces[:width-ansi.StringWidth(line)])
	}
	return out.String()
}

func isolateTranscriptRows(text string) string {
	if strings.IndexByte(text, '\x1b') < 0 && strings.IndexByte(text, '\x9b') < 0 && strings.IndexByte(text, '\x9d') < 0 {
		return text
	}
	p := ansi.GetParser()
	defer ansi.PutParser(p)
	var style uv.Style
	var link uv.Link
	var styleSequence, linkSequence string
	styleDirty, linkDirty := false, false
	p.SetHandler(ansi.Handler{
		HandleCsi: func(cmd ansi.Cmd, params ansi.Params) {
			if cmd == 'm' {
				uv.ReadStyle(params, &style)
				styleDirty = true
			}
		},
		HandleOsc: func(cmd int, data []byte) {
			if cmd == 8 {
				uv.ReadLink(data, &link)
				linkDirty = true
			}
		},
	})
	var out strings.Builder
	out.Grow(len(text))
	start := 0
	for i := 0; i < len(text); i++ {
		p.Advance(text[i])
		if text[i] != '\n' {
			continue
		}
		out.WriteString(text[start:i])
		if !style.IsZero() {
			out.WriteString(ansi.ResetStyle)
		}
		if !link.IsZero() {
			out.WriteString(ansi.ResetHyperlink())
		}
		out.WriteByte('\n')
		if !link.IsZero() {
			if linkDirty {
				linkSequence = ansi.SetHyperlink(link.URL, link.Params)
				linkDirty = false
			}
			out.WriteString(linkSequence)
		}
		if !style.IsZero() {
			if styleDirty {
				styleSequence = style.String()
				styleDirty = false
			}
			out.WriteString(styleSequence)
		}
		start = i + 1
	}
	out.WriteString(text[start:])
	if !style.IsZero() {
		out.WriteString(ansi.ResetStyle)
	}
	if !link.IsZero() {
		out.WriteString(ansi.ResetHyperlink())
	}
	return out.String()
}
