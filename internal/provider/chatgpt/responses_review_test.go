package chatgpt

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestBuildResponsesBodyEncodesValidatedImageDataURI(t *testing.T) {
	body, err := buildRequestBody(protocol.ChatRequest{
		Model: protocol.Model{Provider: ProviderID, ID: "vision"},
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentBlock{
			{Type: protocol.BlockText, Text: "inspect"},
			{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 'G'})
	if !strings.Contains(string(body), `"type":"input_image"`) || !strings.Contains(string(body), `"detail":"high"`) || !strings.Contains(string(body), want) {
		t.Fatalf("high-detail image missing from request: %s", body)
	}
	for _, block := range []protocol.ContentBlock{
		{Type: protocol.BlockImage, MIMEType: "text/plain", Data: []byte("x")},
		{Type: protocol.BlockImage, MIMEType: "image/png"},
	} {
		_, err := buildRequestBody(protocol.ChatRequest{Model: protocol.Model{ID: "vision"}, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentBlock{block}}}})
		if err == nil {
			t.Fatalf("invalid image accepted: %+v", block)
		}
	}
}

func TestResponseInputReserializesOpaqueReasoningBeforeCallAndOutput(t *testing.T) {
	wire := `{"type":"reasoning","id":"reasoning-1","summary":[],"content":[{"type":"reasoning_text","text":"opaque"}],"encrypted_content":"encrypted-value"}`
	items, err := responseInput(protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{
		{Type: protocol.BlockThinking, Text: "visible summary"},
		{Type: protocol.BlockProviderData, Name: "reasoning-1", Data: []byte(wire)},
		{Type: protocol.BlockText, Text: "answer"},
		{Type: protocol.BlockToolCall, ToolCallID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(items)
	text := string(data)
	reasoning := strings.Index(text, `"type":"reasoning"`)
	call := strings.Index(text, `"type":"function_call"`)
	output := strings.Index(text, `"type":"message"`)
	if reasoning < 0 || call < 0 || output < 0 || reasoning > call || reasoning > output {
		t.Fatal("provider continuity order is wrong")
	}
	if strings.Contains(text, "visible summary") {
		t.Fatal("rendered thinking was replayed as provider continuity")
	}
	if !strings.Contains(text, wire) {
		t.Fatal("official reasoning item was not replayed exactly")
	}
}

func TestResponseInputSupportsLegacyOpaqueReasoningAndRejectsMalformedOfficialItem(t *testing.T) {
	items, err := responseInput(protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{
		{Type: protocol.BlockProviderData, Name: "legacy-1", Data: []byte("encrypted-value")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(items)
	if string(data) != `[{"encrypted_content":"encrypted-value","id":"legacy-1","summary":[],"type":"reasoning"}]` {
		t.Fatal("legacy reasoning item was not upgraded to the current wire shape")
	}
	for _, data := range []string{
		`{"type":"reasoning","id":"reasoning-1","encrypted_content":"secret"}`,
		`{"type":"reasoning","id":"other","summary":[]}`,
		`{"type":"reasoning","id":"reasoning-1","summary":[],"unexpected":true}`,
		`{"type":"reasoning","id":"reasoning-1","summary":[{"type":"not_summary","text":"secret"}]}`,
		`{"type":"reasoning","id":"reasoning-1","summary":[],"content":[{"type":"reasoning_text"}]}`,
	} {
		_, err := responseInput(protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Name: "reasoning-1", Data: []byte(data)}}})
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("malformed official reasoning item error = %v", err)
		}
	}
}

func TestResponseInputPreservesMixedToolTextAndImages(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G'}
	items, err := responseInput(protocol.Message{Role: protocol.RoleTool, ToolCallID: "call-image", Content: []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "MCP screenshot"},
		{Type: protocol.BlockImage, MIMEType: "image/png", Data: image},
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(items)
	var decoded []struct {
		Output any `json:"output"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	output, ok := decoded[0].Output.([]any)
	if !ok || len(output) != 2 {
		t.Fatalf("mixed tool output = %s", data)
	}
	text := string(data)
	if !strings.Contains(text, `"type":"input_text"`) || !strings.Contains(text, `"type":"input_image"`) || !strings.Contains(text, `"detail":"high"`) || !strings.Contains(text, base64.StdEncoding.EncodeToString(image)) {
		t.Fatalf("structured tool output = %s", data)
	}

	plain, err := responseInput(protocol.Message{Role: protocol.RoleTool, ToolCallID: "call-text", Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "plain"}}})
	if err != nil {
		t.Fatal(err)
	}
	plainData, _ := json.Marshal(plain)
	if !strings.Contains(string(plainData), `"output":"plain"`) {
		t.Fatalf("text-only output no longer uses a string: %s", plainData)
	}
}
