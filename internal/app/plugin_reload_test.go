package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func awaitReloadReady(t *testing.T, a *App) {
	t.Helper()
	a.StartPluginExtensions()
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.extensions.mu.Lock()
		busy := a.extensions.readyRunning
		a.extensions.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("readiness did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}
func replaceReloadScript(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(path, "main.js"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
}
func reloadTestApp(t *testing.T, paths ...string) *App {
	t.Helper()
	a, err := New(t.Context(), Options{CWD: filepath.Dir(paths[0]), Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, Permission: "deny", JavaScriptPaths: paths})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	awaitReloadReady(t, a)
	return a
}
func TestReloadJavaScriptAPIVersions(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			t.Setenv("SNOW_HOME", t.TempDir())
			cwd := t.TempDir()
			initial := `snow.registerTool({name:"old",description:"old",parameters:{type:"object"},execute(){return {content:[{type:"text",text:"old"}]}}});`
			var path string
			if version == 1 {
				path = writeJSFixture(t, cwd, "demo", initial)
			} else {
				path = writeV2Fixture(t, cwd, "demo", initial, nil, nil)
			}
			neighbor := writeJSFixture(t, cwd, "neighbor", jsEcho)
			a := reloadTestApp(t, path, neighbor)
			manager, reg, generation := a.PluginManager, a.Registry, a.PluginGeneration()
			replaceReloadScript(t, path, `snow.registerTool({name:"new",description:"new",parameters:{type:"object"},execute(){return {content:[{type:"text",text:"new"}]}}});`)
			result, err := a.ReloadPlugin(t.Context(), "demo")
			if err != nil || !result.Applied || result.Generation != generation+1 || result.Fingerprint == "" {
				t.Fatalf("reload=%+v err=%v", result, err)
			}
			if manager != a.PluginManager || reg != a.Registry {
				t.Fatal("stable references replaced")
			}
			if _, ok := reg.Get("plugin_demo_old"); ok {
				t.Fatal("old tool retained")
			}
			tool, ok := reg.Get("plugin_demo_new")
			if !ok {
				t.Fatal("replacement missing")
			}
			output, err := tool.Run(t.Context(), json.RawMessage(`{}`), nil)
			if err != nil || len(output.Content) != 1 || output.Content[0].Text != "new" {
				t.Fatalf("new tool=%+v err=%v", output, err)
			}
			if _, ok := reg.Get("plugin_neighbor_echo"); !ok {
				t.Fatal("neighbor removed")
			}
			second, err := a.ReloadPlugin(t.Context(), "demo")
			if err != nil || !second.Applied || second.Fingerprint != result.Fingerprint || second.Generation != result.Generation+1 {
				t.Fatalf("unchanged-byte reload=%+v %v", second, err)
			}
		})
	}
}
func TestReloadValidationFailurePreservesLiveCommandAndGeneration(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	path := writeV2Fixture(t, cwd, "demo", `snow.registerCommand({name:"run",description:"old",run(){return "old";}});`, []string{"commands"}, nil)
	neighbor := writeV2Fixture(t, cwd, "other", `snow.registerCommand({name:"run",alias:"taken",description:"other",run(){return "other";}});`, []string{"commands"}, nil)
	a := reloadTestApp(t, path, neighbor)
	generation := a.PluginGeneration()
	for _, script := range []string{`not valid js @`, `snow.registerCommand({name:"run",alias:"taken",description:"collision",run(){return "bad";}});`, `while(true){}`} {
		replaceReloadScript(t, path, script)
		result, err := a.ReloadPlugin(t.Context(), "demo")
		if err == nil || result.Applied || a.PluginGeneration() != generation {
			t.Fatalf("bad preparation applied: %+v %v", result, err)
		}
		command, err := a.RunPluginCommand(t.Context(), "demo:run", "")
		if err != nil || command.Content[0].Text != "old" {
			t.Fatalf("old command disrupted %+v %v", command, err)
		}
	}
}
func TestReloadPreservesStateAndSurvivorViewsAndReadiness(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	script := `snow.registerView({name:"panel",placement:"sidebar",title:"Panel"});
 snow.onReady(async (_,ctx)=>{const n=await ctx.storage.get({key:"ready"})||0;await ctx.storage.set({key:"ready",value:n+1});await ctx.ui.update({name:"panel",content:{type:"text",text:"ready"}});});
 snow.registerCommand({name:"read",description:"read",uses:["storage"],async run(_,ctx){return String(await ctx.storage.get({key:"ready"}));}});`
	path := writeV2Fixture(t, cwd, "demo", script, []string{"commands", "storage", "ui"}, nil)
	neighbor := writeV2Fixture(t, cwd, "other", script, []string{"commands", "storage", "ui"}, nil)
	a := reloadTestApp(t, path, neighbor)
	oldTip := a.Session.BranchTip()
	var stale plugin.ExtensionHost
	info := a.PluginInfos()[0]
	stale = &appExtensionHost{app: a, services: a.extensions, info: info, agent: a.Agent, store: a.Session, generation: a.PluginGeneration()}
	oldGeneration := a.PluginGeneration()
	result, err := a.ReloadPlugin(t.Context(), "demo")
	if err != nil || !result.Applied || len(result.Diagnostics) != 0 {
		t.Fatalf("reload=%+v %v", result, err)
	}
	for id, want := range map[string]string{"demo": "2", "other": "1"} {
		got, err := a.RunPluginCommand(t.Context(), id+":read", "")
		if err != nil || got.Content[0].Text != want {
			t.Fatalf("readiness %s=%+v err=%v", id, got, err)
		}
	}
	if a.Session.BranchTip() != oldTip {
		t.Fatal("reload moved transcript tip")
	}
	if _, err := stale.Call(t.Context(), plugin.Invocation{PluginID: info.ID, Generation: oldGeneration, Uses: []string{"storage"}, Kind: "command"}, "storage.get", json.RawMessage(`{"key":"ready"}`)); err == nil {
		t.Fatal("old context accepted")
	}
	for _, view := range a.PluginViews() {
		if view.Content == nil || view.Content.Text != "ready" {
			t.Fatalf("view lost %+v", view)
		}
	}
}
func TestReloadPostCommitDiagnosticsAreApplied(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	path := writeV2Fixture(t, cwd, "demo", `snow.onClose(()=>{throw new Error("cleanup sentinel");});`, nil, nil)
	a := reloadTestApp(t, path)
	replaceReloadScript(t, path, `snow.onReady(()=>{throw new Error("ready sentinel");});snow.registerCommand({name:"new",description:"new",run(){return "installed";}});`)
	// Registering commands requires a manifest grant too.
	writeV2Fixture(t, cwd, "demo", `snow.onReady(()=>{throw new Error("ready sentinel");});snow.registerCommand({name:"new",description:"new",run(){return "installed";}});`, []string{"commands"}, nil)
	result, err := a.ReloadPlugin(t.Context(), "demo")
	if err != nil || !result.Applied || len(result.Diagnostics) != 2 {
		t.Fatalf("receipt=%+v err=%v", result, err)
	}
	command, err := a.RunPluginCommand(t.Context(), "demo:new", "")
	if err != nil || command.Content[0].Text != "installed" {
		t.Fatalf("postcommit rollback %+v %v", command, err)
	}
}
func TestReloadBusyAdmissionDoesNotCancelWork(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	path := writeV2Fixture(t, t.TempDir(), "demo", `snow.registerCommand({name:"wait",description:"wait",uses:["ui"],async run(_,ctx){await ctx.ui.notify("waiting");return "done";}});`, []string{"commands", "ui"}, nil)
	a := reloadTestApp(t, path)
	generation := a.PluginGeneration()
	unlock := a.Agent.LockAdmission()
	result, err := a.ReloadPlugin(t.Context(), "demo")
	unlock()
	if err == nil || result.Applied {
		t.Fatal("reload entered busy admission")
	}
	// Simulate an accepted command and host operation without needing a provider.
	a.extensions.mu.Lock()
	a.extensions.commands["demo:wait"] = func() { t.Error("reload canceled user command") }
	a.extensions.mu.Unlock()
	result, err = a.ReloadPlugin(t.Context(), "demo")
	a.extensions.mu.Lock()
	delete(a.extensions.commands, "demo:wait")
	a.extensions.mu.Unlock()
	if err == nil || result.Applied {
		t.Fatal("reload entered active command")
	}
	a.extensions.sessionMu.RLock()
	result, err = a.ReloadPlugin(t.Context(), "demo")
	a.extensions.sessionMu.RUnlock()
	if err == nil || result.Applied {
		t.Fatal("reload entered accepted host operation")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err = a.ReloadPlugin(ctx, "demo")
	if err == nil || result.Applied || a.PluginGeneration() != generation {
		t.Fatalf("canceled result=%+v %v", result, err)
	}
}
func TestReloadRejectsUnloadedAndDisabledRegistrations(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	t.Setenv("SNOW_HOME", home)
	path := writeJSFixture(t, cwd, "demo", jsEcho)
	other := writeJSFixture(t, cwd, "other", jsEcho)
	writePluginConfig(t, filepath.Join(home, "config.json"), map[string]plugin.JavaScriptSpec{"demo": {Path: path}, "other": {Path: other, Disabled: true}})
	a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	awaitReloadReady(t, a)
	if _, err := a.ReloadPlugin(t.Context(), "missing"); err == nil {
		t.Fatal("unknown reloaded")
	}
	if _, err := a.SetPluginEnabled(t.Context(), "other", true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReloadPlugin(t.Context(), "other"); err == nil || !strings.Contains(err.Error(), "loaded") {
		t.Fatalf("unloaded result %v", err)
	}
	if _, err := a.SetPluginEnabled(t.Context(), "demo", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReloadPlugin(t.Context(), "demo"); err == nil {
		t.Fatal("disabled reloaded")
	}
}
