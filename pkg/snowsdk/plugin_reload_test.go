package snowsdk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSDKPluginReloadAndClosedSession(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"snow-plugin.json": `{"id":"demo","name":"Demo","version":"1","api_version":2,"entry":"main.js","capabilities":["commands"]}`, "main.js": `snow.registerCommand({name:"old",description:"old",run(){return "old";}});`} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(t.Context(), Options{CWD: path, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPlugins: map[string]plugin.JavaScriptSpec{"demo": {Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.WriteFile(filepath.Join(path, "main.js"), []byte(`snow.registerCommand({name:"new",description:"new",run(){return "new";}});`), 0600); err != nil {
		t.Fatal(err)
	}
	var result protocol.PluginReloadResult
	deadline := time.Now().Add(3 * time.Second)
	for {
		result, err = s.ReloadPlugin(t.Context(), "demo")
		if err == nil || !strings.Contains(err.Error(), "readiness") || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil || !result.Applied {
		t.Fatalf("receipt=%+v err=%v", result, err)
	}
	commands, err := s.PluginCommands()
	if err != nil || len(commands) != 1 || commands[0].Name != "new" {
		t.Fatalf("commands=%+v %v", commands, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReloadPlugin(t.Context(), "demo"); err == nil {
		t.Fatal("closed session reloaded")
	}
}
