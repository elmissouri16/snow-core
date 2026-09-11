package app

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/plugindocs"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func pluginDocsEnvironment(t *testing.T) (home, cwd string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	home, cwd = t.TempDir(), t.TempDir()
	t.Setenv("SNOW_HOME", home)
	return home, cwd
}

func newPluginDocsApp(t *testing.T, opts Options) *App {
	t.Helper()
	opts.Provider, opts.NoSession, opts.NoMCP, opts.Permission = "fake", true, true, "deny"
	a, err := New(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

type pluginDocsResponse struct {
	BuildVersion string              `json:"build_version"`
	Content      string              `json:"content"`
	Plugins      []plugindocs.Plugin `json:"plugins"`
	Total        int                 `json:"total"`
}

func runPluginDocs(t *testing.T, a *App, args string) (pluginDocsResponse, string) {
	t.Helper()
	tool, ok := a.Registry.Get(plugindocs.ToolName)
	if !ok {
		t.Fatal("plugin docs tool missing")
	}
	result, err := tool.Run(t.Context(), []byte(args), nil)
	if err != nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("docs=%+v err=%v", result, err)
	}
	text := result.Content[0].Text
	var response pluginDocsResponse
	if err := jsonv2.Unmarshal([]byte(text), &response); err != nil {
		t.Fatal(err)
	}
	return response, text
}

func TestPluginDocsDefaultDeferredDiscovery(t *testing.T) {
	_, cwd := pluginDocsEnvironment(t)
	a := newPluginDocsApp(t, Options{CWD: cwd})
	d, ok := a.Registry.Descriptor(plugindocs.ToolName)
	if !ok || d.Source != tools.SourceBuiltin || d.Risk != permission.RiskRead || d.Effect != tools.EffectReadOnly || d.Schema.Discovery == nil || d.Schema.Discovery.Mode != protocol.ToolDiscoveryDeferred {
		t.Fatalf("descriptor=%+v present=%v", d, ok)
	}
	search, ok := a.Registry.Get("search_tools")
	if !ok {
		t.Fatal("missing search_tools")
	}
	for _, query := range []string{"create a Snow JavaScript plugin", "update an existing TypeScript plugin", "show available existing plugins"} {
		matches, err := a.Router.Search(t.Context(), query, 5)
		if err != nil || !slices.ContainsFunc(matches, func(m tools.ToolMatch) bool { return m.ID == plugindocs.ToolName }) {
			t.Fatalf("query=%q matches=%+v err=%v", query, matches, err)
		}
		args, err := jsonv2.Marshal(map[string]any{"query": query, "limit": 5})
		if err != nil {
			t.Fatal(err)
		}
		result, err := search.Run(t.Context(), args, nil)
		if err != nil || result.IsError {
			t.Fatalf("search=%+v err=%v", result, err)
		}
		details, ok := result.Details.(tools.DiscoveryDetails)
		if !ok || !slices.ContainsFunc(details.Matches, func(m tools.ToolMatch) bool { return m.ID == plugindocs.ToolName }) {
			t.Fatalf("search details=%+v", result.Details)
		}
	}
}

func TestPluginDocsAvailabilityAndBuildVersion(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		tools                     []string
		noSkills, noPlugins, want bool
	}{
		{name: "default", want: true},
		{name: "no skills", noSkills: true, want: true},
		{name: "no plugins", noPlugins: true, want: true},
		{name: "neither", noSkills: true, noPlugins: true, want: true},
		{name: "explicit include", tools: []string{plugindocs.ToolName}, want: true},
		{name: "explicit exclude", tools: []string{"read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, cwd := pluginDocsEnvironment(t)
			a := newPluginDocsApp(t, Options{CWD: cwd, Tools: tc.tools, NoSkills: tc.noSkills, NoPlugins: tc.noPlugins, BuildVersion: "test-plugin-docs-build"})
			if _, ok := a.Registry.Get(plugindocs.ToolName); ok != tc.want {
				t.Fatalf("available=%v want=%v", ok, tc.want)
			}
			if !tc.want {
				return
			}
			for _, args := range []string{`{"action":"overview","limit":20}`, `{"action":"read","path":"api/snow.d.ts","limit":20}`} {
				response, _ := runPluginDocs(t, a, args)
				if response.BuildVersion != "test-plugin-docs-build" || response.Content == "" {
					t.Fatalf("response=%+v", response)
				}
			}
		})
	}
}

