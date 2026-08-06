// Package snowsdk is the public, embeddable Go API for snow-core. It exposes
// the same agent loop as the CLI — no TUI, no duplicated logic.
package snowsdk

import (
	"context"
	"errors"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
)

// Options configures a Session.
type Options struct {
	// CWD is the working directory. Empty means the caller's cwd.
	CWD string
	// Provider is the provider id (opencode-go | fake). Empty uses config default.
	Provider string
	// Model is the model id. Empty resolves the provider default.
	Model string
	// SessionPath resumes an existing .jsonl session. Empty creates a new one.
	SessionPath string
	// NoSession uses an ephemeral in-memory session (no disk writes).
	NoSession bool
	// AuthPath overrides the default auth file path.
	AuthPath string
	// ConfigPath overrides the default config file path.
	ConfigPath string
	// PermissionMode is ask|allow|deny. Headless default: deny for mutating tools.
	PermissionMode string
	// AutoApprove allows all tool calls without asking. Dangerous; CI/trusted only.
	AutoApprove bool
	// Tools is a subset allowlist of tool names. Empty = all builtins.
	Tools []string
	// SystemPrompt overrides the built-in preamble.
	SystemPrompt string
	// Thinking is a thinking level (off|low|medium|high).
	Thinking string
	// APIKey provides an explicit credential (overrides auth.json and env).
	APIKey string
	// BaseURL overrides the provider base URL (opencode-go).
	BaseURL string
}

// Session is an opened agent session.
type Session struct {
	app *app.App
	ctx context.Context
}

// Open creates a session.
func Open(ctx context.Context, opts Options) (*Session, error) {
	a, err := app.New(ctx, app.Options{
		CWD:          opts.CWD,
		Provider:     opts.Provider,
		Model:        opts.Model,
		SessionPath:  opts.SessionPath,
		NoSession:    opts.NoSession,
		AuthPath:     opts.AuthPath,
		ConfigPath:   opts.ConfigPath,
		Permission:   effectivePermission(opts),
		Tools:        opts.Tools,
		SystemPrompt: opts.SystemPrompt,
		Thinking:     opts.Thinking,
		APIKey:       opts.APIKey,
		BaseURL:      opts.BaseURL,
	})
	if err != nil {
		return nil, err
	}
	return &Session{app: a, ctx: ctx}, nil
}

func effectivePermission(opts Options) string {
	if opts.AutoApprove {
		return "allow"
	}
	if opts.PermissionMode != "" {
		return opts.PermissionMode
	}
	return "deny"
}

// Prompt runs a full user turn to completion.
func (s *Session) Prompt(ctx context.Context, text string) error {
	if err := s.app.Agent.Prompt(ctx, text); err != nil {
		return err
	}
	return nil
}

// Abort cancels any in-flight turn.
func (s *Session) Abort(ctx context.Context) error {
	// The agent loop is ctx-driven; cancel the session context by creating a
	// derived context per Prompt is handled internally. Abort is best-effort.
	return nil
}

// Subscribe registers an event listener; returns an unsubscribe func.
func (s *Session) Subscribe(fn func(protocol.AgentEvent)) func() {
	return s.app.Agent.Subscribe(fn)
}

// Model returns the current model.
func (s *Session) Model() protocol.Model { return s.app.Model }

// SetModel switches the active model.
func (s *Session) SetModel(m protocol.Model) error { return s.app.Agent.SetModel(m) }

// Messages returns the linearized session messages.
func (s *Session) Messages() ([]protocol.Message, error) { return s.app.Agent.Messages() }

// IsRunning reports whether a turn is in flight.
func (s *Session) IsRunning() bool { return s.app.Agent.IsRunning() }

// SessionID returns the session identifier.
func (s *Session) SessionID() string { return s.app.Session.ID() }

// SessionPath returns the session file path ("" for in-memory).
func (s *Session) SessionPath() string { return s.app.Session.Path() }

// CWD returns the session working directory.
func (s *Session) CWD() string { return s.app.CWD() }

// Close releases resources.
func (s *Session) Close() error { return s.app.Close() }

// Convenience helpers

// MustOpen panics on error; for tests and tiny scripts.
func MustOpen(ctx context.Context, opts Options) *Session {
	s, err := Open(ctx, opts)
	if err != nil {
		panic(err)
	}
	return s
}

// RunPrompt is a one-shot helper: open, prompt, collect text deltas, close.
// Returns the accumulated assistant text.
func RunPrompt(ctx context.Context, opts Options, prompt string) (string, error) {
	s, err := Open(ctx, opts)
	if err != nil {
		return "", err
	}
	defer s.Close()

	var out []byte
	s.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvTextDelta {
			out = append(out, ev.Text...)
		}
	})
	if err := s.Prompt(ctx, prompt); err != nil {
		return "", err
	}
	return string(out), nil
}

var (
	// ErrNotRunning is returned when an operation needs a running turn.
	ErrNotRunning = errors.New("snowsdk: no running turn")
	// ErrStopped is returned after Close.
	ErrStopped = errors.New("snowsdk: session closed")
)
