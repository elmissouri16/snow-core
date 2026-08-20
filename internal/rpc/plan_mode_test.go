package rpc

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestRPCSetModeAndSessionInfo(t *testing.T) {
	var in, out bytes.Buffer
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	in.WriteString(`{"id":"m1","type":"set_mode","mode":"plan"}` + "\n")
	in.WriteString(`{"id":"i1","type":"session_info"}` + "\n")
	if err := New(context.Background(), a, &in, &out).Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"command":"set_mode","success":true`) || !strings.Contains(out.String(), `collaboration_mode`) || !strings.Contains(out.String(), `plan`) {
		t.Fatalf("output = %s", out.String())
	}
}

func TestRPCPromptAttachedModeValidation(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	s := New(context.Background(), a, &bytes.Buffer{}, &bytes.Buffer{})
	if err := s.handlePrompt(context.Background(), Request{Type: "prompt", Message: "x", Mode: "bogus"}); err == nil {
		t.Fatal("invalid attached mode accepted")
	}
}
