package rpc

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRejectHandshake(t *testing.T) {
	for _, tt := range []struct {
		name, frame string
		want        error
	}{
		{"wrong version", `{"type":"rpc_ready","protocol_version":"2","max_input_bytes":1}`, ErrProtocolVersion},
		{"missing version", `{"type":"rpc_ready","max_input_bytes":1}`, ErrProtocolVersion},
		{"no limit", `{"type":"rpc_ready","protocol_version":"1"}`, ErrMalformedFrame},
		{"negative limit", `{"type":"rpc_ready","protocol_version":"1","max_input_bytes":-1}`, ErrMalformedFrame},
		{"event first", `{"type":"text_delta"}`, ErrMalformedFrame},
		{"malformed", `{"type":`, ErrMalformedFrame},
		{"oversized", strings.Repeat("x", 1025), ErrFrameTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			local, peer := net.Pipe()
			defer peer.Close()
			sent := make(chan struct{})
			go func() { defer close(sent); _, _ = peer.Write([]byte(tt.frame + "\n")) }()
			c, err := New(t.Context(), local, Options{MaxFrameBytes: 1024})
			if c != nil {
				_ = c.Close()
				t.Fatal("invalid handshake accepted")
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("New error = %v, want %v", err, tt.want)
			}
			<-sent
		})
	}
}

func TestHandshakeDeadlineAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancel"}[canceled], func(t *testing.T) {
			local, peer := net.Pipe()
			defer peer.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if canceled {
				cancel()
			}
			c, err := New(ctx, local, Options{HandshakeTimeout: 15 * time.Millisecond})
			if c != nil {
				_ = c.Close()
				t.Fatal("stalled handshake accepted")
			}
			if err == nil {
				t.Fatal("no handshake error")
			}
			if canceled && !errors.Is(err, context.Canceled) {
				t.Fatalf("New error = %v", err)
			}
			if !canceled {
				if e, ok := errors.AsType[net.Error](err); !ok || !e.Timeout() {
					t.Fatalf("New error = %v, want timeout", err)
				}
			}
			if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
				t.Fatalf("connection not closed: %v", err)
			}
		})
	}
}

