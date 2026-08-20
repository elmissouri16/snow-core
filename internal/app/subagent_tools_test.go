package app

import (
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/subagent"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/tools/builtin"
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

	general := subagent.Role{Name: "general", Tools: append([]string(nil), defaults["general"].Tools...)}
	generalReg, generalCaps, err := cloneChildRegistry(parent, general, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRegisteredTool(generalReg, "bash") || !generalCaps.Shell {
		t.Fatal("general role did not receive bash")
	}
	rootPerm := permission.NewService(permission.ModeAllow, nil)
	if got := childPermissionService(rootPerm, generalCaps, false); got != rootPerm {
		t.Fatal("shell-capable child did not inherit root permission service")
	}
	for _, name := range []string{"read", "grep", "glob", "activate_skill", "read_skill_resource"} {
		if hasRegisteredTool(parent, name) && !hasRegisteredTool(generalReg, name) {
			t.Fatalf("general role missing %s", name)
		}
	}
	for _, name := range []string{"write", "edit", "ask_user", "request_user_input", "update_plan", "webfetch"} {
		if hasRegisteredTool(generalReg, name) {
			t.Fatalf("general role unexpectedly received %s", name)
		}
	}
	if generalCaps.Mutation {
		t.Fatal("general role unexpectedly has mutation capability")
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

	implementer := subagent.Role{Name: "implementer"}
	implementerReg, implementerCaps, err := cloneChildRegistry(parent, implementer, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRegisteredTool(implementerReg, "bash") || !implementerCaps.Shell || hasRegisteredTool(implementerReg, "write") || hasRegisteredTool(implementerReg, "edit") {
		t.Fatal("implementer default policy was not shell-only")
	}

	mutatingImplementer := implementer
	mutatingImplementer.AllowMutation = true
	mutatingReg, mutatingCaps, err := cloneChildRegistry(parent, mutatingImplementer, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRegisteredTool(mutatingReg, "bash") || !hasRegisteredTool(mutatingReg, "write") || !hasRegisteredTool(mutatingReg, "edit") || !mutatingCaps.Mutation {
		t.Fatal("dual mutation opt-in did not expose file mutation")
	}

	noGlobalMutationReg, noGlobalCaps, err := cloneChildRegistry(parent, mutatingImplementer, false)
	if err != nil {
		t.Fatal(err)
	}
	if hasRegisteredTool(noGlobalMutationReg, "write") || hasRegisteredTool(noGlobalMutationReg, "edit") || noGlobalCaps.Mutation {
		t.Fatal("role mutation bypassed global mutation policy")
	}
}

func TestSubagentPromptExplainsRoleAndPermissionPolicy(t *testing.T) {
	for _, want := range []string{"list_subagent_models", "never guess model IDs", "name for the child's identity", "task for its assignment", "role for its capability profile", "general role", "explorer role", "implementer role", "permission-gated bash", "ask/allow/deny", "not sandboxed", "do not finish while relevant children are queued or running", "wait_agent with until=all", "one concise line per child"} {
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
	role := subagent.Role{Name: "general", Tools: []string{"read", "bash"}}
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
