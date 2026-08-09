package skills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

const (
	maxListedResources = 200
	maxResourceBytes   = 2 << 20
)

// RegisterTools registers dedicated activation and resource-reading tools only
// when at least one valid skill is available.
func RegisterTools(reg tools.Registry, catalog *Registry) error {
	if catalog == nil || len(catalog.ordered) == 0 {
		return nil
	}
	for _, tool := range []tools.Tool{&ActivateTool{Catalog: catalog}, &ReadResourceTool{Catalog: catalog}} {
		schema := tool.Schema()
		if err := reg.RegisterDescriptor(tools.ToolDescriptor{Schema: schema, Tool: tool, Source: tools.SourceBuiltin, Owner: "skills", OriginalName: schema.Name, Risk: "read"}); err != nil {
			return err
		}
	}
	return nil
}

// ActivateTool loads a SKILL.md body on demand.
type ActivateTool struct{ Catalog *Registry }

func (t *ActivateTool) Schema() tools.ToolSchema {
	names := make([]string, 0, len(t.Catalog.ordered))
	for _, skill := range t.Catalog.ordered {
		names = append(names, skill.Name)
	}
	params, _ := json.Marshal(map[string]any{
		"type": "object", "required": []string{"name"},
		"properties": map[string]any{"name": map[string]any{"type": "string", "enum": names, "description": "Discovered skill name."}},
	})
	return tools.ToolSchema{Name: "activate_skill", Description: "Load the complete instructions for a discovered Agent Skill. Call this before performing a task that matches a skill description.", Parameters: params}
}

func (t *ActivateTool) Run(_ context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.ErrorResult(fmt.Errorf("activate_skill: invalid arguments: %w", err)), nil
	}
	skill, body, err := t.Catalog.load(args.Name)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("activate_skill: %w", err)), nil
	}
	resources, truncated, err := listResources(skill.Directory, maxListedResources)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("activate_skill: list resources: %w", err)), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<skill_content name=%q>\n", skill.Name)
	b.Write(body)
	b.WriteString("\n\nSkill directory: ")
	b.WriteString(skill.Directory)
	b.WriteString("\nRelative paths in this skill are relative to the skill directory.")
	if len(resources) > 0 {
		b.WriteString("\n<skill_resources>\n")
		for _, resource := range resources {
			b.WriteString("  <file>")
			b.WriteString(resource)
			b.WriteString("</file>\n")
		}
		if truncated {
			b.WriteString("  <truncated>true</truncated>\n")
		}
		b.WriteString("</skill_resources>")
	}
	b.WriteString("\n</skill_content>")
	return tools.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(b.String())}, Details: tools.SkillActivationDetails{Name: skill.Name, Content: b.String()}}, nil
}

// ReadResourceTool reads one file confined to a discovered skill directory.
type ReadResourceTool struct{ Catalog *Registry }

func (t *ReadResourceTool) Schema() tools.ToolSchema {
	names := make([]string, 0, len(t.Catalog.ordered))
	for _, skill := range t.Catalog.ordered {
		names = append(names, skill.Name)
	}
	params, _ := json.Marshal(map[string]any{
		"type": "object", "required": []string{"name", "path"},
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "enum": names, "description": "Discovered skill name."},
			"path": map[string]any{"type": "string", "description": "Skill-root-relative resource path."},
		},
	})
	return tools.ToolSchema{Name: "read_skill_resource", Description: "Read one script, reference, template, or asset from an Agent Skill without granting general filesystem access to the skill directory.", Parameters: params}
}

func (t *ReadResourceTool) Run(ctx context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.ErrorResult(fmt.Errorf("read_skill_resource: invalid arguments: %w", err)), nil
	}
	skill, ok := t.Catalog.Get(args.Name)
	if !ok {
		return tools.ErrorResult(fmt.Errorf("read_skill_resource: unknown skill %q", args.Name)), nil
	}
	if err := ctx.Err(); err != nil {
		return tools.ErrorResult(err), nil
	}
	clean := filepath.Clean(filepath.FromSlash(args.Path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return tools.ErrorResult(fmt.Errorf("read_skill_resource: path must stay inside the skill directory")), nil
	}
	root, err := filepath.EvalSymlinks(skill.Directory)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("read_skill_resource: resolve skill: %w", err)), nil
	}
	path, err := filepath.EvalSymlinks(filepath.Join(skill.Directory, clean))
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("read_skill_resource: resolve resource: %w", err)), nil
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return tools.ErrorResult(fmt.Errorf("read_skill_resource: path resolves outside the skill directory")), nil
	}
	data, err := readBounded(path, maxResourceBytes)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("read_skill_resource: %w", err)), nil
	}
	if utf8.Valid(data) && !containsNUL(data) {
		return tools.TextResult(string(data)), nil
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return tools.TextResult(fmt.Sprintf("binary resource %s (%d bytes, base64):\n%s", filepath.ToSlash(clean), len(data), encoded)), nil
}

func containsNUL(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
