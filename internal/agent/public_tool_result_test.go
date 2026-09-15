package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPublicToolResultPreviewStrictContent(t *testing.T) {
	msg := protocol.Message{Role: protocol.RoleTool, Content: []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "public\x00\x1b\x7f\n\ttext", Data: []byte("PRIVATE-DATA"), Arguments: json.RawMessage(`{"secret":"PRIVATE-ARGS"}`)},
		{Type: protocol.BlockThinking, Text: "PRIVATE-THINKING"},
		{Type: protocol.BlockProviderData, Text: "PRIVATE-PROVIDER-TEXT", Data: []byte("PRIVATE-PROVIDER")},
		{Type: protocol.BlockImage, Text: "PRIVATE-IMAGE", Data: []byte("PRIVATE-IMAGE-DATA")},
		{Type: protocol.BlockToolCall, Text: "PRIVATE-CALL", Arguments: json.RawMessage(`{"secret":"PRIVATE-ARGS"}`)},
		{Type: protocol.BlockPlan, Text: "PRIVATE-PLAN"},
		{Type: "future", Text: "PRIVATE-FUTURE"},
		{Type: protocol.BlockText, Text: "second"},
	}, ToolDisplay: &protocol.ToolDisplay{Output: "PRIVATE-DISPLAY"}, PluginDetails: json.RawMessage(`{"secret":"PRIVATE-PLUGIN"}`)}
	preview := publicToolResultPreview(msg, []any{tools.DiffDetails{Diff: "PRIVATE-DIFF"}})
	if preview == nil || preview.Text != "public\n\ttext\nsecond" || preview.Truncated {
		t.Fatalf("public projection: %+v", preview)
	}
	for _, marker := range []any{tools.PrivateDetails{}, new(tools.PrivateDetails), (*tools.PrivateDetails)(nil)} {
		if got := publicToolResultPreview(msg, []any{marker}); got != nil {
			t.Fatalf("private marker %T published result: %+v", marker, got)
		}
	}
	for _, role := range []protocol.Role{protocol.RoleAssistant, protocol.RoleUser, protocol.RoleSystem} {
		msg.Role = role
		if got := publicToolResultPreview(msg, nil); got != nil {
			t.Fatalf("non-tool role %s published result", role)
		}
	}
}

func TestPublicToolResultPreviewUnicodeBounds(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		truncated  bool
	}{
		{"unicode", strings.Repeat("界", 4000), true},
		{"ascii-exact", strings.Repeat("a", 8<<10), false},
		{"ascii-over", strings.Repeat("a", (8<<10)+1), true},
		{"invalid", strings.Repeat("\xff", 8<<10), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preview := publicToolResultPreview(protocol.Message{Role: protocol.RoleTool, Content: []protocol.ContentBlock{protocol.NewTextBlock(tc.text)}}, nil)
			if preview == nil || len(preview.Text) > 8<<10 || !utf8.ValidString(preview.Text) || preview.Truncated != tc.truncated {
				t.Fatalf("bounded preview: %+v", preview)
			}
		})
	}
	preview := publicToolResultPreview(protocol.Message{Role: protocol.RoleTool, Content: []protocol.ContentBlock{protocol.NewTextBlock(strings.Repeat("a", 8<<10)), protocol.NewTextBlock("more")}}, nil)
	if !preview.Truncated || len(preview.Text) != 8<<10 {
		t.Fatal("additional text block not accounted for in bound")
	}
}

