package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/skills"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSkillCompletionQueryMatchingAndReplacement(t *testing.T) {
	query, start, selected, ok := skillCompletionQuery("$re")
	if !ok || query != "re" || start != 0 || len(selected) != 0 {
		t.Fatalf("first query = %q, %d, %v, %v", query, start, selected, ok)
	}
	query, start, selected, ok = skillCompletionQuery("  $review $do")
	if !ok || query != "do" || start != len("  $review ") || len(selected) != 1 || selected[0] != "review" {
		t.Fatalf("second query = %q, %d, %v, %v", query, start, selected, ok)
	}
	for _, text := range []string{"Use $review", "quoted text\n$review", "@review"} {
		if _, _, _, ok := skillCompletionQuery(text); ok {
			t.Fatalf("ordinary text %q opened skill completion", text)
		}
	}
	items := []skillCompletionItem{{Name: "review", Description: "Review code."}, {Name: "release", Description: "Ship code."}, {Name: "docs", Description: "Write docs."}}
	matches := matchSkillCompletions(items, "re", []string{"release"})
	if len(matches) != 1 || matches[0].Name != "review" {
		t.Fatalf("matches = %+v", matches)
	}
	if got := replaceSkillCompletionToken("$re", 0, "review"); got != "$review " {
		t.Fatalf("replacement = %q", got)
	}
}

func TestModelSkillCompletionInsertsLeadingDirectives(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	root := t.TempDir()
	for name, description := range map[string]string{"review": "Review code carefully.", "docs": "Write documentation."} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: " + description + "\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.app.Skills = skills.Discover(skills.Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}})

	m.editor.SetValue("$re")
	if cmd := m.refreshInputCompletions(); cmd != nil {
		t.Fatal("skill completion unexpectedly scheduled asynchronous work")
	}
	if !m.skillVisible || len(m.skillMatches) != 1 || m.skillMatches[0].Name != "review" {
		t.Fatalf("skill picker = visible %v matches %+v", m.skillVisible, m.skillMatches)
	}
	if picker := stripANSI(m.renderSkillCompletionPicker()); !strings.Contains(picker, "$review") || !strings.Contains(picker, "Review code carefully") {
		t.Fatalf("skill picker rendering = %q", picker)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.editor.Value(); got != "$review " {
		t.Fatalf("editor after skill completion = %q", got)
	}
	if m.skillVisible || m.busy {
		t.Fatalf("skill picker submit state = visible %v busy %v", m.skillVisible, m.busy)
	}

	m.editor.SetValue("$review $d")
	m.refreshInputCompletions()
	if !m.skillVisible || len(m.skillMatches) != 1 || m.skillMatches[0].Name != "docs" {
		t.Fatalf("second directive picker = visible %v matches %+v", m.skillVisible, m.skillMatches)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.editor.Value(); got != "$review $docs " {
		t.Fatalf("editor after second skill completion = %q", got)
	}

	m.editor.SetValue("Use $re in pasted prose")
	m.refreshInputCompletions()
	if m.skillVisible {
		t.Fatal("ordinary prose opened the skill picker")
	}
	m.editor.SetValue("$unknown $re")
	m.refreshInputCompletions()
	if m.skillVisible {
		t.Fatal("unknown prior directive opened a completion that could not activate")
	}

	m.busy = true
	m.editor.SetValue("$review")
	m.refreshInputCompletions()
	if !m.skillVisible {
		t.Fatal("exact directive did not open completion before queue acknowledgment")
	}
	_, _ = m.Update(queueSubmitMsg{itemID: "queued-skill", kind: protocol.QueuedInputFollowUp, text: "$review", expanded: "$review", accepted: true, epoch: m.queueEpoch})
	if m.editor.Value() != "" || m.skillVisible {
		t.Fatalf("accepted queue left stale completion: editor=%q visible=%v", m.editor.Value(), m.skillVisible)
	}
}

func TestSkillCompletionExcludesPolicyDisabledSkills(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	root := t.TempDir()
	for _, name := range []string{"review", "release"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: test\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.app.Skills = skills.Discover(skills.Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}, Overrides: map[string]bool{"review": false}})
	m.editor.SetValue("$re")
	m.refreshInputCompletions()
	if !m.skillVisible || len(m.skillMatches) != 1 || m.skillMatches[0].Name != "release" {
		t.Fatalf("disabled skill leaked into completion: %+v", m.skillMatches)
	}
}
