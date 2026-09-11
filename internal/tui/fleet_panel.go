package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Fleet inspectors need more room than a selection-only card, but must not
// become full-screen dashboards as the terminal grows.
const (
	fleetCardMaxWidth  = 120
	fleetCardMaxHeight = 28
)

type fleetPanelLayout struct {
	geometry                  pickerCardGeometry
	innerWidth, innerHeight   int
	bodyHeight                int
	listWidth, listHeight     int
	detailWidth, detailHeight int
	footer                    []string
	wide, separator           bool
}

func (m *Model) fleetPanelLayout(count int, controls string) fleetPanelLayout {
	g := m.boundedCardGeometry(fleetCardMaxWidth, fleetCardMaxHeight)
	if g.innerWidth < 40 || g.innerHeight < 8 {
		controls = "↑↓ · r · Esc"
	}
	footer := wrappedCardRows(controls, g.innerWidth, min(3, max(0, g.innerHeight-3)))
	layout := fleetPanelLayout{
		geometry: g, innerWidth: g.innerWidth, innerHeight: g.innerHeight,
		bodyHeight: max(1, g.innerHeight-1-len(footer)), footer: footer,
	}
	layout.wide = g.innerWidth >= fleetWideMinWidth
	if layout.wide {
		layout.listWidth = g.innerWidth * 38 / 100
		layout.detailWidth = g.innerWidth - layout.listWidth - 1
		layout.listHeight, layout.detailHeight = layout.bodyHeight, layout.bodyHeight
	} else {
		layout.listWidth, layout.detailWidth = g.innerWidth, g.innerWidth
		layout.separator = layout.bodyHeight >= 4
		available := layout.bodyHeight
		if layout.separator {
			available--
		}
		layout.listHeight = min(max(1, count*2), max(1, available/2))
		layout.detailHeight = available - layout.listHeight
	}
	return layout
}

func renderFleetPanel(layout fleetPanelLayout, header, list, detail string) string {
	left := fitFrame(list, layout.listWidth, layout.listHeight)
	var body string
	if layout.wide {
		right := fitFrame(detail, layout.detailWidth, layout.detailHeight)
		divider := styleSep.Render(strings.TrimSuffix(strings.Repeat("│\n", layout.bodyHeight), "\n"))
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
	} else {
		parts := []string{left}
		if layout.separator {
			parts = append(parts, styleSep.Render(strings.Repeat("─", layout.innerWidth)))
		}
		if layout.detailHeight > 0 {
			parts = append(parts, fitFrame(detail, layout.detailWidth, layout.detailHeight))
		}
		body = strings.Join(parts, "\n")
	}
	// On tiny frames prefer the selected identity over title and decoration.
	if layout.innerHeight < 3 {
		return renderPickerCard(left, layout.geometry)
	}
	rows := []string{fitFrame(header, layout.innerWidth, 1), body}
	for _, row := range layout.footer {
		rows = append(rows, styleFooter.Render(row))
	}
	return renderPickerCard(strings.Join(rows, "\n"), layout.geometry)
}