func TestPluginDocsInventoryDoesNotLoadPackages(t *testing.T) {
	for _, noPlugins := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "no plugins"}[noPlugins], func(t *testing.T) {
			home, cwd := pluginDocsEnvironment(t)
			path := writeV2Fixture(t, cwd, "danger", `throw new Error("package must never execute");`, nil, nil)
			// Invalid manifest proves inventory does not even parse an unloaded package.
			if err := os.WriteFile(filepath.Join(path, "snow-plugin.json"), []byte("must not parse this manifest"), 0o600); err != nil {
				t.Fatal(err)
			}
			writePluginConfig(t, filepath.Join(home, "config.json"), map[string]plugin.JavaScriptSpec{
				"danger":    {Path: path, Disabled: !noPlugins},
				"missing":   {Path: filepath.Join(cwd, "does-not-exist"), Disabled: true},
				"tombstone": {Disabled: true},
			})
			a := newPluginDocsApp(t, Options{CWD: cwd, NoPlugins: noPlugins})
			response, text := runPluginDocs(t, a, `{"action":"plugins"}`)
			if response.Total != 3 || len(response.Plugins) != 3 || strings.Contains(text, "must not") {
				t.Fatalf("inventory=%s", text)
			}
			for _, p := range response.Plugins {
				if !p.Registered || p.Loaded || p.APIVersion != 0 || p.Version != "" || p.LoadedPath != "" {
					t.Fatalf("unloaded metadata=%+v", p)
				}
			}
			detail, _ := runPluginDocs(t, a, `{"action":"plugins","plugin_id":"danger"}`)
			if len(detail.Plugins) != 1 || detail.Plugins[0].Enabled != noPlugins {
				t.Fatalf("detail=%+v", detail)
			}
		})
	}
}

