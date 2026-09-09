package javascript

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func TestRuntimeDerivesRiskAndBoundsHostCalls(t *testing.T) {
	_, reg := openRuntime(t, `snow.registerTool({
  name:"bounded", description:"bounded", parameters:{type:"object"}, risk:"read", uses:["write"],
  execute(args,ctx) {
    for(let i=0;i<65;i++)ctx.callTool("write",{});
    return {content:[]};
  }
});`, Options{})
	if reg.tools["bounded"].Risk != "write" {
		t.Fatal("author lowered the host capability risk")
	}
	calls := 0
	_, err := reg.tools["bounded"].Executor(t.Context(), plugin.ToolContext{CallTool: func(context.Context, string, json.RawMessage) (plugin.ToolResult, error) {
		calls++
		return plugin.ToolResult{}, nil
	}}, json.RawMessage(`{}`))
	if calls != MaxHostCalls || err == nil || !strings.Contains(err.Error(), "host call limit") {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestRuntimeCloseCancelsActiveHostCall(t *testing.T) {
	r, reg := openRuntime(t, `snow.registerTool({name:"wait",description:"wait",parameters:{type:"object"},uses:["read"],execute(args,ctx){return ctx.callTool("read",{})}});`, Options{})
	entered := make(chan struct{})
	completed := make(chan error, 1)
	go func() {
		_, err := reg.tools["wait"].Executor(t.Context(), plugin.ToolContext{CallTool: func(ctx context.Context, _ string, _ json.RawMessage) (plugin.ToolResult, error) {
			close(entered)
			<-ctx.Done()
			return plugin.ToolResult{}, ctx.Err()
		}}, json.RawMessage(`{}`))
		completed <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("tool did not enter host")
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("active call succeeded after close")
		}
	case <-time.After(time.Second):
		t.Fatal("active call did not unwind")
	}
	select {
	case <-r.done:
	default:
		t.Fatal("worker still running after close")
	}
}
