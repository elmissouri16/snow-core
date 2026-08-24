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

func TestNewClonesToolGuidanceExclusions(t *testing.T) {
	registry := tools.NewRegistry()
	provider := &scriptedProvider{}
	perm := permission.NewService(permission.ModeAllow, nil)
	excluded := []string{"read"}
	a, err := New(Options{
		Provider: provider, Registry: registry, Session: session.NewMemoryStore(session.Options{CWD: t.TempDir()}),
		Permission: perm, ToolHost: &testHost{cwd: t.TempDir(), perm: perm},
		ToolGuidance: []ToolGuidance{{AnyOf: []string{"write"}, UnlessAny: excluded, Text: "marker"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	excluded[0] = "mutated"
	if got := a.opts.ToolGuidance[0].UnlessAny[0]; got != "read" {
		t.Fatalf("guidance exclusion aliased caller slice: %q", got)
	}
}

func TestContextReportIncludesFixedContextBudget(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(routingTool("read", nil, tools.TextResult("ok"))); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{}
	perm := permission.NewService(permission.ModeAllow, nil)
	a, err := New(Options{
		Provider: provider, Registry: registry, Session: session.NewMemoryStore(session.Options{CWD: t.TempDir()}),
		Permission: perm, ToolHost: &testHost{cwd: t.TempDir(), perm: perm}, SystemPrompt: "system",
		Model: protocol.Model{Provider: provider.ID(), ID: "large", ContextWindow: 128000, SupportsTools: true}, FixedContextBudgetPercent: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	report, err := a.ContextReport()
	if err != nil {
		t.Fatal(err)
	}
	if report.FixedContextBudgetTokens != 32000 || report.FixedContextTokens <= 0 || report.FixedContextOverBudget {
		t.Fatalf("fixed context report=%+v", report)
	}
}

func TestModelCalledSkillActivationRejectedBeforePersistence(t *testing.T) {
	registry := tools.NewRegistry()
	content := `<skill_content name="huge">` + strings.Repeat("instruction ", 10000) + `</skill_content>`
	activation := &testTool{name: "activate_skill", schema: protocol.ToolSchema{Name: "activate_skill", Parameters: json.RawMessage(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		return tools.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(content)}, Details: tools.SkillActivationDetails{Name: "huge", Content: content}}
	}}
	if err := registry.Register(activation); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "activate-1", ToolName: "activate_skill", Arguments: json.RawMessage(`{"name":"huge"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	store := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	perm := permission.NewService(permission.ModeAllow, nil)
	a, err := New(Options{
		Provider: provider, Registry: registry, Session: store, Permission: perm, ToolHost: &testHost{cwd: t.TempDir(), perm: perm},
		Model: protocol.Model{Provider: provider.ID(), ID: "bounded", ContextWindow: 32000, SupportsTools: true}, FixedContextBudgetPercent: 10,
		SkillNames: map[string]bool{"huge": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(context.Background(), "activate the skill"); err != nil {
		t.Fatal(err)
	}
	if len(a.activeSkills) != 0 {
		t.Fatalf("rejected skill became active: %+v", a.activeSkills)
	}
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	foundError := false
	for _, message := range messages {
		if message.Role == protocol.RoleTool && message.ToolName == "activate_skill" {
			foundError = message.IsError && strings.Contains(messageTextForTest(message), "fixed-context tokens")
		}
	}
	if !foundError {
		t.Fatalf("missing bounded activation error in messages: %+v", messages)
	}
	if len(provider.requests) != 2 || strings.Contains(provider.requests[1].System, strings.Repeat("instruction ", 20)) {
		t.Fatalf("rejected skill leaked into continuation: requests=%d", len(provider.requests))
	}
}

func TestRoutedSchemasCannotIncreaseRequestAboveFixedContextBudget(t *testing.T) {
	registry := tools.NewRegistry()
	parameters, err := json.Marshal(map[string]any{
		"type":       "object",
		"properties": map[string]any{"value": map[string]any{"type": "string", "description": strings.Repeat("large schema ", 5000)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deferred := &testTool{name: "large_deferred", schema: protocol.ToolSchema{
		Name: "large_deferred", Description: "large", Parameters: parameters, Discovery: deferredDiscovery("large"),
	}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult { return tools.TextResult("ok") }}
	if err := registry.Register(deferred); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	perm := permission.NewService(permission.ModeAllow, nil)
	a, err := New(Options{
		Provider: provider, Registry: registry, Router: &fakeRouter{count: 1, matches: []tools.ToolMatch{{ID: "large_deferred"}}},
		Session: session.NewMemoryStore(session.Options{CWD: t.TempDir()}), Permission: perm, ToolHost: &testHost{cwd: t.TempDir(), perm: perm},
		Model: protocol.Model{Provider: provider.ID(), ID: "bounded", ContextWindow: 32000, SupportsTools: true}, FixedContextBudgetPercent: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	err = a.Prompt(context.Background(), "use large")
	if err == nil || !strings.Contains(err.Error(), "fixed-context tokens") {
		t.Fatalf("routing budget error=%v", err)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("oversized request reached provider: %d", len(provider.requests))
	}
	if len(a.baseDeferred) != 0 || len(a.searchedDeferred) != 0 {
		t.Fatalf("turn-scoped routing survived completion: base=%v searched=%v", a.baseDeferred, a.searchedDeferred)
	}
}

func TestModelDowngradeRejectsCurrentFixedContext(t *testing.T) {
	registry := tools.NewRegistry()
	provider := &scriptedProvider{}
	perm := permission.NewService(permission.ModeAllow, nil)
	original := protocol.Model{Provider: provider.ID(), ID: "large", ContextWindow: 128000, SupportsTools: true}
	a, err := New(Options{
		Provider: provider, Registry: registry, Session: session.NewMemoryStore(session.Options{CWD: t.TempDir()}),
		Permission: perm, ToolHost: &testHost{cwd: t.TempDir(), perm: perm}, Model: original, FixedContextBudgetPercent: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.activeSkills["large"] = strings.Repeat("active instruction ", 4000)
	err = a.SetModel(protocol.Model{Provider: provider.ID(), ID: "small", ContextWindow: 8000, SupportsTools: true})
	if err == nil || !strings.Contains(err.Error(), "fixed-context tokens") {
		t.Fatalf("model downgrade error=%v", err)
	}
	if got := a.Model(); got.ID != original.ID {
		t.Fatalf("model changed after rejected downgrade: %+v", got)
	}
}
