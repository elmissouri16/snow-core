package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Exercise the real durable boundary: a call is committed, but its result is
// not. A side effect may or may not have happened before the worker was lost.
func TestSQLiteResumeInterruptedToolHistoryStaysUnresolved(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "before-side-effect"
		if committed {
			name = "after-side-effect"
		}
		t.Run(name, func(t *testing.T) {
			root, cwd := t.TempDir(), t.TempDir()
			index := session.NewFileIndex(root)
			store, err := index.Create(cwd)
			if err != nil {
				t.Fatal(err)
			}
			user := protocol.NewUserMessage("user", "", "write a file")
			owner := protocol.NewAssistantMessage("owner", user.ID, "scripted", "m", []protocol.ContentBlock{
				{Type: protocol.BlockToolCall, ToolCallID: "write-call", Name: "write", Arguments: []byte(`{"path":"result.txt","content":"committed"}`)},
			}, protocol.StopToolUse, nil)
			for _, message := range []protocol.Message{user, owner} {
				if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, ParentID: message.ParentID, Message: &message}); err != nil {
					t.Fatal(err)
				}
			}
			original, err := store.Messages()
			if err != nil {
				t.Fatal(err)
			}
			path, id := store.Path(), store.ID()
			output := filepath.Join(cwd, "result.txt")
			if committed {
				if err := os.WriteFile(output, []byte("committed"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = index.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			provider := &scriptedProvider{}
			a, err := New(Options{Provider: provider, Registry: tools.NewRegistry(), Session: store,
				Permission: permission.NewService(permission.ModeDeny, nil),
				Model:      protocol.Model{Provider: provider.ID(), ID: "m", SupportsTools: true}})
			if err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) != 0 {
				t.Fatal("resume automatically requested provider work")
			}
			a.Close()
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = index.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			messages, err := store.Messages()
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 3 || !reflect.DeepEqual(messages[:2], original) {
				t.Fatalf("repair replaced original history: %+v", messages)
			}
			recovery := messages[2]
			if recovery.ParentID != owner.ID || store.BranchTip() != recovery.ID || !recovery.IsError || !recovery.ToolOutcomeUnknown || recovery.PublicToolResult != nil {
				t.Fatalf("invalid durable recovery record: %+v", recovery)
			}
			if count, err := repairInterruptedToolCalls(store, tools.NewRegistry()); err != nil || count != 0 {
				t.Fatalf("repair was not idempotent: count=%d err=%v", count, err)
			}
			data, readErr := os.ReadFile(output)
			if (committed && (readErr != nil || string(data) != "committed")) || (!committed && !os.IsNotExist(readErr)) {
				t.Fatalf("resume changed external side effect: %q, %v", data, readErr)
			}
			projected, _ := protocol.ProjectHistoryTools(messages)
			tool := projected[owner.ID][0]
			if tool.Status != "unresolved" || tool.ResultID != "" || tool.OutputAvailable || tool.Output != "" {
				t.Errorf("interruption invented a definitive tool outcome: %+v", tool)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			page, err := session.NewCatalog(root, cwd).Messages(t.Context(), id, 0, 50, true)
			if err != nil {
				t.Fatal(err)
			}
			tool = page.Messages[1].Tools[0]
			if tool.Status != "unresolved" || tool.ResultID != "" || tool.OutputAvailable || tool.Output != "" {
				t.Errorf("catalog invented a definitive tool outcome: %+v", tool)
			}
		})
	}
}
