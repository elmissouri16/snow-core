package rpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type contendedEventWriter struct {
	mu              sync.Mutex
	output          bytes.Buffer
	responseStarted chan struct{}
	eventFinished   chan struct{}
}

func (w *contendedEventWriter) Write(p []byte) (int, error) {
	response := bytes.Contains(p, []byte(`"id":"contention"`))
	event := bytes.Contains(p, []byte(`"text":"first"`))
	if response {
		close(w.responseStarted)
	}
	if response || event {
		// Each write fits the real process writer's one-second timeout, while
		// waiting behind the response plus writing exceeds the subscriber limit.
		time.Sleep(650 * time.Millisecond)
	}
	w.mu.Lock()
	n, err := w.output.Write(p)
	w.mu.Unlock()
	if event {
		close(w.eventFinished)
	}
	return n, err
}

func TestRPCEventEvictionTerminatesTransportAndCancelsPrompt(t *testing.T) {
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	provider := &rpcQueueProvider{started: make(chan struct{}), release: make(chan struct{})}
	model := a.Agent.Model()
	model.Provider = provider.ID()
	if err := a.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	input, commands := io.Pipe()
	defer commands.Close()
	out := &contendedEventWriter{responseStarted: make(chan struct{}), eventFinished: make(chan struct{})}
	s := New(t.Context(), a, input, newProcessOutputWriter(out))
	stopEvents := s.forwardAgentEvents()
	defer stopEvents()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx) }()
	if _, err := io.WriteString(commands, "{\"id\":\"p1\",\"type\":\"prompt\",\"message\":\"wait\"}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := a.Agent.DrainEvents(ctx); err != nil {
		t.Fatal(err)
	}
	responseDone := make(chan error, 1)
	go func() { responseDone <- s.write(Response{ID: "contention", Type: "response", Success: true}) }()
	<-out.responseStarted
	a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "first"})
	select {
	case err := <-served:
		if !errors.Is(err, agent.ErrEventSubscriberEvicted) {
			t.Fatalf("Serve error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("evicted output left RPC or the prompt blocked")
	}
	if err := <-responseDone; err != nil {
		t.Fatal(err)
	}
	<-out.eventFinished
	if err := stopEvents(); !errors.Is(err, agent.ErrEventSubscriberEvicted) {
		t.Fatalf("cleanup error = %v", err)
	}
	if a.Agent.IsRunning() {
		t.Fatal("provider remained active after output failed")
	}
	messages, err := a.Agent.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 || messages[len(messages)-1].StopReason != protocol.StopAborted {
		t.Fatalf("prompt was not durably aborted: %+v", messages)
	}
}

func TestRPCEventForwardingFlushesBeforeCleanup(t *testing.T) {
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var output bytes.Buffer
	s := New(t.Context(), a, strings.NewReader(""), &output)
	stop := s.forwardAgentEvents()
	defer stop()
	a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "complete"})
	a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvTurnDone})
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "complete") || !strings.Contains(output.String(), "turn_done") {
		t.Fatalf("cleanup lost events: %s", output.String())
	}
}
