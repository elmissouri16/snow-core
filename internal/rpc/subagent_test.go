package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestRPCSubagentCommandsAndFraming(t *testing.T) {
	enabled := true
	a, err := app.New(context.Background(), app.Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "allow", Subagents: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	input := strings.Join([]string{`{"id":"r","type":"subagent_ready"}`, `{"id":"s","type":"subagent_spawn","params":{"name":"rpc","task":"inspect","fork_turns":"none"}}`, `{"id":"l","type":"subagent_list"}`}, "\n") + "\n"
	var out bytes.Buffer
	s := New(context.Background(), a, strings.NewReader(input), &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("output=%q", out.String())
	}
	seen := map[string]bool{}
	for _, line := range lines {
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("interleaved frame %q: %v", line, err)
		}
		if resp.ID != "" {
			seen[resp.ID] = resp.Success
		}
	}
	for _, id := range []string{"r", "s", "l"} {
		if !seen[id] {
			t.Fatalf("missing success %s: %s", id, out.String())
		}
	}
}

func TestRPCSubagentWaitUntilAll(t *testing.T) {
	enabled := true
	a, err := app.New(context.Background(), app.Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "allow", Subagents: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	s := New(context.Background(), a, strings.NewReader(""), &out)
	for _, req := range []Request{
		{ID: "r", Type: "subagent_ready"},
		{ID: "s", Type: "subagent_spawn", Params: json.RawMessage(`{"name":"rpc_wait","task":"inspect","fork_turns":"none"}`)},
		{ID: "w", Type: "subagent_wait", Params: json.RawMessage(`{"timeout_ms":1000,"until":"all"}`)},
	} {
		if err := s.handle(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	s.promptWG.Wait()
	for _, req := range []Request{
		{ID: "c", Type: "subagent_close", Params: json.RawMessage(`{"target":"/root/rpc_wait"}`)},
		{ID: "rr", Type: "subagent_resume", Params: json.RawMessage(`{"target":"/root/rpc_wait"}`)},
	} {
		if err := s.handle(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	waitedAll := false
	lifecycle := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.ID == "w" && resp.Success {
			data, _ := resp.Data.(map[string]any)
			waitedAll, _ = data["all_terminal"].(bool)
		}
		if resp.ID == "c" || resp.ID == "rr" {
			lifecycle[resp.ID] = resp.Success
		}
	}
	if !waitedAll {
		t.Fatalf("subagent_wait until=all did not report terminal descendants: %s", out.String())
	}
	if !lifecycle["c"] || !lifecycle["rr"] {
		t.Fatalf("subagent close/resume failed: %s", out.String())
	}
}
