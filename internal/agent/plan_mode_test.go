package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/tools/builtin"
	"github.com/snow-core/snow/pkg/protocol"
)

func newPlanAgent(t *testing.T, p *scriptedProvider, reg *tools.SimpleRegistry, st *session.MemoryStore) *Agent {
	t.Helper()
	if reg == nil {
		reg = tools.NewRegistry()
		for _, tool := range []tools.Tool{builtin.NewAskUser(), builtin.NewRequestUserInput(), builtin.NewUpdatePlan()} {
			if err := reg.Register(tool); err != nil {
				t.Fatal(err)
			}
		}
	}
	perm := permission.NewService(permission.ModeAllow, nil)
	a, err := New(Options{
		Provider: p, Registry: reg, Session: st, Permission: perm,
		ToolHost: &testHost{cwd: t.TempDir(), perm: perm}, SystemPrompt: "base",
		Model:             protocol.Model{Provider: p.ID(), ID: "m", SupportsTools: true, SupportsThinking: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingMedium}},
		CollaborationMode: protocol.ModePlan,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPlanModeParsesEventsAndPersistsOrderedBlocks(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "Intro\n<proposed"},
		{Type: protocol.EvStreamTextDelta, Text: "_plan>\n# Plan\n- step\n</proposed_plan>\nOutro"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	st := session.NewMemoryStore(session.Options{})
	a := newPlanAgent(t, p, nil, st)
	var text, planText strings.Builder
	var starts, completed int
	a.Subscribe(func(ev protocol.AgentEvent) {
		switch ev.Type {
		case protocol.EvTextDelta:
			text.WriteString(ev.Text)
		case protocol.EvPlanStarted:
			starts++
		case protocol.EvPlanDelta:
			planText.WriteString(ev.Text)
		case protocol.EvPlanCompleted:
			completed++
		}
	})
	if err := a.Prompt(context.Background(), "design it"); err != nil {
		t.Fatal(err)
	}
	if got := text.String(); got != "Intro\nOutro" || strings.Contains(got, "proposed_plan") {
		t.Fatalf("visible text = %q", got)
	}
	if planText.String() != "# Plan\n- step\n" || starts != 1 || completed != 1 {
		t.Fatalf("plan=%q starts=%d completed=%d", planText.String(), starts, completed)
	}
	msgs, _ := st.Messages()
	blocks := msgs[len(msgs)-1].Content
	if len(blocks) != 3 || blocks[0].Type != protocol.BlockText || blocks[1].Type != protocol.BlockPlan || blocks[2].Type != protocol.BlockText {
		t.Fatalf("blocks = %+v", blocks)
	}
	if len(p.requests) != 1 || p.requests[0].Thinking != protocol.ThinkingMedium || !strings.Contains(p.requests[0].System, "<collaboration_mode>") {
		t.Fatalf("request = %+v", p.requests)
	}
	var names []string
	for _, schema := range p.requests[0].Tools {
		names = append(names, schema.Name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "request_user_input") || strings.Contains(joined, "ask_user") || strings.Contains(joined, "update_plan") {
		t.Fatalf("Plan schemas = %v", names)
	}
}

type failAssistantAppendStore struct{ *session.MemoryStore }

func (s *failAssistantAppendStore) Append(entry session.Entry) error {
	if entry.Message != nil && entry.Message.Role == protocol.RoleAssistant {
		return errors.New("assistant append failed")
	}
	return s.MemoryStore.Append(entry)
}

func TestPlanCompletedRequiresDurableAssistantMessage(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "<proposed_plan>\n# Durable\n</proposed_plan>"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	st := &failAssistantAppendStore{MemoryStore: session.NewMemoryStore(session.Options{})}
	perm := permission.NewService(permission.ModeAllow, nil)
	a, err := New(Options{
		Provider: p, Registry: tools.NewRegistry(), Session: st, Permission: perm,
		Model: protocol.Model{Provider: p.ID(), ID: "m"}, CollaborationMode: protocol.ModePlan,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	completed := 0
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvPlanCompleted {
			completed++
		}
	})
	if err := a.Prompt(context.Background(), "plan"); err == nil || !strings.Contains(err.Error(), "assistant append failed") {
		t.Fatalf("prompt err=%v", err)
	}
	if completed != 0 {
		t.Fatalf("published %d durable completion events", completed)
	}
}

func TestDefaultUpdatePlanPublishesImmutableStructuredEvent(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "p1", ToolName: "update_plan", Arguments: []byte(`{"explanation":"working","plan":[{"step":"inspect","status":"in_progress"}]}`)},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
	}, {{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	st := session.NewMemoryStore(session.Options{})
	a := newPlanAgent(t, p, nil, st)
	if err := a.SetMode(protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	seen := make(chan protocol.PlanUpdate, 1)
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvPlanUpdate && ev.PlanUpdate != nil {
			ev.PlanUpdate.Plan[0].Step = "mutated"
		}
	})
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvPlanUpdate && ev.PlanUpdate != nil {
			seen <- *ev.PlanUpdate.Clone()
		}
	})
	if err := a.Prompt(context.Background(), "implement"); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-seen:
		if update.Explanation != "working" || len(update.Plan) != 1 || update.Plan[0].Step != "inspect" {
			t.Fatalf("update=%+v", update)
		}
	default:
		t.Fatal("missing plan_update event")
	}
}

