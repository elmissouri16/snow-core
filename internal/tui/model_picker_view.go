package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *Model) modelModalVisible() bool {
	return m.pickModel || m.pickThinking && m.thinkingReturnToModel
}

func (m *Model) renderModelModal() string {
	switch {
	case m.pickModel:
		return m.renderModelPicker()
	case m.pickThinking && m.thinkingReturnToModel:
		return m.renderModelThinkingPicker()
	default:
		return ""
	}
}

func (m *Model) overlayModelModal(frame string) string {
	return m.overlayCenteredModal(frame, m.renderModelModal())
}

func (m *Model) modelPickerVisibleModels() int {
	models := m.filteredModels()
	start, end := m.modelWindow(models)
	return max(1, end-start)
}

// modelWindow expands around the selection while accounting for provider
// headings and scroll markers as rendered rows.
func (m *Model) modelWindow(models []protocol.Model) (start, end int) {
	if len(models) == 0 {
		return 0, 0
	}
	selected := clampPickerIndex(m.modelIndex, len(models))
	start, end = selected, selected+1
	bodyRows := 2 // first provider heading plus the selected model
	height := m.pickerCardGeometry().listHeight
	if height < 4 {
		return start, end
	}
	rowCount := func(nextStart, nextEnd, nextBody int) int {
		rows := nextBody
		if nextStart > 0 {
			rows++
		}
		if nextEnd < len(models) {
			rows++
		}
		return rows
	}
	for {
		beforeRows, afterRows := 0, 0
		beforeFits, afterFits := false, false
		if start > 0 {
			beforeRows = bodyRows + 1
			if models[start-1].Provider != models[start].Provider {
				beforeRows++
			}
			beforeFits = rowCount(start-1, end, beforeRows) <= height
		}
		if end < len(models) {
			afterRows = bodyRows + 1
			if models[end].Provider != models[end-1].Provider {
				afterRows++
			}
			afterFits = rowCount(start, end+1, afterRows) <= height
		}
		if !beforeFits && !afterFits {
			break
		}
		beforeCount := selected - start
		afterCount := end - selected - 1
		if beforeFits && (!afterFits || beforeCount <= afterCount) {
			start--
			bodyRows = beforeRows
		} else {
			end++
			bodyRows = afterRows
		}
	}
	return start, end
}

// renderModelPicker renders the centered, direct-search model card.
func (m *Model) renderModelPicker() string {
	if !m.pickModel {
		return ""
	}
	geometry := m.pickerCardGeometry()
	models := m.filteredModels()
	headerStatus := fmt.Sprintf("%d available", len(m.modelList))
	if m.modelLoading {
		if len(m.modelList) == 0 {
			headerStatus = "loading models…"
		} else {
			headerStatus += " · refreshing…"
		}
	}
	header := renderPickerCardHeader("Models", headerStatus, geometry.innerWidth)
	search := m.renderModelSearchRow(len(models), geometry.innerWidth)
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	list := lipgloss.NewStyle().
		Width(geometry.innerWidth).
		Height(geometry.listHeight).
		MaxWidth(geometry.innerWidth).
		MaxHeight(geometry.listHeight).
		Render(m.renderModelList(models, geometry.innerWidth, geometry.listHeight))
	parts := []string{header, search, separator, list}
	if geometry.detailHeight > 0 {
		parts = append(parts, separator, m.renderModelDetails(models, geometry.innerWidth, geometry.detailHeight))
	}
	controls := truncateDisplayText(" type to filter · ↑/↓ navigate · PgUp/PgDn · Enter apply · Esc clear/close ", geometry.innerWidth)
	parts = append(parts, styleFooter.Render(controls))
	return renderPickerCard(lipgloss.JoinVertical(lipgloss.Left, parts...), geometry)
}

func (m *Model) renderModelSearchRow(matches, width int) string {
	query := m.modelQuery
	if query == "" {
		query = "type to filter"
	}
	row := fmt.Sprintf(" Search: %s_ · %d matches", query, matches)
	return styleHeaderDim.Render(truncateDisplayText(row, width))
}

