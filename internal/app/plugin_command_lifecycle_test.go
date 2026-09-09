package app

import (
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPluginErrorResultDoesNotApplyBranchTransition(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	dir := writeV2Fixture(t, cwd, "failure", `snow.registerCommand({name:"run", description:"Fail after scheduling", uses:["session"], async run(_,ctx) {
		await ctx.session.fork({name:"failed-command"});
		return {content:[{type:"text",text:"Could not complete"}],isError:true};
	}});`, []string{"commands", "session"}, nil)
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	generation := a.PluginGeneration()
	result, err := a.RunPluginCommand(t.Context(), "failure:run", "")
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if a.PluginGeneration() != generation {
		t.Fatal("failed command applied its queued branch transition")
	}
}

func TestPluginErrorResultClosesOwnedChildren(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	dir := writeV2Fixture(t, cwd, "failure", `snow.registerCommand({name:"run", description:"Fail after spawning", uses:["subagents"], async run(_,ctx) {
		await ctx.subagents.spawn({name:"owned",task:"inspect",role:"explorer",fork_turns:"none"});
		return {content:[{type:"text",text:"Could not complete"}],isError:true};
	}});`, []string{"commands", "subagents"}, nil)
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, Subagents: new(true), JavaScriptPaths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	result, err := a.RunPluginCommand(t.Context(), "failure:run", "")
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	owned, err := a.Subagent(t.Context(), "owned")
	if err != nil || owned.Status != protocol.AgentClosed {
		t.Fatalf("failed command left child open: %+v %v", owned, err)
	}
}

func TestPluginFailedTransitionClosesOwnedChildren(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	dir := writeV2Fixture(t, cwd, "failure", `snow.registerCommand({name:"run", description:"Schedule invalid transition", uses:["subagents","session"], async run(_,ctx) {
		await ctx.subagents.spawn({name:"owned",task:"inspect",role:"explorer",fork_turns:"none"});
		await ctx.session.selectBranch({id:"does-not-exist"});
		return "scheduled";
	}});`, []string{"commands", "subagents", "session"}, nil)
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, Subagents: new(true), JavaScriptPaths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.RunPluginCommand(t.Context(), "failure:run", ""); err == nil {
		t.Fatal("nonexistent branch transition succeeded")
	}
	owned, err := a.Subagent(t.Context(), "owned")
	if err != nil || owned.Status != protocol.AgentClosed {
		t.Fatalf("failed transition left child open: %+v %v", owned, err)
	}
}
