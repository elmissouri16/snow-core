package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestRPCDebugControlsAndDump(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	path := filepath.Join(t.TempDir(), "rpc-dump.json")
	params, _ := json.Marshal(map[string]string{"path": path})
	var in bytes.Buffer
	var out bytes.Buffer
	in.WriteString("{\"id\":\"enable\",\"type\":\"debug_enable\"}\n")
	in.WriteString("{\"id\":\"status\",\"type\":\"debug_status\"}\n")
	request, _ := json.Marshal(Request{ID: "dump", Type: "debug_dump", Params: params})
	in.Write(request)
	in.WriteByte('\n')
	if err := New(context.Background(), a, &in, &out).Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"enable", "status", "dump"} {
		var response Response
		if err := json.Unmarshal(rpcFrame(t, out.String(), "response", id), &response); err != nil {
			t.Fatal(err)
		}
		if !response.Success {
			t.Fatalf("%s response=%+v", id, response)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
