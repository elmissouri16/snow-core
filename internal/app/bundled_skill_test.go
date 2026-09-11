package app

import "testing"

func TestBundledPluginSkillAvailableOutsideCheckout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SNOW_HOME", t.TempDir())
	for _, tc := range []struct {
		name     string
		noSkills bool
		tools    []string
		want     bool
	}{
		{name: "normal", want: true},
		{name: "no skills", noSkills: true},
		{name: "explicit tools exclude skills", tools: []string{"read"}},
		{name: "explicit skill tools", tools: []string{"activate_skill", "read_skill_resource", "deactivate_skill"}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: tc.noSkills, Tools: tc.tools})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.Close() })
			skill, present := a.Skills.Get("snow-js-plugin")
			if present != tc.want {
				t.Fatalf("present=%v want=%v", present, tc.want)
			}
			if present && (skill.Source != "builtin" || skill.Scope != "builtin") {
				t.Fatalf("not embedded: %+v", skill)
			}
			_, available := a.Registry.Descriptor("activate_skill")
			if available != tc.want {
				t.Fatalf("activation available=%v want=%v", available, tc.want)
			}
		})
	}
}
