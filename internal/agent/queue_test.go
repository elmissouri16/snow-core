package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	skillspkg "github.com/elmissouri16/snow-core/internal/skills"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type queuedProvider struct {
	mu       sync.Mutex
	started  chan struct{}
	release  chan struct{}
	first    []protocol.StreamEvent
	later    []protocol.StreamEvent
	requests []protocol.ChatRequest
	calls    int
}

func newQueuedProvider(first []protocol.StreamEvent) *queuedProvider {
	return &queuedProvider{started: make(chan struct{}), release: make(chan struct{}), first: first}
}

func (p *queuedProvider) ID() string                                           { return "queued" }
func (p *queuedProvider) ListModels(context.Context) ([]protocol.Model, error) { return nil, nil }
func (p *queuedProvider) Resolve(_ context.Context, c auth.Credential) (auth.Credential, error) {
	return c, nil
}
func (p *queuedProvider) Chat(_ context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	if call == 0 {
		close(p.started)
		return &gateEventStream{release: p.release, events: p.first}, nil
	}
	events := p.later
	if len(events) == 0 {
		events = []protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}
	}
	return &sliceStream{evs: events}, nil
}

type gateEventStream struct {
	release chan struct{}
	events  []protocol.StreamEvent
	once    sync.Once
	i       int
}

func (s *gateEventStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	var waitErr error
	s.once.Do(func() {
		select {
		case <-s.release:
		case <-ctx.Done():
			waitErr = ctx.Err()
		}
	})
	if waitErr != nil {
		return protocol.StreamEvent{}, waitErr
	}
	if s.i >= len(s.events) {
		return protocol.StreamEvent{}, io.EOF
	}
	ev := s.events[s.i]
	s.i++
	return ev, nil
}
func (s *gateEventStream) Close() error { return nil }

func TestQueuedInputPriorityAndOneAtATime(t *testing.T) {
	p := newQueuedProvider([]protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}})
	a, st := setup(t, p, nil, permission.ModeDeny)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial") }()
	<-p.started

	if err := a.FollowUp("follow"); err != nil {
		t.Fatal(err)
	}
	if err := a.Steer("steer one"); err != nil {
		t.Fatal(err)
	}
	if err := a.Steer("steer two"); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	p.mu.Lock()
	requests := append([]protocol.ChatRequest(nil), p.requests...)
	p.mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("provider requests = %d, want initial + two steers + follow-up", len(requests))
	}
	wantLastUser := []string{"initial", "steer one", "steer two", "follow"}
	for i, req := range requests {
		last := ""
		for _, msg := range req.Messages {
			if msg.Role == protocol.RoleUser && len(msg.Content) > 0 {
				last = msg.Content[0].Text
			}
		}
		if last != wantLastUser[i] {
			t.Fatalf("request %d last user = %q, want %q", i, last, wantLastUser[i])
		}
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	var users []string
	for _, msg := range messages {
		if msg.Role == protocol.RoleUser {
			users = append(users, msg.Content[0].Text)
		}
	}
	if len(users) != 4 || users[0] != "initial" || users[1] != "steer one" || users[2] != "steer two" || users[3] != "follow" {
		t.Fatalf("durable user order = %q", users)
	}
	if got := a.PendingInputs(); len(got.Items) != 0 {
		t.Fatalf("pending after settle = %+v", got)
	}
	if err := a.Steer("late"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("late steer = %v, want ErrNotRunning", err)
	}
}

func TestQueuedSkillMentionActivatesBeforeContinuation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code.\n---\nqueued review instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := skillspkg.Discover(skillspkg.Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}})
	defer catalog.Close()
	registry := tools.NewRegistry()
	if err := skillspkg.RegisterTools(registry, catalog); err != nil {
		t.Fatal(err)
	}
	p := newQueuedProvider([]protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}})
	a, _ := setup(t, p, registry, permission.ModeDeny)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial") }()
	<-p.started
	if err := a.Steer("Use $review for this continuation."); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	requests := append([]protocol.ChatRequest(nil), p.requests...)
	p.mu.Unlock()
	if len(requests) != 2 || strings.Contains(requests[0].System, "queued review instructions") || !strings.Contains(requests[1].System, "queued review instructions") {
		t.Fatalf("queued activation requests = %+v", requests)
	}
}

func TestQueuedPlanInputStartsDistinctPlanResponse(t *testing.T) {
	planEvents := []protocol.StreamEvent{
		{Type: protocol.EvStreamTextDelta, Text: "<proposed_plan>\n# Plan\n- step\n</proposed_plan>"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}
	p := newQueuedProvider(planEvents)
	p.later = planEvents
	a, _ := setup(t, p, nil, permission.ModeDeny)
	if err := a.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	completed := 0
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvPlanCompleted {
			mu.Lock()
			completed++
			mu.Unlock()
		}
	})
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial plan") }()
	<-p.started
	if err := a.Steer("revised plan"); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if completed != 2 {
		t.Fatalf("plan completions = %d, want one per queued instruction", completed)
	}
}

