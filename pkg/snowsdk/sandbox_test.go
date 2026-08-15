package snowsdk

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-core/snow/internal/config"
	internalsandbox "github.com/snow-core/snow/internal/sandbox"
)

func TestSandboxStatusAndExplicitSDKPolicy(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	fake := filepath.Join(home, "smolvm")
	script := `#!/bin/sh
case "$*" in
  "--version") echo "smolvm 1.8.1" ;;
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
	configPath := filepath.Join(home, "config.json")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	manager, err := internalsandbox.New(internalsandbox.Options{
		ProjectRoot: project, StatePath: filepath.Join(home, "sandboxes.json"), Executable: fake,
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Init(context.Background(), internalsandbox.InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}

	session, err := Open(context.Background(), Options{
		CWD: project, ConfigPath: configPath, Provider: "fake", NoSession: true,
		PermissionMode: "deny", NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	status := session.SandboxStatus()
	if !status.Configured || !status.Active || status.Backend != "smolvm" || status.GuestCWD != "/workspace" {
		t.Fatalf("inherited sandbox status = %+v", status)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}

	hostSession, err := Open(context.Background(), Options{
		CWD: project, ConfigPath: configPath, Provider: "fake", NoSession: true,
		PermissionMode: "deny", NoPlugins: true, NoMCP: true, NoSkills: true,
		DisableSandbox: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	status = hostSession.SandboxStatus()
	if status.Configured || status.Active || status.Backend != "host" {
		t.Fatalf("explicit host override status = %+v", status)
	}
	_ = hostSession.Close()
}

func TestSDKRequireSandboxAndPolicyConflict(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	base := Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true}
	base.RequireSandbox = true
	if _, err := Open(context.Background(), base); err == nil {
		t.Fatal("RequireSandbox accepted an uninitialized project")
	}
	base.DisableSandbox = true
	if _, err := Open(context.Background(), base); err == nil {
		t.Fatal("conflicting sandbox policies were accepted")
	}
}
