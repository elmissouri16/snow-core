package app

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func lifecycleApp(t *testing.T, script string, capabilities []string) *App {
	t.Helper()
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	dir := writeV2Fixture(t, cwd, "guard", script, capabilities, nil)
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestPluginSessionGateFailsBeforeBindingsAndBranchMutation(t *testing.T) {
	for _, script := range []string{
		`snow.registerHook("before_session_change",()=>({block:"unfinished workflow"}));`,
		`snow.registerHook("before_session_change",()=>{throw new Error("gate failed")});`,
		`snow.registerHook("before_session_change",()=>({text:"redirect"}));`,
		`snow.registerHook("before_session_change",()=>{while(true){}});`,
	} {
		t.Run(script, func(t *testing.T) {
			a := lifecycleApp(t, script, []string{"hooks"})
			old := a.Session
			oldGeneration := a.extensions.generation.Load()
			oldGoalBinding := a.Goal.Binding()
			branch, err := old.(session.BranchStore).ForkBranch("root")
			if err != nil {
				t.Fatal(err)
			}
			if err := old.(session.BranchStore).SelectBranch("main"); err != nil {
				t.Fatal(err)
			}
			notifications := make(chan protocol.AgentEvent, 4)
			unsubscribe := a.Agent.Subscribe(func(ev protocol.AgentEvent) {
				if ev.Type == protocol.EvPluginSessionChanged {
					notifications <- ev
				}
			})
			defer unsubscribe()
			next := session.NewMemoryStore(session.Options{ID: "next"})
			defer next.Close()
			for _, operation := range []func() error{
				func() error { return a.SetSession(next) },
				func() error { return a.SelectBranch(branch.ID) },
				func() error { _, err := a.ForkBranch("root"); return err },
			} {
				if err := operation(); err == nil {
					t.Fatal("gate allowed transition")
				}
				if a.Session != old || pluginBranchID(a.Session) != "main" || a.Session.BranchTip() != "root" {
					t.Fatal("veto changed session/branch")
				}
				if a.extensions.generation.Load() != oldGeneration || a.Goal.Binding() != oldGoalBinding {
					t.Fatal("veto changed host/goal generation")
				}
			}
			branches, err := old.(session.BranchStore).Branches()
			if err != nil || len(branches) != 2 {
				t.Fatalf("veto created branch: %v %v", branches, err)
			}
			if err := old.Append(session.Entry{Type: session.EntryMeta, Key: "still-open", Value: "yes"}); err != nil {
				t.Fatal("veto closed old store", err)
			}
			if err := a.Agent.DrainEvents(t.Context()); err != nil {
				t.Fatal(err)
			}
			select {
			case <-notifications:
				t.Fatal("veto published committed notification")
			default:
			}
		})
	}
}

func TestPluginSessionGateUsesOldBranchWorkflowAndValidatesTargetsFirst(t *testing.T) {
	a := lifecycleApp(t, `
 let seen=[];
 snow.registerHook("before_session_change",r=>{seen.push(r);return r.workflow.guard ? {block:"old branch guard"}:{}},{workflowKeys:["guard"]});
 snow.registerCommand({name:"seen",description:"inspect",run(){return JSON.stringify(seen)}});
 `, []string{"hooks", "commands", "workflow"})
	workflow := a.Session.(session.WorkflowStateStore)
	state, err := workflow.WorkflowState(t.Context(), "guard")
	if err != nil {
		t.Fatal(err)
	}
	_, err = workflow.ApplyWorkflowState(t.Context(), "guard", state.BranchID, state.TipID, session.WorkflowUpdate{Set: map[string]json.RawMessage{"guard": json.RawMessage(`true`)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func() error{
		func() error { return a.SelectBranch("missing") },
		func() error { _, err := a.ForkBranch("missing"); return err },
		func() error { _, err := a.ForkBranchWithOptions(protocol.BranchForkOptions{Name: "main"}); return err },
	} {
		if err := operation(); err == nil || strings.Contains(err.Error(), "old branch guard") {
			t.Fatalf("invalid target reached gate: %v", err)
		}
	}
	next := session.NewMemoryStore(session.Options{ID: "new-session"})
	defer next.Close()
	if err := a.SetSession(next); err == nil || !strings.Contains(err.Error(), "old branch guard") {
		t.Fatalf("old workflow missing: %v", err)
	}
	result, err := a.RunPluginCommand(t.Context(), "guard:seen", "")
	if err != nil {
		t.Fatal(err)
	}
	var requests []struct {
		SessionChange struct{ Operation, OldSessionID, NewSessionID string }
		Workflow      map[string]bool
	}
	if err := jsonv2.Unmarshal([]byte(result.Content[0].Text), &requests); err != nil {
		t.Fatal(err)
	}
	// Hook JSON uses lower-camel-case keys; inspect the string for exact identities
	// rather than relying on case-insensitive decoding in encoding/json/v2.
	if len(requests) != 1 || !strings.Contains(result.Content[0].Text, `"operation":"set_session"`) || !strings.Contains(result.Content[0].Text, `"newSessionId":"new-session"`) {
		t.Fatalf("requests: %s", result.Content[0].Text)
	}
}

func TestPluginSessionNotificationRunsAfterLocksWithNewHost(t *testing.T) {
	a := lifecycleApp(t, `snow.registerCommand({name:"read",description:"read",uses:["workflow"],async run(_,ctx){return String(await ctx.workflow.get({key:"marker"}))}});`, []string{"commands", "workflow"})
	oldID := a.Session.ID()
	next := session.NewMemoryStore(session.Options{ID: "next-session"})
	state, err := next.WorkflowState(t.Context(), "guard")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.ApplyWorkflowState(t.Context(), "guard", state.BranchID, state.TipID, session.WorkflowUpdate{Set: map[string]json.RawMessage{"marker": json.RawMessage(`"new-host"`)}}); err != nil {
		t.Fatal(err)
	}
	type observation struct {
		change *protocol.PluginSessionChanged
		text   string
		err    error
	}
	observed := make(chan observation, 1)
	unsubscribe := a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type != protocol.EvPluginSessionChanged {
			return
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		result, err := a.RunPluginCommand(ctx, "guard:read", "")
		text := ""
		if len(result.Content) > 0 {
			text = result.Content[0].Text
		}
		observed <- observation{ev.PluginSessionChanged, text, err}
	})
	defer unsubscribe()
	if err := a.SetSession(next); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-observed:
		if got.err != nil || got.text != "new-host" {
			t.Fatalf("observer failed on new host: %+v", got)
		}
		if got.change == nil || got.change.OldSessionID != oldID || got.change.NewSessionID != next.ID() || got.change.Reason != "set_session" || got.change.Generation != a.extensions.generation.Load() {
			t.Fatalf("notification: %+v", got.change)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("observer deadlocked on transition locks")
	}
}

func TestPluginBranchNotificationsAndDetachedForkExclusion(t *testing.T) {
	a := lifecycleApp(t, `snow.registerHook("before_session_change",r=>r.sessionChange.operation==="fork_branch"?{}:{block:"selection guarded"});`, []string{"hooks"})
	notifications := make(chan *protocol.PluginSessionChanged, 4)
	unsubscribe := a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvPluginSessionChanged {
			notifications <- ev.PluginSessionChanged
		}
	})
	defer unsubscribe()
	fork, err := a.ForkBranch("root")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SelectBranch("main"); err == nil {
		t.Fatal("selection not guarded")
	}
	if _, err := a.ForkSession(t.Context(), protocol.SessionForkOptions{}); err != nil {
		t.Fatal("detached fork incorrectly gated", err)
	}
	if err := a.Agent.DrainEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-notifications:
		if event.Reason != "fork_branch" || event.OldBranchID != "main" || event.NewBranchID != fork.ID {
			t.Fatalf("fork event %+v", event)
		}
	default:
		t.Fatal("missing fork notification")
	}
	select {
	case <-notifications:
		t.Fatal("veto/detached fork published active transition")
	default:
	}
}

