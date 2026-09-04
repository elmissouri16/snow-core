package subagent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type taskDeadlineProvider struct {
	*fake.Provider
	phase  string
	warmup bool
	calls  atomic.Int32
}

func (p *taskDeadlineProvider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	if p.calls.Add(1) == 1 && p.warmup {
		return p.Provider.Chat(ctx, req)
	}
	if p.phase == "chat" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &taskDeadlineStream{partial: p.phase == "partial"}, nil
}

type taskDeadlineStream struct{ partial bool }

func (s *taskDeadlineStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if s.partial {
		s.partial = false
		return protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: "Starting the requested work..."}, nil
	}
	<-ctx.Done()
	return protocol.StreamEvent{}, ctx.Err()
}
func (*taskDeadlineStream) Close() error { return nil }

func TestRealAgentTaskDeadlineReportsInterruption(t *testing.T) {
	for _, phase := range []string{"chat", "stream", "partial"} {
		for _, followup := range []bool{false, true} {
			name := phase + "/initial"
			if followup {
				name = phase + "/followup"
			}
			t.Run(name, func(t *testing.T) {
				store := session.NewMemoryStore(session.Options{})
				root := rootAgent(t, store)
				defer root.Close()
				m := New(t.Context(), Limits{MaxConcurrentThreads: 1, MaxAgentsPerSession: 2, MaxDepth: 1, TaskTimeout: 100 * time.Millisecond, DefaultRole: "general", Roles: map[string]Role{"general": {Name: "general"}}})
				defer m.Close(t.Context())
				prov := &taskDeadlineProvider{Provider: fake.NewRecorded(), phase: phase, warmup: followup}
				factory := ChildFactoryFunc(func(_ context.Context, spec ChildSpec) (ChildRuntime, error) {
					return agent.New(agent.Options{Provider: prov, Registry: tools.NewRegistry(), Session: session.NewMemoryStore(session.Options{}), Permission: permission.NewService(permission.ModeDeny, nil), Model: protocol.Model{Provider: "fake", ID: "m", SupportsTools: true}, Identity: spec.State.Agent.Clone()})
				})
				if err := m.Bind(root, factory, root.Publish, store); err != nil {
					t.Fatal(err)
				}
				if err := m.Ready(t.Context()); err != nil {
					t.Fatal(err)
				}
				if _, err := m.Spawn(t.Context(), m.RootCaller(), protocol.SpawnSubagentRequest{Name: "timeout", Task: "perform work", ForkTurns: "none"}); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				if err := m.WaitAll(ctx); err != nil {
					t.Fatal(err)
				}
				if followup {
					state, err := m.Get(ctx, "timeout")
					if err != nil {
						t.Fatal(err)
					}
					if state.Status != protocol.AgentCompleted || state.Error != "" {
						t.Fatalf("successful initial task = %+v", state)
					}
					if err := m.Followup(ctx, m.RootCaller(), "timeout", "continue work"); err != nil {
						t.Fatal(err)
					}
					if err := m.WaitAll(ctx); err != nil {
						t.Fatal(err)
					}
				}
				state, err := m.Get(ctx, "timeout")
				if err != nil {
					t.Fatal(err)
				}
				if state.Status != protocol.AgentInterrupted || !strings.Contains(state.Error, context.DeadlineExceeded.Error()) {
					t.Fatalf("expired child = %+v; want interrupted with deadline reason", state)
				}
				messages, err := root.Messages()
				if err != nil {
					t.Fatal(err)
				}
				last := messages[len(messages)-1]
				payload := last.Content[0].Text
				if last.Role != protocol.RoleAgent || !strings.Contains(payload, "Task interrupted: context deadline exceeded") {
					t.Fatalf("parent did not receive interruption: %+v", last)
				}
				if phase == "partial" && (!strings.Contains(payload, "Partial response (work did not complete)") || !strings.Contains(payload, "Starting the requested work")) {
					t.Fatalf("partial result was not labeled: %q", payload)
				}
			})
		}
	}
}
