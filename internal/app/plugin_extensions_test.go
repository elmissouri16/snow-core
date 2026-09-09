package app

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func writeV2Fixture(t *testing.T, root, id, script string, capabilities, hostTools []string) string {
	t.Helper()
	dir := writeJSFixture(t, root, id, script)
	raw, err := jsonv2.Marshal(map[string]any{"id": id, "name": id, "version": "1", "api_version": 2, "entry": "main.js", "capabilities": capabilities, "host_tools": hostTools})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snow-plugin.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestPluginCommandsStateUIAndSessionIsolation(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	dir := writeV2Fixture(t, cwd, "v2", `
 snow.registerView({name:"panel",title:"Panel",placement:"sidebar"});
 snow.registerCommand({name:"run",description:"run",uses:["storage","ui"],async run(_,ctx){
 const old=await ctx.storage.get({scope:"session",key:"count"})||0;
 await ctx.storage.set({scope:"session",key:"count",value:old+1});
 await ctx.ui.update({name:"panel",content:{type:"text",text:"Count "+(old+1)}});
 return String(old+1);
 }});
 `, []string{"commands", "ui", "storage"}, nil)
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	initialTip := a.Session.BranchTip()
	for i, want := range []string{"1", "2"} {
		result, err := a.RunPluginCommand(t.Context(), "v2:run", "")
		if err != nil || result.Content[0].Text != want {
			t.Fatalf("run %d: %+v %v", i, result, err)
		}
	}
	if tip := a.Session.BranchTip(); tip != initialTip {
		t.Fatalf("state writes changed transcript tip: %s", tip)
	}
	views := a.PluginViews()
	if len(views) != 1 || views[0].Content.Text != "Count 2" {
		t.Fatalf("views: %+v", views)
	}
	views[0].Content.Text = "mutated"
	if a.PluginViews()[0].Content.Text == "mutated" {
		t.Fatal("view alias")
	}
}
func TestReviewTeamExampleEndToEnd(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	path, err := filepath.Abs("../../examples/plugins/review-team")
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	a, err := New(t.Context(), Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, Permission: "deny", Subagents: &enabled, JavaScriptPaths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var mu sync.Mutex
	updates := 0
	a.AttachPluginUI(func(_ context.Context, event protocol.PluginUIEvent) (json.RawMessage, error) {
		mu.Lock()
		defer mu.Unlock()
		updates++
		return []byte("null"), nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err = a.RunPluginCommand(ctx, "review-team", "Review fixture architecture")
	if err != nil {
		t.Fatal(err)
	}
	children, err := a.ListSubagents(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(children.Agents) != 4 {
		t.Fatalf("children=%+v", children)
	}
	for _, child := range children.Agents {
		if child.Agent.Path == protocol.RootAgentPath {
			continue
		}
		if child.Agent.Role != "explorer" || child.Status != protocol.AgentClosed {
			t.Fatalf("child=%+v", child)
		}
	}
	messages, err := a.Agent.Messages()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range messages {
		for _, block := range message.Content {
			if message.Role == protocol.RoleUser && strings.Contains(block.Text, "Synthesize this explicitly requested review") {
				found = true
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !found || updates == 0 {
		t.Fatalf("synthesis=%v updates=%d", found, updates)
	}
}
func TestV2ExamplesInitialize(t *testing.T) {
	for _, name := range []string{"workspace-dashboard", "project-helper-v2"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SNOW_HOME", t.TempDir())
			path, err := filepath.Abs("../../examples/plugins/" + name)
			if err != nil {
				t.Fatal(err)
			}
			a, err := New(t.Context(), Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{path}})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if name == "workspace-dashboard" {
				if _, err := a.RunPluginCommand(t.Context(), name+":refresh", ""); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPluginCommandCancellationClosesOnlyOwnedChildren(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	enabled := true
	path := writeV2Fixture(t, cwd, "workflow", `snow.registerCommand({name:"run",description:"run",uses:["subagents","ui"],async run(_,ctx){await ctx.subagents.spawn({name:"owned",task:"inspect",role:"explorer",fork_turns:"none"});await ctx.ui.notify({text:"started"});await ctx.sleep(60000);}});`, []string{"commands", "ui", "subagents"}, nil)
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, Subagents: &enabled, JavaScriptPaths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.ReadySubagents(); err != nil {
		t.Fatal(err)
	}
	_, err = a.SpawnSubagent(t.Context(), protocol.SpawnSubagentRequest{Name: "unrelated", Task: "inspect", Role: "explorer", ForkTurns: "none"})
	if err != nil {
		t.Fatal(err)
	}
	awaitSubagent(t, a, "unrelated", protocol.AgentCompleted)
	started := make(chan struct{})
	a.AttachPluginUI(func(context.Context, protocol.PluginUIEvent) (json.RawMessage, error) {
		close(started)
		return []byte("null"), nil
	})
	done := make(chan error, 1)
	go func() { _, err := a.RunPluginCommand(t.Context(), "workflow:run", ""); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("command not started")
	}
	if !a.CancelPluginCommand("workflow:run") {
		t.Fatal("cancel failed")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("missing cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("command cancellation hung")
	}
	owned, err := a.Subagent(t.Context(), "owned")
	if err != nil || owned.Status != protocol.AgentClosed {
		t.Fatalf("owned=%+v %v", owned, err)
	}
	unrelated, err := a.Subagent(t.Context(), "unrelated")
	if err != nil || unrelated.Status != protocol.AgentCompleted {
		t.Fatalf("unrelated=%+v %v", unrelated, err)
	}
}

func TestPluginBranchTransitionInvalidatesOldHost(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	path := writeV2Fixture(t, cwd, "brancher", `snow.registerCommand({name:"fork",description:"fork",uses:["session"],async run(_,ctx){await ctx.session.fork({name:"plugin-fork"});return "scheduled"}});`, []string{"commands", "session"}, nil)
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	old := &appExtensionHost{app: a, services: a.extensions, info: a.PluginInfos()[0], agent: a.Agent, store: a.Session, generation: a.PluginGeneration()}
	before := old.Environment().Generation
	if _, err := a.RunPluginCommand(t.Context(), "brancher:fork", ""); err != nil {
		t.Fatal(err)
	}
	if a.PluginGeneration() == before {
		t.Fatal("fork did not invalidate invocation generation")
	}
	if _, err := old.Call(t.Context(), plugin.Invocation{PluginID: "brancher", Kind: "command", Generation: before}, "agent.state", []byte(`{}`)); err == nil {
		t.Fatal("old host could read new branch")
	}
}

func TestPluginFormUsesExistingBrokerAndValidatesTypes(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	path := writeV2Fixture(t, cwd, "form", `snow.registerCommand({name:"ask",description:"ask",uses:["ui"],async run(_,ctx){const form=await ctx.ui.form({title:"Preferences",fields:[{name:"count",title:"Count",type:"number"}]});return typeof form.count+":"+form.count}});`, []string{"commands", "ui"}, nil)
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{path}, UserInputHandler: func(ctx context.Context, request protocol.UserInputRequest) (protocol.UserInputResponse, error) {
		return protocol.UserInputResponse{RequestID: request.ID, Answers: []protocol.UserInputAnswer{{QuestionID: "count", Answer: "7"}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	result, err := a.RunPluginCommand(t.Context(), "form:ask", "")
	if err != nil || result.Content[0].Text != "number:7" {
		t.Fatalf("form %+v %v", result, err)
	}
}
