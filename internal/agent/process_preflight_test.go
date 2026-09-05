package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/tools/builtin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestManagedProcessHardPolicyRunsBeforePermissionAndLaunch(t *testing.T) {
	for _, mode := range []permission.Mode{permission.ModeAsk, permission.ModeAllow} {
		t.Run(string(mode), func(t *testing.T) {
			root := t.TempDir()
			manager := managedprocess.NewManager(managedprocess.Options{CWD: root})
			if err := manager.BindSession("test"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := manager.Close(context.WithoutCancel(t.Context())); err != nil {
					t.Error(err)
				}
			})
			registry := tools.NewRegistry()
			if err := builtin.RegisterProcessTools(registry, manager, builtin.Options{ShellProtectedPaths: []string{filepath.Join(root, "private")}}); err != nil {
				t.Fatal(err)
			}
			tool, _ := registry.Get("process_start")
			asker := &captureAsker{}
			a := newPreflightAgent(t, tool, permission.NewService(mode, asker))
			call := protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "process_start", Arguments: json.RawMessage(`{"command":"printf x > private/output"}`)}
			msg, dispatched, err := a.executeOne(t.Context(), call, "")
			if err != nil {
				t.Fatal(err)
			}
			if dispatched || !msg.IsError || asker.calls != 0 || len(manager.List()) != 0 {
				t.Fatal("managed process bypassed preflight policy")
			}
		})
	}
}
