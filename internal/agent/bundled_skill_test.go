package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/skills"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func pluginNamedSkillAgent(t *testing.T, p *scriptedProvider, st session.Store) *Agent {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".agents", "skills", "snow-js-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: snow-js-plugin\ndescription: User plugin workflow.\n---\nFollow the user plugin workflow."), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := skills.Discover(skills.Options{Home: home, SnowHome: t.TempDir(), CWD: t.TempDir()})
	t.Cleanup(func() { _ = catalog.Close() })
	registry := tools.NewRegistry()
	if err := skills.RegisterTools(registry, catalog); err != nil {
		t.Fatal(err)
	}
	a, err := New(Options{Provider: p, Registry: registry, Session: st, Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base", SkillNames: map[string]bool{"snow-js-plugin": true}, Model: protocol.Model{Provider: "scripted", ID: "m1"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func TestPluginNamedSkillDirectActivationRequiresExactMention(t *testing.T) {
	for _, tc := range []struct {
		name, prompt string
		active       bool
	}{
		{"ordinary plugin request", "Build a JavaScript plugin", false},
		{"bare name", "Use snow-js-plugin", false},
		{"punctuation is not exact", "Use $snow-js-plugin, please", false},
		{"explicit token", "$snow-js-plugin Build a notes plugin", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
			a := pluginNamedSkillAgent(t, p, session.NewMemoryStore(session.Options{}))
			if err := a.Prompt(t.Context(), tc.prompt); err != nil {
				t.Fatal(err)
			}
			if len(p.requests) != 1 {
				t.Fatalf("requests=%d", len(p.requests))
			}
			active := strings.Contains(p.requests[0].System, `<skill_content name="snow-js-plugin">`)
			if active != tc.active {
				t.Fatalf("active=%v, want %v", active, tc.active)
			}
		})
	}
}

func TestModelCanActivatePluginNamedUserSkill(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "attempt", ToolName: "activate_skill", Arguments: json.RawMessage(`{"name":"snow-js-plugin"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	st := session.NewMemoryStore(session.Options{})
	a := pluginNamedSkillAgent(t, p, st)
	if err := a.Prompt(t.Context(), "Build a JavaScript plugin"); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 2 {
		t.Fatalf("requests=%d", len(p.requests))
	}
	if strings.Contains(p.requests[0].System, `<skill_content name="snow-js-plugin">`) {
		t.Fatal("skill active before activation")
	}
	if !strings.Contains(p.requests[1].System, `<skill_content name="snow-js-plugin">`) {
		t.Fatal("model could not activate same-named user skill")
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	activated := false
	for _, m := range messages {
		if m.Role == protocol.RoleTool && m.ToolName == "activate_skill" {
			activated = !m.IsError
		}
	}
	if !activated {
		t.Fatal("model activation was not persisted")
	}
}

func TestPluginNamedUserSkillRestoresOnResume(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	makeProvider := func() *scriptedProvider {
		return &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	}
	first := pluginNamedSkillAgent(t, makeProvider(), st)
	if err := first.Prompt(t.Context(), "$snow-js-plugin Build a plugin"); err != nil {
		t.Fatal(err)
	}
	p := makeProvider()
	resumed := pluginNamedSkillAgent(t, p, st)
	if err := resumed.Prompt(t.Context(), "Continue the plugin"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.requests[0].System, `<skill_content name="snow-js-plugin">`) {
		t.Fatal("explicit activation did not restore")
	}
	if _, err := resumed.ClearActiveSkills(); err != nil {
		t.Fatal(err)
	}
	after := makeProvider()
	cleared := pluginNamedSkillAgent(t, after, st)
	if err := cleared.Prompt(t.Context(), "Continue"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(after.requests[0].System, `<skill_content name="snow-js-plugin">`) {
		t.Fatal("clear did not persist")
	}
}
