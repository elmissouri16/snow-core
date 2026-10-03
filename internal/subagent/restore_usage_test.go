package subagent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestColdRestoreCorrectsCachedChildUsageWithoutActivation(t *testing.T) {
	for _, switchStore := range []bool{false, true} {
		name := "Bind"
		if switchStore {
			name = "setStore"
		}
		t.Run(name, func(t *testing.T) {
			cwd := t.TempDir()
			rootPath := filepath.Join(cwd, "root.db")
			store, err := session.NewSQLiteStore(rootPath, cwd, session.Options{})
			if err != nil {
				t.Fatal(err)
			}
			childDir := rootPath + ".agents"
			if err := os.Mkdir(childDir, 0o700); err != nil {
				t.Fatal(err)
			}
			parentUsage := protocol.Usage{Input: 80, Output: 20, Total: 100, Cost: &protocol.Cost{Currency: "EUR", Total: 0.5}}
			actual := protocol.Usage{Input: 8, Output: 2, Total: 10, CacheReadKnown: true, Cost: &protocol.Cost{Currency: "USD", Total: 0.05}}
			want := (protocol.Usage{}).Add(actual)
			// Closed status does not itself need normalization, so a generation
			// bump here proves usage differences participate in CAS detection.
			record := session.SubagentRecord{ParentBranchID: store.ActiveBranchID(), ChildSessionPath: filepath.Join(childDir, "legacy.db"), State: protocol.SubagentState{
				Agent:  protocol.AgentRef{ThreadID: "legacy", ParentThreadID: "root-" + store.ID(), Path: "/root/legacy", ParentPath: protocol.RootAgentPath, Depth: 1, Role: "general"},
				Status: protocol.AgentClosed, Generation: 7, Usage: new((protocol.Usage{}).Add(parentUsage).Add(actual)),
			}}
			child, err := session.NewSQLiteStore(record.ChildSessionPath, cwd, session.Options{ID: "legacy"})
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range []protocol.Message{
				protocol.NewUserMessage("fork-1-parent-user", "", "inherited task"),
				protocol.NewAssistantMessage("fork-2-parent-assistant", "", "fake", "m", []protocol.ContentBlock{protocol.NewTextBlock("parent answer")}, protocol.StopStop, &parentUsage),
				protocol.NewUserMessage("actual-task", "", "child task"),
				protocol.NewAssistantMessage("actual-answer", "", "fake", "m", []protocol.ContentBlock{protocol.NewTextBlock("child answer")}, protocol.StopStop, &actual),
			} {
				if err := child.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
					t.Fatal(err)
				}
			}
			if err := child.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(record.ChildSessionPath, 0o640); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(record.ChildSessionPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.PutSubagent(record); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = session.OpenSQLiteStore(rootPath, cwd, session.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			initial := session.NewMemoryStore(session.Options{})
			defer initial.Close()
			root := rootAgent(t, initial)
			defer root.Close()
			factoryCalls := 0
			factory := ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) {
				factoryCalls++
				t.Error("cold restore constructed a child runtime/provider")
				return nil, nil
			})
			for attempt := range 2 {
				m := New(t.Context(), Limits{MaxAgentsPerSession: 2})
				bindStore := session.SubagentTaskStore(store)
				if switchStore {
					bindStore = initial
				}
				if err := m.Bind(root, factory, root.Publish, bindStore); err != nil {
					t.Fatal(err)
				}
				if switchStore {
					if err := m.setStore(store); err != nil {
						t.Fatal(err)
					}
				}
				if err := m.Ready(t.Context()); err != nil {
					t.Fatal(err)
				}
				got, err := m.Get(t.Context(), "legacy")
				if err != nil || got.Generation != 8 || !reflect.DeepEqual(got.Usage, &want) || got.Status != protocol.AgentClosed {
					t.Fatalf("attempt %d cached get=%+v err=%v", attempt, got, err)
				}
				list, err := m.List(t.Context(), m.RootCaller(), "")
				if err != nil || len(list.Agents) != 2 || !reflect.DeepEqual(list.Agents[1].Usage, &want) {
					t.Fatalf("cached list=%+v err=%v", list, err)
				}
				fleet, err := m.Usage()
				if err != nil || !reflect.DeepEqual(fleet, want) {
					t.Fatalf("cached fleet=%+v err=%v", fleet, err)
				}
				if err := m.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			persisted, err := store.ListSubagents()
			if err != nil || len(persisted) != 1 || persisted[0].State.Generation != 8 || !reflect.DeepEqual(persisted[0].State.Usage, &want) {
				t.Fatalf("reconciled durable snapshot=%+v err=%v", persisted, err)
			}
			after, err := os.ReadFile(record.ChildSessionPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("child schema/transcript bytes changed: %v", err)
			}
			info, err := os.Stat(record.ChildSessionPath)
			if err != nil || info.Mode().Perm() != 0o640 {
				t.Fatalf("child permissions changed: %v", err)
			}
			if factoryCalls != 0 {
				t.Fatalf("cold restore activated %d runtimes", factoryCalls)
			}
		})
	}
}

func TestColdRestorePreservesUnavailableAndNilCachedUsage(t *testing.T) {
	for _, kind := range []string{"identity-mismatch", "corrupt", "nil"} {
		t.Run(kind, func(t *testing.T) {
			cwd := t.TempDir()
			store, err := session.NewSQLiteStore(filepath.Join(cwd, "root.db"), cwd, session.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			childDir := store.Path() + ".agents"
			if err := os.Mkdir(childDir, 0o700); err != nil {
				t.Fatal(err)
			}
			record := session.SubagentRecord{ParentBranchID: store.ActiveBranchID(), ChildSessionPath: filepath.Join(childDir, "child.db"), State: protocol.SubagentState{
				Agent:  protocol.AgentRef{ThreadID: "child", ParentThreadID: "root-" + store.ID(), Path: "/root/child", ParentPath: protocol.RootAgentPath, Depth: 1},
				Status: protocol.AgentClosed, Generation: 3, Usage: new(protocol.Usage{Total: 123}),
			}}
			if kind == "corrupt" || kind == "nil" {
				if err := os.WriteFile(record.ChildSessionPath, []byte("not a sqlite session"), 0o600); err != nil {
					t.Fatal(err)
				}
				if kind == "nil" {
					record.State.Usage = nil
				}
			} else {
				child, err := session.NewSQLiteStore(record.ChildSessionPath, cwd, session.Options{ID: "wrong-session"})
				if err != nil {
					t.Fatal(err)
				}
				message := protocol.NewUserMessage("existing", "", "another session")
				if err := child.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
					t.Fatal(err)
				}
				if err := child.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.PutSubagent(record); err != nil {
				t.Fatal(err)
			}
			root := rootAgent(t, store)
			defer root.Close()
			m := New(t.Context(), Limits{MaxAgentsPerSession: 1})
			factory := ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) {
				t.Fatal("unexpected child runtime construction")
				return nil, nil
			})
			if err := m.Bind(root, factory, root.Publish, store); err != nil {
				t.Fatalf("unavailable child broke parent startup: %v", err)
			}
			defer m.Close(t.Context())
			if err := m.Ready(t.Context()); err != nil {
				t.Fatal(err)
			}
			got, err := m.Get(t.Context(), "child")
			if err != nil || got.Generation != record.State.Generation || !reflect.DeepEqual(got.Usage, record.State.Usage) {
				t.Fatalf("unavailable/nil cache changed: %+v err=%v", got, err)
			}
		})
	}
}