func TestPlanModeRejectsUpdatePlanAndModeSwitchWhileRunning(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "p1", ToolName: "update_plan", Arguments: []byte(`{"plan":[{"step":"x","status":"in_progress"}]}`)},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
	}, {{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	st := session.NewMemoryStore(session.Options{})
	a := newPlanAgent(t, p, tools.NewRegistry(), st)
	a.opts.ToolHost = nil
	a.mu.Lock()
	a.running = true
	a.mu.Unlock()
	if err := a.SetMode(protocol.ModeDefault); err == nil || !strings.Contains(err.Error(), "while running") {
		t.Fatalf("mode switch error = %v", err)
	}
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
	if err := a.Prompt(context.Background(), "plan"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.Messages()
	found := false
	for _, msg := range msgs {
		if msg.ToolName == "update_plan" && strings.Contains(sessionMessageTextForTest(msg), "not allowed in Plan mode") {
			found = true
		}
	}
	if !found {
		t.Fatalf("messages = %+v", msgs)
	}
	if err := a.TryInternalTurn(context.Background()); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("internal turn error = %v", err)
	}
}

type storeOnly struct{ session.Store }

type failingThreadStateStore struct {
	session.Store
	mode   protocol.CollaborationMode
	getErr error
	setErr error
}

func (s *failingThreadStateStore) CollaborationMode() (protocol.CollaborationMode, error) {
	return s.mode, s.getErr
}
func (s *failingThreadStateStore) SetCollaborationMode(protocol.CollaborationMode) error {
	return s.setErr
}

func TestToolGateUsesCapturedTurnMode(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	a := newPlanAgent(t, &scriptedProvider{}, tools.NewRegistry(), st)
	a.opts.ToolHost = nil
	a.mu.Lock()
	a.turnMode = protocol.ModePlan
	a.mode = protocol.ModeDefault
	a.mu.Unlock()
	call := protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "captured", Name: "update_plan", Arguments: []byte(`{}`)}
	msg, dispatched, err := a.executeOne(context.Background(), call, "")
	if err != nil {
		t.Fatal(err)
	}
	if dispatched {
		t.Fatal("plan-gated tool was reported as dispatched")
	}
	if got := sessionMessageTextForTest(msg); got != "update_plan is a TODO/checklist tool and is not allowed in Plan mode" || !msg.IsError {
		t.Fatalf("result=%q error=%v", got, msg.IsError)
	}
}

func TestSessionAndBranchSwitchesRejectRunningAtomically(t *testing.T) {
	oldStore := session.NewMemoryStore(session.Options{})
	a := newPlanAgent(t, &scriptedProvider{}, nil, oldStore)
	newStore := session.NewMemoryStore(session.Options{})
	if err := newStore.SetCollaborationMode(protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.running = true
	a.mu.Unlock()
	if err := a.SetSession(newStore); err == nil || a.opts.Session != oldStore {
		t.Fatalf("SetSession error=%v session switched=%v", err, a.opts.Session != oldStore)
	}
	if err := a.SelectBranch("main"); err == nil {
		t.Fatal("SelectBranch succeeded while running")
	}
	if _, err := a.Fork(""); err == nil {
		t.Fatal("Fork succeeded while running")
	}
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
	bad := &failingThreadStateStore{Store: newStore, getErr: errors.New("restore failed")}
	if err := a.SetSession(bad); err == nil || a.opts.Session != oldStore {
		t.Fatalf("failed restore error=%v session switched=%v", err, a.opts.Session != oldStore)
	}
}

func TestNewPropagatesThreadStateErrorsAndInvalidMode(t *testing.T) {
	base := session.NewMemoryStore(session.Options{})
	newAgent := func(st session.Store) error {
		_, err := New(Options{Provider: &scriptedProvider{}, Registry: tools.NewRegistry(), Session: st, Model: protocol.Model{Provider: "scripted", ID: "m"}})
		return err
	}
	if err := newAgent(&failingThreadStateStore{Store: base, getErr: errors.New("read failed")}); err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("get error = %v", err)
	}
	if err := newAgent(&failingThreadStateStore{Store: base, mode: "invalid"}); err == nil || !strings.Contains(err.Error(), "collaboration mode") {
		t.Fatalf("invalid mode error = %v", err)
	}
	if err := newAgent(&failingThreadStateStore{Store: base, mode: protocol.ModeDefault, setErr: errors.New("write failed")}); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("set error = %v", err)
	}
	if err := newAgent(&storeOnly{Store: base}); err != nil {
		t.Fatalf("store without thread state: %v", err)
	}
}

