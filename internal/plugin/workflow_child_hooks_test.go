package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/internal/tools"
	public "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestWorkflowLoaderSkipsChildHookWhenOwnerAlsoHasRootWorkflowHook(t *testing.T) {
	manager := NewManager(tools.NewRegistry())
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	p := &javascript.Package{
		Manifest: javascript.Manifest{ID: "workflow-mix", Name: "Workflow mix", Version: "1", APIVersion: 2, Entry: "main.js", Capabilities: []string{"hooks", "workflow"}},
		Config:   json.RawMessage(`{}`),
		Script: []byte(`
   snow.registerHook("before_request",request=>{
    if(request.agent)throw new Error("root-only handler received child work");
    if(Object.keys(request.workflow).join(",")!=="profile")throw new Error("root snapshot not isolated");
    return {context:[{text:request.workflow.profile}]};
   },{workflowKeys:["profile"]});
   snow.registerHook("before_request",request=>{
    if(request.workflow!==undefined)throw new Error("shared hook received root workflow data");
    return {context:[{text:request.agent?"child-shared":"root-shared"}]};
   },{includeSubagents:true});`),
	}
	if err := manager.LoadJavaScript(javascript.New(p, javascript.Options{}), strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err := manager.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	loader := func(ctx context.Context, id string, keys []string) (map[string]json.RawMessage, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		calls++
		if id != "workflow-mix" || !slices.Equal(keys, []string{"profile"}) {
			t.Fatalf("loader input: id=%s keys=%v", id, keys)
		}
		return map[string]json.RawMessage{"profile": json.RawMessage(`"root-profile"`), "private": json.RawMessage(`"never-expose-this"`)}, nil
	}
	root, audits, err := manager.RunHooksWithWorkflow(t.Context(), public.HookRequest{Phase: "before_request"}, loader)
	if err != nil || calls != 1 {
		t.Fatalf("root calls=%d err=%v", calls, err)
	}
	if len(root.Context) != 2 || root.Context[0].Text != "root-profile" || root.Context[1].Text != "root-shared" || root.Workflow != nil {
		t.Fatalf("root result=%+v", root)
	}
	for _, audit := range audits {
		for _, raw := range []json.RawMessage{audit.Original, audit.Effective} {
			if strings.Contains(string(raw), `"workflow"`) || strings.Contains(string(raw), "never-expose-this") {
				t.Fatalf("workflow leaked into audit: %s", raw)
			}
		}
	}
	childRequest := public.HookRequest{Phase: "before_request", Agent: &protocol.AgentRef{Path: "/root/child", Role: "explorer"}, Workflow: map[string]json.RawMessage{"profile": json.RawMessage(`"injected"`)}}
	poisonLoader := func(context.Context, string, []string) (map[string]json.RawMessage, error) {
		calls++
		return nil, errors.New("root workflow loader must not run for child")
	}
	for _, load := range []WorkflowLoader{nil, poisonLoader} {
		child, _, err := manager.RunHooksWithWorkflow(t.Context(), childRequest, load)
		if err != nil || calls != 1 {
			t.Fatalf("child called root loader: calls=%d err=%v", calls, err)
		}
		if len(child.Context) != 1 || child.Context[0].Text != "child-shared" || child.Workflow != nil {
			t.Fatalf("child result=%+v", child)
		}
	}
	// Skipping the loader for a child must not disable subsequent root hydration.
	if _, _, err := manager.RunHooksWithWorkflow(t.Context(), public.HookRequest{Phase: "before_request"}, loader); err != nil || calls != 2 {
		t.Fatalf("subsequent root calls=%d err=%v", calls, err)
	}
}
