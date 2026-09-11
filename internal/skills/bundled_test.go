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

func TestBundledSkillAvailableWithoutTrustOrDiskWrites(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	catalog := Discover(Options{Home: home, SnowHome: filepath.Join(home, ".snow"), CWD: cwd})
	skill, ok := catalog.Get("snow-js-plugin")
	if !ok || skill.Scope != "builtin" || skill.Source != "builtin" || skill.Directory != "builtin:snow-js-plugin" || !skill.ExplicitOnly {
		t.Fatalf("bundled skill = %+v, found=%v, diagnostics=%+v", skill, ok, catalog.Diagnostics())
	}
	if !strings.Contains(catalog.CatalogPrompt(), "<name>snow-js-plugin</name>") {
		t.Fatal("bundled skill missing from catalog")
	}
	tool := &ActivateTool{Catalog: catalog}
	blocked, err := tool.Run(t.Context(), []byte(`{"name":"snow-js-plugin"}`), nil)
	if err != nil || !blocked.IsError || !strings.Contains(blocked.Content[0].Text, "explicit $snow-js-plugin mention") {
		t.Fatalf("model activation was not denied: %+v, err=%v", blocked, err)
	}
	activated, err := tool.RunExplicitSkillActivation(t.Context(), []byte(`{"name":"snow-js-plugin"}`), nil)
	if err != nil || activated.IsError || !strings.Contains(activated.Content[0].Text, "not a filesystem directory") || !strings.Contains(activated.Content[0].Text, "references/api/snow.d.ts") {
		t.Fatalf("explicit activation = %+v, err=%v", activated, err)
	}
	if details, ok := activated.Details.(tools.SkillActivationDetails); !ok || details.Name != skill.Name {
		t.Fatalf("activation details = %#v", activated.Details)
	}
	read := &ReadResourceTool{Catalog: catalog}
	result, err := read.Run(t.Context(), []byte(`{"name":"snow-js-plugin","path":"references/api/snow.d.ts"}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "workflow") {
		t.Fatalf("resource = %+v, err=%v", result, err)
	}
	for _, dir := range []string{home, cwd} {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("discovery wrote to %s: %v, err=%v", dir, entries, err)
		}
	}
}

func TestBundledSkillPrecedenceAndTrust(t *testing.T) {
	home, cwd, explicit := t.TempDir(), t.TempDir(), t.TempDir()
	projectDir := writeSkill(t, filepath.Join(cwd, ".agents", "skills"), "snow-js-plugin", "snow-js-plugin", "Project override.", "project body")
	opts := Options{Home: home, SnowHome: filepath.Join(home, ".snow"), CWD: cwd}
	if got, _ := Discover(opts).Get("snow-js-plugin"); got.Source != "builtin" {
		t.Fatalf("untrusted project overrode builtin: %+v", got)
	}
	userDir := writeSkill(t, filepath.Join(home, ".agents", "skills"), "snow-js-plugin", "snow-js-plugin", "User override.", "user body")
	if got, _ := Discover(opts).Get("snow-js-plugin"); got.Directory != userDir || got.ExplicitOnly {
		t.Fatalf("user override = %+v", got)
	}
	explicitDir := writeSkill(t, explicit, "snow-js-plugin", "snow-js-plugin", "Explicit override.", "explicit body")
	opts.ExtraDirs = []string{explicit}
	if got, _ := Discover(opts).Get("snow-js-plugin"); got.Directory != explicitDir {
		t.Fatalf("explicit override = %+v", got)
	}
	opts.ProjectTrusted = true
	catalog := Discover(opts)
	if got, _ := catalog.Get("snow-js-plugin"); got.Directory != projectDir || got.resources != nil {
		t.Fatalf("trusted override = %+v", got)
	}
	result, err := (&ActivateTool{Catalog: catalog}).Run(t.Context(), []byte(`{"name":"snow-js-plugin"}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "project body") {
		t.Fatalf("ordinary activation changed = %+v, err=%v", result, err)
	}
}

func TestBundledSkillPolicyAndBudget(t *testing.T) {
	for _, opts := range []Options{
		{Disabled: true},
		{Overrides: map[string]bool{"snow-js-plugin": false}},
		{MaxCatalogBytes: 1},
	} {
		opts.Home, opts.SnowHome = t.TempDir(), t.TempDir()
		catalog := Discover(opts)
		if _, ok := catalog.Get("snow-js-plugin"); ok {
			t.Fatalf("disabled bundled skill exposed: %+v", opts)
		}
		if got, ok := catalog.Lookup("snow-js-plugin"); !ok || got.Enabled || got.DisabledBy == "" {
			t.Fatalf("disabled inventory = %+v, found=%v", got, ok)
		}
		result, err := (&ActivateTool{Catalog: catalog}).RunExplicitSkillActivation(t.Context(), []byte(`{"name":"snow-js-plugin"}`), nil)
		if err != nil || !result.IsError {
			t.Fatalf("explicit path bypassed disable: %+v, err=%v", result, err)
		}
	}
	catalog := Discover(Options{Home: t.TempDir(), SnowHome: t.TempDir(), Disabled: true, Overrides: map[string]bool{"snow-js-plugin": true}})
	if _, ok := catalog.Get("snow-js-plugin"); !ok {
		t.Fatal("named enable did not override configured global disable")
	}
	catalog.DisableAll("--no-skills")
	if _, ok := catalog.Get("snow-js-plugin"); ok {
		t.Fatal("runtime disable did not hide bundled skill")
	}
}

func TestBundledResourcesBoundsAndCancellation(t *testing.T) {
	catalog := Discover(Options{Home: t.TempDir(), SnowHome: t.TempDir()})
	skill, _ := catalog.Get("snow-js-plugin")
	resources, truncated, err := listResources(t.Context(), skill, 200)
	if err != nil || truncated || len(resources) < 60 || !slices.Contains(resources, "references/api/snow.d.ts") {
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
	if _, err := catalog.readResource(skill, "references/api/snow.d.ts", 1); err == nil {
		t.Fatal("resource byte limit not enforced")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := listResources(ctx, skill, 200); err == nil {
		t.Fatal("listing ignored cancellation")
	}
	result, err := (&ActivateTool{Catalog: catalog}).RunExplicitSkillActivation(ctx, []byte(`{"name":"snow-js-plugin"}`), nil)
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
	result, err = read.Run(ctx, []byte(`{"name":"snow-js-plugin","path":"references/api/snow.d.ts"}`), nil)
	if err != nil || !result.IsError {
		t.Fatalf("read ignored cancellation: %+v, err=%v", result, err)
	}
}
