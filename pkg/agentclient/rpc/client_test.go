package rpc

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type fixture struct {
	cancel context.CancelFunc
	client *Client
	peer   net.Conn
	reader *bufio.Reader
	ctx    context.Context
	wg     sync.WaitGroup
}

func newFixture(t *testing.T, opts Options) *fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	local, peer := net.Pipe()
	f := &fixture{peer: peer, reader: bufio.NewReader(peer), ctx: ctx, cancel: cancel}
	t.Cleanup(func() {
		cancel()
		_ = peer.Close()
		if f.client != nil {
			_ = f.client.Close()
		}
		f.wg.Wait()
	})
	if err := peer.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	f.wg.Go(func() { writeJSON(t, peer, protocol.NewRPCReady("test")) })
	c, err := New(ctx, local, opts)
	if err != nil {
		t.Fatal(err)
	}
	f.client = c
	return f
}

func writeJSON(t *testing.T, conn net.Conn, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Error(err)
		return
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		t.Error(err)
	}
}

func (f *fixture) request(t *testing.T) protocol.RPCRequest {
	t.Helper()
	line, err := f.reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var r protocol.RPCRequest
	if err := json.Unmarshal(line, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func (f *fixture) reply(t *testing.T, req protocol.RPCRequest, success bool) {
	t.Helper()
	writeJSON(t, f.peer, protocol.RPCResponse{Type: "response", ID: req.ID, Command: req.Type, Success: success})
}

type callResult struct {
	response protocol.RPCResponse
	err      error
}

func (f *fixture) call(ctx context.Context, req protocol.RPCRequest) <-chan callResult {
	result := make(chan callResult, 1)
	f.wg.Go(func() { response, err := f.client.Call(ctx, req); result <- callResult{response, err} })
	return result
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value, ok := <-ch:
		if !ok {
			t.Fatal("unexpected closed channel")
		}
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}

func awaitFailure(t *testing.T, c *Client, want error) {
	t.Helper()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("client did not terminate")
	}
	if err := c.Err(); !errors.Is(err, want) {
		t.Fatalf("Err = %v, want %v", err, want)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	for range c.Events() {
	}
}

func TestHandshakeAndInterleavedCorrelation(t *testing.T) {
	f := newFixture(t, Options{})
	ready := f.client.Ready()
	if ready.Type != protocol.RPCTypeReady || ready.ProtocolVersion != protocol.RPCProtocolVersion || ready.SnowVersion != "test" {
		t.Fatalf("ready = %+v", ready)
	}
	ready.Capabilities[0] = "mutated"
	if f.client.Ready().Capabilities[0] == "mutated" {
		t.Fatal("Ready aliases capabilities")
	}
	// Startup events and future event types must not need a pending call.
	writeJSON(t, f.peer, protocol.AgentEvent{Type: protocol.EvModeChanged})
	first := f.call(f.ctx, protocol.RPCRequest{ID: "caller-id", Type: "session_info"})
	a := f.request(t)
	second := f.call(f.ctx, protocol.RPCRequest{Type: "usage"})
	b := f.request(t)
	if a.ID == "" || a.ID == "caller-id" || a.ID == b.ID {
		t.Fatalf("IDs = %q, %q", a.ID, b.ID)
	}
	writeJSON(t, f.peer, protocol.AgentEvent{Type: "future_event", Text: "hello\u2028world"})
	f.reply(t, b, false)
	f.reply(t, a, true)
	rb, ra := receive(t, second), receive(t, first)
	if rb.err != nil || rb.response.ID != b.ID || rb.response.Success || ra.err != nil || ra.response.ID != a.ID || !ra.response.Success {
		t.Fatalf("results = %+v, %+v", ra, rb)
	}
	if e := receive(t, f.client.Events()); e.AgentEvent == nil || e.AgentEvent.Type != protocol.EvModeChanged {
		t.Fatalf("first event = %+v", e)
	}
	if e := receive(t, f.client.Events()); e.AgentEvent == nil || e.AgentEvent.Type != "future_event" || e.AgentEvent.Text != "hello\u2028world" {
		t.Fatalf("second event = %+v", e)
	}
}

func TestPromptAckIsNotCompletion(t *testing.T) {
	f := newFixture(t, Options{})
	result := f.call(f.ctx, protocol.RPCRequest{Type: "prompt", Message: "hello"})
	req := f.request(t)
	f.reply(t, req, true)
	ack := receive(t, result)
	if ack.err != nil || !ack.response.Success {
		t.Fatalf("ack = %+v", ack)
	}
	select {
	case event := <-f.client.Events():
		t.Fatalf("ack produced event: %+v", event)
	default:
	}
	writeJSON(t, f.peer, protocol.AgentEvent{Type: protocol.EvTurnDone})
	// Legacy second response does not substitute for the definitive terminal.
	f.reply(t, req, false)
	writeJSON(t, f.peer, protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: req.ID, Status: protocol.RPCPromptFailedStatus, Error: "fixture failure"})
	if event := receive(t, f.client.Events()); event.AgentEvent == nil || event.AgentEvent.Type != protocol.EvTurnDone {
		t.Fatalf("turn event = %+v", event)
	}
	if event := receive(t, f.client.Events()); event.PromptCompleted == nil || event.PromptCompleted.RequestID != ack.response.ID || event.PromptCompleted.Status != protocol.RPCPromptFailedStatus {
		t.Fatalf("terminal event = %+v", event)
	}
	if f.client.Err() != nil {
		t.Fatal(f.client.Err())
	}
}

func TestCanceledWaitDiscardsLateResponseAndKeepsCompletion(t *testing.T) {
	f := newFixture(t, Options{MaxPending: 1})
	ctx, cancel := context.WithCancel(f.ctx)
	result := f.call(ctx, protocol.RPCRequest{Type: "prompt"})
	req := f.request(t)
	// The write has finished before cancellation: Call is awaiting a response.
	<-f.client.writeGate
	f.client.writeGate <- struct{}{}
	cancel()
	if got := receive(t, result); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("result = %+v", got)
	}
	f.reply(t, req, true)
	writeJSON(t, f.peer, protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: req.ID, Status: protocol.RPCPromptCompletedStatus})
	if e := receive(t, f.client.Events()); e.PromptCompleted == nil || e.PromptCompleted.RequestID != req.ID {
		t.Fatalf("completion = %+v", e)
	}
	next := f.call(f.ctx, protocol.RPCRequest{Type: "usage"})
	nextReq := f.request(t)
	f.reply(t, nextReq, true)
	if got := receive(t, next); got.err != nil {
		t.Fatal(got.err)
	}
}

