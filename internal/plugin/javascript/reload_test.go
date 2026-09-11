package javascript

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestReloadFreezeRejectsQueuedAndPoppedObservation(t *testing.T) {
	r, reg := openRuntime(t, `snow.on("session_updated",()=>{});`, Options{})
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	go func() {
		done <- r.submit(t.Context(), time.Second, func() error { close(started); <-release; return nil })
	}()
	<-started
	reg.handlers[plugin.EventSessionUpdated][0](plugin.Event{Type: plugin.EventSessionUpdated, Payload: protocol.AgentEvent{Type: protocol.EvSessionUpdated}})
	if resume, err := r.FreezeForReload(); err == nil {
		resume()
		t.Fatal("queued observation and worker allowed")
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	var resume func()
	for {
		var err error
		resume, err = r.FreezeForReload()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if err := r.submit(t.Context(), time.Second, func() error { t.Error("frozen callback executed"); return nil }); err == nil {
		t.Fatal("frozen invocation admitted")
	}
	reg.handlers[plugin.EventSessionUpdated][0](plugin.Event{Type: plugin.EventSessionUpdated})
	r.mu.Lock()
	queued := len(r.queue)
	r.mu.Unlock()
	if queued != 0 {
		t.Fatal("frozen observation admitted")
	}
	r.RetireForReload()
	resume()
	if err := r.submit(t.Context(), time.Second, func() error { return nil }); err == nil {
		t.Fatal("retired runtime resumed")
	}
	reg.handlers[plugin.EventSessionUpdated][0](plugin.Event{Type: plugin.EventSessionUpdated})
	r.mu.Lock()
	queued = len(r.queue)
	r.mu.Unlock()
	if queued != 0 {
		t.Fatal("late old subscription revived retired runtime")
	}
}

func TestReloadFreezeRejectsAsyncObserverUntilAllHostWorkFinishes(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var canceled atomic.Bool
	host := asyncTestHost{call: func(ctx context.Context, _ plugin.Invocation, _ string, _ json.RawMessage) (json.RawMessage, error) {
		close(started)
		select {
		case <-release:
			return []byte(`null`), nil
		case <-ctx.Done():
			canceled.Store(true)
			return nil, ctx.Err()
		}
	}}
	p := fixture(`snow.on("session_updated",async (_,ctx)=>{await ctx.storage.set({key:"x",value:1});});`)
	p.Manifest.APIVersion = 2
	p.Manifest.Capabilities = []string{"storage"}
	r := New(p, Options{})
	reg := newRegistrar()
	if err := r.Register(t.Context(), reg); err != nil {
		t.Fatal(err)
	}
	r.BindHost(host)
	defer r.Close(context.Background())
	reg.handlers[plugin.EventSessionUpdated][0](plugin.Event{Type: plugin.EventSessionUpdated, Payload: protocol.AgentEvent{Type: protocol.EvSessionUpdated}})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("observer did not start")
	}
	if resume, err := r.FreezeForReload(); err == nil {
		resume()
		t.Fatal("async observer allowed")
	}
	if canceled.Load() {
		t.Fatal("freeze canceled host work")
	}
	once.Do(func() { close(release) })
	deadline := time.Now().Add(time.Second)
	for {
		resume, err := r.FreezeForReload()
		if err == nil {
			resume()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
}
