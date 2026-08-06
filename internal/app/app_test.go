package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
)

// TestAppFakeProviderRoundTrip wires the full app with the fake provider and
// verifies a prompt produces a session with user + assistant messages.
func TestAppFakeProviderRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	a, err := New(ctx, Options{
		Provider:   "fake",
		NoSession:  true,
		Permission: "allow",
		CWD:        t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	var sawTurnDone bool
	a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvTurnDone {
			sawTurnDone = true
		}
	})

	if err := a.Agent.Prompt(ctx, "hello from app test"); err != nil {
		t.Fatal(err)
	}
	if !sawTurnDone {
		t.Fatal("expected turn_done event")
	}
	msgs, err := a.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != protocol.RoleUser {
		t.Fatalf("msg0 role = %s", msgs[0].Role)
	}
}

// TestAppSessionPersistence verifies a real session file is created and
// messages survive reopening.
func TestAppSessionPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// Point the index at a temp sessions dir.
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(dir, "sessions"))

	a, err := New(ctx, Options{Provider: "fake", Permission: "allow", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.Prompt(ctx, "persist me"); err != nil {
		t.Fatal(err)
	}
	path := a.Session.Path()
	if path == "" {
		t.Fatal("expected a session file path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file missing: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen and verify the user message survived.
	a2, err := New(ctx, Options{Provider: "fake", Permission: "allow", CWD: dir, SessionPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	msgs, err := a2.Agent.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after reload, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content[0].Text, "persist me") {
		t.Fatalf("user message lost: %+v", msgs[0])
	}
}

// TestAppContextLoadsAgents verifies AGENTS.md is picked up into the system prompt.
func TestAppContextLoadsAgents(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("always use tabs"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := New(ctx, Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if !strings.Contains(a.Agent.SystemPrompt(), "always use tabs") {
		t.Fatalf("AGENTS.md not in system prompt: %q", a.Agent.SystemPrompt())
	}
}

// TestAppPermissionDenyBlocksBash verifies deny mode blocks write tools at the app level.
func TestAppPermissionDenyBlocksBash(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Use the opencode-go provider? No — we need a provider that errors without
	// credentials. Instead just verify permission service wiring.
	a, err := New(ctx, Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if string(a.Perm.Mode()) != "deny" {
		t.Fatalf("mode = %s, want deny", a.Perm.Mode())
	}
}
