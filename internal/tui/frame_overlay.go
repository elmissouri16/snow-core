package tui

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

func truncateDisplayText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if xansi.StringWidth(value) <= width {
		return value
	}
	if width == 1 {
		return xansi.Truncate(value, width, "")
	}
	return xansi.Truncate(value, width, "…")
}

// overlayFrameBlock replaces a bounded rectangular region in an already-fitted
// terminal frame. It preserves ANSI state on both sides of the block and keeps
// the frame's logical dimensions unchanged.
func overlayFrameBlock(frame, block string, x, y, width int) string {
	if frame == "" || block == "" || width <= 0 {
		return frame
	}
	x = max(0, x)
	baseLines := strings.Split(frame, "\n")
	frameWidth := 0
	for _, line := range baseLines {
		frameWidth = max(frameWidth, xansi.StringWidth(line))
	}
	if x >= frameWidth {
		return frame
	}
	width = min(width, frameWidth-x)
	blockLines := strings.Split(block, "\n")
	for index, blockLine := range blockLines {
		row := y + index
		if row < 0 || row >= len(baseLines) {
			continue
		}
		line := baseLines[row]
		lineWidth := xansi.StringWidth(line)
		needed := x + width
		if lineWidth < needed {
			line += strings.Repeat(" ", needed-lineWidth)
			lineWidth = needed
		}
		blockLine = fitFramePrefix(blockLine, width)
		before := fitFramePrefix(line, x)
		afterWidth := max(0, lineWidth-needed)
		after := fitFrameSuffix(line, needed, lineWidth, afterWidth)
		// The base may be in reverse-video selection mode. Reset around each
		// block row; xansi.Cut restores the source style at the start of after.
		baseLines[row] = before + "\x1b[0m" + blockLine + "\x1b[0m" + after
	}
	return strings.Join(baseLines, "\n")
}

// fitFramePrefix returns exactly width terminal cells. If the requested edge
// bisects a wide grapheme, Cut omits that grapheme and the uncovered cell is
// replaced with a space rather than shifting the following overlay content.
func fitFramePrefix(line string, width int) string {
	if width <= 0 {
		return ""
	}
	lineWidth := xansi.StringWidth(line)
	segment := xansi.Cut(line, 0, min(width, lineWidth))
	if segmentWidth := xansi.StringWidth(segment); segmentWidth < width {
		segment += strings.Repeat(" ", width-segmentWidth)
	} else if segmentWidth > width {
		segment = xansi.Truncate(segment, width, "")
		segment += strings.Repeat(" ", width-xansi.StringWidth(segment))
	}
	return segment
}

// fitFrameSuffix preserves the right-hand cells of a line at an exact width.
// A grapheme crossing start is removed as a whole and replaced on the left by
// spaces, preventing it from overlapping the inserted block.
func fitFrameSuffix(line string, start, end, width int) string {
	if width <= 0 {
		return ""
	}
	segment := xansi.Cut(line, start, end)
	segmentWidth := xansi.StringWidth(segment)
	if segmentWidth > width {
		originalWidth := segmentWidth
		for cut := 1; cut <= originalWidth; cut++ {
			candidate := xansi.Cut(segment, cut, originalWidth)
			if xansi.StringWidth(candidate) <= width {
				segment = candidate
				break
			}
		}
		segmentWidth = xansi.StringWidth(segment)
	}
	if segmentWidth < width {
		segment = strings.Repeat(" ", width-segmentWidth) + segment
	}
	return segment
}
