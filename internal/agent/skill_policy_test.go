package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestActiveSkillPolicyPreservesTheEnclosingRequest(t *testing.T) {
	a := &Agent{opts: Options{SystemPrompt: "base"}}
	skill := `<skill_content name="formatter">Only produce the formatted artifact, then stop.</skill_content>`
	system := a.systemPromptForToolsAndSkills(nil, map[string]string{"formatter": skill})

	skillIndex := strings.Index(system, skill)
	policyIndex := strings.Index(system, activeSkillPolicy)
	if skillIndex < 0 || policyIndex < 0 || policyIndex <= skillIndex {
		t.Fatalf("active skill policy must follow skill content: %q", system)
	}
	for _, required := range []string{
		"they do not replace that request",
		"continue any remaining user-requested work",
		"If the user requested only the skill's deliverable, stop after providing it",
		"tool permissions remain authoritative",
	} {
		if !strings.Contains(system, required) {
			t.Errorf("active skill policy missing %q: %q", required, system)
		}
	}
	if inactive := a.systemPromptForToolsAndSkills(nil, nil); strings.Contains(inactive, activeSkillPolicy) {
		t.Fatalf("inactive skill policy added without an active skill: %q", inactive)
	}
}

func TestModelActivatedSkillPolicyPreservesParentTaskContract(t *testing.T) {
	const (
		skillName = "artifact-helper"
		skill     = `<skill_content name="artifact-helper">Produce only the requested artifact, then stop.</skill_content>`
	)
	for _, test := range []struct {
		name     string
		prompt   string
		required []string
	}{
		{
			name:   "limited skill is one step of larger request",
			prompt: "Use the artifact helper to draft release notes, then write them to RELEASE.md.",
			required: []string{
				"they do not replace that request",
				"continue any remaining user-requested work",
			},
		},
		{
			name:   "artifact-only request gains no side effects",
			prompt: "Use the artifact helper to draft release notes only; do not modify files.",
			required: []string{
				"Do not infer or perform additional side effects merely because a skill mentions them",
				"If the user requested only the skill's deliverable, stop after providing it",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := protocol.ToolSchema{
				Name:        "activate_skill",
				Description: "activate",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","enum":["artifact-helper"]}},"required":["name"]}`),
			}
			tool := &testTool{name: "activate_skill", schema: schema, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
				return tools.ToolResult{
					Content: []protocol.ContentBlock{protocol.NewTextBlock(skill)},
					Details: tools.SkillActivationDetails{Name: skillName, Content: skill},
				}
			}}
			registry := tools.NewRegistry()
			if err := registry.RegisterDescriptor(tools.ToolDescriptor{Schema: schema, Tool: tool, Source: tools.SourceBuiltin, Owner: "skills", Risk: permission.RiskRead}); err != nil {
				t.Fatal(err)
			}
			provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
				{{Type: protocol.EvStreamToolCallDone, ToolCallID: "activate", ToolName: "activate_skill", Arguments: json.RawMessage(`{"name":"artifact-helper"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
				{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
			}}
			a, err := New(Options{
				Provider: provider, Registry: registry, Session: session.NewMemoryStore(session.Options{}),
				Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base",
				SkillNames: map[string]bool{skillName: true}, Model: protocol.Model{Provider: "scripted", ID: "m1"},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()

			if err := a.Prompt(t.Context(), test.prompt); err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) != 2 {
				t.Fatalf("provider requests = %d, want 2", len(provider.requests))
			}
			if strings.Contains(provider.requests[0].System, activeSkillPolicy) {
				t.Fatalf("policy present before model activation: %q", provider.requests[0].System)
			}
			continued := provider.requests[1]
			skillIndex := strings.Index(continued.System, skill)
			policyIndex := strings.Index(continued.System, activeSkillPolicy)
			if skillIndex < 0 || policyIndex <= skillIndex {
				t.Fatalf("continuation did not append policy after activated skill: %q", continued.System)
			}
			for _, required := range test.required {
				if !strings.Contains(continued.System, required) {
					t.Errorf("continuation policy missing %q: %q", required, continued.System)
				}
			}
			if len(continued.Messages) == 0 || len(continued.Messages[0].Content) == 0 || continued.Messages[0].Role != protocol.RoleUser || continued.Messages[0].Content[0].Text != test.prompt {
				t.Fatalf("continuation lost enclosing request: %+v", continued.Messages)
			}
		})
	}
}
