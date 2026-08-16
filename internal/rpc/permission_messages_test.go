package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestRPCPermissionReplyUsesAttributedRequestID(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{
		Provider: "fake", NoSession: true, Permission: "ask", CWD: t.TempDir(),
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var output bytes.Buffer
	server := New(context.Background(), a, bytes.NewReader(nil), &output)
	events := make(chan protocol.AgentEvent, 1)
	unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvPermissionRequest {
			events <- event
		}
	})
	defer unsubscribe()

	decision := make(chan permission.Decision, 1)
	go func() {
		value, _ := a.Perm.Authorize(context.Background(), permission.Request{Tool: "bash", Risk: permission.RiskExec, Args: json.RawMessage(`{"command":"true"}`)})
		decision <- value
	}()

	var event protocol.AgentEvent
	select {
	case event = <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for permission event")
	}
	if event.Permission == nil || event.Permission.Request.ID == "" {
		t.Fatalf("permission event missing request ID: %#v", event.Permission)
	}
	requestID := event.Permission.Request.ID
	params, _ := json.Marshal(protocol.RPCPermissionReply{RequestID: requestID, Decision: string(permission.DecisionAllowSession)})
	if err := server.handle(context.Background(), Request{ID: "reply-1", Type: "permission_reply", Params: params}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-decision:
		if got != permission.DecisionAllowSession {
			t.Fatalf("decision = %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for permission decision")
	}
	if err := server.handle(context.Background(), Request{ID: "reply-2", Type: "permission_reply", Params: params}); err == nil {
		t.Fatal("duplicate permission reply unexpectedly succeeded")
	}
}

func TestRPCSessionMessageProjectionStripsPrivateAndBinaryContent(t *testing.T) {
	messages := []protocol.Message{{
		ID: "assistant-1", Role: protocol.RoleAssistant, Timestamp: 1,
		Content: []protocol.ContentBlock{
			{Type: protocol.BlockProviderData, Data: []byte("private continuation")},
			{Type: protocol.BlockImage, MIMEType: "image/png", Data: make([]byte, protocol.RPCMaxInputBytes+1)},
			{Type: protocol.BlockText, Text: strings.Repeat("x", maxRPCObservedTextBytes*2)},
		},
	}}
	projected := projectSessionMessages(messages, 1)
	if len(projected) != 1 || len(projected[0].Content) != 2 {
		t.Fatalf("projected = %#v", projected)
	}
	if projected[0].Content[0].Type != protocol.BlockImage || len(projected[0].Content[0].Data) != 0 {
		t.Fatalf("image payload was not stripped: %#v", projected[0].Content[0])
	}
	for _, block := range projected[0].Content {
		if block.Type == protocol.BlockProviderData {
			t.Fatal("provider_data escaped public transcript projection")
		}
	}
	encoded, err := json.Marshal(protocol.RPCSessionMessages{Messages: projected})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) >= protocol.RPCMaxInputBytes {
		t.Fatalf("projected transcript is %d bytes", len(encoded))
	}
}

func TestRPCSessionMessagesReturnsBoundedTailAndClones(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{
		Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(),
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for i := 0; i < 4; i++ {
		message := protocol.NewUserMessage(string(rune('a'+i)), "", string(rune('A'+i)))
		if err := a.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	server := New(context.Background(), a, bytes.NewReader(nil), &output)
	params, _ := json.Marshal(protocol.RPCSessionMessagesRequest{Limit: 2})
	if err := server.handle(context.Background(), Request{ID: "messages-1", Type: "session_messages", Params: params}); err != nil {
		t.Fatal(err)
	}
	frame := rpcFrame(t, output.String(), "response", "messages-1")
	var response struct {
		Data protocol.RPCSessionMessages `json:"data"`
	}
	if err := json.Unmarshal(frame, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Messages) != 2 || response.Data.Messages[0].ID != "c" || response.Data.Messages[1].ID != "d" {
		t.Fatalf("messages = %#v", response.Data.Messages)
	}
	response.Data.Messages[0].Content[0].Text = "mutated"
	messages, err := a.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if messages[2].Content[0].Text == "mutated" {
		t.Fatal("RPC transcript aliases session state")
	}

	tooMany, _ := json.Marshal(protocol.RPCSessionMessagesRequest{Limit: protocol.RPCSessionMessagesMax + 1})
	if err := server.handle(context.Background(), Request{Type: "session_messages", Params: tooMany}); err == nil {
		t.Fatal("oversized session_messages limit unexpectedly succeeded")
	}
}
