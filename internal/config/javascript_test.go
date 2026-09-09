package config

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func TestChildPluginToolRoleConfiguration(t *testing.T) {
	for _, name := range []string{"plugin_project-helper-v2_files", "plugin_my_plugin_find_files", "plugin_" + strings.Repeat("a", 64) + "_" + strings.Repeat("b", 64)} {
		path := filepath.Join(t.TempDir(), "config.json")
		data, err := jsonv2.Marshal(map[string]any{"subagents": map[string]any{"roles": map[string]any{"plugin_scout": AgentRole{Tools: []string{"read", "glob", name}}}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("valid child tool %s rejected: %v", name, err)
		}
		if got := cfg.Subagents.Roles["plugin_scout"].Tools; len(got) != 3 || got[2] != name {
			t.Fatalf("role lost explicit tool: %v", got)
		}
	}
	for _, name := range []string{"plugin", "plugin_", "plugin__files", "plugin_demo_", "plugin_demo_*", "plugin_Demo_files", "plugin_demo_/read", "plugin_" + strings.Repeat("a", 65) + "_files", "mcp_demo_read", "unknown"} {
		cfg := Default().Subagents
		cfg.Roles["plugin_scout"] = AgentRole{Tools: []string{name}}
		if err := cfg.ValidateSubagents(); err == nil {
			t.Fatalf("invalid tool %q accepted", name)
		}
	}
}

func TestJavaScriptConfigUpdatePreservesConcurrentSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"unknown":{"preserved":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		if err := UpdateJavaScriptPlugins(path, true, func(specs map[string]plugin.JavaScriptSpec) error {
			specs["demo"] = plugin.JavaScriptSpec{Path: "/local/demo"}
			return nil
		}); err != nil {
			t.Error(err)
		}
	})
	wg.Go(func() {
		if err := UpdateSkills(path, func(skills *SkillsConfig) error { skills.Disabled = true; return nil }); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err = jsonv2.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"unknown", "skills", "js_plugins"} {
		if object[key] == nil {
			t.Fatalf("lost %s: %s", key, raw)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JavaScriptPlugins["demo"].Path != "/local/demo" || !cfg.Skills.Disabled {
		t.Fatal("lost concurrent values")
	}
}
