package agent

import (
	"context"
	"encoding/json"
	"fmt"
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

type jsAsker struct {
	requests []permission.Request
	denyHost bool
}

func (a *jsAsker) Ask(_ context.Context, req permission.Request) (permission.Decision, error) {
	a.requests = append(a.requests, req)
	if a.denyHost && req.Plugin != nil && req.Plugin.HostTool != "" {
		return permission.DecisionDeny, nil
	}
	return permission.DecisionAllowSession, nil
}

func jsAgent(t *testing.T, hostTool, body, scope string, perm permission.Service, roots ...string) (*Agent, *session.MemoryStore, string) {
	t.Helper()
	root := t.TempDir()
	if len(roots) > 0 {
		root = roots[0]
	}
	reg := tools.NewRegistry()
	guard := builtin.NewPathGuard([]string{root}, root)
	t.Cleanup(func() { guard.Close() })
	if err := builtin.RegisterBuiltins(reg, builtin.Options{Guard: guard}); err != nil {
		t.Fatal(err)
	}
	p := &javascript.Package{Manifest: javascript.Manifest{ID: "test", Name: "Test", Version: "1", Entry: "main.js", APIVersion: 1, HostTools: []string{hostTool}}, Config: json.RawMessage(`{}`), Script: []byte(`snow.registerTool({name:"test",description:"test",parameters:{type:"object"},uses:["` + hostTool + `"],execute(args,ctx){` + body + `}});`)}
	manager := internalplugin.NewManager(reg, internalplugin.ManagerOptions{CWD: root})
	if err := manager.LoadJavaScript(javascript.New(p, javascript.Options{}), scope); err != nil {
		t.Fatal(err)
	}
	if err := manager.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	st := session.NewMemoryStore(session.Options{})
	a, err := New(Options{Provider: &scriptedProvider{}, Registry: reg, Session: st, Permission: perm, ToolHost: &testHost{cwd: root, perm: perm}, Model: protocol.Model{ID: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.Close()
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return a, st, root
}
func executeJS(t *testing.T, a *Agent, args string) protocol.Message {
	t.Helper()
	msg, _, err := a.executeOne(t.Context(), protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "outer-call", Name: "plugin_test_test", Arguments: json.RawMessage(args)}, "")
	if err != nil {
		t.Fatal(err)
	}
	return msg
}
func TestJavaScriptBuiltinBridgeAndSingleTranscriptResult(t *testing.T) {
	a, st, root := jsAgent(t, "read", `return ctx.callTool("read",args)`, "v1", permission.NewService(permission.ModeDeny, nil))
	if err := os.WriteFile(filepath.Join(root, "hello"), []byte("hello from builtin"), 0600); err != nil {
		t.Fatal(err)
	}
	msg := executeJS(t, a, `{"path":"hello"}`)
	if msg.IsError || !strings.Contains(sessionMessageTextForTest(msg), "hello from builtin") {
		t.Fatalf("result=%+v", msg)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ToolCallID != "outer-call" || messages[0].ToolName != "plugin_test_test" {
		t.Fatalf("messages=%+v", messages)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err = os.WriteFile(outside, []byte("outside-data"), 0600); err != nil {
		t.Fatal(err)
	}
	msg = executeJS(t, a, fmt.Sprintf(`{"path":%q}`, outside))
	if !msg.IsError || strings.Contains(sessionMessageTextForTest(msg), "outside-data") {
		t.Fatalf("outside result=%+v", msg)
	}
}
func TestJavaScriptNestedPermissionsAndPlanMode(t *testing.T) {
	for _, mode := range []string{"deny-host", "deny", "plan", "allow"} {
		t.Run(mode, func(t *testing.T) {
			asker := &jsAsker{denyHost: mode == "deny-host"}
			permissionMode := permission.ModeAsk
			if mode == "deny" {
				permissionMode = permission.ModeDeny
			}
			a, _, root := jsAgent(t, "write", `return ctx.callTool("write",args)`, "v1", permission.NewService(permissionMode, asker))
			if mode == "plan" {
				a.turnMode = protocol.ModePlan
			}
			msg := executeJS(t, a, `{"path":"created","content":"ok"}`)
			_, err := os.Stat(filepath.Join(root, "created"))
			if mode == "allow" {
				if msg.IsError || err != nil {
					t.Fatalf("result=%+v err=%v", msg, err)
				}
			} else if !msg.IsError || !os.IsNotExist(err) {
				t.Fatalf("mutation bypass: result=%+v stat=%v", msg, err)
			}
			if mode == "deny-host" || mode == "allow" {
				if len(asker.requests) != 2 {
					t.Fatalf("requests=%+v", asker.requests)
				}
				nested := asker.requests[1]
				if nested.Plugin == nil || nested.Plugin.HostTool != "write" || nested.Plugin.ParentToolCallID != "outer-call" || nested.Plugin.PluginID != "test" {
					t.Fatalf("origin=%+v", nested.Plugin)
				}
			}
		})
	}
}
func TestJavaScriptChangedScopeCannotReuseApproval(t *testing.T) {
	asker := &jsAsker{}
	perm := permission.NewService(permission.ModeAsk, asker)
	root := t.TempDir()
	for _, scope := range []string{"v1", "v1", "v2"} {
		a, _, _ := jsAgent(t, "write", `return ctx.callTool("write",args)`, scope, perm, root)
		executeJS(t, a, `{"path":"created","content":"ok"}`)
	}
	if len(asker.requests) != 4 {
		t.Fatalf("changed scope reused approval: %d requests", len(asker.requests))
	}
}
func TestJavaScriptShellPreflightRunsBeforeNestedAuthorization(t *testing.T) {
	asker := &jsAsker{}
	a, _, _ := jsAgent(t, "bash", `return ctx.callTool("bash",args)`, "v1", permission.NewService(permission.ModeAsk, asker))
	msg := executeJS(t, a, `{"command":"cat ~/.ssh/id_ed25519"}`)
	if !msg.IsError || len(asker.requests) != 1 {
		t.Fatalf("preflight bypass result=%+v asks=%d", msg, len(asker.requests))
	}
}
