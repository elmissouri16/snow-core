package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type preflightTestTool struct {
	analysis     permission.Analysis
	preflightErr error
	runs         int
}

func (*preflightTestTool) Schema() tools.ToolSchema {
	return protocol.ToolSchema{Name: "analyzed", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (t *preflightTestTool) Preflight(context.Context, json.RawMessage, tools.ToolHost) (permission.Analysis, error) {
	return permission.CloneAnalysis(t.analysis), t.preflightErr
}

func (t *preflightTestTool) Run(context.Context, json.RawMessage, tools.ToolHost) (tools.ToolResult, error) {
	t.runs++
	return tools.TextResult("ran"), nil
}

type captureAsker struct {
	calls int
	req   permission.Request
}

func (a *captureAsker) Ask(_ context.Context, req permission.Request) (permission.Decision, error) {
	a.calls++
	a.req = req
	return permission.DecisionAllow, nil
}

func newPreflightAgent(t *testing.T, tool tools.Tool, perm permission.Service) *Agent {
	t.Helper()
	registry := tools.NewRegistry()
	if err := registry.RegisterDescriptor(tools.ToolDescriptor{
		Schema: tool.Schema(), Tool: tool, Source: tools.SourceBuiltin, Owner: "test",
		Risk: permission.RiskExec, Effect: tools.EffectMutating,
	}); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{}
	a, err := New(Options{
		Provider: provider, Registry: registry, Session: session.NewMemoryStore(session.Options{}),
		Permission: perm, Model: protocol.Model{Provider: provider.ID(), ID: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestExecuteOneHardPolicyDeniesBeforePermissionAndRun(t *testing.T) {
	tool := &preflightTestTool{analysis: permission.Analysis{
		Effects: []permission.Effect{{
			Type: "filesystem", Capability: permission.CapabilityCredentialsRead,
			Operation: "read", Resource: "/home/test/.ssh/id_ed25519", Reason: "protected credential read", Confidence: "high",
		}},
		Capabilities: []permission.Capability{permission.CapabilityCredentialsRead},
		Rememberable: true,
		ScopeKey:     "credential-scope",
	}}
	asker := &captureAsker{}
	a := newPreflightAgent(t, tool, permission.NewService(permission.ModeAsk, asker))
	call := protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "analyzed", Arguments: json.RawMessage(`{}`)}
	msg, dispatched, err := a.executeOne(t.Context(), call, "")
	if err != nil {
		t.Fatal(err)
	}
	if dispatched || tool.runs != 0 || asker.calls != 0 || !msg.IsError {
		t.Fatalf("dispatched=%v runs=%d asks=%d error=%v", dispatched, tool.runs, asker.calls, msg.IsError)
	}
	if got := sessionMessageTextForTest(msg); !strings.Contains(got, "protected credential read") {
		t.Fatalf("result = %q", got)
	}
}

func TestExecuteOnePassesAnalysisToPermission(t *testing.T) {
	analysis := permission.Analysis{
		Effects: []permission.Effect{{
			Type: "filesystem", Capability: permission.CapabilityFilesystemReadExternal,
			Operation: "read", Resource: "/tmp/input", Confidence: "high",
		}},
		Capabilities: []permission.Capability{permission.CapabilityFilesystemReadExternal},
		Paths:        []string{"/tmp/input"},
		Summary:      "reads one external file",
		Rememberable: true,
		ScopeKey:     "external-read",
		ScopeLabel:   "read /tmp/input",
	}
	tool := &preflightTestTool{analysis: analysis}
	asker := &captureAsker{}
	a := newPreflightAgent(t, tool, permission.NewService(permission.ModeAsk, asker))
	call := protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "analyzed", Arguments: json.RawMessage(`{}`)}
	msg, dispatched, err := a.executeOne(t.Context(), call, "")
	if err != nil {
		t.Fatal(err)
	}
	if !dispatched || tool.runs != 1 || asker.calls != 1 || msg.IsError {
		t.Fatalf("dispatched=%v runs=%d asks=%d error=%v", dispatched, tool.runs, asker.calls, msg.IsError)
	}
	if asker.req.ScopeKey != analysis.ScopeKey || asker.req.ScopeLabel != analysis.ScopeLabel || len(asker.req.Effects) != 1 || len(asker.req.Paths) != 1 {
		t.Fatalf("permission request = %+v", asker.req)
	}
}

func TestExecuteOnePreflightErrorDoesNotAskOrRun(t *testing.T) {
	tool := &preflightTestTool{preflightErr: errors.New("invalid shell syntax")}
	asker := &captureAsker{}
	a := newPreflightAgent(t, tool, permission.NewService(permission.ModeAsk, asker))
	call := protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "analyzed", Arguments: json.RawMessage(`{}`)}
	msg, dispatched, err := a.executeOne(t.Context(), call, "")
	if err != nil {
		t.Fatal(err)
	}
	if dispatched || tool.runs != 0 || asker.calls != 0 || !msg.IsError {
		t.Fatalf("dispatched=%v runs=%d asks=%d error=%v", dispatched, tool.runs, asker.calls, msg.IsError)
	}
}
