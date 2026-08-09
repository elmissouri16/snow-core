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

	publicplugin "github.com/snow-core/snow/pkg/plugin"
)

const v2PluginSource = `package main
import ("bufio"; "encoding/json"; "fmt"; "os")
type request struct { JSONRPC string ` + "`json:\"jsonrpc\"`" + `; ID string ` + "`json:\"id\"`" + `; Method string ` + "`json:\"method\"`" + `; Params json.RawMessage ` + "`json:\"params\"`" + ` }
func send(id string, result any) { b,_:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":id,"result":result}); fmt.Println(string(b)) }
func main() { sc:=bufio.NewScanner(os.Stdin); for sc.Scan() { var r request; if json.Unmarshal(sc.Bytes(),&r)!=nil { return }; switch r.Method { case "initialize": send(r.ID,map[string]any{"manifest":map[string]any{"id":"v2","name":"v2","version":"1","protocol_version":2}}); case "tools/list": send(r.ID,map[string]any{"tools":[]any{map[string]any{"name":"echo","description":"echo","parameters":map[string]any{"type":"object"}}}}); case "tools/call": var p struct{Name string ` + "`json:\"name\"`" + `; Arguments json.RawMessage ` + "`json:\"arguments\"`" + `}; _=json.Unmarshal(r.Params,&p); send(r.ID,map[string]any{"content":[]any{map[string]any{"type":"text","text":p.Name}},"is_error":false}); case "shutdown": send(r.ID,map[string]any{}); return } } }
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
			if len(got.Content) != 1 || !strings.Contains(got.Content[0].Text, "echo") {
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
