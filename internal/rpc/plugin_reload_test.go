package rpc

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestRPCPluginReloadReceiptAndValidation(t *testing.T) {
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
	a, err := app.New(t.Context(), app.Options{CWD: path, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	s := New(t.Context(), a, &bytes.Buffer{}, &out)
	if err := os.WriteFile(filepath.Join(path, "main.js"), []byte(`snow.registerCommand({name:"new",description:"new",run(){return "new";}});`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, `null`, `{"id":true}`, `{"id":"demo","extra":1}`} {
		if err := s.handle(t.Context(), Request{Type: "plugin_reload", Params: []byte(raw)}); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if err := s.handle(t.Context(), Request{ID: "reload", Type: "plugin_reload", Params: []byte(`{"id":"demo"}`)}); err != nil {
		t.Fatal(err)
	}
	s.promptWG.Wait()
	responses := decodeRuntimeResponses(t, &out)
	if len(responses) != 1 || responses[0]["success"] != true || responses[0]["id"] != "reload" {
		t.Fatalf("responses=%+v", responses)
	}
	result := responses[0]["data"].(map[string]any)
	if result["applied"] != true || result["plugin_id"] != "demo" || result["fingerprint"] == "" {
		t.Fatalf("receipt=%+v", result)
	}
	commands := a.PluginCommands()
	if len(commands) != 1 || commands[0].Name != "new" {
		t.Fatalf("commands=%+v", commands)
	}
}
