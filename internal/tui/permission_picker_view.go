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

// renderPermissionPicker renders a compact, bounded allow/deny selector. The
// decision rows and keyboard help are always retained at the bottom; details
// consume only the rows left above them.
func (m *Model) renderPermissionPicker() string {
	if !m.permPending || m.permRequest == nil {
		return ""
	}
	req := m.permRequest
	width := permissionContentWidth(m.width)
	maxRows := m.permissionOverlayHeight()
	if maxRows <= 0 {
		return ""
	}

	choiceRows := m.permissionChoiceRows()
	footer := styleFooter.Render("(↑/↓ choose, Enter confirm, Esc deny)")
	if !m.permissionApprovalEnabled() {
		footer = styleFooter.Render("(resize to review, Esc deny)")
	}
	tail := append(choiceRows, footer)
	if len(tail) > maxRows {
		tail = m.compactPermissionChoiceRows(maxRows)
	}
	infoCapacity := max(0, maxRows-len(tail))

	label := "🔐 " + permissionInlineText(req.Tool) + " · " + permissionInlineText(string(req.Risk))
	if m.permAgent != nil {
		label += " · " + permissionInlineText(string(m.permAgent.Path))
	}
	label = styleTool.Render(label)

	var warnings []string
	if req.EffectsTruncated || req.CapabilitiesTruncated || req.PathsTruncated {
		warnings = append(warnings, styleError.Render("Permission analysis was truncated; review the command directly."))
	}
	if req.Unknown {
		warnings = append(warnings, styleError.Render("Unknown child effects cannot be determined statically."))
	}
	executionWarning := ""
	if req.Tool == "bash" {
		executionWarning = styleError.Render("Execution: unrestricted host process")
	}

	criticalCount := 1 + len(warnings)
	if executionWarning != "" {
		criticalCount++
	}
	if criticalCount > infoCapacity {
		rows := make([]string, 0, maxRows)
		for _, row := range append([]string{label}, append(warnings, executionWarning)...) {
			if row != "" && len(rows) < infoCapacity {
				rows = append(rows, row)
			}
		}
		rows = append(rows, tail...)
		return boundedPermissionRows(rows, width, maxRows)
	}

	detailBudget := infoCapacity - criticalCount
	details := permissionRequestDetailRows(req, width, detailBudget)
	remaining := detailBudget - len(details)

	customReason := ""
	if req.Reason != "" && !isInferredEffectSummary(req.Reason) {
		customReason = permissionInlineText(req.Reason)
	}
	scope := ""
	if req.ScopeLabel != "" && req.Rememberable {
		scope = "Remembered scope: " + permissionInlineText(req.ScopeLabel)
	}
	var optional []string
	for _, row := range []string{customReason, scope} {
		if row != "" && remaining > 0 {
			optional = append(optional, row)
			remaining--
		}
	}

	rows := make([]string, 0, maxRows)
	rows = append(rows, label)
	rows = append(rows, details...)
	rows = append(rows, warnings...)
	rows = append(rows, optional...)
	if executionWarning != "" {
		rows = append(rows, executionWarning)
	}
	rows = append(rows, tail...)
	return boundedPermissionRows(rows, width, maxRows)
}

func (m *Model) permissionChoiceRows() []string {
	if !m.permissionApprovalEnabled() {
		return []string{styleError.Render("Approval disabled: resize terminal to review")}
	}
	rows := make([]string, 0, len(m.permissionPickerChoices()))
	for _, option := range m.permissionPickerChoices() {
		line := option.name
		if option.hint != "" {
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

func (m *Model) compactPermissionChoiceRows(maxRows int) []string {
	if maxRows <= 0 {
		return nil
	}
	if !m.permissionApprovalEnabled() {
		return []string{styleError.Render("Approval disabled: resize terminal to review")}
	}
	var labels []string
	for _, option := range m.permissionPickerChoices() {
		label := option.name
		if option.id == m.permChoice {
			label = "›" + label
		}
		labels = append(labels, label)
	}
	choices := styleCompletion.Render(strings.Join(labels, " | "))
	if maxRows == 1 {
		return []string{choices}
	}
	return []string{choices, styleFooter.Render("(←/→ choose, Enter confirm, Esc deny)")}
}

func permissionRequestDetailRows(req *protocol.PermissionRequest, width, budget int) []string {
	if budget <= 0 {
		return nil
	}
	rows := make([]string, 0, budget)
	if req.Tool == "bash" {
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

func (m *Model) permissionNeedsDedicatedFrame() bool {
	return m.permPending && !m.inlineTranscript && m.availableOverlayHeight() < permissionReviewMinHeight
}

func (m *Model) permissionOverlayHeight() int {
	if m.inlineModalOverlay() || m.permissionNeedsDedicatedFrame() {
		return min(max(1, m.managedFrameHeight()), inlineOverlayMaxHeight)
	}
	return min(m.availableOverlayHeight(), inlineOverlayMaxHeight)
}

func (m *Model) permissionApprovalEnabled() bool {
	if m.width < permissionReviewMinWidth || m.permissionOverlayHeight() < permissionReviewMinHeight || m.permRequest == nil {
		return false
	}

	// A Bash approval must expose at least one concrete command/effect row in
	// addition to the tool label, safety warnings, every decision, and help.
	// Otherwise a warning such as "review the command directly" could be paired
	// with an invisible command on a short terminal.
	if m.permRequest.Tool == "bash" {
		criticalRows := 2 // tool label and host-execution warning
		if m.permRequest.EffectsTruncated || m.permRequest.CapabilitiesTruncated || m.permRequest.PathsTruncated {
			criticalRows++
		}
		if m.permRequest.Unknown {
			criticalRows++
		}
		decisionRows := len(m.permissionPickerChoices()) + 1 // choices and footer
		if m.permissionOverlayHeight()-criticalRows-decisionRows < 1 {
			return false
		}
		return permissionRequestHasReviewDetail(m.permRequest)
	}
	return true
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

func permissionContentWidth(terminalWidth int) int {
	if terminalWidth <= 0 {
		return permissionCardMaxWidth
	}
	return max(1, min(terminalWidth-1, permissionCardMaxWidth))
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
