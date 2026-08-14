package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
)

func TestEventBusBoundsSlowSubscriberWithoutBlockingPublish(t *testing.T) {
	bus := newEventBusWithCap(2)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	bus.Subscribe(func(protocol.AgentEvent) {
		once.Do(func() {
			close(entered)
			<-release
		})
	})
	bus.Publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "first"})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not start")
	}
	bus.Publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "second"})
	bus.Publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "third"})
	published := make(chan struct{})
	go func() {
		bus.Publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "fourth"})
		bus.Publish(protocol.AgentEvent{Type: protocol.EvTurnDone})
		close(published)
	}()
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber blocked event producer")
	}
	bus.mu.Lock()
	if len(bus.items) > 2 {
		bus.mu.Unlock()
		t.Fatalf("queued items=%d, want <=2", len(bus.items))
	}
	foundDone := false
	for _, item := range bus.items {
		if event, ok := item.(protocol.AgentEvent); ok && event.Type == protocol.EvTurnDone {
			foundDone = true
		}
	}
	bus.mu.Unlock()
	if !foundDone {
		t.Fatal("new lifecycle event was not retained at capacity")
	}
	close(release)
	bus.Close()
	bus.Wait()
}

func TestEventBusDrainFromCallbackFailsFast(t *testing.T) {
	bus := newEventBusWithCap(2)
	defer func() { bus.Close(); bus.Wait() }()
	result := make(chan error, 1)
	bus.Subscribe(func(protocol.AgentEvent) {
		result <- bus.Drain(context.Background())
	})
	bus.Publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	select {
	case err := <-result:
		if !errors.Is(err, ErrReentrantDrain) {
			t.Fatalf("Drain error=%v, want ErrReentrantDrain", err)
		}
	case <-time.After(time.Second):
		t.Fatal("reentrant drain deadlocked")
	}
}
