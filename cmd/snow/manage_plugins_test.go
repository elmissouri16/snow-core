package main

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
)

func cliJSFixture(t *testing.T, script string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"snow-plugin.json": `{"id":"demo","name":"Demo","version":"1","api_version":1,"entry":"main.js"}`, "main.js": script} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func TestJavaScriptManagementDoesNotExecuteUntilCheck(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	dir := cliJSFixture(t, `throw "intentional-check-error"`)
	if _, err := runCLI(t, "snow", "plugin", "add", dir); err != nil {
		t.Fatal(err)
	}
	output, err := runCLI(t, "snow", "plugin", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []pluginView
	if err = jsonv2.Unmarshal([]byte(output), &entries); err != nil || len(entries) != 1 {
		t.Fatalf("output=%s err=%v", output, err)
	}
	if _, err = runCLI(t, "snow", "plugin", "get", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err = runCLI(t, "snow", "plugin", "check", "demo"); err == nil || !strings.Contains(err.Error(), "intentional-check-error") {
		t.Fatalf("check=%v", err)
	}
	if _, err = runCLI(t, "snow", "plugin", "disable", "demo"); err != nil {
		t.Fatal(err)
	}
	path, _, _ := config.DefaultPaths()
	cfg, err := config.Load(path)
	if err != nil || !cfg.JavaScriptPlugins["demo"].Disabled {
		t.Fatalf("config=%+v err=%v", cfg.JavaScriptPlugins, err)
	}
	if _, err = runCLI(t, "snow", "plugin", "enable", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err = runCLI(t, "snow", "plugin", "remove", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "main.js")); err != nil {
		t.Fatal("remove deleted package files")
	}
}
func TestJavaScriptFlagAndCheckWithoutProvider(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	dir := cliJSFixture(t, `snow.registerTool({name:"echo",description:"echo",parameters:{},execute(){return {content:[]}}});`)
	if _, err := runCLI(t, "snow", "plugin", "add", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "snow", "plugin", "check", "demo", "--provider", "nonexistent-provider"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "snow", "--js-plugin", dir, "--provider", "fake", "--no-session", "--no-mcp", "--no-skills", "--no-subagents", "--thinking", "off", "-p", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "snow", "--js-plugin", "/nonexistent", "--no-plugins", "--provider", "fake", "--no-session", "--no-mcp", "--no-skills", "--no-subagents", "--thinking", "off", "-p", "hello"); err != nil {
		t.Fatal(err)
	}
}

func TestPluginScaffoldAndCommandRunner(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "hello")
	if _, err := runCLI(t, "snow", "plugin", "init", dir, "--typescript"); err != nil {
		t.Fatal(err)
	}
	output, err := runCLI(t, "snow", "--provider", "fake", "--no-session", "--no-mcp", "--no-skills", "--js-plugin", dir, "plugin", "run", "hello:hello", "--json", "--", "tester")
	if err != nil || !strings.Contains(output, "Hello tester") {
		t.Fatalf("command: %s %v", output, err)
	}
	if _, err := runCLI(t, "snow", "plugin", "init", dir); err == nil {
		t.Fatal("scaffold overwrote directory")
	}
}
