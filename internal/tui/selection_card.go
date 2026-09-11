package tui

import (
	"fmt"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// selectionCard is the shared presentation for native list and confirmation
// dialogs. Callers own state/actions; this component owns cell-width clipping,
// selection-following pagination, detail wrapping and a pinned controls region.
// Composer completions deliberately do not use this modal component.
type selectionCard struct {
	title, detail, footer string
	compactFooter         string
	items                 []string
	selected              int
	loading               string
	input                 string
}

type selectionCardLayout struct {
	geometry  pickerCardGeometry
	footer    []string
	detail    []string
	listRows  int
	separator bool
}

func (m *Model) selectionCardLayout(card selectionCard) selectionCardLayout {
	g := m.pickerCardGeometry()
	layout := selectionCardLayout{geometry: g}
	width := g.innerWidth
	layout.footer = wrappedCardRows(card.footer, width, 3)
	if g.innerHeight < 8 || width < 40 {
		controls := card.compactFooter
		if controls == "" {
			controls = "↑↓ · Enter · Esc"
		}
		layout.footer = wrappedCardRows(controls, width, 2)
	}
	if card.input != "" {
		// Keep the insertion point visible when a pasted name exceeds the card.
		layout.footer = append([]string{truncateCardInput(card.input, width)}, layout.footer...)
	}
	layout.footer = layout.footer[:min(len(layout.footer), max(0, g.innerHeight-2))]
	layout.separator = g.innerHeight >= 8
	chrome := 1 + len(layout.footer)
	if layout.separator {
		chrome++
	}
	detailLimit := 1
	if g.innerHeight >= 10 {
		detailLimit = 4
	}
	layout.detail = wrappedCardRows(card.detail, width, min(detailLimit, max(0, g.innerHeight-chrome-1)))
	layout.listRows = max(1, g.innerHeight-chrome-len(layout.detail))
	count := max(1, len(card.items))
	if card.loading != "" {
		count = 1
	}
	layout.listRows = min(layout.listRows, count)
	layout.geometry.innerHeight = min(g.innerHeight, chrome+len(layout.detail)+layout.listRows)
	layout.geometry.outerHeight = min(g.outerHeight, layout.geometry.innerHeight+2)
	return layout
}

func truncateCardInput(input string, width int) string {
	input = sanitizeTerminalLine(input)
	cells := xansi.StringWidth(input)
	if cells > width {
		return "…" + fitFrameSuffix(input, cells-max(0, width-1), cells, max(0, width-1))
	}
	return input
}

func wrappedCardRows(text string, width, limit int) []string {
	if text == "" || limit <= 0 {
		return nil
	}
	text = xansi.Hardwrap(xansi.Wordwrap(sanitizeTerminalText(text), max(1, width), ""), max(1, width), true)
	rows := strings.Split(text, "\n")
	if len(rows) > limit {
		rows = rows[:limit]
		rows[limit-1] = truncateDisplayText(rows[limit-1]+" …", width)
	}
	return rows
}

func (m *Model) renderSelectionCard(card selectionCard) string {
	layout := m.selectionCardLayout(card)
	g := layout.geometry
	selected := clampPickerIndex(card.selected, len(card.items))
	start, end := settingsCardWindow(selected, len(card.items), layout.listRows)
	status := ""
	if len(card.items) > 0 {
		status = fmt.Sprintf("%d of %d", selected+1, len(card.items))
	}
	rows := []string{renderPickerCardHeader(card.title, status, g.innerWidth)}
	if layout.separator {
		rows = append(rows, styleSep.Render(strings.Repeat("─", g.innerWidth)))
	}
	var list []string
	for i := start; i < end; i++ {
		prefix, style := "  ", styleCompletion
		if i == selected {
			prefix, style = "› ", styleCompletionSelected
		}
		list = append(list, style.Render(truncateDisplayText(prefix+sanitizeTerminalLine(card.items[i]), g.innerWidth)))
	}
	if card.loading != "" {
		list = []string{styleHeaderDim.Render(truncateDisplayText(sanitizeTerminalLine(card.loading), g.innerWidth))}
	} else if len(list) == 0 {
		list = []string{styleHeaderDim.Render(truncateDisplayText("None configured or discovered", g.innerWidth))}
	}
	// At extremely small sizes prioritize the selected item over decoration.
	if g.innerHeight < 3 {
		return renderPickerCard(fitFrame(strings.Join(list, "\n"), g.innerWidth, g.innerHeight), g)
	}
	rows = append(rows, fitFrame(strings.Join(list, "\n"), g.innerWidth, layout.listRows))
	for _, row := range layout.detail {
		rows = append(rows, styleHeaderDim.Render(row))
	}
	for _, row := range layout.footer {
		rows = append(rows, styleFooter.Render(row))
	}
	return renderPickerCard(strings.Join(rows, "\n"), g)
}

func (m *Model) selectionModalVisible() bool {
	return m.confirmGoalReplace || m.planPrompt || m.pickFork || m.pickSession || m.pickTree || m.pickInfo || m.pickPermissionMode
}

func (m *Model) renderSelectionModal() string {
	switch {
	case m.confirmGoalReplace:
		return m.renderSelectionCard(selectionCard{title: "Replace unfinished goal?", items: []string{"Replace unfinished goal"}, footer: "Enter replace · Esc cancel"})
	case m.planPrompt:
		return m.renderPlanImplementationPrompt()
	case m.pickFork:
		return m.renderForkPicker()
	case m.pickSession:
		return m.renderSessionPicker()
	case m.pickTree:
		return m.renderTreePicker()
	case m.pickInfo:
		return m.renderInfoPicker()
	case m.pickPermissionMode:
		return m.renderPermissionModePicker()
	default:
		return ""
	}
}
