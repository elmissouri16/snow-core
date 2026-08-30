package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type deadlineOnlyReader struct {
	mu            sync.Mutex
	deadlines     []time.Time
	readStarted   chan struct{}
	interrupted   chan struct{}
	readOnce      sync.Once
	interruptOnce sync.Once
}

func (r *deadlineOnlyReader) Read([]byte) (int, error) {
	r.readOnce.Do(func() { close(r.readStarted) })
	<-r.interrupted
	return 0, io.EOF
}

func (r *deadlineOnlyReader) SetReadDeadline(deadline time.Time) error {
	r.mu.Lock()
	r.deadlines = append(r.deadlines, deadline)
	call := len(r.deadlines)
	r.mu.Unlock()
	// Serve probes an expired deadline and clears it before scanning. Only the
	// later cancellation deadline should unblock the active read.
	if call >= 3 && !deadline.IsZero() {
		r.interruptOnce.Do(func() { close(r.interrupted) })
	}
	return nil
}

type unavailableDeadlineReader struct{}

func (*unavailableDeadlineReader) Read([]byte) (int, error) { return 0, io.EOF }
func (*unavailableDeadlineReader) SetReadDeadline(time.Time) error {
	return os.ErrNoDeadline
}

type unavailableDeadlineWriter struct{ wrote bool }

type failAfterWriter struct {
	writes int
	err    error
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 2 {
		return 0, w.err
	}
	return len(p), nil
}

func (*failAfterWriter) RPCWriteBounded() bool { return true }

func (w *unavailableDeadlineWriter) Write(p []byte) (int, error) {
	w.wrote = true
	return len(p), nil
}
func (*unavailableDeadlineWriter) SetWriteDeadline(time.Time) error { return os.ErrNoDeadline }

func TestProcessInputReaderInterruptsInheritedPipeFallback(t *testing.T) {
	base := &blockingReadCloser{closed: make(chan struct{})}
	reader := newProcessInputReader(base)
	if !reader.InterruptsReadOnClose() {
		t.Fatal("process input reader must declare close interruption")
	}
	done := make(chan error, 1)
	go func() {
		_, err := reader.Read(make([]byte, 1))
		done <- err
	}()
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not interrupt inherited input read")
	}
}

func TestProcessOutputWriterBoundsInheritedPipeFallback(t *testing.T) {
	release := make(chan struct{})
	writer := &processOutputWriter{
		out:     &blockingWriter{release: release},
		timeout: 10 * time.Millisecond,
	}
	if !writer.RPCWriteBounded() {
		t.Fatal("process output writer must declare bounded writes")
	}
	_, err := writer.Write([]byte("blocked"))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("write error = %v, want timeout", err)
	}
	close(release)

	var out bytes.Buffer
	writer = &processOutputWriter{out: &out, timeout: time.Second}
	if n, err := writer.Write([]byte("ok")); err != nil || n != 2 || out.String() != "ok" {
		t.Fatalf("write n=%d err=%v output=%q", n, err, out.String())
	}
}

func TestRPCPropagatesShortWrites(t *testing.T) {
	var in bytes.Buffer
	in.WriteString(`{"id":"r1","type":"bogus"}` + "\n")
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := New(context.Background(), a, &in, shortWriter{}).Serve(context.Background()); err != io.ErrShortWrite {
		t.Fatalf("Serve error = %v, want %v", err, io.ErrShortWrite)
	}
}

func TestRPCPromptAdmissionWriteFailurePreventsProviderWork(t *testing.T) {
	sentinel := errors.New("admission write failed")
	writer := &failAfterWriter{err: sentinel}
	provider := &rpcQueueProvider{started: make(chan struct{}), release: make(chan struct{})}
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	model := a.Agent.Model()
	model.Provider = provider.ID()
	if err := a.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader(`{"id":"p1","type":"prompt","message":"must not run"}` + "\n")
	err = New(t.Context(), a, input, writer).Serve(t.Context())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Serve error = %v, want %v", err, sentinel)
	}
	select {
	case <-provider.started:
		t.Fatal("provider started after prompt admission write failed")
	default:
	}
}

func TestRPCRejectsUnavailableOutputDeadlineBeforeWrite(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	writer := &unavailableDeadlineWriter{}
	server := New(context.Background(), a, strings.NewReader(""), writer)
	err = server.write(Response{Type: "event"})
	if err == nil || !strings.Contains(err.Error(), "deadline unavailable") || writer.wrote {
		t.Fatalf("write error=%v wrote=%v", err, writer.wrote)
	}
}

func TestRPCRejectsOutputWithoutBoundedWriteContract(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	writer := &blockingWriter{release: make(chan struct{})}
	server := New(context.Background(), a, strings.NewReader(""), writer)
	err = server.write(Response{Type: "event"})
	if err == nil || !strings.Contains(err.Error(), "bounded writes") {
		t.Fatalf("write error=%v, want bounded-output rejection", err)
	}
	server.mu.Lock()
	server.mu.Unlock()
}

func TestRPCRejectsNegativeWaitTimeoutBeforeStartingWorker(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	server := New(context.Background(), a, strings.NewReader(""), io.Discard)
	err = server.handle(context.Background(), Request{Type: "subagent_wait", Params: json.RawMessage(`{"timeout_ms":-1}`)})
	if err == nil || !strings.Contains(err.Error(), "cannot be negative") {
		t.Fatalf("negative timeout error = %v", err)
	}
}