func TestPendingLimitAndClose(t *testing.T) {
	f := newFixture(t, Options{MaxPending: 1})
	result := f.call(f.ctx, protocol.RPCRequest{Type: "subagent_wait"})
	_ = f.request(t)
	if _, err := f.client.Call(f.ctx, protocol.RPCRequest{Type: "usage"}); !errors.Is(err, ErrTooManyPending) {
		t.Fatalf("pending error = %v", err)
	}
	var closes sync.WaitGroup
	for range 8 {
		closes.Go(func() { _ = f.client.Close() })
	}
	closes.Wait()
	if got := receive(t, result); !errors.Is(got.err, ErrClosed) {
		t.Fatalf("result = %+v", got)
	}
	awaitFailure(t, f.client, ErrClosed)
	if _, err := f.client.Call(f.ctx, protocol.RPCRequest{Type: "usage"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("post-close call = %v", err)
	}
}

func TestEventOverflowIsExplicitAndWakesCalls(t *testing.T) {
	f := newFixture(t, Options{EventBuffer: 1})
	pending := f.call(f.ctx, protocol.RPCRequest{Type: "usage"})
	_ = f.request(t)
	writeJSON(t, f.peer, protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "retained"})
	writeJSON(t, f.peer, protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: "1", Status: protocol.RPCPromptCompletedStatus})
	if got := receive(t, pending); !errors.Is(got.err, ErrEventOverflow) {
		t.Fatalf("pending = %+v", got)
	}
	if event := receive(t, f.client.Events()); event.AgentEvent == nil || event.AgentEvent.Text != "retained" {
		t.Fatalf("retained = %+v", event)
	}
	awaitFailure(t, f.client, ErrEventOverflow)
}

