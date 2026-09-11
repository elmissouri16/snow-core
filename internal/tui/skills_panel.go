package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/elmissouri16/snow-core/internal/app"
)

type skillsPanelState struct {
	active, saving bool
	generation     uint64
	err            string
}

type skillPolicySavedMsg struct {
	generation uint64
	status     app.SkillStatus
	err        error
}

func (m *Model) resetSkillsPanel() {
	// A save belongs to the model lifetime, not the open panel. Reopening
	// Skills must stay in the saving state until its result arrives.
	m.skillsPanel = skillsPanelState{generation: m.skillsPanel.generation, saving: m.skillsPanel.saving}
}

func skillPolicyItem(status app.SkillStatus) statusInfoItem {
	state := "disabled"
	if status.Enabled {
		state = "enabled"
	}
	if status.RestartRequired {
		state += " (restart)"
	}
	skill := status.Skill
	detail := fmt.Sprintf("%s policy; applies after restart. ", status.PolicyScope)
	if status.RestartRequired {
		running := "disabled"
		if skill.Enabled {
			running = "enabled"
		}
		detail = "Restart required; currently " + running + ". " + detail
	}
	if skill.DisabledBy != "" {
		detail += "Current catalog: " + skill.DisabledBy + ". "
	}
	detail += skill.Description + " · " + skill.Location
	return statusInfoItem{
		Label:  fmt.Sprintf("%s  ·  %s  ·  %s/%s", skill.Name, state, skill.Scope, skill.Source),
		Detail: detail, Skill: new(status),
	}
}

func (m *Model) startSkillsInfo() (tea.Model, tea.Cmd) {
	statuses, err := m.app.SkillStatuses()
	items := make([]statusInfoItem, 0, len(statuses))
	for _, status := range statuses {
		items = append(items, skillPolicyItem(status))
	}
	m.startInfoPicker("Agent Skills", items)
	m.skillsPanel.active = true
	if err != nil {
		m.skillsPanel.err = "Cannot load policy: " + err.Error()
	}
	return m, nil
}

func (m *Model) handleSkillsPanelKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if msg.Code == tea.KeyEscape {
		m.closeInfoPicker()
		return true, nil
	}
	if m.skillsPanel.saving || len(m.infoItems) == 0 {
		return true, nil
	}
	if msg.Code != tea.KeyEnter && msg.Code != tea.KeySpace {
		return false, nil
	}
	if m.infoIndex < 0 || m.infoIndex >= len(m.infoItems) || m.infoItems[m.infoIndex].Skill == nil {
		return true, nil
	}
	status := *m.infoItems[m.infoIndex].Skill
	m.skillsPanel.saving, m.skillsPanel.err = true, ""
	m.skillsPanel.generation++
	generation, a, ctx := m.skillsPanel.generation, m.app, m.ctx
	return true, func() tea.Msg {
		result, err := a.SetSkillEnabled(ctx, status.Skill.Name, !status.Enabled)
		return skillPolicySavedMsg{generation: generation, status: result, err: err}
	}
}

func (m *Model) applySkillPolicySaved(msg skillPolicySavedMsg) {
	if msg.generation != m.skillsPanel.generation {
		return
	}
	m.skillsPanel.saving = false
	if !m.pickInfo || !m.skillsPanel.active {
		return
	}
	if msg.err != nil {
		m.skillsPanel.err = "Save failed: " + msg.err.Error()
		return
	}
	for i, item := range m.infoItems {
		if item.Skill != nil && item.Skill.Skill.Name == msg.status.Skill.Name {
			m.infoItems[i] = skillPolicyItem(msg.status)
			break
		}
	}
}

func (m *Model) decorateSkillsCard(card selectionCard) selectionCard {
	if m.skillsPanel.err != "" {
		card.detail = m.skillsPanel.err + "\n" + card.detail
	}
	controls := "Esc close"
	if len(m.infoItems) > 0 {
		action, closeHint := "enable", "Esc close"
		if status := m.infoItems[clampPickerIndex(m.infoIndex, len(m.infoItems))].Skill; status != nil {
			if status.Enabled {
				action = "disable"
			}
			if status.RestartRequired {
				closeHint = "Esc · restart req."
			}
		}
		controls = "Enter " + action + "\n" + closeHint
		card.footer = "↑/↓ inspect · PgUp/PgDn page\nEnter/Space " + action + " · Esc close · applies after restart"
	} else {
		card.footer = controls
	}
	card.compactFooter = controls
	if m.skillsPanel.saving {
		card.footer, card.compactFooter = "Saving… · Esc close", "Saving… · Esc close"
	}
	return card
}
