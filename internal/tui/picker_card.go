package tui

import "github.com/charmbracelet/lipgloss"

const (
	pickerCardMaxWidth  = 80
	pickerCardMaxHeight = 24
)

type pickerCardGeometry struct {
	outerWidth, outerHeight int
	innerWidth, innerHeight int
	listHeight              int
	detailHeight            int
}

func (m *Model) pickerCardGeometry() pickerCardGeometry {
	frameWidth := m.managedFrameWidth()
	frameHeight := m.managedFrameHeight()
	// Direct renderer tests and startup loading states can be evaluated before
	// the first WindowSizeMsg. Use the component's maximum geometry until the
	// real managed frame arrives.
	if m.width <= 0 {
		frameWidth = pickerCardMaxWidth + 4
	}
	if m.height <= 0 {
		frameHeight = pickerCardMaxHeight + 4
	}
	outerWidth := min(pickerCardMaxWidth, max(1, frameWidth-4))
	outerHeight := min(pickerCardMaxHeight, max(1, frameHeight-4))
	// Very small terminals cannot retain the normal two-cell outer gutter. Use
	// the complete managed dimension and preserve the required modal rows.
	if outerWidth < 20 {
		outerWidth = frameWidth
	}
	if outerHeight < 8 {
		outerHeight = frameHeight
	}
	geometry := pickerCardGeometry{
		outerWidth: outerWidth, outerHeight: outerHeight,
		innerWidth: max(1, outerWidth-2), innerHeight: max(1, outerHeight-2),
	}
	// Model selection uses a title, search row, separator, controls, and an
	// optional detail region. Other centered flows may use the raw dimensions.
	available := max(1, geometry.innerHeight-4)
	geometry.listHeight = available
	if available >= 8 {
		geometry.detailHeight = min(3, available-7)
		geometry.listHeight = available - geometry.detailHeight - 1
	}
	return geometry
}

func (m *Model) overlayCenteredModal(frame, block string) string {
	if block == "" || frame == "" {
		return frame
	}
	width := transcriptSelectionBlockWidth(block)
	height := lipgloss.Height(block)
	x := max(0, (m.managedFrameWidth()-width)/2)
	y := max(0, (m.managedFrameHeight()-height)/2)
	return overlayFrameBlock(frame, block, x, y, width)
}

func renderPickerCardHeader(title, status string, width int) string {
	title = sanitizeTerminalLine(title)
	status = sanitizeTerminalLine(status)
	titleText := " " + title + " "
	titleView := styleHeader.Render(truncateDisplayText(titleText, width))
	remaining := max(0, width-lipgloss.Width(titleView))
	return titleView + styleHeaderDim.Render(truncateDisplayText(status, remaining))
}

func renderPickerCard(content string, geometry pickerCardGeometry) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Width(geometry.innerWidth).
		Height(geometry.innerHeight).
		Render(content)
}