func TestPluginSessionGateCanceledContext(t *testing.T) {
	a := lifecycleApp(t, `snow.registerHook("before_session_change",()=>({}));`, []string{"hooks"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := a.beforePluginSessionChange(ctx, a.pluginTransitionRequest("select_branch", a.Session))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestPluginBranchSelectionHookAndCommittedNotification(t *testing.T) {
	a := lifecycleApp(t, `
 let seen;
 snow.registerHook("before_session_change",r=>{seen=r.sessionChange;return {}});
 snow.registerCommand({name:"seen",description:"inspect",run(){return JSON.stringify(seen)}});
 `, []string{"hooks", "commands"})
	fork, err := a.ForkBranch("root")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.DrainEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	notifications := make(chan *protocol.PluginSessionChanged, 1)
	unsubscribe := a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvPluginSessionChanged {
			notifications <- ev.PluginSessionChanged
		}
	})
	defer unsubscribe()
	generation := a.extensions.generation.Load()
	if err := a.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	result, err := a.RunPluginCommand(t.Context(), "guard:seen", "")
	if err != nil || len(result.Content) != 1 {
		t.Fatalf("hook observation: %+v %v", result, err)
	}
	for _, field := range []string{`"operation":"select_branch"`, `"oldBranchId":"` + fork.ID + `"`, `"newBranchId":"main"`, `"oldSessionId":"` + a.Session.ID() + `"`, `"newSessionId":"` + a.Session.ID() + `"`} {
		if !strings.Contains(result.Content[0].Text, field) {
			t.Fatalf("hook field %s missing from %s", field, result.Content[0].Text)
		}
	}
	if err := a.Agent.DrainEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-notifications:
		if event.Reason != "select_branch" || event.OldBranchID != fork.ID || event.NewBranchID != "main" || event.OldSessionID != a.Session.ID() || event.NewSessionID != a.Session.ID() || event.Generation != generation+1 {
			t.Fatalf("selection event %+v", event)
		}
	default:
		t.Fatal("missing selection notification")
	}
}
