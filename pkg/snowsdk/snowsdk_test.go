package snowsdk

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
)

func TestRunPromptFakeProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := RunPrompt(ctx, Options{
		Provider:   "fake",
		NoSession:  true,
		PermissionMode: "allow",
	}, "hello")
	if err != nil {
		t.Fatal(err)
	}
	// Fake provider with empty script yields no text; just verify no error.
	_ = out
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
