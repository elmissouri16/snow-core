package rpc

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func TestJavaScriptDiagnosticsStayInsideRPCFrames(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"snow-plugin.json": `{"id":"rpc-test","name":"RPC test","version":"1","api_version":1,"entry":"main.js"}`, "main.js": `snow.log("info","plugin-log-marker");`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, CWD: dir, JavaScriptPlugins: map[string]plugin.JavaScriptSpec{"rpc-test": {Path: dir}}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader("{\"id\":\"d1\",\"type\":\"diagnostics\"}\n"), &out)
	if err = srv.Serve(t.Context()); err != nil {
		t.Fatal(err)
	}
	found := false
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		var frame map[string]any
		if err = jsonv2.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("non-JSON RPC output: %q", line)
		}
		if strings.Contains(line, "plugin-log-marker") {
			found = true
			if frame["type"] != "response" {
				t.Fatalf("log escaped diagnostics response: %s", line)
			}
		}
	}
	if !found {
		t.Fatalf("plugin diagnostics missing: %s", out.String())
	}
}
