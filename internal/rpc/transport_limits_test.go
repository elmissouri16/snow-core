package rpc

import (
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

	"github.com/snow-core/snow/internal/app"
)

type deadlineOnlyReader struct {
	once        sync.Once
	interrupted chan struct{}
}

func (r *deadlineOnlyReader) Read([]byte) (int, error) {
	<-r.interrupted
	return 0, io.EOF
}

func (r *deadlineOnlyReader) SetReadDeadline(time.Time) error {
	r.once.Do(func() { close(r.interrupted) })
	return nil
}

type unavailableDeadlineReader struct{}

func (*unavailableDeadlineReader) Read([]byte) (int, error) { return 0, io.EOF }
func (*unavailableDeadlineReader) SetReadDeadline(time.Time) error {
	return os.ErrNoDeadline
}

type unavailableDeadlineWriter struct{ wrote bool }

func (w *unavailableDeadlineWriter) Write(p []byte) (int, error) {
	w.wrote = true
	return len(p), nil
}
func (*unavailableDeadlineWriter) SetWriteDeadline(time.Time) error { return os.ErrNoDeadline }

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
	reader := &deadlineOnlyReader{interrupted: make(chan struct{})}
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(context.Background(), a, reader, io.Discard).Serve(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not interrupt deadline-only reader")
	}
}

func TestRPCCancellationClosesAndJoinsScanner(t *testing.T) {
	reader := &blockingReadCloser{closed: make(chan struct{})}
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
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
	for i := 0; i < maxConcurrentWaits; i++ {
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
