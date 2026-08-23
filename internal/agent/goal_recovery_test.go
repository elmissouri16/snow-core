package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestInterruptedNonReadToolDefersActiveGoalBeforeAutoResume(t *testing.T) {
	store, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "recover.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller, err := goalpkg.New(store, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Create("do not replay unknown write", nil, false); err != nil {
		t.Fatal(err)
	}
	user := protocol.NewUserMessage("u", "", "write")
	assistant := protocol.NewAssistantMessage("a", "u", "test", "m", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "call-1", Name: "write", Arguments: json.RawMessage(`{}`)}}, protocol.StopToolUse, nil)
	if err := store.AppendBatch([]session.Entry{{Type: session.EntryMessage, ID: user.ID, Message: &user}, {Type: session.EntryMessage, ID: assistant.ID, ParentID: user.ID, Message: &assistant}}); err != nil {
		t.Fatal(err)
	}
	registry := tools.NewRegistry()
	if err := registry.Register(&testTool{name: "write", schema: protocol.ToolSchema{Name: "write", Parameters: json.RawMessage(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		t.Fatal("interrupted write must not be replayed")
		return tools.TextResult("unexpected")
	}}); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{}
	agent, err := New(Options{Provider: provider, Registry: registry, Session: store, Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: provider.ID(), ID: "m", SupportsTools: true}, Goal: controller})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	deferred, err := controller.Deferred()
	if err != nil {
		t.Fatal(err)
	}
	if !deferred {
		t.Fatal("active goal was not deferred after unknown mutating outcome")
	}
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 || messages[2].Role != protocol.RoleTool || !messages[2].IsError {
		t.Fatalf("repaired messages=%+v", messages)
	}
}
