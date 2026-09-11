package app

import (
	"os"
	"path/filepath"
	"testing"
)

func writePluginNamedUserSkill(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".agents", "skills", "snow-js-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: snow-js-plugin\ndescription: User plugin workflow.\n---\nFollow the user plugin workflow."), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPluginNamedSkillRequiresFilesystemInstallation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		installed bool
		noSkills  bool
		tools     []string
		want      bool
	}{
		{name: "no builtin"},
		{name: "user skill", installed: true, want: true},
		{name: "no skills", installed: true, noSkills: true},
		{name: "explicit tools exclude skills", installed: true, tools: []string{"read"}},
		{name: "explicit skill tools", installed: true, tools: []string{"activate_skill", "read_skill_resource", "deactivate_skill"}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("SNOW_HOME", t.TempDir())
			if tc.installed {
				writePluginNamedUserSkill(t, home)
			}
			a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: tc.noSkills, Tools: tc.tools})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.Close() })
			skill, present := a.Skills.Get("snow-js-plugin")
			if present != tc.want {
				t.Fatalf("present=%v want=%v", present, tc.want)
			}
			if present && (skill.Source != "agents" || skill.Scope != "user") {
				t.Fatalf("not user-installed: %+v", skill)
			}
			for _, name := range []string{"activate_skill", "read_skill_resource", "deactivate_skill"} {
				_, available := a.Registry.Descriptor(name)
				if available != tc.want {
					t.Fatalf("%s available=%v want=%v", name, available, tc.want)
				}
			}
		})
	}
}
