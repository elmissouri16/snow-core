package skills

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/tools"
)

func TestDiscoveryDoesNotProvideBuiltinSkills(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	catalog := Discover(Options{Home: home, SnowHome: filepath.Join(home, ".snow"), CWD: cwd})
	if len(catalog.Inventory()) != 0 || catalog.CatalogPrompt() != "" {
		t.Fatalf("unexpected skills: %+v", catalog.Inventory())
	}
	registry := tools.NewRegistry()
	if err := RegisterTools(registry, catalog); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"activate_skill", "read_skill_resource", "deactivate_skill"} {
		if _, ok := registry.Get(name); ok {
			t.Fatalf("%s registered without filesystem skills", name)
		}
	}
	for _, dir := range []string{home, cwd} {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("discovery wrote to %s: %v, err=%v", dir, entries, err)
		}
	}
}

func TestPluginNamedSkillPrecedenceAndTrust(t *testing.T) {
	home, cwd, explicit := t.TempDir(), t.TempDir(), t.TempDir()
	projectDir := writeSkill(t, filepath.Join(cwd, ".agents", "skills"), "snow-js-plugin", "snow-js-plugin", "Project override.", "project body")
	opts := Options{Home: home, SnowHome: filepath.Join(home, ".snow"), CWD: cwd}
	if got, ok := Discover(opts).Get("snow-js-plugin"); ok {
		t.Fatalf("untrusted project skill exposed: %+v", got)
	}
	userDir := writeSkill(t, filepath.Join(home, ".agents", "skills"), "snow-js-plugin", "snow-js-plugin", "User override.", "user body")
	if got, _ := Discover(opts).Get("snow-js-plugin"); got.Directory != userDir {
		t.Fatalf("user override = %+v", got)
	}
	explicitDir := writeSkill(t, explicit, "snow-js-plugin", "snow-js-plugin", "Explicit override.", "explicit body")
	opts.ExtraDirs = []string{explicit}
	if got, _ := Discover(opts).Get("snow-js-plugin"); got.Directory != explicitDir {
		t.Fatalf("explicit override = %+v", got)
	}
	opts.ProjectTrusted = true
	catalog := Discover(opts)
	if got, _ := catalog.Get("snow-js-plugin"); got.Directory != projectDir {
		t.Fatalf("trusted override = %+v", got)
	}
	result, err := (&ActivateTool{Catalog: catalog}).Run(t.Context(), []byte(`{"name":"snow-js-plugin"}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "project body") {
		t.Fatalf("ordinary activation changed = %+v, err=%v", result, err)
	}
}

func TestPluginNamedSkillPolicyAndBudget(t *testing.T) {
	for _, opts := range []Options{
		{Disabled: true},
		{Overrides: map[string]bool{"snow-js-plugin": false}},
		{MaxCatalogBytes: 1},
	} {
		opts.Home, opts.SnowHome = t.TempDir(), t.TempDir()
		writeSkill(t, filepath.Join(opts.Home, ".agents", "skills"), "snow-js-plugin", "snow-js-plugin", "User plugin workflow.", "user body")
		catalog := Discover(opts)
		if _, ok := catalog.Get("snow-js-plugin"); ok {
			t.Fatalf("disabled user skill exposed: %+v", opts)
		}
		if got, ok := catalog.Lookup("snow-js-plugin"); !ok || got.Enabled || got.DisabledBy == "" {
			t.Fatalf("disabled inventory = %+v, found=%v", got, ok)
		}
		result, err := (&ActivateTool{Catalog: catalog}).Run(t.Context(), []byte(`{"name":"snow-js-plugin"}`), nil)
		if err != nil || !result.IsError {
			t.Fatalf("activation bypassed disable: %+v, err=%v", result, err)
		}
	}
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "snow-js-plugin", "snow-js-plugin", "User plugin workflow.", "user body")
	catalog := Discover(Options{Home: home, SnowHome: t.TempDir(), Disabled: true, Overrides: map[string]bool{"snow-js-plugin": true}})
	if _, ok := catalog.Get("snow-js-plugin"); !ok {
		t.Fatal("named enable did not override configured global disable")
	}
	catalog.DisableAll("--no-skills")
	if _, ok := catalog.Get("snow-js-plugin"); ok {
		t.Fatal("runtime disable did not hide user skill")
	}
}

func TestPluginNamedResourcesBoundsAndCancellation(t *testing.T) {
	home := t.TempDir()
	dir := writeSkill(t, filepath.Join(home, ".agents", "skills"), "snow-js-plugin", "snow-js-plugin", "User plugin workflow.", "user body")
	if err := os.WriteFile(filepath.Join(dir, "references", "extra.md"), []byte("extra resource"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := Discover(Options{Home: home, SnowHome: t.TempDir()})
	skill, _ := catalog.Get("snow-js-plugin")
	resources, truncated, err := listResources(t.Context(), skill, 200)
	if err != nil || truncated || len(resources) != 2 || !slices.Contains(resources, "references/guide.md") {
		t.Fatalf("resources=%v truncated=%v err=%v", resources, truncated, err)
	}
	resources, truncated, err = listResources(t.Context(), skill, 1)
	if err != nil || !truncated || len(resources) != 1 {
		t.Fatalf("bounded resources=%v truncated=%v err=%v", resources, truncated, err)
	}
	for _, name := range []string{"../SKILL.md", "/SKILL.md", "references/../../SKILL.md", `..\SKILL.md`, ".", "references", "missing"} {
		if _, err := catalog.readResource(skill, name, maxResourceBytes); err == nil {
			t.Errorf("read %q succeeded", name)
		}
	}
	if _, err := catalog.readResource(skill, "references/guide.md", 1); err == nil {
		t.Fatal("resource byte limit not enforced")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := listResources(ctx, skill, 200); err == nil {
		t.Fatal("listing ignored cancellation")
	}
	result, err := (&ActivateTool{Catalog: catalog}).Run(ctx, []byte(`{"name":"snow-js-plugin"}`), nil)
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].Text, "canceled") {
		t.Fatalf("activation ignored cancellation: %+v, err=%v", result, err)
	}
	read := &ReadResourceTool{Catalog: catalog}
	for _, raw := range []string{`{"name":"snow-js-plugin","path":"../SKILL.md"}`, `{"name":"snow-js-plugin","path":"/SKILL.md"}`} {
		result, err := read.Run(t.Context(), []byte(raw), nil)
		if err != nil || !result.IsError {
			t.Fatalf("read escape succeeded: %+v, err=%v", result, err)
		}
	}
	result, err = read.Run(ctx, []byte(`{"name":"snow-js-plugin","path":"references/guide.md"}`), nil)
	if err != nil || !result.IsError {
		t.Fatalf("read ignored cancellation: %+v, err=%v", result, err)
	}
}
