package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
)

const v2PluginSource = `package main
import ("bufio"; "encoding/json"; "fmt"; "os")
type request struct { JSONRPC string ` + "`json:\"jsonrpc\"`" + `; ID string ` + "`json:\"id\"`" + `; Method string ` + "`json:\"method\"`" + `; Params json.RawMessage ` + "`json:\"params\"`" + ` }
var eventCount int
func send(id string, result any) { b,_:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":id,"result":result}); fmt.Println(string(b)) }
func main() { sc:=bufio.NewScanner(os.Stdin); for sc.Scan() { var r request; if json.Unmarshal(sc.Bytes(),&r)!=nil { return }; if r.Method=="notifications/event" { eventCount++; continue }; if r.Method=="notifications/cancelled" { continue }; switch r.Method { case "initialize": send(r.ID,map[string]any{"manifest":map[string]any{"id":"v2","name":"v2","version":"1","protocol_version":2,"capabilities":[]string{"base"}},"supported_events":[]string{"tool_start"}}); case "tools/list": send(r.ID,map[string]any{"tools":[]any{map[string]any{"name":"echo","description":"echo","parameters":map[string]any{"type":"object"},"risk":"read","capabilities":[]string{"echo"}}}}); case "tools/call": var p struct{Name string ` + "`json:\"name\"`" + `; Arguments json.RawMessage ` + "`json:\"arguments\"`" + `}; _=json.Unmarshal(r.Params,&p); send(r.ID,map[string]any{"content":[]any{map[string]any{"type":"text","text":fmt.Sprintf("%s:%d",p.Name,eventCount)}},"details":map[string]any{"source":"v2"},"is_error":false}); case "shutdown": send(r.ID,map[string]any{}); return } } }
`

func buildV2Plugin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := dir + "/main.go"
	if err := os.WriteFile(src, []byte(v2PluginSource), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := dir + "/plugin"
	out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput()
	if err != nil {
		t.Fatalf("build v2 plugin: %v\n%s", err, out)
	}
	return bin
}

func TestExternalHostV2ConcurrentStringCorrelation(t *testing.T) {
	bin := buildV2Plugin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := SpawnExternal(ctx, publicplugin.PluginSpec{ID: "v2", Command: []string{bin}, Enabled: true}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(context.Background())
	init, err := h.Initialize(ctx, "test", t.TempDir(), "session", nil)
	if err != nil {
		t.Fatal(err)
	}
	if init.Manifest.ID != "v2" || len(init.Tools) != 1 {
		t.Fatalf("init = %+v", init)
	}
	if !h.SupportsEvent(publicplugin.EventToolStart) || h.SupportsEvent(publicplugin.EventTextDelta) {
		t.Fatalf("supported events = %+v", init.SupportedEvents)
	}
	if init.Tools[0].Risk != "read" || len(init.Tools[0].Capabilities) != 1 {
		t.Fatalf("tool metadata = %+v", init.Tools[0])
	}
	init.Manifest.Capabilities[0] = "mutated"
	gotManifest := h.Manifest()
	if len(gotManifest.Capabilities) != 1 || gotManifest.Capabilities[0] != "base" {
		t.Fatalf("manifest alias = %+v", gotManifest)
	}
	gotManifest.Capabilities[0] = "mutated-again"
	if got := h.Manifest(); got.Capabilities[0] != "base" {
		t.Fatalf("manifest return alias = %+v", got)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := h.Call(ctx, "echo", fmt.Sprintf("call-%d", i), json.RawMessage(`{"i":1}`), nil)
			if err != nil {
				errs <- err
				return
			}
			if len(got.Content) != 1 || !strings.Contains(got.Content[0].Text, "echo") || !strings.Contains(string(got.Details), `"source":"v2"`) {
				errs <- fmt.Errorf("result = %+v", got)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestValidateExternalSchemaRejectsInvalidRisk(t *testing.T) {
	err := validateSchemas([]publicplugin.ExternalToolDefinition{{
		Name: "unsafe", Parameters: json.RawMessage(`{"type":"object"}`), Risk: "admin",
	}})
	if err == nil || !strings.Contains(err.Error(), "invalid risk") {
		t.Fatalf("error = %v", err)
	}
}

func TestProgressNotificationRequiresCallID(t *testing.T) {
	called := 0
	host := &ExternalHost{maxProgress: 1024, pending: map[string]*pendingV2{
		"1": {callID: "call-1", progress: func(ProgressNotification) { called++ }},
		"2": {callID: "call-2", progress: func(ProgressNotification) { called++ }},
	}}
	host.notification("notifications/progress", json.RawMessage(`{"message":"missing"}`))
	if called != 0 {
		t.Fatalf("empty call id reached %d callbacks", called)
	}
	host.notification("notifications/progress", json.RawMessage(`{"call_id":"call-1","message":"ok"}`))
	if called != 1 {
		t.Fatalf("targeted progress reached %d callbacks", called)
	}
}

func TestRedactDiagnosticsCredentialForms(t *testing.T) {
	input := strings.Join([]string{
		`token=fixture-one`,
		`"token": "fixture-two", "status": "ok"`,
		`Authorization: Bearer fixture-three`,
		`password = fixture-four`,
		`api_key:'fixture-five'; retained`,
		`OPENAI_API_KEY=fixture-six`,
		`GITHUB_TOKEN=fixture-seven`,
		`X-API-Key: fixture-eight`,
		`Proxy-Authorization: Bearer fixture-nine`,
		`{"token":"abc\"fixture-ten"}`,
		`ordinary diagnostic`,
	}, "\n")
	got := redactDiagnostics(input)
	for _, secret := range []string{"fixture-one", "fixture-two", "fixture-three", "fixture-four", "fixture-five", "fixture-six", "fixture-seven", "fixture-eight", "fixture-nine", "fixture-ten"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redaction leaked %q: %s", secret, got)
		}
	}
	if strings.Count(got, "[REDACTED]") != 10 || strings.Contains(got, `"status": "ok"`) || !strings.Contains(got, "ordinary diagnostic") {
		t.Fatalf("redaction = %q", got)
	}
}
