package snowsdk

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestRunPromptFakeProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := RunPrompt(ctx, Options{
		Provider:       "fake",
		NoSession:      true,
		PermissionMode: "allow",
	}, "hello")
	if err != nil {
		t.Fatal(err)
	}
	// Fake provider with empty script yields no text; just verify no error.
	_ = out
}

type sdkQueueProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *sdkQueueProvider) ID() string { return "sdk-queue" }
func (p *sdkQueueProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (p *sdkQueueProvider) Resolve(_ context.Context, credential auth.Credential) (auth.Credential, error) {
	return credential, nil
}
func (p *sdkQueueProvider) Chat(_ context.Context, _ auth.Credential, _ protocol.ChatRequest) (protocol.EventStream, error) {
	first := false
	p.once.Do(func() {
		first = true
		close(p.started)
	})
	if first {
		return &sdkGateStream{release: p.release}, nil
	}
	return &sdkGateStream{}, nil
}

type sdkGateStream struct {
	release <-chan struct{}
	done    bool
}

func (s *sdkGateStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if s.done {
		return protocol.StreamEvent{}, io.EOF
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return protocol.StreamEvent{}, ctx.Err()
		}
	}
	s.done = true
	return protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}, nil
}
func (*sdkGateStream) Close() error { return nil }