func TestPluginDocsInventoryTrustAndExplicitPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name              string
		trusted, explicit bool
		scope             string
	}{
		{name: "untrusted", scope: "global"},
		{name: "trusted", trusted: true, scope: "project"},
		{name: "explicit", trusted: true, explicit: true, scope: "explicit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, cwd := pluginDocsEnvironment(t)
			global := filepath.Join(home, "config.json")
			globalPath, projectPath := filepath.Join(home, "global-missing"), filepath.Join(cwd, "project-missing")
			writePluginConfig(t, global, map[string]plugin.JavaScriptSpec{"demo": {Path: globalPath, Disabled: true}})
			if !tc.trusted {
				if _, err := config.Update(global, func(c *config.Config) error { c.DefaultProjectTrust = "deny"; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			writePluginConfig(t, filepath.Join(cwd, ".snow", "config.json"), map[string]plugin.JavaScriptSpec{"demo": {Path: projectPath, Disabled: true}, "project-only": {Disabled: true}})
			opts := Options{CWD: cwd}
			wantPath := globalPath
			if tc.trusted {
				wantPath = projectPath
			}
			if tc.explicit {
				wantPath = writeV2Fixture(t, cwd, "demo", `snow.registerCommand({name:"run",description:"run",run(){return "ok";}});`, []string{"commands"}, nil)
				opts.JavaScriptPaths = []string{wantPath}
			}
			a := newPluginDocsApp(t, opts)
			response, _ := runPluginDocs(t, a, `{"action":"plugins","plugin_id":"demo"}`)
			if len(response.Plugins) != 1 {
				t.Fatalf("response=%+v", response)
			}
			p := response.Plugins[0]
			if p.Scope != tc.scope || p.Path != wantPath || p.Loaded != tc.explicit {
				t.Fatalf("plugin=%+v", p)
			}
			all, _ := runPluginDocs(t, a, `{"action":"plugins"}`)
			if slices.ContainsFunc(all.Plugins, func(p plugindocs.Plugin) bool { return p.ID == "project-only" }) != tc.trusted {
				t.Fatalf("project trust inventory=%+v", all)
			}
		})
	}
}

func TestPluginDocsLoadedMetadataExcludesPrivateStateAndTracksSavedChanges(t *testing.T) {
	home, cwd := pluginDocsEnvironment(t)
	path := writeV2Fixture(t, cwd, "demo", `
 snow.registerView({name:"panel",title:"private-title-sentinel",placement:"sidebar"});
 snow.registerTool({name:"echo",description:"echo",parameters:{type:"object"},execute(){return {content:[{type:"text",text:"ok"}]};}});
 snow.registerCommand({name:"run",description:"Run demo",argumentHint:"[text]",alias:"demo-run",uses:["storage","ui"],async run(_,ctx){
 await ctx.storage.set({scope:"session",key:"private-key-sentinel",value:"private-storage-sentinel"});
 await ctx.ui.update({name:"panel",content:{type:"text",text:"private-ui-sentinel"}});
 return "ok";
 }});
 `, []string{"commands", "ui", "storage"}, nil)
	manifestPath := filepath.Join(path, "snow-plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := jsonv2.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["settings"] = []map[string]any{{"name": "token", "type": "string", "default": "private-default-sentinel"}}
	saveManifest := func() {
		t.Helper()
		data, err := jsonv2.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	saveManifest()
	global := filepath.Join(home, "config.json")
	specs := map[string]plugin.JavaScriptSpec{"demo": {Path: path, Config: []byte(`{"token":"private-config-sentinel"}`)}}
	writePluginConfig(t, global, specs)
	a := newPluginDocsApp(t, Options{CWD: cwd})
	awaitReloadReady(t, a)
	if _, err := a.RunPluginCommand(t.Context(), "demo:run", ""); err != nil {
		t.Fatal(err)
	}
	if views := a.PluginViews(); len(views) != 1 || views[0].Content.Text != "private-ui-sentinel" {
		t.Fatalf("dynamic state fixture=%+v", views)
	}
	detail := func() plugindocs.Plugin {
		t.Helper()
		response, text := runPluginDocs(t, a, `{"action":"plugins","plugin_id":"demo"}`)
		for _, secret := range []string{"private-config-sentinel", "private-default-sentinel", "private-ui-sentinel", "private-title-sentinel", "private-key-sentinel", "private-storage-sentinel"} {
			if strings.Contains(text, secret) {
				t.Fatalf("private fixture data %q leaked", secret)
			}
		}
		if len(response.Plugins) != 1 {
			t.Fatalf("detail=%+v", response)
		}
		return response.Plugins[0]
	}
	p := detail()
	if !p.Loaded || !p.Enabled || !p.Registered || p.RestartRequired || p.APIVersion != 2 || p.Version != "1" || p.Name != "demo" || p.LoadedPath != path {
		t.Fatalf("loaded=%+v", p)
	}
	if !slices.Contains(p.Tools, "plugin_demo_echo") || len(p.Commands) != 1 || p.Commands[0].ID != "demo:run" || p.Commands[0].ArgumentHint != "[text]" || p.Commands[0].Alias != "demo-run" || len(p.Settings) != 1 || p.Settings[0].Name != "token" || p.Settings[0].Type != "string" || len(p.Views) != 1 {
		t.Fatalf("metadata=%+v", p)
	}
	summary, _ := runPluginDocs(t, a, `{"action":"plugins"}`)
	if len(summary.Plugins) != 1 || len(summary.Plugins[0].Commands) != 0 || len(summary.Plugins[0].Tools) != 0 || len(summary.Plugins[0].Settings) != 0 {
		t.Fatalf("summary disclosed details=%+v", summary)
	}
	manifest["version"] = "2"
	saveManifest()
	if p := detail(); p.Version != "1" {
		t.Fatalf("disk edit changed loaded metadata=%+v", p)
	}
	if result, err := a.ReloadPlugin(t.Context(), "demo"); err != nil || !result.Applied {
		t.Fatalf("reload=%+v err=%v", result, err)
	}
	if p := detail(); p.Version != "2" {
		t.Fatalf("reload metadata=%+v", p)
	}
	if _, err := a.SetPluginEnabled(t.Context(), "demo", false); err != nil {
		t.Fatal(err)
	}
	if p := detail(); p.Enabled || !p.Loaded || !p.Registered || !p.RestartRequired {
		t.Fatalf("saved disable=%+v", p)
	}
	replacement := filepath.Join(cwd, "replacement-not-loaded")
	specs["demo"] = plugin.JavaScriptSpec{Path: replacement}
	writePluginConfig(t, global, specs)
	if p := detail(); p.Path != replacement || p.LoadedPath != path || !p.RestartRequired || p.Version != "2" {
		t.Fatalf("changed registration=%+v", p)
	}
	writePluginConfig(t, global, map[string]plugin.JavaScriptSpec{})
	if p := detail(); p.Registered || !p.Loaded || !p.RestartRequired || p.Version != "2" || !slices.Contains(p.Tools, "plugin_demo_echo") {
		t.Fatalf("removed registration=%+v", p)
	}
}
