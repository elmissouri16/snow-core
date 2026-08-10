package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestAgentThinkingConfiguration(t *testing.T) {
	provider := &e2eProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "ok"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	store := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	model := protocol.Model{
		Provider:         "e2e",
		ID:               "reasoning",
		SupportsThinking: true,
		ThinkingLevels:   []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingHigh},
	}
	a, err := New(Options{
		Provider:         provider,
		Registry:         tools.NewRegistry(),
		Session:          store,
		Permission:       permission.NewService(permission.ModeAllow, nil),
		Model:            model,
		Thinking:         protocol.ThinkingLow,
		ReasoningSummary: protocol.ReasoningSummaryConcise,
		TextVerbosity:    protocol.TextVerbosityMedium,
		Auth:             auth.NewMemoryStoreForTest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Thinking() != protocol.ThinkingLow {
		t.Fatalf("initial thinking = %q", a.Thinking())
	}
	if a.ReasoningSummary() != protocol.ReasoningSummaryConcise || a.TextVerbosity() != protocol.TextVerbosityMedium {
		t.Fatalf("initial response settings = summary:%q verbosity:%q", a.ReasoningSummary(), a.TextVerbosity())
	}
	if err := a.SetReasoningSummary(protocol.ReasoningSummaryDetailed); err != nil {
		t.Fatal(err)
	}
	if err := a.SetTextVerbosity(protocol.TextVerbosityHigh); err != nil {
		t.Fatal(err)
	}
	if err := a.SetThinking(protocol.ThinkingHigh); err != nil {
		t.Fatal(err)
	}
	if a.Thinking() != protocol.ThinkingHigh {
		t.Fatalf("updated thinking = %q", a.Thinking())
	}
	if err := a.SetThinking(protocol.ThinkingMedium); err == nil {
		t.Fatal("unsupported thinking level was accepted")
	}
	if err := a.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	requests := provider.Requests()
	if len(requests) != 1 || requests[0].Thinking != protocol.ThinkingHigh || requests[0].ReasoningSummary != protocol.ReasoningSummaryDetailed || requests[0].TextVerbosity != protocol.TextVerbosityHigh {
		t.Fatalf("provider requests = %+v", requests)
	}

	// Switching to a model with no advertised effort preserves the setting,
	// but the next prompt is rejected before another provider request.
	if err := a.SetModel(protocol.Model{Provider: "e2e", ID: "plain"}); err != nil {
		t.Fatal(err)
	}
	if a.Thinking() != protocol.ThinkingHigh {
		t.Fatalf("thinking changed during model switch: %q", a.Thinking())
	}
	if err := a.Prompt(context.Background(), "rejected"); err == nil {
		t.Fatal("prompt with unsupported model effort succeeded")
	}
	if got := len(provider.Requests()); got != 1 {
		t.Fatalf("provider requests after rejected prompt = %d, want 1", got)
	}
}

func TestEventSubscriberCanPromptAndSetModelReentrantly(t *testing.T) {
	provider := &e2eProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamTextDelta, Text: "outer"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "inner"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, err := New(Options{
		Provider: provider, Registry: tools.NewRegistry(), Session: session.NewMemoryStore(session.Options{CWD: t.TempDir()}),
		Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: "e2e", ID: "initial"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	result := make(chan error, 1)
	var once sync.Once
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type != protocol.EvTurnDone {
			return
		}
		once.Do(func() {
			if err := a.SetModel(protocol.Model{Provider: "e2e", ID: "callback"}); err != nil {
				result <- err
				return
			}
			result <- a.Prompt(context.Background(), "nested prompt")
		})
	})
	if err := a.Prompt(context.Background(), "outer prompt"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reentrant event callback deadlocked")
	}
	if got := len(provider.Requests()); got != 2 {
		t.Fatalf("provider calls=%d want 2", got)
	}
}

func TestExternalCloseWaitsForUnrelatedEventCallback(t *testing.T) {
	a, err := New(Options{
		Provider: &e2eProvider{}, Registry: tools.NewRegistry(), Session: session.NewMemoryStore(session.Options{CWD: t.TempDir()}),
		Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: "e2e", ID: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	a.Subscribe(func(protocol.AgentEvent) {
		close(started)
		<-release
	})
	a.Publish(a.StateEvent())
	<-started
	closed := make(chan struct{})
	go func() { a.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("external Close returned while callback was active")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after callback returned")
	}
}

func TestEventObserversReceiveIndependentPayloads(t *testing.T) {
	a, err := New(Options{
		Provider: &e2eProvider{}, Registry: tools.NewRegistry(), Session: session.NewMemoryStore(session.Options{CWD: t.TempDir()}),
		Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: "e2e", ID: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	seen := make(chan string, 1)
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.ThreadGoal != nil && ev.ThreadGoal.Goal != nil {
			ev.ThreadGoal.Goal.Objective = "mutated"
		}
	})
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.ThreadGoal != nil && ev.ThreadGoal.Goal != nil {
			seen <- ev.ThreadGoal.Goal.Objective
		}
	})
	goal := &protocol.ThreadGoal{GoalID: "g", Objective: "original"}
	a.Publish(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: goal}})
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "original" {
		t.Fatalf("second observer saw %q", got)
	}
	if goal.Objective != "original" {
		t.Fatalf("publisher payload mutated: %q", goal.Objective)
	}
}

