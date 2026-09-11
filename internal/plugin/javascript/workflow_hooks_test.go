package javascript

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func workflowHookRuntime(t *testing.T, script string) *Runtime {
	t.Helper()
	p := fixture(script)
	p.Manifest.APIVersion = 2
	p.Manifest.Capabilities = []string{"hooks", "workflow"}
	r := New(p, Options{})
	t.Cleanup(func() {
		if err := r.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return r
}

func workflowHookKeysJSON(t *testing.T, start, count int) string {
	t.Helper()
	keys := make([]string, count)
	for i := range count {
		keys[i] = fmt.Sprintf("key-%02d", start+i)
	}
	raw, err := jsonv2.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestWorkflowHookRegistrationBoundsUnionPerPhase(t *testing.T) {
	for _, test := range []struct {
		name        string
		secondStart int
		secondPhase string
		wantError   bool
		wantUnion   int
	}{
		{name: "disjoint 33 plus 33 rejected", secondStart: 33, secondPhase: "before_request", wantError: true},
		{name: "overlap 33 plus 33 permits union 64", secondStart: 31, secondPhase: "before_request", wantUnion: 64},
		{name: "identical 33 plus 33 deduplicated", secondStart: 0, secondPhase: "before_request", wantUnion: 33},
		{name: "different phases have independent budgets", secondStart: 33, secondPhase: "before_prompt", wantUnion: 33},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := `snow.registerHook("before_request",()=>({}),{workflowKeys:` + workflowHookKeysJSON(t, 0, 33) + `});` +
				`snow.registerHook("` + test.secondPhase + `",()=>({}),{workflowKeys:` + workflowHookKeysJSON(t, test.secondStart, 33) + `});`
			r := workflowHookRuntime(t, script)
			err := r.Register(t.Context(), newRegistrar())
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "workflow hook key limit exceeded") {
					t.Fatalf("registration error=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			keys := r.HookWorkflowKeys("before_request")
			if len(keys) != test.wantUnion || !slices.IsSorted(keys) {
				t.Fatalf("union=%v, want %d sorted keys", keys, test.wantUnion)
			}
			// Public snapshots of requested keys must not expose registration backing data.
			keys[0] = "changed"
			if slices.Contains(r.HookWorkflowKeys("before_request"), "changed") {
				t.Fatal("key union shares registration memory")
			}
		})
	}
}

func TestWorkflowHooksIsolateHandlerSnapshotsAndFillMissingNull(t *testing.T) {
	r := workflowHookRuntime(t, `
 snow.registerHook("before_request",request=>{
  if(Object.keys(request.workflow).sort().join(",")!=="missing,shared")throw new Error("first handler received unrelated keys");
  if(!Object.prototype.hasOwnProperty.call(request.workflow,"missing") || request.workflow.missing!==null)throw new Error("missing key is not explicit null");
  if(request.workflow.shared.value!==1)throw new Error("first handler snapshot was mutated");
  request.workflow.shared.value=999;
  request.workflow.injected=true;
  return {context:[{text:"first"}]};
 },{workflowKeys:["shared","missing"]});
 snow.registerHook("before_request",request=>{
  if(Object.keys(request.workflow).sort().join(",")!=="second,shared")throw new Error("second handler received unrelated keys");
  if(request.workflow.shared.value!==1 || request.workflow.second!=="two")throw new Error("second handler received mutated values");
  return {context:[{text:"second"}]};
 },{workflowKeys:["shared","second"]});
 snow.registerHook("before_request",request=>{
  if(request.workflow!==undefined)throw new Error("handler without keys received workflow");
  return {context:[{text:"third"}]};
 });`)
	if err := r.Register(t.Context(), newRegistrar()); err != nil {
		t.Fatal(err)
	}
	request := plugin.HookRequest{Phase: "before_request", Workflow: map[string]json.RawMessage{
		"shared": json.RawMessage(`{"value":1}`), "second": json.RawMessage(`"two"`), "hidden": json.RawMessage(`"not requested"`),
	}}
	for range 2 {
		result, err := r.RunHook(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Context) != 3 || result.Context[0].Text != "first" || result.Context[1].Text != "second" || result.Context[2].Text != "third" {
			t.Fatalf("result=%+v", result)
		}
		if string(request.Workflow["shared"]) != `{"value":1}` || len(request.Workflow) != 3 {
			t.Fatalf("caller snapshot changed: %+v", request.Workflow)
		}
	}
}

func TestWorkflowKeysRejectChildHookOptIn(t *testing.T) {
	for _, phase := range []string{"before_prompt", "before_request", "before_tool", "after_tool"} {
		t.Run(phase, func(t *testing.T) {
			r := workflowHookRuntime(t, `snow.registerHook("`+phase+`",()=>({}),{workflowKeys:["profile"],includeSubagents:true});`)
			if err := r.Register(t.Context(), newRegistrar()); err == nil || !strings.Contains(err.Error(), "root-only") {
				t.Fatalf("child workflow hook accepted: %v", err)
			}
		})
	}
}