func TestSteerWaitsForCompleteToolBatch(t *testing.T) {
	first := []protocol.StreamEvent{
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{}`)},
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "c2", ToolName: "read", Arguments: json.RawMessage(`{}`)},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
	}
	p := newQueuedProvider(first)
	tool := &testTool{name: "read", schema: protocol.ToolSchema{Name: "read", Parameters: json.RawMessage(`{}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		return tools.TextResult("ok")
	}}
	reg := tools.NewRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}
	a, st := setup(t, p, reg, permission.ModeDeny)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial") }()
	<-p.started
	if err := a.Steer("after tools"); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 5 || messages[2].Role != protocol.RoleTool || messages[3].Role != protocol.RoleTool || messages[4].Role != protocol.RoleUser || messages[4].Content[0].Text != "after tools" {
		t.Fatalf("tool/steer order = %+v", messages)
	}
}

func TestAbortClearsQueuedInputsAndPublishesEmptySnapshot(t *testing.T) {
	p := newBlockingProvider()
	a, _ := setup(t, p, nil, permission.ModeDeny)
	var mu sync.Mutex
	var snapshots []protocol.InputQueue
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvQueueUpdated && ev.Queue != nil {
			mu.Lock()
			snapshots = append(snapshots, *ev.Queue.Clone())
			mu.Unlock()
		}
	})
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial") }()
	<-p.started
	if err := a.Steer("queued"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.AbortContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := a.PendingInputs(); len(got.Items) != 0 {
		t.Fatalf("pending after abort = %+v", got)
	}
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(snapshots) < 2 || len(snapshots[0].Items) != 1 || len(snapshots[len(snapshots)-1].Items) != 0 {
		t.Fatalf("queue snapshots = %+v", snapshots)
	}
}

func TestQueueSnapshotsPublishInMutationOrder(t *testing.T) {
	p := newBlockingProvider()
	a, _ := setup(t, p, nil, permission.ModeDeny)
	var mu sync.Mutex
	var sizes []int
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvQueueUpdated && ev.Queue != nil {
			mu.Lock()
			sizes = append(sizes, len(ev.Queue.Items))
			mu.Unlock()
		}
	})
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial") }()
	<-p.started
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.Steer("queued"); err != nil {
				t.Errorf("Steer: %v", err)
			}
		}()
	}
	wg.Wait()
	a.Abort()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sizes) != 33 {
		t.Fatalf("snapshot sizes = %v", sizes)
	}
	for i := 0; i < 32; i++ {
		if sizes[i] != i+1 {
			t.Fatalf("snapshot sizes out of mutation order: %v", sizes)
		}
	}
	if sizes[len(sizes)-1] != 0 {
		t.Fatalf("final queue snapshot = %d, want 0: %v", sizes[len(sizes)-1], sizes)
	}
}

func TestAbortDrainAndQueuedDeliveryHaveExactlyOneWinner(t *testing.T) {
	p := newQueuedProvider([]protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}})
	a, st := setup(t, p, nil, permission.ModeDeny)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial") }()
	<-p.started
	if err := a.Steer("boundary"); err != nil {
		t.Fatal(err)
	}
	// Hold the queue transaction so natural-stop selection and interactive
	// abort-drain contend at the exact persistence boundary.
	a.queuePublishMu.Lock()
	close(p.release)
	clearedCh := make(chan protocol.InputQueue, 1)
	go func() { clearedCh <- a.ClearPendingInputs() }()
	a.queuePublishMu.Unlock()
	cleared := <-clearedCh
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	persisted := false
	for _, message := range messages {
		if message.Role == protocol.RoleUser && message.Content[0].Text == "boundary" {
			persisted = true
		}
	}
	clearedInput := len(cleared.Items) == 1 && cleared.Items[0].Text == "boundary"
	if persisted == clearedInput {
		t.Fatalf("queued input must be exactly persisted or restored: persisted=%v cleared=%+v messages=%+v", persisted, cleared, messages)
	}
}

func TestProviderFailureDeliversAcceptedQueueBeforeReturning(t *testing.T) {
	p := newQueuedProvider([]protocol.StreamEvent{{Type: protocol.EvStreamError, Err: errors.New("provider failed")}})
	a, st := setup(t, p, nil, permission.ModeDeny)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial") }()
	<-p.started
	if err := a.FollowUp("deliver after failure"); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatalf("Prompt error = %v", err)
	}
	if len(a.PendingInputs().Items) != 0 {
		t.Fatalf("provider failure left queue = %+v", a.PendingInputs())
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range messages {
		if msg.Role == protocol.RoleUser && len(msg.Content) > 0 && msg.Content[0].Text == "deliver after failure" {
			found = true
		}
	}
	if !found {
		t.Fatalf("accepted follow-up was lost: %+v", messages)
	}
	p.mu.Lock()
	requests := append([]protocol.ChatRequest(nil), p.requests...)
	p.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider requests=%d, want failed request plus queued retry", len(requests))
	}
}

