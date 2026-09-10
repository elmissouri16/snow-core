package snowsdk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func TestSDKPluginManagementAcrossSessions(t *testing.T) {
	home := t.TempDir()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SNOW_HOME", home)
	for name, content := range map[string]string{
		"snow-plugin.json": `{"id":"demo","name":"Demo","version":"1","api_version":2,"entry":"main.js","capabilities":["commands"]}`,
		"main.js":          `snow.registerCommand({name:"run",description:"run",run(){return "ok";}});`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.UpdateJavaScriptPlugins(filepath.Join(home, "config.json"), true, func(specs map[string]plugin.JavaScriptSpec) error {
		specs["demo"] = plugin.JavaScriptSpec{Path: dir, Disabled: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	opts := Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, CWD: t.TempDir(), PermissionMode: "deny"}
	s, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	statuses, err := s.PluginStatuses()
	if err != nil || len(statuses) != 1 || statuses[0].Enabled || statuses[0].Loaded {
		t.Fatalf("statuses=%+v err=%v", statuses, err)
	}
	status, err := s.SetPluginEnabled(t.Context(), "demo", true)
	if err != nil || !status.Enabled || status.Loaded || !status.RestartRequired {
		t.Fatalf("enabled=%+v err=%v", status, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PluginStatuses(); err == nil {
		t.Fatalf("closed list err=%v", err)
	}
	if _, err := s.SetPluginEnabled(t.Context(), "demo", false); err == nil {
		t.Fatalf("closed toggle err=%v", err)
	}
	restarted, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if _, err := restarted.RunPluginCommand(t.Context(), "demo:run", ""); err != nil {
		t.Fatalf("enabled command unavailable after reopen: %v", err)
	}
	status, err = restarted.SetPluginEnabled(t.Context(), "demo", false)
	if err != nil || status.Enabled || !status.Loaded || !status.RestartRequired {
		t.Fatalf("disabled=%+v err=%v", status, err)
	}
}
