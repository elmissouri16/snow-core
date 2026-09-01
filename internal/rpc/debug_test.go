package rpc

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
)

func TestRPCDebugControlsAndDump(t *testing.T) {
	t.Setenv("SNOW_HOME", filepath.Join(t.TempDir(), "snow-home"))
	a, err := app.New(t.Context(), app.Options{Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: t.TempDir()})
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
	if err := New(t.Context(), a, &in, &out).Serve(t.Context()); err != nil {
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
	persisted, err := config.Load(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.Debug.Enabled {
		t.Fatal("debug_enable did not persist debug.enabled")
	}
}
