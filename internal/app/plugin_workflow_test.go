package app

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func workflowTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	script := `
 snow.registerCommand({name:"update",description:"update",uses:["workflow","tool_policy"],async run(input,ctx){await ctx.workflow.update(JSON.parse(input));return "updated"}});
 snow.registerCommand({name:"clear",description:"clear",uses:["tool_policy"],async run(_,ctx){await ctx.tools.clearRestriction();return "cleared"}});
 snow.registerCommand({name:"get",description:"get",uses:["workflow"],async run(key,ctx){return JSON.stringify(await ctx.workflow.get({key}))}});
 snow.registerCommand({name:"fail",description:"fail",uses:["workflow"],async run(_,ctx){await ctx.workflow.set({key:"completed",value:true});throw new Error("later failure")}});
 snow.registerCommand({name:"unauthorized",description:"unauthorized",uses:["workflow"],async run(_,ctx){await ctx.workflow.update({set:{bad:true},toolRestriction:{allow:[]}})}});
 snow.registerHook("before_request",request=>({context:[{text:"profile="+request.workflow.profile}]}),{workflowKeys:["profile"]});
 `
	var paths []string
	for _, id := range []string{"alpha", "beta"} {
		paths = append(paths, writeV2Fixture(t, cwd, id, script, []string{"commands", "workflow", "tool_policy", "hooks"}, nil))
	}
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", Permission: "allow", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: paths})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}
func runWorkflow(t *testing.T, a *App, id, input string) {
	t.Helper()
	if _, err := a.RunPluginCommand(t.Context(), id, input); err != nil {
		t.Fatal(err)
	}
}
func TestWorkflowPoliciesComposeAndRestoreBranches(t *testing.T) {
	a := workflowTestApp(t)
	original := a.Session.BranchTip()
	runWorkflow(t, a, "alpha:update", `{"set":{"profile":"reviewer"},"toolRestriction":{"allow":["read","grep"]}}`)
	alphaTip := a.Session.BranchTip()
	if alphaTip == original {
		t.Fatal("workflow did not advance append-only tip")
	}
	runWorkflow(t, a, "beta:update", `{"set":{"profile":"architect"},"toolRestriction":{"deny":["grep"]}}`)
	if !a.Agent.ToolAvailable("read") || a.Agent.ToolAvailable("grep") || a.Agent.ToolAvailable("write") {
		t.Fatal("restrictions did not intersect")
	}
	_, err := a.Agent.InvokePluginTool(t.Context(), plugin.Invocation{PluginID: "alpha", Name: "manual"}, "grep", json.RawMessage(`{"pattern":"anything"}`))
	if err == nil || !strings.Contains(err.Error(), "restricted by plugin beta") {
		t.Fatalf("dispatch bypass: %v", err)
	}
	main := a.Session.(session.ActiveBranchStore).ActiveBranchID()
	fork, err := a.ForkBranch(alphaTip)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Agent.ToolAvailable("grep") {
		t.Fatal("historical fork used sibling policy")
	}
	runWorkflow(t, a, "alpha:clear", "")
	if !a.Agent.ToolAvailable("write") {
		t.Fatal("own restriction not cleared")
	}
	if err := a.SelectBranch(main); err != nil {
		t.Fatal(err)
	}
	runWorkflow(t, a, "alpha:clear", "")
	if a.Agent.ToolAvailable("grep") {
		t.Fatal("clear removed another owner's restriction")
	}
	if err := a.SelectBranch(fork.ID); err != nil {
		t.Fatal(err)
	}
	if !a.Agent.ToolAvailable("grep") {
		t.Fatal("branch state was globally overwritten")
	}
	messages, err := a.Session.Messages()
	if err != nil || len(messages) != 0 {
		t.Fatalf("metadata entered provider messages: %+v %v", messages, err)
	}
}
func TestWorkflowFreshOwnedHookSnapshotsAndNoAuditLeak(t *testing.T) {
	a := workflowTestApp(t)
	runWorkflow(t, a, "alpha:update", `{"set":{"profile":"reviewer","private":"do-not-export"}}`)
	runWorkflow(t, a, "beta:update", `{"set":{"profile":"architect"}}`)
	result, changes, err := a.PluginManager.RunHooksWithWorkflow(t.Context(), plugin.HookRequest{Phase: "before_request"}, a.loadPluginWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Context) != 2 || result.Context[0].Text != "profile=reviewer" || result.Context[1].Text != "profile=architect" {
		t.Fatalf("wrong owned snapshot: %+v", result)
	}
	if result.Workflow != nil {
		t.Fatal("workflow escaped hook input")
	}
	raw, _ := jsonv2.Marshal(changes)
	if strings.Contains(string(raw), "do-not-export") || strings.Contains(string(raw), `"workflow"`) {
		t.Fatalf("workflow leaked into audits: %s", raw)
	}
	runWorkflow(t, a, "alpha:update", `{"set":{"profile":"debugger"}}`)
	result, _, err = a.PluginManager.RunHooksWithWorkflow(t.Context(), plugin.HookRequest{Phase: "before_request"}, a.loadPluginWorkflow)
	if err != nil || result.Context[0].Text != "profile=debugger" {
		t.Fatalf("stale snapshot: %+v %v", result, err)
	}
}
func TestWorkflowWriteAuthorityAndFailureAtomicity(t *testing.T) {
	a := workflowTestApp(t)
	tip := a.Session.BranchTip()
	if _, err := a.RunPluginCommand(t.Context(), "alpha:unauthorized", ""); err == nil {
		t.Fatal("combined mutation bypassed uses")
	}
	if a.Session.BranchTip() != tip {
		t.Fatal("failed atomic update changed tip")
	}
	if _, err := a.RunPluginCommand(t.Context(), "alpha:fail", ""); err == nil {
		t.Fatal("missing later failure")
	}
	state, err := a.Session.(session.WorkflowStateStore).WorkflowState(t.Context(), "alpha")
	if err != nil || string(state.Values["completed"]) != "true" {
		t.Fatalf("committed side effect was lost: %+v %v", state, err)
	}
	info := a.PluginInfos()[0]
	host := &appExtensionHost{app: a, services: a.extensions, info: info, agent: a.Agent, store: a.Session, generation: a.PluginGeneration()}
	inv := plugin.Invocation{PluginID: info.ID, Kind: "observer", Uses: []string{"workflow"}, Generation: a.PluginGeneration()}
	tip = a.Session.BranchTip()
	if _, err := host.Call(t.Context(), inv, "workflow.set", json.RawMessage(`{"key":"x","value":1}`)); err == nil {
		t.Fatal("observer wrote workflow")
	}
	if a.Session.BranchTip() != tip {
		t.Fatal("observer failure changed tip")
	}
	if _, err := a.ForkBranch(tip); err != nil {
		t.Fatal(err)
	}
	inv.Kind = "command"
	if _, err := host.Call(t.Context(), inv, "workflow.set", json.RawMessage(`{"key":"x","value":1}`)); err == nil || !strings.Contains(err.Error(), "session changed") {
		t.Fatalf("stale context wrote: %v", err)
	}
}
func TestPluginPolicyCannotLoosenNativePlanMode(t *testing.T) {
	a := workflowTestApp(t)
	if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	runWorkflow(t, a, "alpha:update", `{"set":{"profile":"off"},"toolRestriction":null}`)
	runWorkflow(t, a, "beta:clear", "")
	if a.Agent.ToolAvailable("write") {
		t.Fatal("policy clear loosened Plan Mode")
	}
	_, err := a.Agent.InvokePluginTool(t.Context(), plugin.Invocation{PluginID: "alpha", Name: "manual"}, "write", json.RawMessage(`{"path":"no-write","content":"denied"}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "plan") {
		t.Fatalf("plan dispatch not enforced: %v", err)
	}
}
func TestPluginPolicyProjectionFailureIsFailClosed(t *testing.T) {
	a := workflowTestApp(t)
	if err := a.Session.Append(session.Entry{Type: session.EntryMeta, Key: session.MetaPluginWorkflow, Value: `{"version":900}`}); err != nil {
		t.Fatal(err)
	}
	a.refreshPluginToolPolicy()
	if a.Agent.ToolAvailable("read") {
		t.Fatal("corrupt journal widened eligibility")
	}
	h := &appPluginHooks{manager: a.PluginManager, app: a}
	if !h.HasHook("before_request", false) {
		t.Fatal("failure cannot stop request")
	}
	if _, _, err := h.RunHooks(context.Background(), plugin.HookRequest{Phase: "before_request"}); err == nil {
		t.Fatal("corrupt policy did not stop request")
	}
}
