package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/elmissouri16/snow-core/internal/app"
	"testing"
)

func TestRPCGoalCommandsUseStructuredData(t *testing.T) {
	a, e := app.New(context.Background(), app.Options{Provider: "fake", Permission: "allow", CWD: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	var out bytes.Buffer
	s := New(context.Background(), a, &bytes.Buffer{}, &out)
	if e := s.handle(context.Background(), Request{ID: "1", Type: "goal_create", Params: json.RawMessage(`{"objective":"ship"}`)}); e != nil {
		t.Fatal(e)
	}
	a.Agent.Abort()
	if e := s.handle(context.Background(), Request{ID: "2", Type: "goal_continue"}); e != nil {
		t.Fatal(e)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) < 2 {
		t.Fatalf("output=%q", out.String())
	}
	var r Response
	if e := json.Unmarshal(lines[0], &r); e != nil {
		t.Fatal(e)
	}
	if !r.Success || r.Data == nil || r.Error != "" {
		t.Fatalf("response=%+v", r)
	}
	var continued Response
	if e := json.Unmarshal(lines[len(lines)-1], &continued); e != nil {
		t.Fatal(e)
	}
	if continued.Command != "goal_continue" || !continued.Success || continued.Data == nil {
		t.Fatalf("continue response=%+v", continued)
	}
}