func TestSDKActiveQueueMethodsAndSnapshots(t *testing.T) {
	s, err := Open(context.Background(), Options{Provider: "fake", NoSession: true, PermissionMode: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	provider := &sdkQueueProvider{started: make(chan struct{}), release: make(chan struct{})}
	model := s.Model()
	model.Provider = provider.ID()
	if err := s.app.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	var queueEvents int
	s.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvQueueUpdated {
			queueEvents++
		}
	})
	done := make(chan error, 1)
	go func() { done <- s.Prompt(context.Background(), "initial") }()
	<-provider.started
	if err := s.Steer(context.Background(), "steer"); err != nil {
		t.Fatal(err)
	}
	if err := s.FollowUp(context.Background(), "follow"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.PendingInputs()
	if err != nil || len(snapshot.Items) != 2 {
		t.Fatalf("PendingInputs = %+v, %v", snapshot, err)
	}
	snapshot.Items[0].Text = "mutated"
	independent, _ := s.PendingInputs()
	if independent.Items[0].Text != "steer" {
		t.Fatalf("PendingInputs aliased agent state: %+v", independent)
	}
	close(provider.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if queueEvents < 4 {
		t.Fatalf("queue events = %d, want enqueue and delivery snapshots", queueEvents)
	}
	messages, err := s.Messages()
	if err != nil {
		t.Fatal(err)
	}
	var users []string
	for _, message := range messages {
		if message.Role == protocol.RoleUser {
			users = append(users, message.Content[0].Text)
		}
	}
	if strings.Join(users, ",") != "initial,steer,follow" {
		t.Fatalf("durable users = %q", users)
	}
}

func TestOpenSubscribeEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s, err := Open(ctx, Options{Provider: "fake", NoSession: true, PermissionMode: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var got []string
	s.Subscribe(func(ev protocol.AgentEvent) {
		got = append(got, string(ev.Type))
	})

	if err := s.Prompt(ctx, "hi"); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "turn_done") {
		t.Fatalf("expected turn_done in events, got: %s", joined)
	}
	msgs, err := s.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 { // user + assistant
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if s.SessionID() == "" {
		t.Fatal("expected session id")
	}
}

func TestSkillInventoryIncludesPolicyDisabledSkills(t *testing.T) {
	snowHome := t.TempDir()
	t.Setenv("SNOW_HOME", snowHome)
	skillDir := filepath.Join(snowHome, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code.\n---\nReview carefully.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(snowHome, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"skills":{"disabled":true,"overrides":{"review":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(context.Background(), Options{
		Provider:       "fake",
		NoSession:      true,
		PermissionMode: "allow",
		NoMCP:          true,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if got := s.Skills(); len(got) != 0 {
		t.Fatalf("active skills = %+v, want none", got)
	}
	var review *SkillInfo
	for _, skill := range s.SkillInventory() {
		if skill.Name == "review" {
			skill := skill
			review = &skill
			break
		}
	}
	if review == nil || review.Enabled || review.DisabledBy == "" {
		t.Fatalf("review skill inventory entry = %+v", review)
	}
}

func TestBranchesAndFork(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{Provider: "fake", NoSession: true, PermissionMode: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages()
	if err != nil || len(messages) == 0 {
		t.Fatalf("messages = %+v, err=%v", messages, err)
	}
	branches, err := s.Branches()
	if err != nil || len(branches) != 1 || !branches[0].Active {
		t.Fatalf("branches = %+v, err=%v", branches, err)
	}
	fork, err := s.Fork(messages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !fork.Active || fork.TipID != messages[0].ID {
		t.Fatalf("fork = %+v", fork)
	}
	branches, err = s.Branches()
	if err != nil || len(branches) != 2 {
		t.Fatalf("branches after fork = %+v, err=%v", branches, err)
	}
	if err := s.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Messages(); err != nil || len(got) != len(messages) {
		t.Fatalf("selected main messages = %d, err=%v", len(got), err)
	}
	renamed, err := s.RenameBranch(fork.ID, "experiment")
	if err != nil || renamed.Name != "experiment" {
		t.Fatalf("rename=%+v err=%v", renamed, err)
	}
	if err := s.DeleteBranch(fork.ID); err != nil {
		t.Fatal(err)
	}
	named, err := s.ForkNamed("main", messages[0].ID, "named")
	if err != nil || named.Name != "named" || named.ParentID != "main" {
		t.Fatalf("named=%+v err=%v", named, err)
	}
}

func TestRootQueueAPIErrorsAndSnapshots(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{Provider: "fake", NoSession: true, PermissionMode: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Steer(ctx, "idle"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("idle Steer = %v, want ErrNotRunning", err)
	}
	if err := s.FollowUp(ctx, "idle"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("idle FollowUp = %v, want ErrNotRunning", err)
	}
	queue, err := s.PendingInputs()
	if err != nil || len(queue.Items) != 0 {
		t.Fatalf("PendingInputs = %+v, %v", queue, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Steer(cancelled, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Steer = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.FollowUp(ctx, "closed"); !errors.Is(err, ErrStopped) {
		t.Fatalf("closed FollowUp = %v, want ErrStopped", err)
	}
	if _, err := s.PendingInputs(); !errors.Is(err, ErrStopped) {
		t.Fatalf("closed PendingInputs = %v, want ErrStopped", err)
	}
}

func TestClosedSessionReturnsErrStopped(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{Provider: "fake", NoSession: true, PermissionMode: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Prompt(ctx, "hi"); !errors.Is(err, ErrStopped) {
		t.Fatalf("Prompt after close = %v, want ErrStopped", err)
	}
	if _, err := s.Messages(); !errors.Is(err, ErrStopped) {
		t.Fatalf("Messages after close = %v, want ErrStopped", err)
	}
	if _, err := s.Branches(); !errors.Is(err, ErrStopped) {
		t.Fatalf("Branches after close = %v, want ErrStopped", err)
	}
	if err := s.Close(); !errors.Is(err, ErrStopped) {
		t.Fatalf("double Close = %v, want ErrStopped", err)
	}
}

func TestThinkingAndModelsAPI(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{Provider: "fake", NoSession: true, PermissionMode: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Thinking() != protocol.ThinkingOff {
		t.Fatalf("default thinking = %q, want off", s.Thinking())
	}
	if s.ReasoningSummary() != protocol.ReasoningSummaryAuto || s.TextVerbosity() != protocol.TextVerbosityLow {
		t.Fatalf("default response settings = summary:%q verbosity:%q", s.ReasoningSummary(), s.TextVerbosity())
	}
	if err := s.SetReasoningSummary(protocol.ReasoningSummaryDetailed); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTextVerbosity(protocol.TextVerbosityHigh); err != nil {
		t.Fatal(err)
	}
	if s.ReasoningSummary() != protocol.ReasoningSummaryDetailed || s.TextVerbosity() != protocol.TextVerbosityHigh {
		t.Fatalf("updated response settings = summary:%q verbosity:%q", s.ReasoningSummary(), s.TextVerbosity())
	}
	s.app.Models[0].Upgrade = &protocol.ModelUpgrade{Model: "fake-2", Message: "upgrade"}
	summarySupported := true
	s.app.Models[0].SupportsReasoningSummary = &summarySupported
	models := s.Models()
	if len(models) != 1 || models[0].ID != "fake-1" {
		t.Fatalf("models = %+v", models)
	}
	models[0].ID = "changed"
	models[0].ThinkingLevels = append(models[0].ThinkingLevels, protocol.ThinkingHigh)
	models[0].Upgrade.Model = "changed-upgrade"
	*models[0].SupportsReasoningSummary = false
	if got := s.Models()[0]; got.ID == "changed" || len(got.ThinkingLevels) != 0 || got.Upgrade == nil || got.Upgrade.Model != "fake-2" || got.SupportsReasoningSummary == nil || !*got.SupportsReasoningSummary {
		t.Fatalf("models returned an aliased value: %+v", got)
	}
	if err := s.SetThinking(protocol.ThinkingHigh); err == nil {
		t.Fatal("unsupported thinking level was accepted")
	}
	if err := s.SetThinking(protocol.ThinkingOff); err != nil {
		t.Fatal(err)
	}
}

func TestResponseOptionsInitializeSession(t *testing.T) {
	s, err := Open(context.Background(), Options{
		Provider: "fake", NoSession: true, PermissionMode: "allow",
		ReasoningSummary: "concise", TextVerbosity: "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.ReasoningSummary() != protocol.ReasoningSummaryConcise || s.TextVerbosity() != protocol.TextVerbosityMedium {
		t.Fatalf("initialized response settings = summary:%q verbosity:%q", s.ReasoningSummary(), s.TextVerbosity())
	}
}

func TestOpenMissingProvider(t *testing.T) {
	ctx := context.Background()
	_, err := Open(ctx, Options{Provider: "nope", NoSession: true})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestAutoApproveDenyDefault(t *testing.T) {
	// Default headless permission is deny; auto-approve maps to allow.
	if effectivePermission(Options{}) != "deny" {
		t.Fatal("default should be deny")
	}
	if effectivePermission(Options{AutoApprove: true}) != "allow" {
		t.Fatal("autoapprove should map to allow")
	}
	if effectivePermission(Options{PermissionMode: "ask"}) != "ask" {
		t.Fatal("explicit mode should win")
	}
}

func TestUserInputHandler(t *testing.T) {
	var seen protocol.UserInputRequest
	s, err := Open(context.Background(), Options{
		Provider: "fake", NoSession: true, PermissionMode: "allow",
		UserInputHandler: func(_ context.Context, request protocol.UserInputRequest) (protocol.UserInputResponse, error) {
			seen = request
			return protocol.UserInputResponse{Answers: []protocol.UserInputAnswer{{QuestionID: "choice", Answer: "A"}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	request := protocol.UserInputRequest{ID: "ask-sdk", Questions: []protocol.UserInputQuestion{{ID: "choice", Header: "Choice", Question: "Choose?"}}}
	response, err := s.app.RequestUserInput(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if seen.ID != request.ID || response.RequestID != request.ID || response.Answers[0].Answer != "A" {
		t.Fatalf("seen=%+v response=%+v", seen, response)
	}
}
