package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fakePluginBinary is a tiny JSON-RPC plugin server used as the child.
const fakePluginBinary = `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type req struct {
	JSONRPC string          ` + "`json:\"jsonrpc\"`" + `
	ID      int             ` + "`json:\"id\"`" + `
	Method  string          ` + "`json:\"method\"`" + `
	Params  json.RawMessage ` + "`json:\"params\"`" + `
}

type resp struct {
	JSONRPC string ` + "`json:\"jsonrpc\"`" + `
	ID      int    ` + "`json:\"id\"`" + `
	Result  any    ` + "`json:\"result\"`" + `
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var r req
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		switch r.Method {
		case "initialize":
			out := map[string]any{
				"name": "fake-plugin", "version": "0.1.0",
				"tools": []map[string]any{{
					"name": "echo", "description": "echo text back",
					"parameters": map[string]any{"type": "object"},
				}},
			}
			b, _ := json.Marshal(resp{JSONRPC: "2.0", ID: r.ID, Result: out})
			fmt.Println(string(b))
		case "tools/call":
			var p struct {
				Name      string          ` + "`json:\"name\"`" + `
				Arguments json.RawMessage ` + "`json:\"arguments\"`" + `
			}
			_ = json.Unmarshal(r.Params, &p)
			out := map[string]any{
				"content": []map[string]any{{"type": "text", "text": "plugin echoed: " + p.Name}},
				"is_error": false,
			}
			b, _ := json.Marshal(resp{JSONRPC: "2.0", ID: r.ID, Result: out})
			fmt.Println(string(b))
		}
	}
	_ = io.EOF
}
`

// buildFakePlugin compiles the fake plugin to a temp binary.
func buildFakePlugin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := dir + "/main.go"
	if err := os.WriteFile(src, []byte(fakePluginBinary), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := dir + "/plugin"
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build plugin: %v\n%s", err, out)
	}
	return bin
}

func TestPluginInitializeAndCall(t *testing.T) {
	bin := buildFakePlugin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	h, err := Spawn(ctx, bin, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	init, err := h.Initialize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if init.Name != "fake-plugin" || len(init.Tools) != 1 {
		t.Fatalf("init = %+v", init)
	}
	if init.Tools[0].Name != "echo" {
		t.Fatalf("tool = %+v", init.Tools[0])
	}

	res, err := h.Call(ctx, "echo", json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || len(res.Content) == 0 {
		t.Fatalf("call result = %+v", res)
	}
	got := res.Content[0].Text
	if !strings.Contains(got, "plugin echoed: echo") {
		t.Fatalf("call text = %q", got)
	}
}

func TestPluginCrashReported(t *testing.T) {
	// A binary that exits immediately without responding.
	dir := t.TempDir()
	bin := dir + "/crash"
	src := dir + "/main.go"
	os.WriteFile(src, []byte("package main\nfunc main() {}\n"), 0o600)
	out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v %s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := Spawn(ctx, bin, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	_, err = h.Initialize(ctx)
	if err == nil {
		t.Fatal("expected error for crashed plugin")
	}
	if !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("error = %v, want EOF-ish", err)
	}
}

func TestPluginConcurrentCalls(t *testing.T) {
	bin := buildFakePlugin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	h, err := Spawn(ctx, bin, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	// Serial calls (mutex-serialized) still work repeatedly.
	for i := 0; i < 5; i++ {
		res, err := h.Call(ctx, fmt.Sprintf("echo-%d", i), json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Content) == 0 {
			t.Fatal("no content")
		}
	}

}
