// Package rpc implements Snow's JSONL RPC client over an already-connected
// net.Conn. It does not launch processes, run agents, or grant permissions.
//
// A Client owns its connection. The connection must honor net.Conn's concurrent
// operation, deadline, and Close contracts; an adapter around uninterruptible
// streams is not sufficient. Consume Events continuously, including during
// calls, to avoid an explicit ErrEventOverflow failure. Event text and remote
// errors are untrusted and may contain sensitive data; the client never logs it.
package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var (
	ErrClosed          = errors.New("rpc client: closed")
	ErrProtocolVersion = errors.New("rpc client: unsupported protocol version")
	ErrMalformedFrame  = errors.New("rpc client: malformed frame")
	ErrFrameTooLarge   = errors.New("rpc client: frame too large")
	ErrEventOverflow   = errors.New("rpc client: event buffer overflow")
	ErrTooManyPending  = errors.New("rpc client: too many pending calls")
)

// Options sets resource bounds. Zero values use the documented defaults.
// Negative values are invalid. The limits bound encoded frames, buffered event
// count, and pending response count, not the memory of caller-owned requests.
// Decoded JSON may occupy more memory than its encoded representation.
type Options struct {
	// MaxFrameBytes bounds each incoming and outgoing JSON payload, excluding LF.
	// Default: 16 MiB. Maximum: 64 MiB. The advertised server input limit may
	// further reduce the outgoing bound.
	MaxFrameBytes int
	// EventBuffer is the number of queued notifications. Default: 64; max: 4096.
	EventBuffer int
	// MaxPending bounds concurrent Call operations. Default: 64; max: 4096.
	MaxPending int
	// HandshakeTimeout bounds the initial rpc_ready read. Default: 10 seconds.
	HandshakeTimeout time.Duration
	// WriteTimeout bounds each request write, even without a context deadline.
	// Default: 10 seconds. Waiting for a response is bounded by Call's context.
	WriteTimeout time.Duration
}

func (o Options) defaults() (Options, error) {
	if o.MaxFrameBytes == 0 {
		o.MaxFrameBytes = protocol.RPCMaxInputBytes
	}
	if o.EventBuffer == 0 {
		o.EventBuffer = 64
	}
	if o.MaxPending == 0 {
		o.MaxPending = 64
	}
	if o.HandshakeTimeout == 0 {
		o.HandshakeTimeout = 10 * time.Second
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = 10 * time.Second
	}
	if o.MaxFrameBytes < 1 || o.MaxFrameBytes > 64*1024*1024 || o.EventBuffer < 1 || o.EventBuffer > 4096 || o.MaxPending < 1 || o.MaxPending > 4096 || o.HandshakeTimeout < 0 || o.WriteTimeout < 0 {
		return o, errors.New("rpc client: invalid resource bounds")
	}
	return o, nil
}

// Event is one asynchronous notification in wire order. Exactly one field is
// non-nil. PromptCompleted, not a successful prompt response or turn_done,
// denotes the definitive terminal result. CompactionCompleted similarly marks
// cleanup of an explicitly started compaction, not its compaction_done progress.
// Unknown future agent event types are delivered as AgentEvent with their Type
// preserved; unknown fields are ignored.
// Payload ownership passes to the single consumer of Events.
type Event struct {
	CompactionCompleted *protocol.RPCCompactionCompleted
	ProjectCompleted    *protocol.RPCProjectCompleted
	AgentEvent          *protocol.AgentEvent
	GoalRunCompleted    *protocol.RPCGoalRunCompleted
	PromptCompleted     *protocol.RPCPromptCompleted
}

type pendingCall struct {
	command  string
	response chan protocol.RPCResponse
}

// Client supports concurrent calls and one event consumer. It must not be
// copied. Construct it with New; the zero value is not usable.
type Client struct {
	conn        net.Conn
	opts        Options
	ready       protocol.RPCReady
	events      chan Event
	done        chan struct{}
	stopped     chan struct{}
	readDone    chan struct{}
	writeGate   chan struct{}
	stopContext func() bool
	mu          sync.Mutex
	err         error
	nextID      uint64
	pending     map[string]*pendingCall
}

// New takes ownership of conn (including on initialization failure), validates
// rpc_ready and its protocol version, then continuously drains frames. ctx is
// the entire client's lifetime, not just the handshake. Cancellation closes the
// transport. Invalid options or a nil context/connection are rejected without
// taking ownership. Use a real net.Conn or an adapter honoring its contracts.
func New(ctx context.Context, conn net.Conn, opts Options) (*Client, error) {
	if ctx == nil || conn == nil {
		return nil, errors.New("rpc client: context and connection are required")
	}
	opts, err := opts.defaults()
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, opts: opts, events: make(chan Event, opts.EventBuffer), done: make(chan struct{}), stopped: make(chan struct{}), readDone: make(chan struct{}), writeGate: make(chan struct{}, 1), pending: make(map[string]*pendingCall)}
	c.writeGate <- struct{}{}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := conn.SetReadDeadline(time.Now().Add(opts.HandshakeTimeout)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rpc client: handshake deadline: %w", err)
	}
	// Probe write deadlines before accepting the transport, rather than discover
	// an unusable connection after registering the first request.
	if err := conn.SetWriteDeadline(time.Now().Add(opts.WriteTimeout)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rpc client: write deadline: %w", err)
	}
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rpc client: clear write deadline: %w", err)
	}
	c.stopContext = context.AfterFunc(ctx, func() { c.fail(ctx.Err()) })
	handshake := make(chan struct{})
	go c.readLoop(handshake)
	select {
	case <-handshake:
		if err := ctx.Err(); err != nil {
			c.fail(err)
			_ = c.Close()
			return nil, err
		}
		return c, nil
	case <-c.done:
		_ = c.Close()
		return nil, c.Err()
	}
}

