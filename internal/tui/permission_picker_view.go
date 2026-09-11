package tui

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"unicode"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	permissionCommandRuneLimit = 512
	permissionEffectRowLimit   = 6
	permissionCardMaxWidth     = 100
	permissionReviewMinWidth   = 20
	permissionReviewMinHeight  = 7
)

type permissionEffectGroup struct {
	effect  protocol.PermissionEffect
	count   int
	reasons []string
}

// renderPermissionPicker returns the complete card block. The modal host owns
// centering; both rendering and the approval gate use the same inner geometry.
func (m *Model) renderPermissionPicker() string {
	if !m.permPending || m.permRequest == nil {
		return ""
	}
	geometry := m.permissionCardGeometry()
	width, maxRows := geometry.innerWidth, geometry.innerHeight
	if maxRows <= 0 || width <= 0 {
		return renderPickerCard("", geometry)
	}
	enabled := m.permissionApprovalEnabled()
	tail := m.permissionChoiceRows()
	footer := "↑/↓ choose · Enter confirm · Esc deny"
	if width < 40 {
		footer = "↑↓ Enter · Esc deny"
	}
	if !enabled {
		footer = "Resize to review · Esc deny"
		if width < 28 {
			footer = "Resize · Esc deny"
		}
	}
	tail = append(tail, styleFooter.Render(footer))
	if len(tail) > maxRows {
		tail = tail[:maxRows]
	}
	infoCapacity := maxRows - len(tail)
	req := m.permRequest
	label := "🔐 " + permissionInlineText(req.Tool) + " · " + permissionInlineText(string(req.Risk))
	if m.permAgent != nil {
		label += " · " + permissionInlineText(string(m.permAgent.Path))
	}
	warnings := m.permissionSafetyRows(width)
	detailBudget := max(0, infoCapacity-1-len(warnings))
	// Disabled cards can still expose a command when possible, but never
	// borrow rows from the disabled status and safe dismissal controls.
	if !enabled && infoCapacity > 1 {
		detailBudget = max(1, detailBudget)
	}
	rows := make([]string, 0, maxRows)
	if infoCapacity > 0 {
		rows = append(rows, styleTool.Render(label))
		rows = append(rows, permissionRequestDetailRows(req, width, detailBudget)...)
		rows = append(rows, warnings...)
		if len(rows) > infoCapacity {
			rows = rows[:infoCapacity]
		}
		if len(rows) < infoCapacity && req.Reason != "" && !isInferredEffectSummary(req.Reason) {
			rows = append(rows, permissionInlineText(req.Reason))
		}
		if len(rows) < infoCapacity && req.ScopeLabel != "" && req.Rememberable {
			rows = append(rows, "Remembered scope: "+permissionInlineText(req.ScopeLabel))
		}
	}
	rows = append(rows, tail...)
	// Short requests should be compact cards, not a full-height box with the
	// controls floating above unused space. The cap still owns review gating.
	geometry.innerHeight = min(geometry.innerHeight, max(1, len(rows)))
	geometry.outerHeight = min(geometry.outerHeight, geometry.innerHeight+2)
	return renderPickerCard(boundedPermissionRows(rows, width, maxRows), geometry)
}

func (m *Model) permissionChoiceRows() []string {
	if !m.permissionApprovalEnabled() {
		return []string{styleError.Render("Approval disabled")}
	}
	width := m.permissionCardGeometry().innerWidth
	rows := make([]string, 0, len(m.permissionPickerChoices()))
	for _, option := range m.permissionPickerChoices() {
		line := option.name
		// Keep each action intact before spending width on its explanatory hint.
		if option.hint != "" && xansi.StringWidth(line)+xansi.StringWidth(option.hint)+6 <= width {
			line += "  (" + option.hint + ")"
		}
		if option.id == m.permChoice {
			rows = append(rows, styleCompletionSelected.Render("› "+line))
		} else {
			rows = append(rows, styleCompletion.Render("  "+line))
		}
	}
	return rows
}

