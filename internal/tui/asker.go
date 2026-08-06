package tui

import (
	"context"
	"errors"
	"sync"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/pkg/protocol"
)

// tuiAsker implements permission.Asker for the interactive TUI. Ask publishes
// a permission_request event into the transcript and blocks until the user
// responds via /allow or /deny. Respond delivers the decision.
type tuiAsker struct {
	mu      sync.Mutex
	eventCh chan protocol.AgentEvent
	respCh  chan permission.Decision
	pending bool
}

func newTUIAsker(eventCh chan protocol.AgentEvent) *tuiAsker {
	return &tuiAsker{
		eventCh: eventCh,
		respCh:  make(chan permission.Decision, 1),
	}
}

// Ask implements permission.Asker.
func (a *tuiAsker) Ask(ctx context.Context, req permission.Request) (permission.Decision, error) {
	a.mu.Lock()
	if a.pending {
		a.mu.Unlock()
		return permission.DecisionDeny, errors.New("tui: a permission request is already pending")
	}
	a.pending = true
	a.mu.Unlock()

	// Reset the response channel for this request.
	select {
	case <-a.respCh:
	default:
	}

	a.eventCh <- protocol.AgentEvent{
		Type: protocol.EvPermissionRequest,
		Permission: &protocol.Permission{
			Request: protocol.PermissionRequest{
				Tool:   req.Tool,
				Args:   req.Args,
				Paths:  req.Paths,
				Risk:   string(req.Risk),
				Reason: req.Reason,
			},
		},
	}

	select {
	case d := <-a.respCh:
		a.mu.Lock()
		a.pending = false
		a.mu.Unlock()
		return d, nil
	case <-ctx.Done():
		a.mu.Lock()
		a.pending = false
		a.mu.Unlock()
		return permission.DecisionDeny, ctx.Err()
	}
}

// Respond resolves a pending request.
func (a *tuiAsker) Respond(d permission.Decision) error {
	a.mu.Lock()
	if !a.pending {
		a.mu.Unlock()
		return errors.New("tui: no pending permission request")
	}
	a.mu.Unlock()
	a.respCh <- d
	return nil
}

var _ permission.Asker = (*tuiAsker)(nil)
