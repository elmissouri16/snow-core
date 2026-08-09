package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/tools"
)

func writeSkill(t *testing.T, root, dir, name, description, body string) string {
	t.Helper()
	skillDir := filepath.Join(root, dir)
	if err := os.MkdirAll(filepath.Join(skillDir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\nmetadata:\n  author: test\n---\n" + body
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "references", "guide.md"), []byte("resource guide"), 0o644); err != nil {
		t.Fatal(err)
	}
	return skillDir
}

func TestDiscoverPrecedenceTrustAndProgressiveDisclosure(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "review", "review", "User review instructions.", "user body")
	projectDir := writeSkill(t, filepath.Join(cwd, ".agents", "skills"), "review", "review", "Project review instructions.", "project body")

	untrusted := Discover(Options{Home: home, SnowHome: filepath.Join(home, ".snow"), CWD: cwd})
	if got, _ := untrusted.Get("review"); got.Directory == projectDir || got.Scope != "user" {
		t.Fatalf("untrusted skill = %+v", got)
	}
	if len(untrusted.Diagnostics()) == 0 || !strings.Contains(untrusted.Diagnostics()[0].Message, "trust") {
		t.Fatalf("diagnostics = %+v", untrusted.Diagnostics())
	}

	trusted := Discover(Options{Home: home, SnowHome: filepath.Join(home, ".snow"), CWD: cwd, ProjectTrusted: true})
	got, ok := trusted.Get("review")
	if !ok || got.Directory != projectDir || got.Scope != "project" {
		t.Fatalf("trusted skill = %+v", got)
	}
	if strings.Contains(trusted.CatalogPrompt(), "project body") || !strings.Contains(trusted.CatalogPrompt(), "Project review instructions") {
		t.Fatalf("catalog should disclose metadata only: %q", trusted.CatalogPrompt())
	}
}

func TestActivationAndResourceReadAreConfined(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "pdf-tools", "pdf-tools", "Process PDFs.", "Follow the PDF workflow.")
	catalog := Discover(Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}})
	registry := tools.NewRegistry()
	if err := RegisterTools(registry, catalog); err != nil {
		t.Fatal(err)
	}

	activate, ok := registry.Get("activate_skill")
	if !ok {
		t.Fatal("activate_skill missing")
	}
	result, err := activate.Run(context.Background(), json.RawMessage(`{"name":"pdf-tools"}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "Follow the PDF workflow") || !strings.Contains(result.Content[0].Text, "references/guide.md") {
		t.Fatalf("activation = %+v, err = %v", result, err)
	}
	if details, ok := result.Details.(tools.SkillActivationDetails); !ok || details.Name != "pdf-tools" {
		t.Fatalf("details = %#v", result.Details)
	}

	read, ok := registry.Get("read_skill_resource")
	if !ok {
		t.Fatal("read_skill_resource missing")
	}
	resource, err := read.Run(context.Background(), json.RawMessage(`{"name":"pdf-tools","path":"references/guide.md"}`), nil)
	if err != nil || resource.IsError || resource.Content[0].Text != "resource guide" {
		t.Fatalf("resource = %+v, err = %v", resource, err)
	}
	escape, err := read.Run(context.Background(), json.RawMessage(`{"name":"pdf-tools","path":"../SKILL.md"}`), nil)
	if err != nil || !escape.IsError || !strings.Contains(escape.Content[0].Text, "inside") {
		t.Fatalf("escape = %+v, err = %v", escape, err)
	}
}

func TestLenientNameValidationAndRequiredDescription(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "mismatch", "Upper_Name", "Still load for compatibility.", "body")
	missing := filepath.Join(root, "missing")
	if err := os.MkdirAll(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missing, "SKILL.md"), []byte("---\nname: missing\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := Discover(Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}})
	if _, ok := catalog.Get("Upper_Name"); !ok {
		t.Fatal("leniently valid skill was skipped")
	}
	if _, ok := catalog.Get("missing"); ok {
		t.Fatal("skill without description should be skipped")
	}
	if len(catalog.Diagnostics()) < 2 {
		t.Fatalf("diagnostics = %+v", catalog.Diagnostics())
	}
}

func TestFrontmatterMayEndAtEOFAndCatalogIsBounded(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("skill-%03d", i)
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---", name, strings.Repeat("description ", 100))
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	catalog := Discover(Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}})
	if len(catalog.List()) != 100 {
		t.Fatalf("skills = %d, want 100", len(catalog.List()))
	}
	prompt := catalog.CatalogPrompt()
	if len(prompt) > defaultCatalogBytes {
		t.Fatalf("catalog bytes = %d, limit = %d", len(prompt), defaultCatalogBytes)
	}
	if !strings.Contains(prompt, "<truncated>true</truncated>") {
		t.Fatalf("catalog should report truncation: %q", prompt)
	}
}

func TestPolicyKeepsDisabledSkillsInInventoryOnly(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "review", "review", "Review code.", "review body")
	writeSkill(t, root, "deploy", "deploy", "Deploy code.", "deploy body")
	catalog := Discover(Options{
		Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}, Disabled: true,
		DisabledReason: "disabled by global skills policy",
		Overrides:      map[string]bool{"review": true},
	})
	if got := catalog.List(); len(got) != 1 || got[0].Name != "review" || !got[0].Enabled {
		t.Fatalf("enabled list = %+v", got)
	}
	if got := catalog.Inventory(); len(got) != 2 || got[0].Name != "deploy" || got[0].Enabled || got[0].DisabledBy == "" {
		t.Fatalf("inventory = %+v", got)
	}
	if _, ok := catalog.Get("deploy"); ok {
		t.Fatal("disabled skill available to activation")
	}
	if disabled, ok := catalog.Lookup("deploy"); !ok || disabled.Enabled {
		t.Fatalf("disabled lookup = %+v, %v", disabled, ok)
	}
	if strings.Contains(catalog.CatalogPrompt(), "Deploy code") {
		t.Fatal("disabled skill leaked into system catalog")
	}
	registry := tools.NewRegistry()
	if err := RegisterTools(registry, catalog); err != nil {
		t.Fatal(err)
	}
	activate, _ := registry.Get("activate_skill")
	if strings.Contains(string(activate.Schema().Parameters), "deploy") {
		t.Fatal("disabled skill leaked into activation enum")
	}
}
