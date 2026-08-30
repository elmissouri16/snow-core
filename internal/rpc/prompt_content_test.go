package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRPCContentPromptRequiresValidAttachments(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)

	cases := []struct {
		name    string
		request Request
		wantSub string
	}{
		{"empty content", Request{Type: "prompt", Content: []protocol.ContentBlock{}}, "content must not be empty"},
		{"text-only without message", Request{Type: "prompt", Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "hi"}}}, "requires a message"},
		{"provider data block", Request{Type: "prompt", Message: "x", Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Text: "opaque"}}}, "not allowed"},
		{"thinking block", Request{Type: "prompt", Message: "x", Content: []protocol.ContentBlock{{Type: protocol.BlockThinking, Text: "think"}}}, "not allowed"},
		{"image without mime", Request{Type: "prompt", Content: []protocol.ContentBlock{{Type: protocol.BlockImage, Data: []byte{1}}}}, "requires mime_type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := srv.handlePrompt(context.Background(), tc.request)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("handlePrompt = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestRPCContentPromptValidatesModelVisionAndRuns(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	model := a.Agent.Model()
	model.SupportsVision = false
	if err := a.Agent.SetProviderAndModel(a.Providers["fake"], model); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	err = srv.handlePrompt(context.Background(), Request{Type: "prompt", Message: "look", Content: []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{1}}}})
	if err == nil || !strings.Contains(err.Error(), "does not support image input") {
		t.Fatalf("handlePrompt = %v, want vision rejection", err)
	}

	model2 := a.Agent.Model()
	model2.SupportsVision = true
	if err := a.Agent.SetProviderAndModel(a.Providers["fake"], model2); err != nil {
		t.Fatal(err)
	}
	if err := srv.handlePrompt(context.Background(), Request{ID: "mm1", Type: "prompt", Message: "look", Content: []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{1, 2}}}}); err != nil {
		t.Fatal(err)
	}
	srv.promptWG.Wait()
	var completed protocol.RPCPromptCompleted
	if err := json.Unmarshal(rpcFrame(t, out.String(), protocol.RPCTypePromptCompleted, ""), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Status != protocol.RPCPromptCompletedStatus {
		t.Fatalf("completion = %+v", completed)
	}
}

func TestRPCContentPromptPreservesMixedBlockOrdering(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	provider := &rpcCaptureProvider{requests: make(chan protocol.ChatRequest, 1)}
	model := a.Agent.Model()
	model.Provider = provider.ID()
	model.SupportsVision = true
	if err := a.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &out)
	content := []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "before image"},
		{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{1, 2, 3}},
		{Type: protocol.BlockText, Text: "after image"},
	}
	if err := srv.handlePrompt(t.Context(), Request{ID: "mixed", Type: "prompt", Message: "legacy message", Content: content}); err != nil {
		t.Fatal(err)
	}
	srv.promptWG.Wait()
	req := <-provider.requests
	if len(req.Messages) == 0 {
		t.Fatal("provider received no messages")
	}
	got := req.Messages[len(req.Messages)-1].Content
	if len(got) != 4 || got[0].Type != protocol.BlockText || got[0].Text != "legacy message" || got[1].Text != "before image" || got[2].Type != protocol.BlockImage || got[3].Text != "after image" {
		t.Fatalf("provider content = %+v", got)
	}
	var completed protocol.RPCPromptCompleted
	if err := json.Unmarshal(rpcFrame(t, out.String(), protocol.RPCTypePromptCompleted, "mixed"), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Status != protocol.RPCPromptCompletedStatus {
		t.Fatalf("completion = %+v", completed)
	}
}

func TestRPCSecondPromptDoesNotCancelActivePrompt(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	active := make(chan struct{})
	srv.promptDone = active
	cancelled := false
	srv.cancel = func() { cancelled = true }
	err = srv.handlePrompt(context.Background(), Request{ID: "p2", Type: "prompt", Message: "replacement"})
	if err == nil || !strings.Contains(err.Error(), "use steer, follow_up, or abort") {
		t.Fatalf("second prompt error = %v", err)
	}
	if cancelled {
		t.Fatal("second prompt implicitly cancelled active work")
	}
}
