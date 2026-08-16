package permission

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
)

var (
	// ErrManualClosed reports that the host interaction channel has closed.
	ErrManualClosed = errors.New("permission: manual interaction closed")
	// ErrPermissionRequestNotFound reports a stale, duplicate, or out-of-order
	// reply. Only the currently published FIFO request can be resolved.
	ErrPermissionRequestNotFound = errors.New("permission: request is not pending")
	permissionIDFallback         atomic.Uint64
)

// NewRequestID returns an opaque process-local permission correlation ID.
func NewRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "perm-" + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("perm-%x-%x", time.Now().UnixNano(), permissionIDFallback.Add(1))
}

type manualResult struct {
	decision Decision
	err      error
}

type manualPending struct {
	id     string
	req    Request
	result chan manualResult
}

// ManualAsker is a FIFO permission broker for remote interactive surfaces.
// It publishes at most one request at a time and binds every reply to the
// currently active opaque request ID.
type ManualAsker struct {
	mu         sync.Mutex
	dispatchMu sync.Mutex
	queue      []*manualPending
	closed     bool
	publish    func(protocol.AgentEvent)
}

// NewManualAsker constructs a manual permission broker. publish should enqueue
// rather than synchronously invoke untrusted observer callbacks.
func NewManualAsker(publish func(protocol.AgentEvent)) *ManualAsker {
	return &ManualAsker{publish: publish}
}

// Ask implements Asker.
func (a *ManualAsker) Ask(ctx context.Context, req Request) (Decision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pending := &manualPending{id: NewRequestID(), req: req, result: make(chan manualResult, 1)}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return DecisionDeny, ErrManualClosed
	}
	publish := len(a.queue) == 0
	a.queue = append(a.queue, pending)
	a.mu.Unlock()
	if publish {
		a.publishPending(pending)
	}

	select {
	case result := <-pending.result:
		return result.decision, result.err
	case <-ctx.Done():
		if a.cancel(pending.id) {
			return DecisionDeny, ctx.Err()
		}
		// A reply won the race after the context became done.
		result := <-pending.result
		return result.decision, result.err
	}
}

func (a *ManualAsker) publishPending(pending *manualPending) {
	if pending == nil || a.publish == nil {
		return
	}
	a.dispatchMu.Lock()
	defer a.dispatchMu.Unlock()
	a.mu.Lock()
	current := !a.closed && len(a.queue) > 0 && a.queue[0] == pending
	a.mu.Unlock()
	if !current {
		return
	}
	req := pending.req
	public := protocol.PermissionRequest{
		ID: pending.id, Tool: req.Tool, Args: append([]byte(nil), req.Args...),
		Paths: append([]string(nil), req.Paths...), Risk: string(req.Risk), Reason: req.Reason,
	}
	a.publish(protocol.AgentEvent{
		Type: protocol.EvPermissionRequest, Agent: req.Agent.Clone(),
		Permission: &protocol.Permission{Request: public},
	})
}

// Reply resolves the currently published request. Remote callers cannot grant
// global allow-always rules.
func (a *ManualAsker) Reply(requestID string, decision Decision) error {
	if requestID == "" {
		return ErrPermissionRequestNotFound
	}
	switch decision {
	case DecisionAllow, DecisionAllowSession, DecisionDeny:
	default:
		return fmt.Errorf("permission: unsupported remote decision %q", decision)
	}

	a.mu.Lock()
	if a.closed || len(a.queue) == 0 || a.queue[0].id != requestID {
		a.mu.Unlock()
		return ErrPermissionRequestNotFound
	}
	pending := a.queue[0]
	a.queue = a.queue[1:]
	var next *manualPending
	if len(a.queue) > 0 {
		next = a.queue[0]
	}
	a.mu.Unlock()

	pending.result <- manualResult{decision: decision}
	if next != nil {
		a.publishPending(next)
	}
	return nil
}

// Reject is equivalent to replying with deny.
func (a *ManualAsker) Reject(requestID string) error {
	return a.Reply(requestID, DecisionDeny)
}

func (a *ManualAsker) cancel(requestID string) bool {
	a.mu.Lock()
	index := -1
	for i, pending := range a.queue {
		if pending.id == requestID {
			index = i
			break
		}
	}
	if index < 0 {
		a.mu.Unlock()
		return false
	}
	wasActive := index == 0
	a.queue = append(a.queue[:index], a.queue[index+1:]...)
	var next *manualPending
	if wasActive && len(a.queue) > 0 {
		next = a.queue[0]
	}
	a.mu.Unlock()
	if next != nil {
		a.publishPending(next)
	}
	return true
}

// Close releases pending requests as denied and rejects future interaction.
func (a *ManualAsker) Close() {
	if a == nil {
		return
	}
	a.dispatchMu.Lock()
	defer a.dispatchMu.Unlock()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	pending := a.queue
	a.queue = nil
	a.mu.Unlock()
	for _, request := range pending {
		request.result <- manualResult{decision: DecisionDeny}
	}
}

var _ Asker = (*ManualAsker)(nil)
