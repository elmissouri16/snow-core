package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	internalplugin "github.com/elmissouri16/snow-core/internal/plugin"
	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/tools/builtin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPluginHookFinalArgumentsAndActualResult(t *testing.T) {
	for _, kind := range []string{"rewrite", "invalid", "post-failure"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			reg := tools.NewRegistry()
			guard := builtin.NewPathGuard([]string{root}, root)
			defer guard.Close()
			if err := builtin.RegisterBuiltins(reg, builtin.Options{Guard: guard}); err != nil {
				t.Fatal(err)
			}
			script := `snow.registerHook("before_tool",()=>({arguments:{path:"effective.txt",content:"written"}}));`
			if kind == "invalid" {
				script = `snow.registerHook("before_tool",()=>({arguments:{path:42}}));`
			}
			if kind == "post-failure" {
				script += `snow.registerHook("after_tool",()=>{throw new Error("post-failed")});`
			}
			p := &javascript.Package{Manifest: javascript.Manifest{ID: "hooks", Name: "Hooks", Version: "1", APIVersion: 2, Entry: "main.js", Capabilities: []string{"hooks"}}, Config: json.RawMessage(`{}`), Script: []byte(script)}
			manager := internalplugin.NewManager(reg)
			defer manager.Close(context.Background())
			if err := manager.LoadJavaScript(javascript.New(p, javascript.Options{}), "pinned"); err != nil {
				t.Fatal(err)
			}
			if err := manager.Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			asker := &jsAsker{}
			perm := permission.NewService(permission.ModeAsk, asker)
			store := session.NewMemoryStore(session.Options{})
			a, err := New(Options{Provider: &scriptedProvider{}, Registry: reg, Session: store, Permission: perm, ToolHost: &testHost{cwd: root, perm: perm}, Model: protocol.Model{ID: "test"}, PluginHooks: manager})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			result, _, runErr := a.executeOne(t.Context(), protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "write", Arguments: json.RawMessage(`{"path":"original.txt","content":"original"}`)}, store.BranchTip())
			if kind == "invalid" {
				if !result.IsError || len(asker.requests) != 0 {
					t.Fatalf("invalid rewrite reached permission: %+v %+v", result, asker.requests)
				}
				return
			}
			raw, err := os.ReadFile(filepath.Join(root, "effective.txt"))
			if err != nil || string(raw) != "written" {
				t.Fatalf("write %s %v", raw, err)
			}
			if len(asker.requests) != 1 || !strings.Contains(string(asker.requests[0].Args), "effective.txt") {
				t.Fatalf("permission saw wrong args: %+v", asker.requests)
			}
			if result.IsError || len(result.PluginTransforms) == 0 {
				t.Fatalf("actual result/audit lost: %+v", result)
			}
			if kind == "post-failure" {
				if runErr == nil || !strings.Contains(runErr.Error(), "tool executed") {
					t.Fatalf("post error %v", runErr)
				}
				messages, _ := store.Messages()
				if len(messages) != 1 || messages[0].IsError {
					t.Fatal("actual completed result not persisted")
				}
			} else if runErr != nil {
				t.Fatal(runErr)
			}
		})
	}
}

func TestPluginToolCardPersistsWithoutProviderMetadata(t *testing.T) {
	reg := tools.NewRegistry()
	manager := internalplugin.NewManager(reg)
	defer manager.Close(context.Background())
	p := &javascript.Package{Manifest: javascript.Manifest{ID: "cards", Name: "Cards", Version: "1", APIVersion: 2, Entry: "main.js", Capabilities: []string{"ui"}}, Config: json.RawMessage(`{}`), Script: []byte(`snow.registerTool({name:"card",description:"card",parameters:{type:"object"},execute(){return {content:[{type:"text",text:"model result"}],details:{label:"private card"}}}});snow.registerToolRenderer("card",result=>({type:"text",text:result.details.label}));`)}
	if err := manager.LoadJavaScript(javascript.New(p, javascript.Options{}), "pinned"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	store := session.NewMemoryStore(session.Options{})
	perm := permission.NewService(permission.ModeDeny, nil)
	a, err := New(Options{Provider: &scriptedProvider{}, Registry: reg, Session: store, Permission: perm, ToolHost: &testHost{}, Model: protocol.Model{ID: "test"}, PluginHooks: manager})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	_, _, err = a.executeOne(t.Context(), protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "card", Name: "plugin_cards_card", Arguments: json.RawMessage(`{}`)}, store.BranchTip())
	if err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ToolDisplay == nil || messages[0].ToolDisplay.Plugin == nil || messages[0].ToolDisplay.Plugin.Text != "private card" {
		t.Fatalf("card not persisted: %+v", messages)
	}
	projected := providerMessages(messages)
	if projected[0].ToolDisplay != nil || len(projected[0].PluginDetails) > 0 || projected[0].Content[0].Text != "model result" {
		t.Fatalf("provider projection %+v", projected)
	}
}