func paddedRPCFrame(size int) []byte {
	prefix := []byte(`{"id":"limit","type":"unknown","padding":"`)
	suffix := []byte(`"}`)
	padding := size - len(prefix) - len(suffix)
	frame := make([]byte, 0, size+1)
	frame = append(frame, prefix...)
	frame = append(frame, bytes.Repeat([]byte("x"), padding)...)
	frame = append(frame, suffix...)
	return append(frame, '\n')
}

func TestRPCFrameLimitMatchesAdvertisedPayloadSize(t *testing.T) {
	for _, tc := range []struct {
		name       string
		size       int
		terminated bool
		wantErr    error
	}{
		{name: "maximum", size: protocol.RPCMaxInputBytes, terminated: true},
		{name: "over maximum", size: protocol.RPCMaxInputBytes + 1, terminated: true, wantErr: bufio.ErrTooLong},
		{name: "over maximum without newline", size: protocol.RPCMaxInputBytes + 1, wantErr: bufio.ErrTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			var out bytes.Buffer
			frame := paddedRPCFrame(tc.size)
			if !tc.terminated {
				frame = frame[:len(frame)-1]
			}
			err = New(t.Context(), a, bytes.NewReader(frame), &out).Serve(t.Context())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Serve error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && !strings.Contains(out.String(), `"id":"limit"`) {
				t.Fatalf("maximum-size frame was not processed: %q", out.String())
			}
		})
	}
}

func TestRPCRejectsMalformedUTF8BeforeDispatch(t *testing.T) {
	provider := &rpcQueueProvider{started: make(chan struct{}), release: make(chan struct{})}
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	model := a.Agent.Model()
	model.Provider = provider.ID()
	if err := a.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	input := append([]byte(`{"id":"bad","type":"prompt","message":"`), 0xff)
	input = append(input, []byte(`"}`+"\n")...)
	var out bytes.Buffer
	if err := New(t.Context(), a, bytes.NewReader(input), &out).Serve(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "input is not valid UTF-8") {
		t.Fatalf("output = %q", out.String())
	}
	select {
	case <-provider.started:
		t.Fatal("provider called for malformed UTF-8")
	default:
	}
}

func TestRPCRejectsUnavailableInputDeadlineBeforeScanning(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	err = New(context.Background(), a, &unavailableDeadlineReader{}, io.Discard).Serve(context.Background())
	if err == nil || !strings.Contains(err.Error(), "deadline unavailable") {
		t.Fatalf("Serve error=%v, want unavailable-deadline rejection", err)
	}
}

func TestRPCRejectsUninterruptiblePlainReader(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	reader := &blockingReader{block: make(chan struct{})}
	err = New(context.Background(), a, reader, io.Discard).Serve(context.Background())
	if err == nil || !strings.Contains(err.Error(), "interrupts reads") {
		t.Fatalf("Serve error=%v, want interruptible-input rejection", err)
	}
}

func TestRPCCancellationInterruptsDeadlineOnlyReader(t *testing.T) {
	reader := &deadlineOnlyReader{readStarted: make(chan struct{}), interrupted: make(chan struct{})}
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- New(context.Background(), a, reader, io.Discard).Serve(ctx) }()
	select {
	case <-reader.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not begin the deadline-only read")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not interrupt deadline-only reader")
	}
	reader.mu.Lock()
	deadlines := append([]time.Time(nil), reader.deadlines...)
	reader.mu.Unlock()
	if len(deadlines) < 3 || deadlines[0].IsZero() || !deadlines[1].IsZero() || deadlines[2].IsZero() {
		t.Fatalf("read deadlines = %v, want probe, clear, cancellation", deadlines)
	}
}

func TestRPCCancellationClosesAndJoinsScanner(t *testing.T) {
	reader := &blockingReadCloser{closed: make(chan struct{})}
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- New(context.Background(), a, reader, io.Discard).Serve(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not join its scanner after cancellation")
	}
	select {
	case <-reader.closed:
	default:
		t.Fatal("RPC input was not closed")
	}
}

func TestRPCBoundsConcurrentWaitWorkers(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	server := New(context.Background(), a, strings.NewReader(""), io.Discard)
	for range maxConcurrentWaits {
		server.waitSlots <- struct{}{}
	}
	err = server.handle(context.Background(), Request{Type: "subagent_wait", Params: json.RawMessage(`{"timeout_ms":1}`)})
	if err == nil || !strings.Contains(err.Error(), "concurrent subagent_wait limit") {
		t.Fatalf("wait saturation error = %v", err)
	}
}

func TestRPCScannerErrorUsesOrderlyShutdown(t *testing.T) {
	sentinel := errors.New("input failed")
	reader := &terminalErrorReader{data: []byte(`{"id":"p1","type":"prompt","message":"hello"}` + "\n"), err: sentinel}
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	err = New(context.Background(), a, reader, &out).Serve(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Serve error = %v", err)
	}
	if a.Agent.IsRunning() {
		t.Fatal("Serve returned while prompt worker was still running")
	}
}