// Safety messages wrap instead of silently losing their meaning at narrow
// widths. Their physical row count is also charged to the approval gate.
func (m *Model) permissionSafetyRows(width int) []string {
	if m.permRequest == nil || width <= 0 {
		return nil
	}
	req := m.permRequest
	var messages []string
	if req.EffectsTruncated || req.CapabilitiesTruncated || req.PathsTruncated {
		messages = append(messages, "Permission analysis was truncated; review the command directly.")
	}
	if req.Unknown {
		messages = append(messages, "Unknown child effects cannot be determined statically.")
	}
	if shellPermissionTool(req.Tool) {
		messages = append(messages, "Execution: unrestricted host process")
	}
	var rows []string
	for _, message := range messages {
		wrapped := xansi.Hardwrap(xansi.Wordwrap(message, width, ""), width, true)
		for line := range strings.SplitSeq(wrapped, "\n") {
			rows = append(rows, styleError.Render(line))
		}
	}
	return rows
}

func permissionRequestDetailRows(req *protocol.PermissionRequest, width, budget int) []string {
	if budget <= 0 {
		return nil
	}
	rows := make([]string, 0, budget)
	if shellPermissionTool(req.Tool) {
		var args struct {
			Command string `json:"command"`
		}
		if jsonv2.Unmarshal(req.Args, &args) == nil && strings.TrimSpace(args.Command) != "" {
			command := permissionInlineText(strings.TrimSpace(args.Command))
			command = truncateRunes(command, permissionCommandRuneLimit)
			rows = append(rows, truncatePermissionLine("Command: "+command, width))
		}
	}
	if len(rows) == budget {
		return rows
	}
	if len(req.Effects) > 0 {
		return append(rows, permissionEffectRows(req.Effects, width, budget-len(rows))...)
	}
	if len(req.Paths) > 0 {
		return append(rows, permissionPathRows(req.Paths, width, budget-len(rows))...)
	}
	return rows
}

func permissionEffectRows(effects []protocol.PermissionEffect, width, budget int) []string {
	if budget <= 0 {
		return nil
	}
	groups := groupPermissionEffects(effects)
	visible := min(len(groups), permissionEffectRowLimit)
	if budget == 1 {
		line := fmt.Sprintf("Effects (%d): ", len(effects))
		if visible > 0 {
			line += formatPermissionEffectGroup(groups[0])
		}
		if len(groups) > 1 {
			line += fmt.Sprintf("; +%d group(s)", len(groups)-1)
		}
		return []string{truncatePermissionLine(permissionInlineText(line), width)}
	}
	visible = min(visible, budget-1)
	header := fmt.Sprintf("Effects (%d):", len(effects))
	if visible < len(groups) {
		header = fmt.Sprintf("Effects (%d; %d group(s) shown):", len(effects), visible)
	}
	rows := []string{truncatePermissionLine(header, width)}
	for _, group := range groups[:visible] {
		line := "  " + permissionInlineText(formatPermissionEffectGroup(group))
		rows = append(rows, truncatePermissionLine(line, width))
	}
	return rows
}

func permissionPathRows(paths []string, width, budget int) []string {
	if budget <= 0 {
		return nil
	}
	if budget == 1 {
		line := fmt.Sprintf("Paths (%d): %s", len(paths), permissionInlineText(paths[0]))
		if len(paths) > 1 {
			line += fmt.Sprintf("; +%d", len(paths)-1)
		}
		return []string{truncatePermissionLine(line, width)}
	}
	visible := min(len(paths), budget-1, permissionEffectRowLimit)
	header := fmt.Sprintf("Paths (%d):", len(paths))
	if visible < len(paths) {
		header = fmt.Sprintf("Paths (%d; %d shown):", len(paths), visible)
	}
	rows := []string{header}
	for _, path := range paths[:visible] {
		rows = append(rows, truncatePermissionLine("  "+permissionInlineText(path), width))
	}
	return rows
}

func (m *Model) permissionCardGeometry() pickerCardGeometry {
	geometry := m.pickerCardGeometry()
	// Permission requests need more horizontal room than ordinary pickers for
	// commands and effects, while retaining the shared gutters and height cap.
	frameWidth := m.managedFrameWidth()
	if m.width <= 0 {
		frameWidth = permissionCardMaxWidth + 4
	}
	geometry.outerWidth = min(permissionCardMaxWidth, max(1, frameWidth-4))
	if geometry.outerWidth < 20 {
		geometry.outerWidth = frameWidth
	}
	geometry.innerWidth = max(1, geometry.outerWidth-2)
	return geometry
}

