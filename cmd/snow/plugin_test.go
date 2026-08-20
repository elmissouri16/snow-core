package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/pluginsdk"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const pluginCheckFixture = `package main
import ("bufio"; "encoding/json"; "fmt"; "os")
type request struct { ID string ` + "`json:\"id\"`" + `; Method string ` + "`json:\"method\"`" + ` }
func send(id string, result any) { data,_:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":id,"result":result}); fmt.Println(string(data)) }
func main() { fmt.Fprintln(os.Stderr,"{\"token\": \"fixture-secret\", \"status\":\"startup\"}"); scanner:=bufio.NewScanner(os.Stdin); for scanner.Scan() { var req request; if json.Unmarshal(scanner.Bytes(),&req)!=nil { return }; switch req.Method { case "initialize": send(req.ID,map[string]any{"manifest":map[string]any{"id":"check","name":"Check Fixture","version":"1.2.3","protocol_version":2,"capabilities":[]string{"base"}},"capabilities":[]string{"runtime"},"supported_events":[]string{"tool_end"},"limits":map[string]int{"calls":4}}); case "tools/list": send(req.ID,map[string]any{"tools":[]any{map[string]any{"name":"lookup","description":"lookup","parameters":map[string]any{"type":"object"},"risk":"network","capabilities":[]string{"lookup"}}}}); case "shutdown": send(req.ID,map[string]any{}); return } } }
`

func TestPluginHumanOutputEscapesTerminalControls(t *testing.T) {
	if got := terminalSafe("ok\x1b[31m\nnext"); got != `ok\x1b[31m\x0anext` {
		t.Fatalf("terminalSafe = %q", got)
	}
}

func TestPluginManagementLifecycleIsDisabledByDefaultAndRedacted(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest := `{"id":"managed","command":["python3","plugin.py","--token=command-secret","-H","Authorization: Bearer header-secret","--credential","credential-secret","--cookie=cookie-secret"],"enabled":true,"env":["TOKEN=environment-secret"],"config":{"secret":"runtime-secret"}}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runPluginCommand(configPath, "plugin", "add", manifestPath, "--json")
	if err != nil {
		t.Fatalf("add: %v, stderr=%s", err, stderr)
	}
	var receipt commandReceipt
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil || receipt.Action != "added" || receipt.Name != "managed" {
		t.Fatalf("add receipt = %+v, err=%v, raw=%q", receipt, err, stdout)
	}
	declarations, err := config.LoadPluginDeclarations(configPath)
	if err != nil || len(declarations) != 1 || declarations[0].Enabled {
		t.Fatalf("staged declarations = %+v, %v", declarations, err)
	}

	stdout, stderr, err = runPluginCommand(configPath, "plugin", "list", "--json")
	if err != nil {
		t.Fatalf("list: %v, stderr=%s", err, stderr)
	}
	for _, secret := range []string{"command-secret", "header-secret", "credential-secret", "cookie-secret", "environment-secret", "runtime-secret"} {
		if strings.Contains(stdout, secret) {
			t.Fatalf("list leaked %s: %s", secret, stdout)
		}
	}
	var views []pluginConfigView
	if err := json.Unmarshal([]byte(stdout), &views); err != nil || len(views) != 1 || views[0].Enabled || views[0].Env[0] != "TOKEN=[redacted]" || !views[0].ConfigPresent {
		t.Fatalf("list views = %+v, err=%v, raw=%s", views, err, stdout)
	}

	if _, stderr, err = runPluginCommand(configPath, "plugin", "enable", "managed", "--json"); err != nil {
		t.Fatalf("enable: %v, stderr=%s", err, stderr)
	}
	declarations, _ = config.LoadPluginDeclarations(configPath)
	if !declarations[0].Enabled {
		t.Fatal("enable did not persist")
	}
	if _, stderr, err = runPluginCommand(configPath, "plugin", "disable", "managed", "--json"); err != nil {
		t.Fatalf("disable: %v, stderr=%s", err, stderr)
	}
	if _, stderr, err = runPluginCommand(configPath, "plugin", "remove", "managed", "--json"); err != nil {
		t.Fatalf("remove: %v, stderr=%s", err, stderr)
	}
	declarations, _ = config.LoadPluginDeclarations(configPath)
	if len(declarations) != 0 {
		t.Fatalf("remove left declarations: %+v", declarations)
	}
}

func TestProjectPluginEnableRequiresExplicitProjectDeclaration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	project := t.TempDir()
	t.Chdir(project)
	globalConfig := filepath.Join(home, "config.json")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	global := `{"plugins":[{"id":"sensitive","command":["runtime","--auth=global-secret"],"enabled":true,"env":["TOKEN=global-secret"],"config":{"secret":"global-secret"}}]}`
	if err := os.WriteFile(globalConfig, []byte(global), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runPluginCommand("", "plugin", "enable", "sensitive", "--project", "--json")
	if err == nil || !strings.Contains(err.Error(), "target scope") {
		t.Fatalf("project enable error = %v", err)
	}
	projectConfig := filepath.Join(project, ".snow", "config.json")
	if data, readErr := os.ReadFile(projectConfig); readErr == nil && strings.Contains(string(data), "global-secret") {
		t.Fatalf("project toggle copied global secrets: %s", data)
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
}

func TestPluginListShowsGlobalProjectExplicitPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	project := t.TempDir()
	t.Chdir(project)
	configPath := filepath.Join(home, "config.json")
	global := `{"default_project_trust":"allow","plugins":[{"id":"layered","command":["global"],"enabled":true}]}`
	if err := os.WriteFile(configPath, []byte(global), 0o600); err != nil {
		t.Fatal(err)
	}
	projectConfig := filepath.Join(project, ".snow", "config.json")
	if err := os.MkdirAll(filepath.Dir(projectConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectConfig, []byte(`{"plugins":[{"id":"layered","command":["project"],"enabled":false}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	explicitManifest := filepath.Join(project, "explicit.json")
	if err := os.WriteFile(explicitManifest, []byte(`{"id":"layered","command":["explicit"],"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runPluginCommand(configPath, "--plugin", explicitManifest, "plugin", "list", "--all", "--json")
	if err != nil {
		t.Fatalf("list: %v, stderr=%s", err, stderr)
	}
	var views []pluginConfigView
	if err := json.Unmarshal([]byte(stdout), &views); err != nil {
		t.Fatalf("decode views: %v, raw=%s", err, stdout)
	}
	if len(views) != 3 || views[0].Scope != "global" || !views[0].Shadowed || views[1].Scope != "project" || !views[1].Shadowed || views[2].Scope != "explicit" || views[2].Shadowed {
		t.Fatalf("precedence views = %+v", views)
	}
}

func TestPluginListDoesNotStartConfiguredProcess(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	sentinel := filepath.Join(dir, "started")
	spec := publicplugin.PluginSpec{ID: "sentinel", Command: []string{"sh", "-c", "touch " + sentinel}, Enabled: true}
	if err := config.AddPlugin(configPath, true, spec, false); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runPluginCommand(configPath, "plugin", "list", "--json")
	if err != nil {
		t.Fatalf("list: %v, stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "sentinel") {
		t.Fatalf("missing configured plugin: %s", stdout)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("plugin list started executable: %v", err)
	}
}

func TestPluginSDKVendorCommandCopiesEmbeddedRuntimeWithoutExecutingIt(t *testing.T) {
	pluginDir := t.TempDir()
	stdout, stderr, err := runPluginCommand("", "--mode=json", "plugin", "sdk", "vendor", "--runtime", "python", pluginDir)
	if err != nil {
		t.Fatalf("vendor: %v, stderr=%s", err, stderr)
	}
	var receipt pluginsdk.Receipt
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatalf("receipt: %v, raw=%s", err, stdout)
	}
	if receipt.Runtime != pluginsdk.RuntimePython || receipt.Replaced || len(receipt.Files) == 0 || stderr != "" {
		t.Fatalf("receipt=%+v stderr=%q", receipt, stderr)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "vendor", "python", "snow_plugin", "runtime.py")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("vendor command unexpectedly created a manifest: %v", err)
	}

	if _, _, err := runPluginCommand("", "plugin", "sdk", "vendor", "--runtime", "python", pluginDir, "--json"); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("duplicate vendor error = %v", err)
	}
	stdout, stderr, err = runPluginCommand("", "plugin", "sdk", "vendor", "--runtime", "python", pluginDir, "--replace", "--json")
	if err != nil {
		t.Fatalf("replace: %v, stderr=%s", err, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil || !receipt.Replaced {
		t.Fatalf("replacement receipt=%+v err=%v raw=%s", receipt, err, stdout)
	}
}

func TestPluginSDKVendorCommandValidatesRuntime(t *testing.T) {
	_, _, err := runPluginCommand("", "plugin", "sdk", "vendor", "--runtime", "ruby", t.TempDir(), "--json")
	if err == nil || !strings.Contains(err.Error(), "python or javascript") {
		t.Fatalf("runtime error = %v", err)
	}
}

func runPluginCommand(configPath string, args ...string) (string, string, error) {
	root := &cobra.Command{Use: "snow", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", "", "")
	root.PersistentFlags().String("mode", "", "")
	root.PersistentFlags().StringArray("plugin", nil, "")
	root.AddCommand(pluginCmd())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	commandArgs := append([]string(nil), args...)
	if configPath != "" {
		commandArgs = append([]string{"--config", configPath}, commandArgs...)
	}
	root.SetArgs(commandArgs)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestInspectExternalPlugin(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain unavailable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	binary := filepath.Join(dir, "plugin")
	if err := os.WriteFile(source, []byte(pluginCheckFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(goBin, "build", "-o", binary, source).CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := inspectExternalPlugin(ctx, publicplugin.PluginSpec{ID: "check", Command: []string{binary}, Capabilities: []string{"configured"}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Valid || view.ID != "check" || view.ProtocolVersion != publicplugin.ProtocolVersion {
		t.Fatalf("view = %+v", view)
	}
	if strings.Join(view.Capabilities, ",") != "base,configured,runtime" {
		t.Fatalf("capabilities = %+v", view.Capabilities)
	}
	if len(view.Tools) != 1 || view.Tools[0].Name != "lookup" || view.Tools[0].Risk != "network" || strings.Join(view.Tools[0].Capabilities, ",") != "base,configured,lookup,runtime" {
		t.Fatalf("tools = %+v", view.Tools)
	}
	if len(view.SupportedEvents) != 1 || view.SupportedEvents[0] != publicplugin.EventToolEnd {
		t.Fatalf("events = %+v", view.SupportedEvents)
	}
	if view.Diagnostics != "[REDACTED]" || strings.Contains(view.Diagnostics, "fixture-secret") {
		t.Fatalf("diagnostics = %q", view.Diagnostics)
	}

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	printPluginCheck(cmd, view)
	printed := output.String()
	for _, want := range []string{"Status: valid", "configured", "lookup [risk=network", "capabilities=base,configured,lookup,runtime", string(protocol.EvToolEnd), "calls=4", "[REDACTED]"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("output missing %q:\n%s", want, printed)
		}
	}
}
