package rpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func testSubagentMessagesState(threadID, path string, generation uint64) protocol.SubagentState {
	return protocol.SubagentState{
		Agent: protocol.AgentRef{
			ThreadID: threadID,
			Path:     protocol.AgentPath(path),
			Depth:    1,
		},
		Status:     protocol.AgentCompleted,
		Generation: generation,
	}
}

func testSubagentMessages(count int) []protocol.Message {
	messages := make([]protocol.Message, count)
	parentID := ""
	for i := range count {
		id := "message-" + string(rune('a'+i))
		messages[i] = protocol.NewUserMessage(id, parentID, "public "+id)
		parentID = id
	}
	return messages
}

func TestBuildSubagentMessagesPageBindsCursorToAgentSnapshot(t *testing.T) {
	state := testSubagentMessagesState("thread-1", "/root/child", 7)
	messages := testSubagentMessages(3)
	params := protocol.RPCSubagentMessagesParams{
		Target: "/root/child", Limit: 1, MaxBytes: minSubagentMessagesBytes,
	}
	first, err := buildSubagentMessagesPage("request-1", state, messages, params)
	if err != nil {
		t.Fatal(err)
	}
	if first.Agent.ThreadID != "thread-1" || first.Generation != 7 || first.Start != 0 || first.Total != 3 || !first.HasMore || first.NextCursor == "" || len(first.Messages) != 1 {
		t.Fatalf("first page = %+v", first)
	}

	// Appending to the child transcript must not move the cursor's stable total.
	messages = append(messages, protocol.NewUserMessage("message-d", "message-c", "later"))
	state.Generation = 8
	params.Cursor = first.NextCursor
	second, err := buildSubagentMessagesPage("request-2", state, messages, params)
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation != 7 || second.Start != 1 || second.Total != 3 || len(second.Messages) != 1 {
		t.Fatalf("second page = %+v", second)
	}

	other := testSubagentMessagesState("thread-2", "/root/other", 1)
	if _, err := buildSubagentMessagesPage("request-3", other, messages, params); err == nil || !strings.Contains(err.Error(), "selected agent") {
		t.Fatalf("wrong-agent cursor error = %v", err)
	}
}

func TestBuildSubagentMessagesPageRejectsTamperedCursorAndPrivateContinuity(t *testing.T) {
	state := testSubagentMessagesState("thread-1", "/root/child", 3)
	messages := testSubagentMessages(2)
	messages[0].Content = append(messages[0].Content, protocol.ContentBlock{
		Type: protocol.BlockProviderData,
		Data: []byte("opaque-provider-continuity"),
	})
	messages = publicMessages(messages)
	params := protocol.RPCSubagentMessagesParams{
		Target: "/root/child", Limit: 1, MaxBytes: minSubagentMessagesBytes,
	}
	first, err := buildSubagentMessagesPage("request-1", state, messages, params)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range first.Messages[0].Content {
		if block.Type == protocol.BlockProviderData {
			t.Fatal("provider continuity crossed subagent_messages boundary")
		}
	}

	payload, err := base64.RawURLEncoding.DecodeString(first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)-1] ^= 1
	params.Cursor = base64.RawURLEncoding.EncodeToString(payload)
	if _, err := buildSubagentMessagesPage("request-2", state, messages, params); err == nil {
		t.Fatal("tampered cursor unexpectedly accepted")
	}
}

func TestRPCSubagentMessagesUsesTrustedAppFacade(t *testing.T) {
	enabled := true
	a, err := app.New(t.Context(), app.Options{
		CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true,
		NoMCP: true, NoSkills: true, Permission: "allow", Subagents: &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	var output bytes.Buffer
	server := New(t.Context(), a, strings.NewReader(""), &output)
	requests := []Request{
		{ID: "ready", Type: "subagent_ready"},
		{ID: "spawn", Type: "subagent_spawn", Params: json.RawMessage(`{"name":"rpc_messages","task":"inspect","fork_turns":"none"}`)},
		{ID: "messages", Type: "subagent_messages", Params: json.RawMessage(`{"target":"/root/rpc_messages","limit":1,"max_bytes":16384}`)},
	}
	for _, request := range requests {
		if err := server.handle(context.Background(), request); err != nil {
			t.Fatalf("handle %s: %v", request.Type, err)
		}
	}

	found := false
	for line := range strings.SplitSeq(strings.TrimSpace(output.String()), "\n") {
		var response struct {
			ID      string                           `json:"id"`
			Success bool                             `json:"success"`
			Data    protocol.RPCSubagentMessagesPage `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatal(err)
		}
		if response.ID != "messages" {
			continue
		}
		found = true
		if !response.Success || response.Data.Agent.Path != "/root/rpc_messages" || response.Data.Agent.ThreadID == "" {
			t.Fatalf("subagent_messages response = %s", line)
		}
		if response.Data.Total < len(response.Data.Messages) || len(response.Data.Messages) > 1 {
			t.Fatalf("subagent_messages bounds = %+v", response.Data)
		}
	}
	if !found {
		t.Fatalf("missing subagent_messages response: %s", output.String())
	}
}

func TestSubagentMessagesFramesConformToSchemas(t *testing.T) {
	requestFrame := []byte(`{"id":"messages","type":"subagent_messages","params":{"target":"/root/child","limit":16,"max_bytes":65536}}`)
	if err := resolveWireSchema(t, "request.schema.json").Validate(decodedJSON(t, requestFrame)); err != nil {
		t.Fatalf("request schema: %v", err)
	}

	page := protocol.RPCSubagentMessagesPage{
		Agent:      testSubagentMessagesState("thread-1", "/root/child", 4).Agent,
		Generation: 4,
		Messages:   testSubagentMessages(1),
		Start:      0,
		Total:      1,
		HasMore:    false,
	}
	frame, err := json.Marshal(Response{ID: "messages", Type: "response", Command: "subagent_messages", Success: true, Data: page})
	if err != nil {
		t.Fatal(err)
	}
	if err := resolveWireSchema(t, "response.schema.json").Validate(decodedJSON(t, frame)); err != nil {
		t.Fatalf("response schema: %v\n%s", err, frame)
	}
}