func (m *Model) permissionApprovalEnabled() bool {
	geometry := m.permissionCardGeometry()
	if m.width <= 0 || m.height <= 0 || geometry.innerWidth < permissionReviewMinWidth || geometry.innerHeight < permissionReviewMinHeight || m.permRequest == nil {
		return false
	}
	// Reserve exactly what the renderer consumes: label, complete wrapped
	// warnings, every choice, controls, and a concrete review row when needed.
	remaining := geometry.innerHeight - 1 - len(m.permissionSafetyRows(geometry.innerWidth)) - len(m.permissionPickerChoices()) - 1
	req := m.permRequest
	if shellPermissionTool(req.Tool) {
		return remaining >= 1 && permissionRequestHasReviewDetail(req)
	}
	if len(req.Effects) > 0 || len(req.Paths) > 0 {
		return remaining >= 1
	}
	return remaining >= 0
}

func permissionRequestHasReviewDetail(req *protocol.PermissionRequest) bool {
	if len(req.Effects) > 0 || len(req.Paths) > 0 {
		return true
	}
	var args struct {
		Command string `json:"command"`
	}
	return jsonv2.Unmarshal(req.Args, &args) == nil && strings.TrimSpace(args.Command) != ""
}

func groupPermissionEffects(effects []protocol.PermissionEffect) []permissionEffectGroup {
	groups := make([]permissionEffectGroup, 0, len(effects))
	indexes := make(map[string]int, len(effects))
	for _, effect := range effects {
		key := strings.Join([]string{
			effect.Type,
			effect.Capability,
			effect.Operation,
			effect.Resource,
			effect.Command,
			fmt.Sprint(effect.Dynamic),
		}, "\x00")
		index, exists := indexes[key]
		if !exists {
			indexes[key] = len(groups)
			groups = append(groups, permissionEffectGroup{effect: effect})
			index = len(groups) - 1
		}
		groups[index].count++
		reason := strings.TrimSpace(effect.Reason)
		if reason != "" && !slices.Contains(groups[index].reasons, reason) {
			groups[index].reasons = append(groups[index].reasons, reason)
		}
	}
	return groups
}

func formatPermissionEffectGroup(group permissionEffectGroup) string {
	effect := group.effect
	operation := strings.TrimSpace(effect.Operation)
	if operation == "" {
		operation = strings.TrimSpace(effect.Type)
	}
	if operation == "" {
		operation = "effect"
	}
	line := operation
	if effect.Command != "" {
		line += " " + effect.Command
	}
	if group.count > 1 {
		line += fmt.Sprintf(" ×%d", group.count)
	}
	if effect.Resource != "" {
		line += " → " + effect.Resource
	}
	if effect.Dynamic && operation != "unknown" && operation != "incomplete" {
		line += " (dynamic)"
	}
	if (operation == "unknown" || operation == "incomplete") && len(group.reasons) > 0 {
		line += " — " + strings.Join(group.reasons, "; ")
	}
	return line
}

func permissionInlineText(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\r':
			b.WriteString(`\r`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if unicode.IsControl(r) {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return sanitizeTerminalText(b.String())
}

func truncatePermissionLine(line string, width int) string {
	if xansi.StringWidth(line) <= width {
		return line
	}
	return xansi.Truncate(line, width, "…")
}

func boundedPermissionRows(rows []string, width, height int) string {
	if len(rows) > height {
		rows = rows[:height]
	}
	for i := range rows {
		rows[i] = truncatePermissionLine(rows[i], width)
	}
	return strings.Join(rows, "\n")
}

func isInferredEffectSummary(reason string) bool {
	count, ok := strings.CutSuffix(strings.TrimSpace(reason), " inferred effect(s)")
	if !ok || count == "" {
		return false
	}
	for _, r := range count {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func shellPermissionTool(name string) bool { return name == "bash" || name == "process_start" }
