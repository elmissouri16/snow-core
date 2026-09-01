package rpc

import (
	"bytes"
	json "encoding/json/v2"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestContextReportRPCIsCorrelatedAndSecretSafe(t *testing.T) {
	a, err := app.New(t.Context(), app.Options{
		Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(),
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	const secret = "rpc-context-secret-that-must-not-leak"
	if err := a.Agent.Prompt(t.Context(), secret); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	srv := New(t.Context(), a, &bytes.Buffer{}, &out)
	if err := srv.handle(t.Context(), Request{ID: "context-1", Type: "context"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("context response leaked prompt content: %s", out.String())
	}
	var response struct {
		ID      string                    `json:"id"`
		Command string                    `json:"command"`
		Success bool                      `json:"success"`
		Data    protocol.RPCContextReport `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "context-1" || response.Command != "context" || !response.Success {
		t.Fatalf("response = %+v", response)
	}
	if response.Data.MessageCount == 0 || len(response.Data.Categories) == 0 {
		t.Fatalf("context report lacks counts: %+v", response.Data)
	}
	for _, category := range response.Data.Categories {
		if category.Name == "" || category.Bytes < 0 || category.EstimatedTokens < 0 || category.Items < 0 {
			t.Fatalf("invalid category: %+v", category)
		}
	}
}

func TestSkillsClearRPCClearsOnlyActiveSessionState(t *testing.T) {
	a, err := app.New(t.Context(), app.Options{
		Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(),
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	var out bytes.Buffer
	srv := New(t.Context(), a, &bytes.Buffer{}, &out)
	if err := srv.handle(t.Context(), Request{ID: "clear-1", Type: "skills_clear"}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID      string                        `json:"id"`
		Command string                        `json:"command"`
		Success bool                          `json:"success"`
		Data    protocol.RPCSkillsClearResult `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "clear-1" || response.Command != "skills_clear" || !response.Success {
		t.Fatalf("response = %+v", response)
	}
	if response.Data.Cleared != 0 || len(response.Data.Catalog.Skills) != 0 {
		t.Fatalf("clear result = %+v", response.Data)
	}
	if err := srv.handle(t.Context(), Request{ID: "bad", Type: "skills_clear", Params: []byte(`{}`)}); err == nil {
		t.Fatal("skills_clear accepted params")
	}
}