func TestMalformedFramesTerminateReader(t *testing.T) {
	for _, tt := range []struct{ name, frame string }{
		{"syntax", `{"type":`},
		{"array", `[{"type":"text_delta"}]`},
		{"null", `null`},
		{"empty object", `{}`},
		{"whitespace", `  `},
		{"multiple values", `{"type":"text_delta"} {}`},
		{"duplicate key", `{"type":"text_delta","type":"usage"}`},
		{"invalid utf8", "{\"type\":\"text_delta\",\"text\":\"\xff\"}"},
		{"repeated ready", `{"type":"rpc_ready","protocol_version":"1","max_input_bytes":1}`},
		{"response without id", `{"type":"response","command":"usage","success":true}`},
		{"response without command", `{"type":"response","id":"1","success":true}`},
		{"response missing success", `{"type":"response","id":"1","command":"usage"}`},
		{"response null success", `{"type":"response","id":"1","command":"usage","success":null}`},
		{"response invalid success", `{"type":"response","id":"1","command":"usage","success":"yes"}`},
		{"terminal no ID", `{"type":"prompt_completed","status":"completed"}`},
		{"terminal bad status", `{"type":"prompt_completed","request_id":"1","status":"unknown"}`},
		{"typed event invalid", `{"type":"text_delta","text":{}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Options{})
			_, _ = f.peer.Write([]byte(tt.frame + "\n"))
			awaitFailure(t, f.client, ErrMalformedFrame)
		})
	}
}

func TestFrameBoundAndLFFraming(t *testing.T) {
	const limit = 2048
	f := newFixture(t, Options{MaxFrameBytes: limit})
	// Empty lines are ignored; CRLF is accepted JSON whitespace, not a separate
	// boundary. U+2028 within a JSON string is not a frame separator.
	_, _ = f.peer.Write([]byte("\n\n{\"type\":\"text_delta\",\"text\":\"a\u2028b\"}\r\n"))
	if e := receive(t, f.client.Events()); e.AgentEvent == nil || e.AgentEvent.Text != "a\u2028b" {
		t.Fatalf("event = %+v", e)
	}
	prefix, suffix := `{"type":"text_delta","text":"`, `"}`
	exact := prefix + strings.Repeat("x", limit-len(prefix)-len(suffix)) + suffix
	if len(exact) != limit {
		t.Fatal("bad test frame")
	}
	_, _ = f.peer.Write([]byte(exact + "\n"))
	if e := receive(t, f.client.Events()); e.AgentEvent == nil || len(e.AgentEvent.Text) != limit-len(prefix)-len(suffix) {
		t.Fatalf("exact-limit event = %+v", e)
	}
	_, _ = f.peer.Write([]byte(exact + " \n"))
	awaitFailure(t, f.client, ErrFrameTooLarge)
}

func TestEOFTerminatesAndRejectsUnterminatedFrame(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "eof", true: "partial"}[partial], func(t *testing.T) {
			f := newFixture(t, Options{})
			if partial {
				_, _ = f.peer.Write([]byte(`{"type":"text_delta"}`))
			}
			_ = f.peer.Close()
			want := io.EOF
			if partial {
				want = ErrMalformedFrame
			}
			awaitFailure(t, f.client, want)
		})
	}
}

func TestResponseCommandMismatch(t *testing.T) {
	f := newFixture(t, Options{})
	result := f.call(f.ctx, protocol.RPCRequest{Type: "usage"})
	req := f.request(t)
	req.Type = "session_info"
	f.reply(t, req, true)
	if got := receive(t, result); !errors.Is(got.err, ErrMalformedFrame) {
		t.Fatalf("call error = %v", got.err)
	}
	awaitFailure(t, f.client, ErrMalformedFrame)
}

func TestServerAdvertisedInputLimit(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	ready := protocol.NewRPCReady("test")
	ready.MaxInputBytes = 40
	ready.Capabilities = append(ready.Capabilities, "future_capability")
	sent := make(chan struct{})
	go func() { defer close(sent); writeJSON(t, peer, ready) }()
	c, err := New(t.Context(), local, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	<-sent
	if _, err := c.Call(t.Context(), protocol.RPCRequest{Type: "prompt", Message: strings.Repeat("x", 40)}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("Call = %v", err)
	}
	if c.Err() != nil {
		t.Fatal(c.Err())
	}
}

func TestRequestEncodedBoundaryAndRawParams(t *testing.T) {
	req := protocol.RPCRequest{Type: "usage", ID: "1", Params: []byte(`{"nested":[true,1,"x"]}`)}
	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := encodeRequest(req, len(encoded))
	if err != nil || string(payload) != string(encoded)+"\n" {
		t.Fatalf("exact encoded bound: %q %v", payload, err)
	}
	if _, err := encodeRequest(req, len(encoded)-1); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("over bound: %v", err)
	}
	req.Params = []byte(`{"secret":`)
	if _, err := encodeRequest(req, 2048); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("invalid raw JSON error: %v", err)
	}
}

type brokenConn struct {
	net.Conn
	readDeadline, writeDeadline, short bool
}

func (c brokenConn) SetReadDeadline(d time.Time) error {
	if c.readDeadline {
		return errors.New("read deadlines unavailable")
	}
	return c.Conn.SetReadDeadline(d)
}
func (c brokenConn) SetWriteDeadline(d time.Time) error {
	if c.writeDeadline {
		return errors.New("write deadlines unavailable")
	}
	return c.Conn.SetWriteDeadline(d)
}
func (c brokenConn) Write(p []byte) (int, error) {
	if c.short {
		return len(p) - 1, nil
	}
	return c.Conn.Write(p)
}

func TestUnavailableDeadlineRejectedAndTransportClosed(t *testing.T) {
	for _, read := range []bool{false, true} {
		local, peer := net.Pipe()
		conn := brokenConn{Conn: local, readDeadline: read, writeDeadline: !read}
		c, err := New(t.Context(), conn, Options{})
		if c != nil || err == nil {
			t.Fatalf("New = %v, %v", c, err)
		}
		if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
			t.Fatalf("peer not closed: %v", err)
		}
		_ = peer.Close()
	}
}

func TestShortWriteClosesConnection(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	sent := make(chan struct{})
	go func() { defer close(sent); writeJSON(t, peer, protocol.NewRPCReady("test")) }()
	c, err := New(t.Context(), brokenConn{Conn: local, short: true}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	<-sent
	if _, err := c.Call(t.Context(), protocol.RPCRequest{Type: "usage"}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Call error = %v", err)
	}
	awaitFailure(t, c, io.ErrShortWrite)
}

func TestInvalidOptionsDoNotTakeOwnership(t *testing.T) {
	for _, opts := range []Options{{MaxFrameBytes: -1}, {MaxFrameBytes: 64*1024*1024 + 1}, {EventBuffer: -1}, {EventBuffer: 4097}, {MaxPending: -1}, {MaxPending: 4097}, {HandshakeTimeout: -1}, {WriteTimeout: -1}} {
		local, peer := net.Pipe()
		c, err := New(t.Context(), local, opts)
		if c != nil || err == nil {
			t.Fatalf("options %+v accepted", opts)
		}
		if err := local.SetDeadline(time.Now()); err != nil {
			t.Fatalf("invalid options took connection: %v", err)
		}
		_ = local.Close()
		_ = peer.Close()
	}
	if _, err := New(t.Context(), nil, Options{}); err == nil {
		t.Fatal("nil connection accepted")
	}
}

func TestCancelInProgressHandshake(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		c, err := New(ctx, local, Options{})
		if c != nil {
			_ = c.Close()
		}
		result <- err
	}()
	// Make progress on an incomplete frame so New is known to be reading.
	if _, err := peer.Write([]byte(`{"type":`)); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := receive(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("New error = %v", err)
	}
}
