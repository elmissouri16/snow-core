package app

import (
	"strings"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/subagent"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/tools/builtin"
)

func testChildParentRegistry(t *testing.T) *tools.SimpleRegistry {
	t.Helper()
	reg := tools.NewRegistry()
	if err := builtin.RegisterBuiltins(reg, builtin.Options{BashTimeout: time.Second, Roots: []string{t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func hasRegisteredTool(reg tools.Registry, name string) bool {
	_, ok := reg.Get(name)
	return ok
}

func TestCloneChildRegistryRoleCapabilities(t *testing.T) {
	parent := testChildParentRegistry(t)
	defaults := config.Default().Subagents.Roles

	general := subagent.Role{Name: "default", Tools: append([]string(nil), defaults["default"].Tools...)}
	generalReg, generalCaps, err := cloneChildRegistry(parent, general, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRegisteredTool(generalReg, "bash") || !generalCaps.Shell {
		t.Fatal("default role did not receive bash")
	}
	rootPerm := permission.NewService(permission.ModeAllow, nil)
	if got := childPermissionService(rootPerm, generalCaps, false); got != rootPerm {
		t.Fatal("shell-capable child did not inherit root permission service")
	}
	for _, name := range []string{"read", "grep", "glob", "activate_skill", "read_skill_resource"} {
		if hasRegisteredTool(parent, name) && !hasRegisteredTool(generalReg, name) {
			t.Fatalf("default role missing %s", name)
		}
	}
	for _, name := range []string{"write", "edit", "ask_user", "request_user_input", "update_plan", "webfetch"} {
		if hasRegisteredTool(generalReg, name) {
			t.Fatalf("default role unexpectedly received %s", name)
		}
	}
	if generalCaps.Mutation {
		t.Fatal("default role unexpectedly has mutation capability")
	}

	explorer := subagent.Role{Name: "explorer", Tools: append([]string(nil), defaults["explorer"].Tools...)}
	explorerReg, explorerCaps, err := cloneChildRegistry(parent, explorer, false)
	if err != nil {
		t.Fatal(err)
	}
	if hasRegisteredTool(explorerReg, "bash") || explorerCaps.Shell {
		t.Fatal("explorer role received bash")
	}
	if got := childPermissionService(rootPerm, explorerCaps, false); got == rootPerm {
		t.Fatal("read-only explorer unexpectedly inherited root permission service")
	}

	worker := subagent.Role{Name: "worker"}
	workerReg, workerCaps, err := cloneChildRegistry(parent, worker, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRegisteredTool(workerReg, "bash") || !workerCaps.Shell || hasRegisteredTool(workerReg, "write") || hasRegisteredTool(workerReg, "edit") {
		t.Fatal("worker default policy was not shell-only")
	}

	mutatingWorker := worker
	mutatingWorker.AllowMutation = true
	mutatingReg, mutatingCaps, err := cloneChildRegistry(parent, mutatingWorker, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRegisteredTool(mutatingReg, "bash") || !hasRegisteredTool(mutatingReg, "write") || !hasRegisteredTool(mutatingReg, "edit") || !mutatingCaps.Mutation {
		t.Fatal("dual mutation opt-in did not expose file mutation")
	}

	noGlobalMutationReg, noGlobalCaps, err := cloneChildRegistry(parent, mutatingWorker, false)
	if err != nil {
		t.Fatal(err)
	}
	if hasRegisteredTool(noGlobalMutationReg, "write") || hasRegisteredTool(noGlobalMutationReg, "edit") || noGlobalCaps.Mutation {
		t.Fatal("role mutation bypassed global mutation policy")
	}
}

func TestSubagentPromptExplainsRoleAndPermissionPolicy(t *testing.T) {
	for _, want := range []string{"default role", "general alias", "explorer role", "worker role", "permission-gated bash", "ask/allow/deny", "not sandboxed", "do not finish while relevant children are queued or running", "wait_agent with until=all", "one concise line per child"} {
		if !strings.Contains(subagentPromptGuidance, want) {
			t.Fatalf("subagent prompt missing %q", want)
		}
	}
}

func TestCloneChildRegistryHonorsParentToolAllowlist(t *testing.T) {
	parent := testChildParentRegistry(t)
	limited, err := tools.CloneRegistry(parent, func(desc tools.ToolDescriptor) bool {
		return desc.Schema.Name != "bash"
	})
	if err != nil {
		t.Fatal(err)
	}
	role := subagent.Role{Name: "default", Tools: []string{"read", "bash"}}
	child, caps, err := cloneChildRegistry(limited, role, false)
	if err != nil {
		t.Fatal(err)
	}
	if hasRegisteredTool(child, "bash") || caps.Shell {
		t.Fatal("child restored bash outside the parent tool allowlist")
	}
	if !hasRegisteredTool(child, "read") {
		t.Fatal("parent tool allowlist removed an unrelated read tool")
	}
}