func TestCloseAndCancellationInterruptBlockedWrite(t *testing.T) {
	for _, reason := range []string{"close", "call", "lifetime", "timeout"} {
		t.Run(reason, func(t *testing.T) {
			opts := Options{}
			if reason == "timeout" {
				opts.WriteTimeout = 20 * time.Millisecond
			}
			f := newFixture(t, opts)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			result := f.call(ctx, protocol.RPCRequest{Type: "usage"})
			// Read only one byte, guaranteeing a partial frame blocked in net.Pipe.
			if _, err := io.ReadFull(f.peer, make([]byte, 1)); err != nil {
				t.Fatal(err)
			}
			switch reason {
			case "close":
				_ = f.client.Close()
			case "call":
				cancel()
			case "lifetime":
				// Exercise New's lifetime callback independently of Call's context.
				f.cancel()
			}
			if got := receive(t, result); got.err == nil {
				t.Fatal("blocked write returned success")
			}
			if reason == "call" && !errors.Is(f.client.Err(), context.Canceled) {
				t.Fatalf("Err = %v", f.client.Err())
			}
			_ = f.client.Close()
		})
	}
}

func TestLifetimeCancellationClosesIdleReader(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var wg sync.WaitGroup
	wg.Go(func() { writeJSON(t, peer, protocol.NewRPCReady("test")) })
	c, err := New(ctx, local, Options{})
	if err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	cancel()
	awaitFailure(t, c, context.Canceled)
}

func TestOutgoingBoundsAndEncoding(t *testing.T) {
	f := newFixture(t, Options{MaxFrameBytes: 2048})
	for _, req := range []protocol.RPCRequest{
		{Type: "prompt", Message: strings.Repeat("x", 2048)},
		{Type: "auth_login_start", Secret: strings.Repeat("s", 2048)},
	} {
		if _, err := f.client.Call(f.ctx, req); !errors.Is(err, ErrFrameTooLarge) {
			t.Fatalf("oversized request error = %v", err)
		}
	}
	result := f.call(f.ctx, protocol.RPCRequest{Type: "prompt", Message: "line\nquote\"\u2028"})
	req := f.request(t)
	if req.Message != "line\nquote\"\u2028" {
		t.Fatalf("message = %q", req.Message)
	}
	f.reply(t, req, true)
	if got := receive(t, result); got.err != nil {
		t.Fatal(got.err)
	}
	if f.client.Err() != nil {
		t.Fatal(f.client.Err())
	}
}

func TestCancelBeforeWriteDoesNotCloseClient(t *testing.T) {
	f := newFixture(t, Options{})
	// Hold the write gate so cancellation occurs before any bytes are sent.
	<-f.client.writeGate
	ctx, cancel := context.WithCancel(f.ctx)
	waiting := f.call(ctx, protocol.RPCRequest{Type: "usage"})
	cancel()
	if got := receive(t, waiting); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("queued call error = %v", got.err)
	}
	f.client.writeGate <- struct{}{}
	if f.client.Err() != nil {
		t.Fatal(f.client.Err())
	}
	next := f.call(f.ctx, protocol.RPCRequest{Type: "usage"})
	req := f.request(t)
	f.reply(t, req, true)
	if got := receive(t, next); got.err != nil {
		t.Fatal(got.err)
	}
}

func TestCancelPendingResponseDeadline(t *testing.T) {
	f := newFixture(t, Options{})
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Millisecond)
	defer cancel()
	result := f.call(ctx, protocol.RPCRequest{Type: "subagent_wait"})
	req := f.request(t)
	<-f.client.writeGate
	f.client.writeGate <- struct{}{}
	if got := receive(t, result); !errors.Is(got.err, context.DeadlineExceeded) {
		t.Fatalf("Call error = %v", got.err)
	}
	f.reply(t, req, true)
	if f.client.Err() != nil {
		t.Fatal(f.client.Err())
	}
}