func TestAgentLifecycleAndConfiguration(t *testing.T) {
	provider := &e2eProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "ok"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	root := t.TempDir()
	store := session.NewMemoryStore(session.Options{CWD: root})
	perm := permission.NewService(permission.ModeAllow, nil)
	a, err := New(Options{
		Provider:     provider,
		Registry:     tools.NewRegistry(),
		Session:      store,
		Permission:   perm,
		SystemPrompt: "configured",
		Model:        protocol.Model{Provider: "e2e", ID: "initial"},
		Auth:         auth.NewMemoryStoreForTest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.SystemPrompt() != "configured" || a.IsRunning() {
		t.Fatalf("initial state: prompt=%q running=%v", a.SystemPrompt(), a.IsRunning())
	}
	if err := a.SetModel(protocol.Model{}); err == nil {
		t.Fatal("SetModel should reject a model without provider")
	}
	if err := a.SetModel(protocol.Model{Provider: "e2e"}); err == nil {
		t.Fatal("SetModel should reject a model without an id")
	}
	summarySupported := true
	changed := protocol.Model{Provider: "e2e", ID: "changed", SupportsReasoningSummary: &summarySupported, Upgrade: &protocol.ModelUpgrade{Model: "next"}}
	var eventModel *protocol.Model
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvModelChanged && ev.Model != nil && ev.Model.ID == "changed" {
			eventModel = ev.Model
		}
	})
	if err := a.SetModel(changed); err != nil {
		t.Fatal(err)
	}
	*changed.SupportsReasoningSummary = false
	changed.Upgrade.Model = "caller-changed"
	if eventModel == nil {
		t.Fatal("model change not published")
	}
	*eventModel.SupportsReasoningSummary = false
	eventModel.Upgrade.Model = "event-changed"
	returned := a.Model()
	*returned.SupportsReasoningSummary = false
	returned.Upgrade.Model = "getter-changed"
	if got := a.Model(); got.ID != "changed" || got.SupportsReasoningSummary == nil || !*got.SupportsReasoningSummary || got.Upgrade == nil || got.Upgrade.Model != "next" {
		t.Fatalf("model metadata aliases caller, event, or getter: %+v", got)
	}
	if err := a.SetProvider(nil); err == nil {
		t.Fatal("SetProvider should reject nil")
	}
	if err := a.SetSession(nil); err == nil {
		t.Fatal("SetSession should reject nil")
	}
	replacement := session.NewMemoryStore(session.Options{CWD: root})
	if err := a.SetSession(replacement); err != nil {
		t.Fatal(err)
	}
	if msgs, err := a.Messages(); err != nil || len(msgs) != 0 {
		t.Fatalf("replacement session messages = %v, err=%v", msgs, err)
	}
	if err := a.Prompt(context.Background(), "run configured agent"); err != nil {
		t.Fatal(err)
	}
	if a.IsRunning() {
		t.Fatal("agent remains running after Prompt")
	}
	msgs, err := a.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || !strings.Contains(msgs[1].Content[0].Text, "ok") {
		t.Fatalf("messages after prompt = %+v", msgs)
	}

	invalidCases := []struct {
		name string
		opts Options
		want string
	}{
		{name: "provider", opts: Options{Registry: tools.NewRegistry(), Session: store}, want: "provider required"},
		{name: "registry", opts: Options{Provider: provider, Session: store}, want: "tool registry required"},
		{name: "session", opts: Options{Provider: provider, Registry: tools.NewRegistry()}, want: "session required"},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("New error = %v, want %q", err, tc.want)
			}
		})
	}
}
