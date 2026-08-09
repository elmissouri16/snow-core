package tui

import (
	"context"
	"errors"
	"sync"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/pkg/protocol"
)

// tuiAsker is an attributed FIFO interaction broker. Concurrent root/child
// permission requests are serialized and every response channel remains bound
// to the request that created it.
type pendingPermission struct {
	id       uint64
	request  permission.Request
	response chan permission.Decision
}

type tuiAsker struct {
	mu      sync.Mutex
	events  *agentEventMailbox
	publish func(protocol.AgentEvent)
	nextID  uint64
	queue   []*pendingPermission
	pending *pendingPermission // current FIFO head
}

func newTUIAsker(events *agentEventMailbox) *tuiAsker { return &tuiAsker{events: events} }
func (a *tuiAsker) SetPublisher(publish func(protocol.AgentEvent)) {
	a.mu.Lock()
	a.publish = publish
	a.mu.Unlock()
}

func (a *tuiAsker) Ask(ctx context.Context, req permission.Request) (permission.Decision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	a.nextID++
	pending := &pendingPermission{id: a.nextID, request: req, response: make(chan permission.Decision, 1)}
	a.queue = append(a.queue, pending)
	head := len(a.queue) == 1
	if head {
		a.pending = pending
	}
	a.mu.Unlock()
	if head {
		a.publishPending(pending)
	}
	select {
	case d := <-pending.response:
		return d, nil
	case <-ctx.Done():
		a.mu.Lock()
		wasHead := len(a.queue) > 0 && a.queue[0] == pending
		for i, item := range a.queue {
			if item == pending {
				a.queue = append(a.queue[:i], a.queue[i+1:]...)
				break
			}
		}
		var next *pendingPermission
		if wasHead && len(a.queue) > 0 {
			next = a.queue[0]
		}
		if wasHead {
			a.pending = next
		}
		a.mu.Unlock()
		if next != nil {
			a.publishPending(next)
		}
		return permission.DecisionDeny, ctx.Err()
	}
}

func (a *tuiAsker) publishPending(p *pendingPermission) {
	req := p.request
	public := protocol.PermissionRequest{Tool: req.Tool, Args: append([]byte(nil), req.Args...), Paths: append([]string(nil), req.Paths...), Risk: string(req.Risk), Reason: req.Reason}
	event := protocol.AgentEvent{Type: protocol.EvPermissionRequest, Agent: req.Agent.Clone(), Permission: &protocol.Permission{Request: public}}
	a.mu.Lock()
	publish := a.publish
	a.mu.Unlock()
	if publish != nil {
		publish(event)
	} else {
		a.events.Push(event)
	}
}

func (a *tuiAsker) Respond(d permission.Decision) error {
	a.mu.Lock()
	if len(a.queue) == 0 {
		a.mu.Unlock()
		return errors.New("tui: no pending permission request")
	}
	pending := a.queue[0]
	a.queue = a.queue[1:]
	var next *pendingPermission
	if len(a.queue) > 0 {
		next = a.queue[0]
	}
	a.pending = next
	a.mu.Unlock()
	select {
	case pending.response <- d:
		if next != nil {
			a.publishPending(next)
		}
		return nil
	default:
		return errors.New("tui: permission request already resolved")
	}
}

var _ permission.Asker = (*tuiAsker)(nil)