func (m *Model) renderModelList(models []protocol.Model, width, height int) string {
	if len(models) == 0 {
		message := "  no matching models"
		if m.modelLoading && len(m.modelList) == 0 {
			message = "  loading models…"
		}
		return styleFooter.Render(truncateDisplayText(message, width))
	}
	selected := clampPickerIndex(m.modelIndex, len(models))
	if height <= 2 {
		lines := make([]string, 0, height)
		if height == 2 {
			lines = append(lines, styleHeaderDim.Render(truncateDisplayText("  "+models[selected].Provider, width)))
		}
		lines = append(lines, m.renderModelListRow(models[selected], true, width))
		return strings.Join(lines, "\n")
	}
	start, end := m.modelWindow(models)
	lines := make([]string, 0, height)
	hasTopMarker := start > 0
	if hasTopMarker {
		lines = append(lines, styleHeaderDim.Render(truncateDisplayText("  ↑ more", width)))
	}
	for i := start; i < end; i++ {
		if i == start || models[i].Provider != models[i-1].Provider {
			lines = append(lines, styleHeaderDim.Render(truncateDisplayText("  "+models[i].Provider, width)))
		}
		lines = append(lines, m.renderModelListRow(models[i], i == selected, width))
	}
	hasBottomMarker := end < len(models)
	if hasBottomMarker {
		lines = append(lines, styleHeaderDim.Render(truncateDisplayText("  ↓ more", width)))
	}
	// The smallest supported frame can have fewer rows than the heading plus
	// both markers. Drop markers before ever clipping the selected model.
	if len(lines) > height && hasTopMarker {
		lines = lines[1:]
	}
	if len(lines) > height && hasBottomMarker {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderModelListRow(model protocol.Model, selected bool, width int) string {
	line := model.ID
	if model.DisplayName != "" && !strings.EqualFold(model.DisplayName, model.ID) {
		line += "  (" + model.DisplayName + ")"
	}
	if m.app != nil && model.Provider == m.app.Model.Provider && model.ID == m.app.Model.ID {
		line += "  ✓ current"
	}
	prefix := "  "
	style := styleCompletion
	if selected {
		prefix = "› "
		style = styleCompletionSelected
	}
	return style.Render(truncateDisplayText(prefix+line, width))
}

func (m *Model) renderModelDetails(models []protocol.Model, width, height int) string {
	var lines []string
	if len(models) > 0 {
		selected := models[clampPickerIndex(m.modelIndex, len(models))]
		var metadata []string
		if selected.ContextWindow > 0 {
			metadata = append(metadata, "ctx "+formatTokenCount(int64(selected.ContextWindow)))
		}
		if levels := selected.SupportedThinkingLevels(); len(levels) > 1 {
			parts := make([]string, 0, len(levels)-1)
			for _, level := range levels[1:] {
				parts = append(parts, string(level))
			}
			metadata = append(metadata, "thinking "+strings.Join(parts, "/"))
		}
		if len(metadata) > 0 {
			lines = append(lines, styleHeaderDim.Render(truncateDisplayText(" "+strings.Join(metadata, " · "), width)))
		}
		if selected.Description != "" && len(lines) < height {
			wrapped := xansi.Wordwrap(selected.Description, max(1, width-2), "")
			for _, line := range strings.Split(wrapped, "\n") {
				lines = append(lines, styleFooter.Render(truncateDisplayText(" "+line, width)))
				if len(lines) == height {
					break
				}
			}
		}
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxWidth(width).MaxHeight(height).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderModelThinkingPicker() string {
	if !m.pickThinking || !m.thinkingReturnToModel || m.thinkingModel == nil {
		return ""
	}
	geometry := m.pickerCardGeometry()
	header := renderPickerCardHeader("Thinking effort", "for "+m.thinkingModel.Provider+"/"+m.thinkingModel.ID, geometry.innerWidth)
	instruction := styleHeaderDim.Render(truncateDisplayText(" Select the effort to apply with this model", geometry.innerWidth))
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	start, end := 0, len(m.thinkingList)
	if end > geometry.listHeight {
		start = clampPickerIndex(m.thinkingIndex-geometry.listHeight/2, end)
		if start+geometry.listHeight > end {
			start = end - geometry.listHeight
		}
		end = start + geometry.listHeight
	}
	var rows []string
	for i := start; i < end; i++ {
		prefix := "  "
		style := styleCompletion
		if i == m.thinkingIndex {
			prefix = "› "
			style = styleCompletionSelected
		}
		rows = append(rows, style.Render(truncateDisplayText(prefix+string(m.thinkingList[i]), geometry.innerWidth)))
	}
	list := lipgloss.NewStyle().
		Width(geometry.innerWidth).
		Height(geometry.listHeight).
		MaxWidth(geometry.innerWidth).
		MaxHeight(geometry.listHeight).
		Render(strings.Join(rows, "\n"))
	parts := []string{header, instruction, separator, list}
	if geometry.detailHeight > 0 {
		parts = append(parts, separator, m.renderModelDetails([]protocol.Model{*m.thinkingModel}, geometry.innerWidth, geometry.detailHeight))
	}
	parts = append(parts, styleFooter.Render(truncateDisplayText(" ↑/↓ navigate · Enter apply · Esc back ", geometry.innerWidth)))
	return renderPickerCard(lipgloss.JoinVertical(lipgloss.Left, parts...), geometry)
}
