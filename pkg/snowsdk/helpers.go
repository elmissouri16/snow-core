package snowsdk

import (
	"context"
	"errors"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Close releases resources. Subsequent calls return ErrStopped.
func (s *Session) Close() error {
	if s == nil {
		return ErrStopped
	}
	s.mu.Lock()
	if s.closed || s.app == nil {
		s.mu.Unlock()
		return ErrStopped
	}
	s.closed = true
	a := s.app
	s.mu.Unlock()
	return a.Close()
}

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
func RunPrompt(ctx context.Context, opts Options, prompt string) (result string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s, err := Open(ctx, opts)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, s.Close()) }()

	var out []byte
	s.Subscribe(func(ev protocol.AgentEvent) {
		// One-shot output is the root assistant response. Child streams are
		// attributed and remain available through Subscribe, but must not be
		// concatenated into the convenience result.
		if ev.Agent == nil && ev.Type == protocol.EvTextDelta {
			out = append(out, ev.Text...)
		}
	})
	if err := s.ReadySubagents(); err != nil {
		return "", err
	}
	if err := s.Prompt(ctx, prompt); err != nil {
		return "", err
	}
	if a, err := s.activeApp(); err == nil {
		if err := a.Agent.WaitGoal(ctx); err != nil {
			return "", err
		}
		if err := a.WaitSubagentsIdle(ctx); err != nil {
			return "", err
		}
		if err := a.Agent.DrainEvents(ctx); err != nil {
			return "", err
		}
	}
	return string(out), nil
}