// Ready returns an independent copy of the validated handshake. Unknown
// capabilities are preserved, not rejected.
func (c *Client) Ready() protocol.RPCReady {
	r := c.ready
	r.Capabilities = slices.Clone(r.Capabilities)
	return r
}

// Events returns the bounded, ordered notification stream. The reader never
// waits for a consumer: overflow terminates the client with ErrEventOverflow.
// On termination, queued events remain readable and the channel closes. Check
// Err after closure; channel closure alone is not successful prompt completion.
func (c *Client) Events() <-chan Event { return c.events }

// Done closes as soon as the client has a terminal error (including Close).
// Close additionally joins its reader and any in-progress request write.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err returns nil while active or the first terminal failure. Explicit Close
// records ErrClosed. Remote command rejection is returned in RPCResponse and
// does not terminate the client.
func (c *Client) Err() error { c.mu.Lock(); defer c.mu.Unlock(); return c.err }

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	clear(c.pending)
	close(c.done)
	c.mu.Unlock()
	_ = c.conn.Close()
	close(c.stopped)
}

// Close is idempotent, interrupts I/O, and waits for the reader and active write
// to exit. It returns nil; use Err to inspect the terminal reason. Close does
// not send a shutdown or abort command and does not manage any server process.
func (c *Client) Close() error {
	c.fail(ErrClosed)
	c.stopContext()
	<-c.stopped
	<-c.readDone
	<-c.writeGate
	c.writeGate <- struct{}{}
	return nil
}

// Call assigns a unique ID (replacing req.ID), writes one command, and waits
// for its first correlated response. Success=false is a remote rejection, not
// a Go error. A successful prompt response is admission only; match its ID to
// a PromptCompleted.RequestID from Events, which may already be buffered.
//
// Canceling a response wait removes its pending entry but does not abort remote
// work. Late responses, including the legacy second failed-prompt response, are
// ignored; definitive prompt failures still arrive as PromptCompleted events.
// Cancellation during a write terminates the connection because a partial JSONL
// frame cannot be safely resumed. Errors after transmission do not imply that
// the server did not execute the command. Use an explicit abort command if
// desired, and never automatically retry a potentially executed mutation.
func (c *Client) Call(ctx context.Context, req protocol.RPCRequest) (protocol.RPCResponse, error) {
	var zero protocol.RPCResponse
	if ctx == nil {
		return zero, errors.New("rpc client: context is required")
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if req.Type == "" {
		return zero, errors.New("rpc client: command type is required")
	}
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return zero, err
	}
	if len(c.pending) >= c.opts.MaxPending {
		c.mu.Unlock()
		return zero, ErrTooManyPending
	}
	// Refuse wraparound rather than ever reuse a correlation ID.
	if c.nextID == ^uint64(0) {
		c.mu.Unlock()
		return zero, errors.New("rpc client: request IDs exhausted")
	}
	c.nextID++
	req.ID = strconv.FormatUint(c.nextID, 10)
	p := &pendingCall{command: req.Type, response: make(chan protocol.RPCResponse, 1)}
	c.pending[req.ID] = p
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, req.ID); c.mu.Unlock() }()

	payload, err := encodeRequest(req, min(c.opts.MaxFrameBytes, c.ready.MaxInputBytes))
	if err != nil {
		return zero, err
	}
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-c.done:
		return zero, c.Err()
	case <-c.writeGate:
	}
	err = c.write(ctx, payload)
	c.writeGate <- struct{}{}
	if err != nil {
		return zero, err
	}
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-c.done:
		return zero, c.Err()
	case response := <-p.response:
		return response, nil
	}
}

func (c *Client) write(ctx context.Context, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(c.opts.WriteTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		c.fail(err)
		return c.Err()
	}
	// A context callback closes the connection to interrupt a blocked Write.
	// Join a fired callback before returning; no background write is abandoned.
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { c.fail(ctx.Err()); close(canceled) })
	n, err := c.conn.Write(payload)
	if !stop() {
		<-canceled
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && n != len(payload) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = c.conn.SetWriteDeadline(time.Time{})
	}
	if err != nil {
		c.fail(err)
		return c.Err()
	}
	return nil
}
