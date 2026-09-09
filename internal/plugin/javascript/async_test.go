package javascript

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

type asyncTestHost struct {
	call func(context.Context, plugin.Invocation, string, json.RawMessage) (json.RawMessage, error)
}

func (h asyncTestHost) Environment() plugin.Environment {
	return plugin.Environment{SessionID: "session", CWD: "/project", Kind: "root", UI: true}
}
func (h asyncTestHost) Call(ctx context.Context, inv plugin.Invocation, op string, args json.RawMessage) (json.RawMessage, error) {
	return h.call(ctx, inv, op, args)
}
func openAsyncRuntime(t *testing.T, script string, host asyncTestHost) *Runtime {
	t.Helper()
	p := fixture(script)
	p.Manifest.APIVersion = 2
	p.Manifest.Capabilities = []string{"commands", "ui", "storage", "hooks", "tools", "agent"}
	r := New(p, Options{})
	if err := r.Register(t.Context(), newRegistrar()); err != nil {
		t.Fatal(err)
	}
	r.BindHost(host)
	t.Cleanup(func() {
		if err := r.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return r
}
func TestAsyncCommandsYieldAndPreserveContext(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	r := openAsyncRuntime(t, `
let saved;
snow.registerCommand({name:"wait",description:"wait",uses:["ui"],async run(input,ctx){saved=ctx;let value=await ctx.ui.input({title:input});return ctx.sessionId+":"+value;}});
snow.registerCommand({name:"ping",description:"ping",run(){return "pong";}});
snow.registerCommand({name:"expired",description:"expired",uses:["ui"],async run(){return await saved.ui.input({});}});
`, asyncTestHost{call: func(ctx context.Context, inv plugin.Invocation, op string, args json.RawMessage) (json.RawMessage, error) {
		if inv.Name != "wait" || op != "ui.input" {
			t.Errorf("wrong invocation: %+v %s", inv, op)
		}
		close(started)
		select {
		case <-release:
			return []byte(`"answer"`), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})
	done := make(chan error, 1)
	go func() {
		result, err := r.RunCommand(t.Context(), "wait", "question")
		if err == nil && (len(result.Content) != 1 || result.Content[0].Text != "session:answer") {
			err = errors.New("wrong result")
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("command did not suspend")
	}
	result, err := r.RunCommand(t.Context(), "ping", "")
	if err != nil || result.Content[0].Text != "pong" {
		t.Fatalf("worker blocked: %+v %v", result, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunCommand(t.Context(), "expired", ""); err == nil || !strings.Contains(err.Error(), "no longer active") {
		t.Fatalf("retained authority: %v", err)
	}
}
func TestAsyncCancellationJoinsHostOperation(t *testing.T) {
	started := make(chan struct{})
	var finished atomic.Bool
	r := openAsyncRuntime(t, `snow.registerCommand({name:"wait",description:"wait",uses:["ui"],async run(_,ctx){return await ctx.ui.input({});}});`, asyncTestHost{call: func(ctx context.Context, _ plugin.Invocation, _ string, _ json.RawMessage) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		finished.Store(true)
		return nil, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := r.RunCommand(ctx, "wait", ""); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || !finished.Load() {
		t.Fatalf("cancellation did not settle host: %v", err)
	}
}
func TestHooksCannotCallHostAndCannotDisappear(t *testing.T) {
	r := openAsyncRuntime(t, `snow.registerHook("before_tool",async (_,ctx)=>{await ctx.agent.state();return {};});`, asyncTestHost{call: func(context.Context, plugin.Invocation, string, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("hook reached host")
		return nil, nil
	}})
	for range 2 {
		if !r.HasHook("before_tool", false) {
			t.Fatal("failed hook disappeared")
		}
		if _, err := r.RunHook(t.Context(), plugin.HookRequest{Phase: "before_tool", Tool: "write"}); err == nil || !strings.Contains(err.Error(), "cannot perform host operations") {
			t.Fatalf("hook failure=%v", err)
		}
	}
}

func TestPromiseSettlementDoesNotDisableRuntime(t *testing.T) {
	r := openAsyncRuntime(t, `snow.registerCommand({name:"fast",description:"fast",uses:["ui"],async run(_,ctx){await ctx.ui.notify({text:"ok"});return "done"}});`, asyncTestHost{call: func(context.Context, plugin.Invocation, string, json.RawMessage) (json.RawMessage, error) {
		return []byte("null"), nil
	}})
	for range 200 {
		result, err := r.RunCommand(t.Context(), "fast", "")
		if err != nil || result.Content[0].Text != "done" {
			t.Fatalf("settlement: %+v %v", result, err)
		}
	}
}

func BenchmarkAsyncCommand(b *testing.B) {
	p := fixture(`snow.registerCommand({name:"ping",description:"ping",run(){return "pong"}});`)
	p.Manifest.APIVersion = 2
	p.Manifest.Capabilities = []string{"commands"}
	r := New(p, Options{})
	if err := r.Register(b.Context(), newRegistrar()); err != nil {
		b.Fatal(err)
	}
	defer r.Close(context.Background())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.RunCommand(b.Context(), "ping", ""); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAsyncHostPanicBecomesCommandError(t *testing.T) {
	r := openAsyncRuntime(t, `snow.registerCommand({name:"panic",description:"panic",uses:["ui"],async run(_,ctx){await ctx.ui.notify({text:"test"})}});`, asyncTestHost{call: func(context.Context, plugin.Invocation, string, json.RawMessage) (json.RawMessage, error) {
		panic("private panic details")
	}})
	_, err := r.RunCommand(t.Context(), "panic", "")
	if err == nil || !strings.Contains(err.Error(), "host callback panicked") || strings.Contains(err.Error(), "private panic") {
		t.Fatalf("panic isolation: %v", err)
	}
}
