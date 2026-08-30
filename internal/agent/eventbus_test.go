package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
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

func TestEventBusRetainsEarlierProtectedEventsAtCapacity(t *testing.T) {
	bus := newEventBusWithCap(2)
	entered := make(chan struct{})
	release := make(chan struct{})
	bus.Subscribe(func(protocol.AgentEvent) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
	})
	bus.Publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	<-entered
	bus.Publish(protocol.AgentEvent{Type: protocol.EvTurnDone})
	bus.Publish(protocol.AgentEvent{Type: protocol.EvModelChanged})
	bus.Publish(protocol.AgentEvent{Type: protocol.EvToolStart})
	bus.Publish(protocol.AgentEvent{Type: protocol.EvSubagentStatus})

	bus.mu.Lock()
	got := make([]protocol.AgentEventType, 0, len(bus.items))
	for _, item := range bus.items {
		if event, ok := item.(protocol.AgentEvent); ok {
			got = append(got, event.Type)
		}
	}
	bus.mu.Unlock()
	if len(got) != 2 || got[0] != protocol.EvTurnDone || got[1] != protocol.EvToolStart {
		t.Fatalf("protected queue=%v, want [turn_done tool_start]", got)
	}
	close(release)
	bus.Close()
	bus.Wait()
}

func TestEventBusBackpressuresInsteadOfDroppingProtectedEvent(t *testing.T) {
	bus := newEventBusWithCap(2)
	entered := make(chan struct{})
	release := make(chan struct{})
	bus.Subscribe(func(protocol.AgentEvent) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
	})
	bus.Publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	<-entered
	bus.Publish(protocol.AgentEvent{Type: protocol.EvToolStart})
	bus.Publish(protocol.AgentEvent{Type: protocol.EvToolEnd})
	published := make(chan struct{})
	go func() {
		bus.Publish(protocol.AgentEvent{Type: protocol.EvTurnDone})
		close(published)
	}()
	select {
	case <-published:
		t.Fatal("protected event was dropped instead of backpressured")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("protected publisher did not resume when queue space opened")
	}
	bus.Close()
	bus.Wait()
}

func TestEventBusEvictsPermanentlyBlockedSubscriber(t *testing.T) {
	bus := newEventBusWithCap(4)
	blocked := make(chan struct{})
	bus.Subscribe(func(protocol.AgentEvent) { <-blocked })
	delivered := make(chan struct{}, 1)
	bus.Subscribe(func(protocol.AgentEvent) { delivered <- struct{}{} })
	bus.Publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	select {
	case <-delivered:
	case <-time.After(2 * eventSubscriberTimeout):
		t.Fatal("blocked subscriber stranded later delivery")
	}
	bus.Close()
	bus.Wait()
}

func TestEventBusSubscriberWorkerSurvivesPanicAndPreservesOrder(t *testing.T) {
	bus := newEventBusWithCap(8)
	defer func() { bus.Close(); bus.Wait() }()
	var mu sync.Mutex
	var got []string
	bus.Subscribe(func(event protocol.AgentEvent) {
		if event.Text == "panic" {
			panic("expected test panic")
		}
		mu.Lock()
		got = append(got, event.Text)
		mu.Unlock()
	})
	for _, text := range []string{"one", "panic", "two", "three"} {
		bus.Publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: text})
	}
	if err := bus.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 || got[0] != "one" || got[1] != "two" || got[2] != "three" {
		t.Fatalf("delivery order=%v", got)
	}
}

func TestEventBusUnsubscribeInsideCallbackDoesNotDelayDrain(t *testing.T) {
	bus := newEventBusWithCap(4)
	defer func() { bus.Close(); bus.Wait() }()
	var unsubscribe func()
	called := make(chan struct{})
	unsubscribe = bus.Subscribe(func(protocol.AgentEvent) {
		unsubscribe()
		close(called)
	})
	bus.Publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("subscriber was not called")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if err := bus.Drain(ctx); err != nil {
		t.Fatalf("drain after callback unsubscribe: %v", err)
	}
}

func BenchmarkEventBusDispatch256(b *testing.B) {
	bus := newEventBusWithCap(eventBusMaxItems)
	bus.Subscribe(func(protocol.AgentEvent) {})
	defer func() { bus.Close(); bus.Wait() }()
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		for range 256 {
			bus.Publish(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "delta"})
		}
		if err := bus.Drain(ctx); err != nil {
			b.Fatal(err)
		}
	}
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
