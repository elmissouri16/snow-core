package javascript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type registrar struct {
	tools    map[string]plugin.ToolDefinition
	handlers map[plugin.EventType][]plugin.EventHandler
}

func newRegistrar() *registrar {
	return &registrar{tools: map[string]plugin.ToolDefinition{}, handlers: map[plugin.EventType][]plugin.EventHandler{}}
}
func (s *registrar) RegisterTool(def plugin.ToolDefinition) error {
	if err := plugin.ValidateIdentifier("tool name", def.Name); err != nil {
		return err
	}
	if _, exists := s.tools[def.Name]; exists {
		return errors.New("duplicate tool")
	}
	s.tools[def.Name] = def
	return nil
}
func (s *registrar) Subscribe(t plugin.EventType, fn plugin.EventHandler) func() {
	s.handlers[t] = append(s.handlers[t], fn)
	return func() {}
}
func fixture(script string) *Package {
	return &Package{Manifest: Manifest{ID: "test", Name: "Test", Version: "1", APIVersion: 1, Entry: "main.js", HostTools: []string{"read", "write", "bash"}}, Script: []byte(script), Config: json.RawMessage(`{}`)}
}
func openRuntime(t testing.TB, script string, opts Options) (*Runtime, *registrar) {
	t.Helper()
	r := New(fixture(script), opts)
	reg := newRegistrar()
	if err := r.Register(t.Context(), reg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return r, reg
}
func callTool(t testing.TB, reg *registrar, name string, args string) (plugin.ToolResult, error) {
	t.Helper()
	return reg.tools[name].Executor(t.Context(), plugin.ToolContext{}, json.RawMessage(args))
}

func TestRuntimeToolsAreSerialAndContextExpires(t *testing.T) {
	_, reg := openRuntime(t, `
 let counter=0, saved;
 snow.registerTool({name:"count",description:"count",parameters:{type:"object"},execute(args,ctx){counter++;saved=ctx;return {content:[{type:"text",text:String(counter)}]}}});
 snow.registerTool({name:"expired",description:"expired",parameters:{type:"object"},execute(){saved.progress("bad");return {content:[]}}});
 `, Options{})
	var wg sync.WaitGroup
	results := make(chan string, 20)
	for range 20 {
		wg.Go(func() {
			res, err := callTool(t, reg, "count", `{}`)
			if err != nil {
				t.Error(err)
				return
			}
			results <- res.Content[0].Text
		})
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for value := range results {
		if seen[value] {
			t.Fatalf("duplicate counter %s", value)
		}
		seen[value] = true
	}
	if len(seen) != 20 {
		t.Fatalf("counter results=%d", len(seen))
	}
	if _, err := callTool(t, reg, "expired", `{}`); err == nil || !strings.Contains(err.Error(), "no longer active") {
		t.Fatalf("expired context error=%v", err)
	}
}

func TestRuntimeHostCapabilitiesAndMetadata(t *testing.T) {
	_, reg := openRuntime(t, `snow.registerTool({name:"read",description:"read",parameters:{type:"object"},uses:["read"],execute(args,ctx){return ctx.callTool(args.tool,{path:"hello"})}});`, Options{})
	if reg.tools["read"].Risk != "read" {
		t.Fatal("wrong risk")
	}
	calls := 0
	tc := plugin.ToolContext{CallTool: func(ctx context.Context, name string, args json.RawMessage) (plugin.ToolResult, error) {
		calls++
		return plugin.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock("hello")}, Details: map[string]string{"secret": "must not escape"}}, nil
	}}
	result, err := reg.tools["read"].Executor(t.Context(), tc, json.RawMessage(`{"tool":"read"}`))
	if err != nil || result.Content[0].Text != "hello" || result.Details != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	_, err = reg.tools["read"].Executor(t.Context(), tc, json.RawMessage(`{"tool":"write"}`))
	if err == nil || !strings.Contains(err.Error(), "not declared") || calls != 1 {
		t.Fatalf("undeclared host call: calls=%d err=%v", calls, err)
	}
}

func TestRuntimeRejectsInvalidResults(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"promise", `return Promise.resolve({content:[]})`, "Promise"},
		{"thenable", `return {then: function(){},content:[]}`, "thenable"},
		{"cycle", `let x={};x.x=x;return x`, "exception"},
		{"image", `return {content:[{type:"image",text:"x"}]}`, "text blocks only"},
		{"undefined", `return undefined`, "not JSON serializable"},
		{"large", `return {content:[{type:"text",text:"x".repeat(2000)}]}`, "exceeds"},
		{"throw_object", `throw {toString(){while(true){}}}`, "exception"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, reg := openRuntime(t, `snow.registerTool({name:"test",description:"test",parameters:{type:"object"},execute(){`+tc.body+`}})`, Options{MaxOutputBytes: 1024})
			_, err := callTool(t, reg, "test", `{}`)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %s", err, tc.want)
			}
		})
	}
}

