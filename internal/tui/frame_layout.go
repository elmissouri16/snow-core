package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/displaywidth"
)

// Use the renderer's Unicode width tables directly, retaining its fast ASCII
// runs rather than repeatedly constructing a grapheme iterator for each byte.
var frameWidthOptions = displaywidth.Options{ControlSequences: true, ControlSequences8Bit: true}

func lineCellWidth(line string) int {
	return frameWidthOptions.String(line)
}

func fitFrame(frame string, width, height int) string {
	return fitFrameAligned(frame, width, height, false)
}

func fitFrameBottom(frame string, width, height int) string {
	return fitFrameAligned(frame, width, height, true)
}

// Frame components already own their layout. Wrap only an overflowing row,
// then isolate terminal styles and pad/clip once at the final frame boundary.
func fitFrameAligned(frame string, width, height int, bottom bool) string {
	width, height = max(1, width), max(1, height)
	frame = strings.ReplaceAll(frame, "\t", "    ")
	frame = strings.ReplaceAll(frame, "\r\n", "\n")
	for line := range strings.SplitSeq(frame, "\n") {
		if lineCellWidth(line) > width {
			frame = ansi.Wrap(frame, width, "")
			break
		}
	}
	frame = isolateTranscriptRows(frame)
	lines := strings.Split(frame, "\n")
	spaces := strings.Repeat(" ", width)
	var out strings.Builder
	out.Grow(len(frame))
	padTop := 0
	if bottom {
		padTop = max(0, height-len(lines))
	}
	for row := range height {
		if row > 0 {
			out.WriteByte('\n')
		}
		index := row - padTop
		if index < 0 || index >= len(lines) {
			out.WriteString(spaces)
			continue
		}
		line := lines[index]
		cells := lineCellWidth(line)
		if cells > width {
			line = frameWidthOptions.TruncateString(line, width, "")
			cells = lineCellWidth(line)
		}
		out.WriteString(line)
		out.WriteString(spaces[:width-cells])
	}
	return out.String()
}
