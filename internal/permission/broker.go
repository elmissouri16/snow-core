package permission

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Handler lets a trusted host asynchronously resolve a permission request. It
// is invoked only after the permission_request event has been published. A
// returned decision string must be one of allow, allow_session, allow_always,
// or deny; an error resolves the request to deny.
type Handler func(ctx context.Context, req protocol.PermissionRequest) (protocol.PermissionResponse, error)

type decisionResult struct {
	decision Decision
	err      error
}

type pendingPermission struct {
	id      string
	request protocol.PermissionRequest
	agent   *protocol.AgentRef
	ctx     context.Context
	result  chan decisionResult
}

// Broker is a trusted-host interactive permission asker. It preserves the
// deny-by-default contract: in ask mode it blocks only when an embedded
// handler is present or manual replies are enabled. Otherwise it denies fast
// without publishing. Root and subagent requests are activated FIFO, and each
// response remains bound to the request that created it.
type Broker struct {
	mu      sync.Mutex
	handler Handler
	manual  bool
	closed  bool
	nextID  uint64
	queue   []*pendingPermission
	pending *pendingPermission
	publish func(protocol.AgentEvent)
}

// NewBroker creates a broker. The service remembers allow_session and
// allow_always after the broker returns those decisions from Authorize.
func NewBroker(_ *SimpleService) *Broker { return &Broker{} }

// SetHandler installs the embedded trusted-host handler, if any.
func (b *Broker) SetHandler(h Handler) {
	b.mu.Lock()
	b.handler = h
	b.mu.Unlock()
	b.activateNext()
}

// SetPublisher installs the callback used to publish permission_request events
// through the normalized agent event stream.
func (b *Broker) SetPublisher(publish func(protocol.AgentEvent)) {
	b.mu.Lock()
	b.publish = publish
	b.mu.Unlock()
}

// EnableManual permits a host to resolve requests through Reply or Reject
// instead of an in-process handler.
func (b *Broker) EnableManual() {
	b.mu.Lock()
	if !b.closed {
		b.manual = true
	}
	b.mu.Unlock()
}

// HasHandler reports whether Ask can resolve without a manual host.
func (b *Broker) HasHandler() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.handler != nil
}

// Ask implements permission.Asker. It registers req, publishes its event
// before any handler runs, and blocks until a handler/client replies, rejects,
// or the context is cancelled. With no handler and no manual replies enabled,
// it denies immediately without publishing.
func (b *Broker) Ask(ctx context.Context, req Request) (Decision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	public := protocol.PermissionRequest{
		Tool:   req.Tool,
		Args:   slices.Clone(req.Args),
		Paths:  slices.Clone(req.Paths),
		Risk:   string(req.Risk),
		Reason: req.Reason,
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return DecisionDeny, errors.New("permission: broker is closed")
	}
	if b.handler == nil && !b.manual {
		b.mu.Unlock()
		return DecisionDeny, nil
	}
	b.nextID++
	public.ID = fmt.Sprintf("perm-%d", b.nextID)
	pending := &pendingPermission{
		id: public.ID, request: public, agent: req.Agent.Clone(), ctx: ctx,
		result: make(chan decisionResult, 1),
	}
	b.queue = append(b.queue, pending)
	b.mu.Unlock()
	b.activateNext()

	select {
	case res := <-pending.result:
		return res.decision, res.err
	case <-ctx.Done():
		b.cancel(pending)
		return DecisionDeny, ctx.Err()
	}
}

// activateNext publishes and starts the handler for exactly one queue head.
// The synchronous publish intentionally happens before the handler starts.
func (b *Broker) activateNext() {
	b.mu.Lock()
	if b.closed || b.pending != nil || len(b.queue) == 0 {
		b.mu.Unlock()
		return
	}
	pending := b.queue[0]
	b.pending = pending
	publish := b.publish
	handler := b.handler
	b.mu.Unlock()

	if publish != nil {
		publish(protocol.AgentEvent{
			Type: protocol.EvPermissionRequest, Agent: pending.agent,
			Permission: &protocol.Permission{Request: pending.request},
		})
	}
	if handler == nil {
		return
	}

	// A synchronous event subscriber may have manually resolved the request.
	b.mu.Lock()
	active := b.pending == pending
	b.mu.Unlock()
	if !active {
		return
	}
	go func() {
		response, err := handler(pending.ctx, pending.request)
		if err != nil {
			b.resolve(pending, DecisionDeny, err)
			return
		}
		if response.RequestID != pending.id {
			b.resolve(pending, DecisionDeny, fmt.Errorf("permission: response id %q does not match request %q", response.RequestID, pending.id))
			return
		}
		b.applyDecision(pending, response.Decision)
	}()
}

func (b *Broker) applyDecision(pending *pendingPermission, decision protocol.PermissionDecision) bool {
	switch decision {
	case protocol.PermissionAllow, protocol.PermissionAllowSession, protocol.PermissionAllowAlways, protocol.PermissionDeny:
		return b.resolve(pending, Decision(decision), nil)
	default:
		return b.resolve(pending, DecisionDeny, fmt.Errorf("permission: invalid handler decision %q", decision))
	}
}

// Reply resolves the active request with an explicit decision.
func (b *Broker) Reply(requestID string, decision Decision) error {
	switch decision {
	case DecisionAllow, DecisionAllowSession, DecisionAllowAlways, DecisionDeny:
	default:
		return fmt.Errorf("permission: invalid decision %q", decision)
	}

	b.mu.Lock()
	pending := b.pending
	if pending == nil {
		b.mu.Unlock()
		return errors.New("no permission request is pending")
	}
	if requestID != pending.id {
		id := pending.id
		b.mu.Unlock()
		return fmt.Errorf("permission request id %q does not match pending request %q", requestID, id)
	}
	b.mu.Unlock()
	if !b.resolve(pending, decision, nil) {
		return errors.New("permission request is no longer pending")
	}
	return nil
}

// Reject declines the active request, resolving it to deny.
func (b *Broker) Reject(requestID string) error {
	return b.Reply(requestID, DecisionDeny)
}

// Close releases every blocked request and rejects future ones.
func (b *Broker) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	queue := b.queue
	b.queue = nil
	b.pending = nil
	b.mu.Unlock()

	err := errors.New("permission: broker is closed")
	for _, pending := range queue {
		pending.result <- decisionResult{decision: DecisionDeny, err: err}
	}
}

// resolve completes only the currently active request. This guards against
// stale handler completions and manual/handler double replies.
func (b *Broker) resolve(pending *pendingPermission, decision Decision, err error) bool {
	b.mu.Lock()
	if b.pending != pending || len(b.queue) == 0 || b.queue[0] != pending {
		b.mu.Unlock()
		return false
	}
	b.queue = b.queue[1:]
	b.pending = nil
	b.mu.Unlock()

	pending.result <- decisionResult{decision: decision, err: err}
	b.activateNext()
	return true
}

func (b *Broker) cancel(pending *pendingPermission) {
	b.mu.Lock()
	active := b.pending == pending
	for i, item := range b.queue {
		if item == pending {
			b.queue = append(b.queue[:i], b.queue[i+1:]...)
			break
		}
	}
	if active {
		b.pending = nil
	}
	b.mu.Unlock()
	if active {
		b.activateNext()
	}
}

var _ Asker = (*Broker)(nil)