func TestInternalPersistenceFailureDoesNotConsumeQueuedInputAsRecovery(t *testing.T) {
	p := newQueuedProvider([]protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}})
	base := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	store := &failAssistantAppendStore{MemoryStore: base}
	a, err := New(Options{
		Provider: p, Registry: tools.NewRegistry(), Session: store,
		Permission: permission.NewService(permission.ModeDeny, nil),
		Model:      protocol.Model{Provider: p.ID(), ID: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial") }()
	<-p.started
	if err := a.FollowUp("must not mask persistence failure"); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "assistant append failed") {
		t.Fatalf("Prompt error=%v", err)
	}
	p.mu.Lock()
	requests := len(p.requests)
	p.mu.Unlock()
	if requests != 1 {
		t.Fatalf("provider requests=%d, internal failure triggered queue recovery", requests)
	}
	messages, err := base.Messages()
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.Role == protocol.RoleUser && messageTextForTest(message) == "must not mask persistence failure" {
			t.Fatalf("queued input consumed after internal failure: %+v", messages)
		}
	}
	pending := a.PendingInputs()
	if len(pending.Items) != 1 || pending.Items[0].Text != "must not mask persistence failure" {
		t.Fatalf("queued input was not recoverable: %+v", pending)
	}
	if err := a.Prompt(context.Background(), "new prompt"); !errors.Is(err, ErrPromptRejected) || !strings.Contains(err.Error(), "ClearPendingInputs") {
		t.Fatalf("new prompt with unrecovered queue error=%v", err)
	}
	recovered := a.ClearPendingInputs()
	if len(recovered.Items) != 1 || recovered.Items[0].Text != "must not mask persistence failure" {
		t.Fatalf("recovered queue=%+v", recovered)
	}
}

func TestMaxTurnsPreservesQueueWithoutPersistingQueuedUser(t *testing.T) {
	p := newQueuedProvider([]protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}})
	a, st := setup(t, p, nil, permission.ModeDeny)
	a.opts.MaxTurns = 1
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial") }()
	<-p.started
	if err := a.Steer("must not become a ghost"); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err == nil || err.Error() != "agent: max turns reached" {
		t.Fatalf("Prompt error = %v, want max turns", err)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, msg := range messages {
		if msg.Role == protocol.RoleUser {
			users++
			if msg.Content[0].Text == "must not become a ghost" {
				t.Fatal("max-turn rejected queue input was persisted")
			}
		}
	}
	pending := a.PendingInputs()
	if users != 1 || len(pending.Items) != 1 || pending.Items[0].Text != "must not become a ghost" {
		t.Fatalf("users=%d pending=%+v messages=%+v", users, pending, messages)
	}
	if recovered := a.ClearPendingInputs(); len(recovered.Items) != 1 || recovered.Items[0].Text != "must not become a ghost" {
		t.Fatalf("recovered max-turn queue=%+v", recovered)
	}
}

func TestAutomaticPromptQueueClosureIsAtomicWithAdmission(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, _ := setup(t, p, nil, permission.ModeDeny)
	defer a.Close()
	for i := 0; i < 100; i++ {
		a.mu.Lock()
		a.running = true
		a.autoRunning = true
		a.queueAccepting = true
		a.queuedInputs = nil
		a.mu.Unlock()

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var pending bool
		var queueErr error
		go func() {
			defer wg.Done()
			<-start
			pending = a.closeAutomaticQueueForPrompt()
		}()
		go func() {
			defer wg.Done()
			<-start
			_, queueErr = a.QueueInput(protocol.QueuedInputSteer, "race")
		}()
		close(start)
		wg.Wait()
		snapshot := a.PendingInputs()
		if queueErr == nil {
			if !pending || len(snapshot.Items) != 1 || snapshot.Items[0].Text != "race" {
				t.Fatalf("iteration %d: admitted queue was lost: pending=%v snapshot=%+v", i, pending, snapshot)
			}
		} else if !errors.Is(queueErr, ErrNotRunning) || pending || len(snapshot.Items) != 0 {
			t.Fatalf("iteration %d: rejected queue state err=%v pending=%v snapshot=%+v", i, queueErr, pending, snapshot)
		}
		a.ClearPendingInputs()
	}
	a.mu.Lock()
	a.running = false
	a.autoRunning = false
	a.mu.Unlock()
}

func TestQueuedInputValidationAndSnapshotIsolation(t *testing.T) {
	p := newBlockingProvider()
	a, _ := setup(t, p, nil, permission.ModeDeny)
	if err := a.Steer("idle"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("idle steer = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "initial") }()
	<-p.started
	if err := a.Steer("   "); err == nil {
		t.Fatal("empty queued input accepted")
	}
	if err := a.Steer(string(make([]byte, maxQueuedInputBytes+1))); err == nil {
		t.Fatal("oversized queued input accepted")
	}
	if err := a.Steer("one"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < maxPendingRootInputs; i++ {
		if err := a.FollowUp("queued"); err != nil {
			t.Fatalf("queue item %d: %v", i, err)
		}
	}
	if err := a.Steer("overflow"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("full queue error = %v", err)
	}
	snapshot := a.PendingInputs()
	snapshot.Items[0].Text = "mutated"
	if got := a.PendingInputs().Items[0].Text; got != "one" {
		t.Fatalf("snapshot aliased internal queue: %q", got)
	}
	a.Abort()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
