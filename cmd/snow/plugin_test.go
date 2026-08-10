package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	publicplugin "github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
)

const pluginCheckFixture = `package main
import ("bufio"; "encoding/json"; "fmt"; "os")
type request struct { ID string ` + "`json:\"id\"`" + `; Method string ` + "`json:\"method\"`" + ` }
func send(id string, result any) { data,_:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":id,"result":result}); fmt.Println(string(data)) }
func main() { fmt.Fprintln(os.Stderr,"{\"token\": \"fixture-secret\", \"status\":\"startup\"}"); scanner:=bufio.NewScanner(os.Stdin); for scanner.Scan() { var req request; if json.Unmarshal(scanner.Bytes(),&req)!=nil { return }; switch req.Method { case "initialize": send(req.ID,map[string]any{"manifest":map[string]any{"id":"check","name":"Check Fixture","version":"1.2.3","protocol_version":2,"capabilities":[]string{"base"}},"capabilities":[]string{"runtime"},"supported_events":[]string{"tool_end"},"limits":map[string]int{"calls":4}}); case "tools/list": send(req.ID,map[string]any{"tools":[]any{map[string]any{"name":"lookup","description":"lookup","parameters":map[string]any{"type":"object"},"risk":"network","capabilities":[]string{"lookup"}}}}); case "shutdown": send(req.ID,map[string]any{}); return } } }
`

func TestInspectExternalPlugin(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain unavailable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	binary := filepath.Join(dir, "plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
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
