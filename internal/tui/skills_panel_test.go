package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/skills"
)

func skillsPanelTestModel(t *testing.T) *Model {
	t.Helper()
	m := modelPickerTestModel(t, 100, 30)
	m.app.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	m.app.ProjectAllowed = false
	root := t.TempDir()
	for i := range 15 {
		name := fmt.Sprintf("skill-%02d", i)
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Test skill 界面.\n---\nInstructions\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m.app.Skills = skills.Discover(skills.Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}})
	m.editor.SetValue("preserve my draft")
	m.startSkillsInfo()
	return m
}

func TestSkillsPanelTogglePersistsAndStaysOpenAcrossResize(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(fmt.Sprintf("inline=%v", inline), func(t *testing.T) {
			m := skillsPanelTestModel(t)
			m.inlineTranscript = inline
			m.infoIndex = 14
			name := m.infoItems[m.infoIndex].Skill.Skill.Name
			_, cmd := m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEnter})
			if cmd == nil || !m.pickInfo || !m.skillsPanel.saving {
				t.Fatal("Enter did not start a save inside the panel")
			}
			if _, duplicate := m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeySpace}); duplicate != nil {
				t.Fatal("repeated input started overlapping saves")
			}
			m.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
			if !strings.Contains(stripANSI(m.renderInfoPicker()), "Saving") {
				t.Fatal("small card hid save progress")
			}
			m.Update(cmd())
			if !m.pickInfo || m.skillsPanel.saving || m.infoIndex != 14 || m.infoItems[14].Skill.Enabled {
				t.Fatal("save lost selection or failed to update saved state")
			}
			cfg, err := config.Load(m.app.ConfigPath)
			if enabled, exists := cfg.Skills.Overrides[name]; err != nil || !exists || enabled {
				t.Fatalf("saved disable missing: %+v err=%v", cfg.Skills, err)
			}
			for _, size := range [][2]int{{20, 8}, {40, 12}, {80, 24}, {240, 70}} {
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				card := m.renderInfoPicker()
				assertCenteredSelectionCard(t, m, card)
				for _, want := range []string{"Enter", "enable", "Esc", "restart"} {
					if !strings.Contains(stripANSI(card), want) {
						t.Fatalf("%v hid %q:\n%s", size, want, card)
					}
				}
			}
			_, cmd = m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeySpace})
			if cmd == nil {
				t.Fatal("Space did not enable selected skill")
			}
			m.Update(cmd())
			if !m.infoItems[14].Skill.Enabled || m.infoItems[14].Skill.RestartRequired || !m.pickInfo {
				t.Fatal("re-enable failed or closed the panel")
			}
			m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.pickInfo || m.editor.Value() != "preserve my draft" {
				t.Fatal("closing the panel lost composer draft")
			}
		})
	}
}

func TestSkillsPanelSaveFailureAndStaleReply(t *testing.T) {
	m := skillsPanelTestModel(t)
	m.app.ConfigPath = t.TempDir()
	_, cmd := m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(cmd())
	if !m.pickInfo || !m.infoItems[0].Skill.Enabled || !strings.Contains(stripANSI(m.renderInfoPicker()), "Save failed:") {
		t.Fatal("failed save changed policy or hid the error")
	}
	m.app.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	_, cmd = m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.startInfoPicker("MCP servers", []statusInfoItem{{Label: "server"}})
	m.Update(cmd())
	if m.infoTitle != "MCP servers" || m.skillsPanel.active || m.infoItems[0].Label != "server" {
		t.Fatal("late save overwrote another panel")
	}
	m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.pickInfo {
		t.Fatal("MCP Enter no longer closes its read-only inspector")
	}
}

func TestSkillsPanelReopenedDuringSaveReceivesFreshPolicy(t *testing.T) {
	m := skillsPanelTestModel(t)
	_, cmd := m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.startSkillsInfo()
	if !m.skillsPanel.saving {
		t.Fatal("reopening forgot the pending save")
	}
	if _, duplicate := m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEnter}); duplicate != nil {
		t.Fatal("reopening admitted an overlapping save")
	}
	m.Update(cmd())
	if m.skillsPanel.saving || m.infoItems[0].Skill.Enabled || !m.infoItems[0].Skill.RestartRequired {
		t.Fatal("reopened panel retained stale policy after save completion")
	}
	_, cmd = m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(cmd())
	if !m.infoItems[0].Skill.Enabled {
		t.Fatal("next action repeated disable instead of enabling")
	}
}

func TestSkillsPanelInitialFailureAndEmptyInventory(t *testing.T) {
	m := skillsPanelTestModel(t)
	m.app.ConfigPath = t.TempDir()
	m.startSkillsInfo()
	if !strings.Contains(stripANSI(m.renderInfoPicker()), "Cannot load policy:") {
		t.Fatal("initial policy error is hidden")
	}
	m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.app.Skills = nil
	m.startSkillsInfo()
	if !m.pickInfo || !strings.Contains(stripANSI(m.renderInfoPicker()), "None configured") {
		t.Fatal("empty inventory not displayed")
	}
	if _, cmd := m.handleInfoPick(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || !m.pickInfo {
		t.Fatal("empty inventory accepted a mutation")
	}
}
