package subagent

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestInheritedContextUsageInitializationFleetAndReopen(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, forkTurns := range []string{"", "all", "1", "none"} {
			t.Run(fmt.Sprintf("durable=%t/fork=%s", durable, forkTurns), func(t *testing.T) {
				ctx := t.Context()
				cwd := t.TempDir()
				parent := session.NewMemoryStore(session.Options{CWD: cwd})
				defer parent.Close()
				parentUsage := protocol.Usage{Input: 80, Output: 20, Total: 100, Requests: 1, CacheRead: 10, CacheReadKnown: true, Cost: &protocol.Cost{Currency: "USD", Total: 0.5}}
				messages := []protocol.Message{
					protocol.NewUserMessage("u1", "", "inspect"),
					protocol.NewAssistantMessage("a1", "u1", "fake", "m", []protocol.ContentBlock{protocol.NewTextBlock("parent answer")}, protocol.StopStop, &parentUsage),
				}
				for i := range messages {
					if err := parent.Append(session.Entry{Type: session.EntryMessage, ID: messages[i].ID, Message: &messages[i]}); err != nil {
						t.Fatal(err)
					}
				}
				original, err := parent.Messages()
				if err != nil {
					t.Fatal(err)
				}
				root := rootAgent(t, parent)
				defer root.Close()
				manager := New(ctx, Limits{MaxConcurrentThreads: 1, MaxAgentsPerSession: 2, MaxDepth: 1, TaskTimeout: time.Second, MinWait: time.Millisecond, DefaultWait: time.Millisecond, MaxWait: time.Second})
				var paths []string
				actual := protocol.Usage{Input: 8, Output: 2, Total: 10, CacheReadKnown: true, Cost: &protocol.Cost{Currency: "USD", Total: 0.05}}
				factory := ChildFactoryFunc(func(_ context.Context, spec ChildSpec) (ChildRuntime, error) {
					forked, err := ForkContext(spec.ParentMessages, spec.ForkTurns, cwd, spec.State.Agent.ThreadID)
					if err != nil {
						return nil, err
					}
					var store session.Store = forked
					if durable {
						path := filepath.Join(cwd, spec.State.Agent.ThreadID+".db")
						opened, err := session.NewSQLiteStore(path, cwd, session.Options{ID: spec.State.Agent.ThreadID})
						if err != nil {
							return nil, err
						}
						copied, err := forked.Messages()
						_ = forked.Close()
						if err != nil {
							return nil, err
						}
						for i := range copied {
							if err := opened.Append(session.Entry{Type: session.EntryMessage, ID: copied[i].ID, Message: &copied[i]}); err != nil {
								return nil, err
							}
						}
						store = opened
						paths = append(paths, path)
					}
					t.Cleanup(func() { _ = store.Close() })
					provider := fake.New([]fake.Step{{Kind: fake.StepText, Text: "child answer"}, {Kind: fake.StepUsage, Usage: &actual}, {Kind: fake.StepDone, Stop: protocol.StopStop}})
					child, err := agent.New(agent.Options{Provider: provider, Registry: tools.NewRegistry(), Session: store, Permission: permission.NewService(permission.ModeDeny, nil), Model: protocol.Model{Provider: "fake", ID: "m", ContextWindow: 128000}})
					if err != nil {
						return nil, err
					}
					usage, err := child.Usage()
					if err != nil || !reflect.DeepEqual(usage, protocol.Usage{}) || provider.CallCount() != 0 {
						t.Errorf("before request: usage=%+v err=%v calls=%d", usage, err, provider.CallCount())
					}
					contextMessages, err := child.ContextMessages()
					if err != nil {
						return nil, err
					}
					wantMessages := 2
					if forkTurns == "none" {
						wantMessages = 0
					}
					if len(contextMessages) != wantMessages {
						t.Errorf("inherited context=%+v", contextMessages)
					}
					for _, message := range contextMessages {
						if message.Usage != nil {
							t.Errorf("inherited usage=%+v", message.Usage)
						}
					}
					if wantMessages != 0 && contextMessages[1].Content[0].Text != "parent answer" {
						t.Error("parent context content changed")
					}
					return child, nil
				})
				if err := manager.Bind(root, factory, root.Publish, parent); err != nil {
					t.Fatal(err)
				}
				if err := manager.Ready(ctx); err != nil {
					t.Fatal(err)
				}
				defer manager.Close(ctx)
				for _, name := range []string{"one", "two"} {
					if _, err := manager.Spawn(ctx, manager.RootCaller(), protocol.SpawnSubagentRequest{Name: name, Task: "child task", ForkTurns: forkTurns}); err != nil {
						t.Fatal(err)
					}
					awaitToolState(t, manager, name, protocol.AgentCompleted)
					state, err := manager.Get(ctx, name)
					if err != nil {
						t.Fatal(err)
					}
					want := (protocol.Usage{}).Add(actual)
					if !reflect.DeepEqual(state.Usage, &want) {
						t.Fatalf("child incurred usage=%+v want=%+v", state.Usage, want)
					}
				}
				fleet, err := manager.Usage()
				wantFleet := (protocol.Usage{}).Add(actual).Add(actual)
				if err != nil || !reflect.DeepEqual(fleet, wantFleet) {
					t.Fatalf("fleet=%+v want=%+v err=%v", fleet, wantFleet, err)
				}
				after, err := parent.Messages()
				// Terminal mail is appended, but the originals and their usage remain intact.
				if err != nil || len(after) < len(original) || !reflect.DeepEqual(after[:len(original)], original) {
					t.Fatalf("parent originals changed: %+v err=%v", after, err)
				}
				if durable {
					if err := manager.Close(ctx); err != nil {
						t.Fatal(err)
					}
					for _, path := range paths {
						opened, err := session.OpenSQLiteStore(path, cwd, session.Options{})
						if err != nil {
							t.Fatal(err)
						}
						reopened := rootAgent(t, opened)
						usage, err := reopened.Usage()
						want := (protocol.Usage{}).Add(actual)
						if err != nil || !reflect.DeepEqual(usage, want) {
							t.Errorf("reopened incurred usage=%+v want=%+v err=%v", usage, want, err)
						}
						reopened.Close()
						_ = opened.Close()
					}
				}
			})
		}
	}
}
