package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/permission"
	internalsandbox "github.com/snow-core/snow/internal/sandbox"
)

func TestAppRejectsCorruptSandboxAuthorityState(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "sandboxes.json"), []byte(`{"version":1,"projects":{"relative":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(context.Background(), Options{
		CWD: project, Provider: "fake", NoSession: true, Permission: "allow",
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("corrupt sandbox authority was treated as host execution: %v", err)
	}
	hostOnly, err := New(context.Background(), Options{
		CWD: project, Provider: "fake", NoSession: true, Permission: "allow",
		NoPlugins: true, NoMCP: true, NoSkills: true, DisableSandbox: true,
	})
	if err != nil {
		t.Fatalf("explicit host escape still loaded corrupt sandbox state: %v", err)
	}
	defer hostOnly.Close()
	if status := hostOnly.SandboxStatus(); status.Active || status.Configured || status.Backend != "host" {
		t.Fatalf("explicit host status = %+v", status)
	}
}

func TestAppRoutesBashThroughConfiguredProjectSandbox(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	fake := filepath.Join(home, "smolvm")
	script := `#!/bin/sh
case "$*" in
  "--version") echo "smolvm 1.8.1" ;;
  *"machine exec"*) echo "sandboxed-command" ;;
  *"machine status"*) echo "State: Running" ;;
  *) echo "ok" ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "mkfs.ext4"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", home+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := config.Default()
	cfg.Sandbox.Executable = fake
	cfg.PermissionMode = "allow"
	configPath := filepath.Join(home, "config.json")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(home, "sandboxes.json")
	manager, err := internalsandbox.New(internalsandbox.Options{
		ProjectRoot: project, StatePath: statePath, Executable: fake,
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Init(context.Background(), internalsandbox.InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}

	runtime, err := New(context.Background(), Options{
		CWD: project, ConfigPath: configPath, SandboxStatePath: statePath,
		Provider: "fake", Permission: "allow", NoSession: true,
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if runtime.Sandbox == nil || !runtime.Sandbox.Active() {
		t.Fatal("app did not activate configured sandbox")
	}
	tool, ok := runtime.Registry.Get("bash")
	if !ok {
		t.Fatal("bash tool missing")
	}
	args, _ := json.Marshal(map[string]any{"command": "printf host-command"})
	result, err := tool.Run(context.Background(), args, &toolHost{
		cwd: project, roots: []string{project}, perm: permission.NewService(permission.ModeAllow, nil), reg: runtime.Registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "sandboxed-command") || strings.Contains(result.Content[0].Text, "host-command") {
		t.Fatalf("sandboxed Bash result = %+v", result)
	}
}