type interruptedPlanProvider struct{}

func (*interruptedPlanProvider) ID() string { return "interrupt" }
func (*interruptedPlanProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (*interruptedPlanProvider) Resolve(_ context.Context, c auth.Credential) (auth.Credential, error) {
	return c, nil
}
func (*interruptedPlanProvider) Chat(context.Context, protocol.ChatRequest) (protocol.EventStream, error) {
	return &interruptedPlanStream{}, nil
}

type interruptedPlanStream struct{ sent bool }

func (s *interruptedPlanStream) Next(context.Context) (protocol.StreamEvent, error) {
	if !s.sent {
		s.sent = true
		return protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: "<proposed_plan>\npartial"}, nil
	}
	return protocol.StreamEvent{}, context.Canceled
}
func (*interruptedPlanStream) Close() error { return nil }

var _ provider.Provider = (*interruptedPlanProvider)(nil)
var _ protocol.EventStream = (*interruptedPlanStream)(nil)

func TestInterruptedAndErroredPlansDoNotComplete(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    provider.Provider
		want string
	}{
		{name: "cancelled", p: &interruptedPlanProvider{}, want: "partial"},
		{name: "stream error", p: &scriptedProvider{scripts: [][]protocol.StreamEvent{{
			{Type: protocol.EvStreamTextDelta, Text: "<proposed_plan>\npartial"},
			{Type: protocol.EvStreamError, Err: errors.New("stream failed")},
		}}}, want: "partial"},
		{name: "error after close", p: &scriptedProvider{scripts: [][]protocol.StreamEvent{{
			{Type: protocol.EvStreamTextDelta, Text: "<proposed_plan>\nclosed\n</proposed_plan>\n"},
			{Type: protocol.EvStreamError, Err: errors.New("late failure")},
		}}}, want: "closed\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := session.NewMemoryStore(session.Options{})
			a, err := New(Options{Provider: tc.p, Registry: tools.NewRegistry(), Session: st,
				Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: tc.p.ID(), ID: "m"}, CollaborationMode: protocol.ModePlan})
			if err != nil {
				t.Fatal(err)
			}
			var completed int
			a.Subscribe(func(ev protocol.AgentEvent) {
				if ev.Type == protocol.EvPlanCompleted {
					completed++
				}
			})
			_ = a.Prompt(context.Background(), "plan")
			if completed != 0 {
				t.Fatalf("completed events = %d", completed)
			}
			msgs, _ := st.Messages()
			last := msgs[len(msgs)-1]
			if got := sessionMessageTextForTest(last); got != tc.want {
				t.Fatalf("partial plan = %q, want %q", got, tc.want)
			}
			for _, block := range last.Content {
				if block.Type == protocol.BlockPlan && block.PlanComplete {
					t.Fatalf("interrupted plan marked complete: %+v", block)
				}
			}
		})
	}
}

func TestEmptyPlanDoesNotComplete(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "<proposed_plan>\n\n</proposed_plan>"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a := newPlanAgent(t, p, nil, session.NewMemoryStore(session.Options{}))
	var completed int
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvPlanCompleted {
			completed++
		}
	})
	if err := a.Prompt(context.Background(), "plan"); err != nil {
		t.Fatal(err)
	}
	if completed != 0 {
		t.Fatalf("completed events = %d", completed)
	}
}

func sessionMessageTextForTest(msg protocol.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		b.WriteString(block.Text)
	}
	return b.String()
}