func TestPublicToolResultEventPreservesLegacyPrivatePreview(t *testing.T) {
	for _, private := range []bool{false, true} {
		name := "public"
		var details any = tools.DiffDetails{Diff: "PRIVATE-DIFF"}
		if private {
			name, details = "private", tools.PrivateDetails{}
		}
		t.Run(name, func(t *testing.T) {
			tool := &testTool{name: "edit", schema: protocol.ToolSchema{Name: "edit", Parameters: json.RawMessage(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
				return tools.ToolResult{Content: []protocol.ContentBlock{
					protocol.NewTextBlock("public result"),
					{Type: protocol.BlockThinking, Text: "PRIVATE-THINKING"},
					{Type: protocol.BlockProviderData, Data: []byte("PRIVATE-PROVIDER")},
				}, Details: details}
			}}
			registry := tools.NewRegistry()
			if err := registry.Register(tool); err != nil {
				t.Fatal(err)
			}
			provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
				{{Type: protocol.EvStreamToolCallDone, ToolCallID: "call", ToolName: "edit", Arguments: json.RawMessage(`{"path":"PRIVATE-ARGS"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
				{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
			}}
			a, _ := setup(t, provider, registry, permission.ModeAllow)
			t.Cleanup(a.Close)
			var end protocol.AgentEvent
			a.Subscribe(func(event protocol.AgentEvent) {
				if event.Type == protocol.EvToolEnd {
					end = event
				}
			})
			if err := a.Prompt(t.Context(), "run tool"); err != nil {
				t.Fatal(err)
			}
			if private {
				if end.ToolResult != nil || end.ToolOutput != "(private goal state updated)" {
					t.Fatalf("private result: %+v", end)
				}
			} else if end.ToolResult == nil || end.ToolResult.Text != "public result" || end.ToolOutput != "PRIVATE-DIFF" {
				t.Fatalf("public and legacy preview channels mixed: %+v", end)
			}
		})
	}
}

func TestPublicToolResultPersistsAcrossReopen(t *testing.T) {
	for _, tc := range []struct {
		name      string
		text      string
		detail    any
		private   bool
		isError   bool
		truncated bool
	}{
		{name: "public", text: "public\x1b result", detail: tools.DiffDetails{Diff: "PRIVATE-DIFF"}},
		{name: "empty"},
		{name: "bounded", text: strings.Repeat("x", 8193), truncated: true},
		{name: "error", text: "public error", isError: true},
		{name: "private-value", text: "PRIVATE-TEXT", detail: tools.PrivateDetails{}, private: true},
		{name: "private-pointer", text: "PRIVATE-TEXT", detail: new(tools.PrivateDetails), private: true},
		{name: "private-typed-nil", text: "PRIVATE-TEXT", detail: (*tools.PrivateDetails)(nil), private: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			path := filepath.Join(cwd, "session.db")
			store, err := session.NewSQLiteStore(path, cwd, session.Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			a, err := New(Options{
				Provider: &scriptedProvider{}, Registry: tools.NewRegistry(), Session: store,
				Permission: permission.NewService(permission.ModeAllow, nil),
				Model:      protocol.Model{Provider: "test", ID: "m"},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(a.Close)
			var ends []protocol.AgentEvent
			a.Subscribe(func(event protocol.AgentEvent) {
				if event.Type == protocol.EvToolEnd {
					ends = append(ends, event)
				}
			})
			msg := protocol.NewToolResultMessage("result", store.BranchTip(), "call", "edit", []protocol.ContentBlock{
				protocol.NewTextBlock(tc.text),
				{Type: protocol.BlockThinking, Text: "PRIVATE-THINKING"},
				{Type: protocol.BlockProviderData, Data: []byte("PRIVATE-PROVIDER")},
			}, tc.isError)
			msg.PluginDetails = json.RawMessage(`{"secret":"PRIVATE-PLUGIN"}`)
			// Never trust a caller-supplied preview over the current private marker.
			msg.PublicToolResult = &protocol.ToolResultPreview{Text: "UNTRUSTED-PREVIEW"}
			if err := a.appendToolResult(store.BranchTip(), msg, tc.detail); err != nil {
				t.Fatal(err)
			}
			if err := a.bus.Drain(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(ends) != 1 {
				t.Fatalf("tool end events = %d", len(ends))
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := session.OpenSQLiteStore(path, cwd, session.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			messages, err := reopened.Messages()
			if err != nil || len(messages) != 1 {
				t.Fatalf("reopened messages: %v, %v", messages, err)
			}
			persisted := messages[0]
			if !reflect.DeepEqual(persisted.PublicToolResult, ends[0].ToolResult) {
				t.Fatalf("persisted public provenance %+v differs from event %+v", persisted.PublicToolResult, ends[0].ToolResult)
			}
			if persisted.Content[0].Text != tc.text || len(persisted.PluginDetails) == 0 {
				t.Fatal("persistence changed original model content or private metadata")
			}
			if tc.private {
				if persisted.PublicToolResult != nil {
					t.Fatalf("private provenance persisted: %+v", persisted.PublicToolResult)
				}
				return
			}
			wantText := strings.ReplaceAll(tc.text, "\x1b", "")
			wantText = wantText[:min(len(wantText), 8192)]
			if persisted.PublicToolResult == nil || persisted.PublicToolResult.Text != wantText || persisted.PublicToolResult.Truncated != tc.truncated {
				t.Fatalf("unexpected persisted public preview: %+v", persisted.PublicToolResult)
			}
			ends[0].ToolResult.Text = "subscriber mutation"
			if persisted.PublicToolResult.Text != wantText {
				t.Fatal("event aliases persisted preview")
			}
		})
	}
}

type failPublicToolResultStore struct {
	session.Store
	err error
}

func (s *failPublicToolResultStore) Append(session.Entry) error { return s.err }

func TestPublicToolResultAppendFailureEmitsNoEvent(t *testing.T) {
	a, store := setup(t, &scriptedProvider{}, nil, permission.ModeAllow)
	t.Cleanup(a.Close)
	appendErr := errors.New("tool result append failed")
	a.opts.Session = &failPublicToolResultStore{Store: store, err: appendErr}
	var events []protocol.AgentEvent
	a.Subscribe(func(event protocol.AgentEvent) { events = append(events, event) })
	msg := protocol.NewToolResultMessage("result", store.BranchTip(), "call", "read", []protocol.ContentBlock{protocol.NewTextBlock("public")}, false)
	if err := a.appendToolResult(store.BranchTip(), msg); !errors.Is(err, appendErr) {
		t.Fatalf("append error = %v", err)
	}
	if err := a.bus.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("append failure published events: %+v", events)
	}
	messages, err := store.Messages()
	if err != nil || len(messages) != 0 {
		t.Fatalf("failed append changed history: %v, %v", messages, err)
	}
}