func TestRuntimeCancellationDisablesAndUnwindsHost(t *testing.T) {
	for _, body := range []string{`while(true){}`, `ctx.callTool("read",{})`} {
		t.Run(body, func(t *testing.T) {
			_, reg := openRuntime(t, `snow.registerTool({name:"test",description:"test",parameters:{type:"object"},uses:["read"],execute(args,ctx){`+body+`;return {content:[]}}})`, Options{})
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			stopped := make(chan struct{})
			tc := plugin.ToolContext{CallTool: func(ctx context.Context, _ string, _ json.RawMessage) (plugin.ToolResult, error) {
				<-ctx.Done()
				close(stopped)
				return plugin.ToolResult{}, ctx.Err()
			}}
			_, err := reg.tools["test"].Executor(ctx, tc, json.RawMessage(`{}`))
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(body, "callTool") {
				select {
				case <-stopped:
				default:
					t.Fatal("returned before host stopped")
				}
			}
			if _, err = callTool(t, reg, "test", `{}`); err == nil || !strings.Contains(err.Error(), "disabled") {
				t.Fatalf("disabled error=%v", err)
			}
		})
	}
}

func TestRuntimeObserversDoNotBlockAndKeepOrder(t *testing.T) {
	logs := make(chan string, 10)
	r, reg := openRuntime(t, `snow.on("turn_done",()=>snow.log("info","one"));snow.on("turn_done",()=>snow.log("info","two"));`, Options{Diagnostic: func(_, message string) { logs <- message }})
	event := plugin.Event{Type: plugin.EventTurnDone, Payload: protocol.AgentEvent{Type: protocol.EvTurnDone}}
	for _, fn := range reg.handlers[plugin.EventTurnDone] {
		fn(event)
	}
	for _, want := range []string{"one", "two"} {
		select {
		case got := <-logs:
			if got != want {
				t.Fatalf("got %s want %s", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("observer did not run")
		}
	}

	// Hold the worker in an admitted job while filling its real observation queue.
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- r.submit(t.Context(), time.Second, func() error { close(started); <-release; return nil })
	}()
	<-started
	for range MaxQueueEvents + 1 {
		reg.handlers[plugin.EventTurnDone][0](event)
	}
	r.mu.Lock()
	off := r.observationsOff
	r.mu.Unlock()
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if !off {
		t.Fatal("overflow did not disable observers")
	}

}

func TestRuntimeRegistrationValidationAndInitTimeout(t *testing.T) {
	for _, script := range []string{`snow.on("made_up",()=>{})`, `snow.registerTool({name:"bad",description:"bad",parameters:{},uses:["webfetch"],execute(){}})`, `snow.onClose(()=>{});snow.onClose(()=>{})`, `while(true){}`} {
		r := New(fixture(script), Options{})
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		if err := r.Register(ctx, newRegistrar()); err == nil {
			t.Fatal("invalid registration succeeded")
		}
		cancel()
		if err := r.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeSourceMapsAndGlobalsHaveNoIO(t *testing.T) {
	_, reg := openRuntime(t, `snow.registerTool({name:"test",description:"test",parameters:{},execute(){return {content:[{type:"text",text:[typeof require,typeof process,typeof fetch,typeof console,typeof setTimeout].join(",")}]}}});
 //# sourceMappingURL=/this/file/must/not/be/read
 `, Options{})
	result, err := callTool(t, reg, "test", `{}`)
	if err != nil || result.Content[0].Text != "undefined,undefined,undefined,undefined,undefined" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func BenchmarkRuntimeStartup(b *testing.B) {
	for b.Loop() {
		r := New(fixture(`snow.on("turn_done",()=>{})`), Options{})
		if err := r.Register(b.Context(), newRegistrar()); err != nil {
			b.Fatal(err)
		}
		if err := r.Close(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkRuntimeTool(b *testing.B) {
	for _, host := range []bool{false, true} {
		b.Run(fmt.Sprint(host), func(b *testing.B) {
			body := `return {content:[{type:"text",text:"ok"}]}`
			uses := "[]"
			if host {
				body = `return ctx.callTool("read",{})`
				uses = `["read"]`
			}
			_, reg := openRuntime(b, `snow.registerTool({name:"test",description:"test",parameters:{},uses:`+uses+`,execute(args,ctx){`+body+`}})`, Options{})
			tc := plugin.ToolContext{CallTool: func(context.Context, string, json.RawMessage) (plugin.ToolResult, error) {
				return plugin.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock("ok")}}, nil
			}}
			for b.Loop() {
				if _, err := reg.tools["test"].Executor(b.Context(), tc, json.RawMessage(`{}`)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestRuntimeLoadedPackageAndTopLevelLog(t *testing.T) {
	dir := packageDir(t)
	if err := os.WriteFile(filepath.Join(dir, "main.js"), []byte(`snow.log("info","loaded");snow.registerTool({name:"echo",description:"echo",parameters:{type:"object"},execute(args){return {content:[{type:"text",text:"ok"}]}}});`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := ReadPackage(t.Context(), dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	r := New(p, Options{Diagnostic: func(_, message string) { t.Log(message) }})
	reg := newRegistrar()
	if err := r.Register(t.Context(), reg); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkRuntimeObservation(b *testing.B) {
	r, reg := openRuntime(b, `snow.on("turn_done",()=>ack())`, Options{})
	ack := make(chan struct{}, 1)
	if err := r.submit(b.Context(), time.Second, func() error { return r.vm.Set("ack", func() { ack <- struct{}{} }) }); err != nil {
		b.Fatal(err)
	}
	event := plugin.Event{Type: plugin.EventTurnDone, Payload: protocol.AgentEvent{Type: protocol.EvTurnDone}}
	for b.Loop() {
		reg.handlers[plugin.EventTurnDone][0](event)
		<-ack
	}
}
