package tui

import (
	"fmt"
	"strconv"
	"strings"
)

func (m *Model) infoCard() selectionCard {
	card := selectionCard{title: fmt.Sprintf("%s (%d)", m.infoTitle, len(m.infoItems)), selected: m.infoIndex,
		footer: "↑/↓ inspect · PgUp/PgDn page · Enter/Esc close"}
	if m.infoLoading {
		card.loading = "Loading…"
	}
	for _, item := range m.infoItems {
		card.items = append(card.items, item.Label)
	}
	if len(m.infoItems) > 0 {
		card.detail = m.infoItems[clampPickerIndex(m.infoIndex, len(m.infoItems))].Detail
	}
	if m.skillsPanel.active {
		card = m.decorateSkillsCard(card)
	}
	return card
}

func (m *Model) infoPickerVisibleItems() int {
	return m.selectionCardLayout(m.infoCard()).listRows
}

func (m *Model) infoWindow() (start, end int) {
	return settingsCardWindow(m.infoIndex, len(m.infoItems), m.infoPickerVisibleItems())
}

func (m *Model) renderInfoPicker() string {
	if !m.pickInfo {
		return ""
	}
	return m.renderSelectionCard(m.infoCard())
}

func (m *Model) sessionCard() selectionCard {
	card := selectionCard{title: fmt.Sprintf("sessions (%d)", len(m.sessions)), selected: m.sessionIndex,
		footer: "↑/↓ choose · Enter resume · Esc cancel\nr rename · d delete · PgUp/PgDn scroll"}
	if m.sessionLoading {
		card.loading = "loading sessions…"
		if m.sessionDeleteInFlight {
			card.loading = "deleting session and subagent histories…"
			card.footer = "Deleting; please wait"
		}
	}
	active := currentSessionID(m.app)
	for _, info := range m.sessions {
		card.items = append(card.items, formatSessionPickerInfo(info, active))
	}
	if m.sessionRenaming {
		card.input = "Rename session: " + m.sessionRenameInput + "_"
		card.footer = "Enter save · Esc cancel"
	}
	if m.sessionDeleting && m.sessionIndex >= 0 && m.sessionIndex < len(m.sessions) {
		name := m.sessions[m.sessionIndex].Name
		if name == "" {
			name = shortSessionID(m.sessions[m.sessionIndex].ID)
		}
		card.title = "Permanently delete " + strconv.Quote(name) + "?"
		card.detail = "Deletes this session and its subagent histories. This cannot be undone."
		card.footer = "Enter confirm · Esc cancel"
		card.compactFooter = card.footer
	}
	return card
}

func (m *Model) sessionPickerVisibleItems() int {
	return m.selectionCardLayout(m.sessionCard()).listRows
}

func (m *Model) sessionWindow() (start, end int) {
	return settingsCardWindow(m.sessionIndex, len(m.sessions), m.sessionPickerVisibleItems())
}

func (m *Model) sessionPickerRows() int {
	return m.selectionCardLayout(m.sessionCard()).geometry.outerHeight
}

func (m *Model) renderSessionPicker() string {
	if !m.pickSession {
		return ""
	}
	return m.renderSelectionCard(m.sessionCard())
}

func (m *Model) treeCard() selectionCard {
	card := selectionCard{title: fmt.Sprintf("branches (%d)", len(m.branches)), selected: m.branchIndex,
		footer: fmt.Sprintf("%s choose · %s switch · %s cancel\n%s fork · %s rename · %s delete", m.keys.PickerDown.Help().Key, m.keys.Accept.Help().Key, m.keys.Close.Help().Key, m.keys.BranchFork.Help().Key, m.keys.BranchRename.Help().Key, m.keys.BranchDelete.Help().Key)}
	if m.treeLoading {
		card.loading = "loading branches…"
	}
	parents := branchParents(m.branches)
	for _, branch := range m.branches {
		marker := "  "
		if branch.Active {
			marker = "✓ "
		}
		name := branch.Name
		if name == "" {
			name = branch.ID
		}
		indent := strings.Repeat("  ", branchDepth(parents, branch))
		connector := "└─ "
		if branch.ParentID == "" {
			connector = ""
		}
		card.items = append(card.items, fmt.Sprintf("%s%s%s%s  ·  %s  ·  %d messages", marker, indent, connector, name, shortSessionID(branch.ID), branch.Messages))
	}
	if len(m.branches) > 0 {
		card.detail = m.branches[clampPickerIndex(m.branchIndex, len(m.branches))].Preview
	}
	switch m.branchAction {
	case "fork":
		card.input = "Fork name (blank = automatic): " + m.branchInput + "_"
		card.footer = "Enter create · Esc cancel"
	case "rename":
		card.input = "Rename: " + m.branchInput + "_"
		card.footer = "Enter save · Esc cancel"
	case "delete":
		card.title = "Delete selected leaf branch?"
		card.footer = m.keys.Confirm.Help().Key + " confirm · " + m.keys.Close.Help().Key + " cancel"
		card.compactFooter = card.footer
	}
	return card
}

func (m *Model) treePickerVisibleItems() int {
	return m.selectionCardLayout(m.treeCard()).listRows
}

func (m *Model) treeWindow() (start, end int) {
	return settingsCardWindow(m.branchIndex, len(m.branches), m.treePickerVisibleItems())
}

func (m *Model) treePickerRows() int {
	return m.selectionCardLayout(m.treeCard()).geometry.outerHeight
}

func (m *Model) renderTreePicker() string {
	if !m.pickTree {
		return ""
	}
	return m.renderSelectionCard(m.treeCard())
}

func (m *Model) renderForkPicker() string {
	if !m.pickFork {
		return ""
	}
	card := selectionCard{title: "Fork conversation", items: forkChoices, selected: m.forkIndex,
		footer: "↑/↓ choose · Enter confirm · Esc cancel"}
	if m.forkLoading {
		card.loading = "validating and creating fork…"
		card.footer = "Creating safely; the current workspace remains active"
	}
	return m.renderSelectionCard(card)
}

func (m *Model) renderPermissionModePicker() string {
	if !m.pickPermissionMode {
		return ""
	}
	return m.renderSelectionCard(selectionCard{title: "permissions", selected: m.permissionModeIndex,
		items:  []string{"ask", "allow", "deny"},
		detail: "ask: request approval · allow: grant tool authority · deny: refuse gated tools",
		footer: "↑/↓ choose · Enter apply · Esc cancel"})
}
