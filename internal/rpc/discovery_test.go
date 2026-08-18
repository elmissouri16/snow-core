package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/app"
)

func TestRPCDiscoveryCommandsReturnSecretFreeSnapshots(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{
		Provider: "fake", NoSession: true, CWD: t.TempDir(), Permission: "allow",
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	input := strings.Join([]string{
		`{"id":"mcp1","type":"mcp_servers"}`,
		`{"id":"sk1","type":"skills"}`,
		`{"id":"sb1","type":"sandbox_status"}`,
		"",
	}, "\n")
	var out bytes.Buffer
	if err := New(context.Background(), a, strings.NewReader(input), &out).Serve(context.Background()); err != nil {
		t.Fatal(err)
	}

	must := func(id string) Response {
		t.Helper()
		var resp Response
		if err := json.Unmarshal(rpcFrame(t, out.String(), "response", id), &resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Success {
			t.Fatalf("%s failed: %+v", id, resp)
		}
		return resp
	}

	mcpData, ok := must("mcp1").Data.(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers data has type %T", must("mcp1").Data)
	}
	servers, ok := mcpData["servers"].([]any)
	if !ok || len(servers) != 0 {
		t.Fatalf("mcp servers = %#v", mcpData["servers"])
	}

	skillData, ok := must("sk1").Data.(map[string]any)
	if !ok {
		t.Fatalf("skills data has type %T", must("sk1").Data)
	}
	skills, ok := skillData["skills"].([]any)
	if !ok || len(skills) != 0 {
		t.Fatalf("skills = %#v", skillData["skills"])
	}
	if diagnostics, ok := skillData["diagnostics"]; ok && diagnostics == nil {
		t.Fatal("skill diagnostics must not encode null")
	}

	sandboxData, ok := must("sb1").Data.(map[string]any)
	if !ok {
		t.Fatalf("sandbox_status data has type %T", must("sb1").Data)
	}
	status, ok := sandboxData["status"].(map[string]any)
	if !ok || status["backend"] != "host" || status["configured"] != false || status["active"] != false {
		t.Fatalf("sandbox status = %#v", sandboxData["status"])
	}
}
