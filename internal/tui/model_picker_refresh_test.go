package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestModelPickerCtrlRRefreshPreservesFilterAndSelection(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startModelPick()
	m.modelQuery = m.app.Model.ID
	_, cmd := m.handleModelPick(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd == nil || !m.modelLoading || m.modelQuery != m.app.Model.ID {
		t.Fatalf("refresh cmd=%v loading=%v query=%q", cmd != nil, m.modelLoading, m.modelQuery)
	}
	if _, duplicate := m.handleModelPick(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}); duplicate != nil {
		t.Fatal("repeated Ctrl+R started concurrent refresh")
	}
	_, _ = m.Update(cmd())
	models := m.filteredModels()
	if !m.pickModel || m.modelLoading || m.modelQuery != m.app.Model.ID || len(models) == 0 || models[m.modelIndex].ID != m.app.Model.ID {
		t.Fatalf("refresh lost picker state: open=%v loading=%v query=%q models=%v", m.pickModel, m.modelLoading, m.modelQuery, models)
	}
	if !strings.Contains(stripANSI(m.renderModelModal()), "Ctrl+R refresh") {
		t.Fatal("model picker does not advertise refresh")
	}
	_, cmd = m.handleModelPick(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m.clearModelPick()
	_, _ = m.Update(cmd())
	if m.pickModel {
		t.Fatal("late refresh reopened closed picker")
	}
}

func TestModelPickerSuccessfulEmptyRefreshDropsWithdrawnModels(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startModelPick()
	_, _ = m.Update(modelListMsg{generation: m.pickerGeneration, models: []protocol.Model{}})
	if len(m.modelList) != 0 || m.pickModel {
		t.Fatalf("empty authoritative catalog retained models=%v picker=%v", m.modelList, m.pickModel)
	}
}
