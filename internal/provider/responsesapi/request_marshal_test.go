package responsesapi

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestAppendResponseJSONStringMatchesEncodingJSON(t *testing.T) {
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	for _, value := range []string{
		string(allBytes),
		"plain ASCII / path",
		"HTML <script>& and separators \u2028\u2029",
		"Unicode 世界 😀",
	} {
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		got := appendResponseJSONString(nil, value)
		if !bytes.Equal(got, want) {
			t.Fatalf("quoted string changed wire encoding\n got: %q\nwant: %q", got, want)
		}
	}
}

func TestRequestMarshalerMatchesEncodingJSON(t *testing.T) {
	summarySupported := true
	temperature := 1e-9
	parallel := false
	continuity := json.RawMessage(`{"type":"reasoning","id":"reasoning-1","summary":[{"type":"summary_text","text":"summary"}],"content":[{"type":"reasoning_text","text":"private"}],"encrypted_content":"opaque"}`)
	request := protocol.ChatRequest{
		Model: protocol.Model{
			Provider: "same", ID: "model<&>", SupportsTools: true, SupportsThinking: true,
			SupportsVerbosity: true, SupportsReasoningSummary: &summarySupported,
			ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingHigh},
		},
		Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentBlock{
				{Type: protocol.BlockText, Text: "user <text> & quote \" slash \\ line\nseparator \u2028 invalid " + string([]byte{0xff})},
				{Type: protocol.BlockPlan, Text: "draft"},
				{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}},
			}},
			{Role: protocol.RoleAssistant, Provider: "same", StopReason: protocol.StopStop, Content: []protocol.ContentBlock{
				{Type: protocol.BlockProviderData, Name: "reasoning-1", Data: continuity},
				{Type: protocol.BlockToolCall, ToolCallID: "call-1", Name: "read", Arguments: json.RawMessage("  {\"path\":\"README.md\"}  ")},
				{Type: protocol.BlockText, Text: "answer"},
				{Type: protocol.BlockPlan, Text: "complete", PlanComplete: true},
			}},
			{Role: protocol.RoleTool, ToolCallID: "call-1", Content: []protocol.ContentBlock{
				{Type: protocol.BlockText, Text: "tool text"},
				{Type: protocol.BlockImage, MIMEType: "image/jpeg", Data: []byte{1, 2, 3, 4}},
			}},
			{Role: protocol.RoleAssistant, Provider: "same", StopReason: protocol.StopStop, Content: []protocol.ContentBlock{
				{Type: protocol.BlockProviderData, Name: "legacy-1", Data: []byte("legacy-opaque")},
				{Type: protocol.BlockToolCall, ToolCallID: "call-2", Name: "grep", Arguments: json.RawMessage(`{"pattern":"main"}`)},
			}},
			{Role: protocol.RoleTool, ToolCallID: "call-2", Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "plain output"}}},
			{Role: protocol.RoleCustom, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "custom"}}},
		},
		Tools: []protocol.ToolSchema{
			{Name: "read", Description: "Read <safe> data.", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
			{Name: "empty", Parameters: nil},
		},
		System:           "system <script>\u2029 " + string([]byte{0xfe}),
		MaxTokens:        1234,
		Temperature:      &temperature,
		Thinking:         protocol.ThinkingHigh,
		ReasoningSummary: protocol.ReasoningSummaryDetailed,
		TextVerbosity:    protocol.TextVerbosityHigh,
		InternalContext:  []protocol.InternalContextFragment{{Source: "goal", Text: "trusted state"}},
	}
	opts := RequestOptions{
		ProviderID: "same", IncludeEncryptedReasoning: true, PromptCacheKey: "cache<&>",
		ToolChoice: "auto", ParallelToolCalls: &parallel,
	}
	body, err := buildRequestBody(request, opts)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := marshalRequestBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("specialized request JSON changed wire encoding\n got: %s\nwant: %s", got, want)
	}
}

func TestRequestMarshalerMatchesEmptyAndNegativeZeroFields(t *testing.T) {
	negativeZero := math.Copysign(0, -1)
	parallel := true
	for _, request := range []protocol.ChatRequest{
		{Model: protocol.Model{ID: "empty"}},
		{Model: protocol.Model{ID: "negative-zero"}, Temperature: &negativeZero},
	} {
		body, err := buildRequestBody(request, RequestOptions{ParallelToolCalls: &parallel})
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		got, err := marshalRequestBody(body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("request %q changed wire encoding\n got: %s\nwant: %s", request.Model.ID, got, want)
		}
	}
}

func TestRequestMarshalerMatchesNilInput(t *testing.T) {
	body := responsesRequest{Model: "m"}
	want, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := marshalRequestBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("nil input changed wire encoding: got %s, want %s", got, want)
	}
}

func TestRequestMarshalerRejectsMalformedRawJSON(t *testing.T) {
	body := responsesRequest{Model: "m", Input: []any{json.RawMessage(`{"type":`)}}
	if _, err := marshalRequestBody(body); err == nil || strings.Contains(err.Error(), `{"type":`) {
		t.Fatalf("malformed raw JSON error = %v", err)
	}
	body = responsesRequest{Model: "m", Input: []any{struct{}{}}}
	if _, err := marshalRequestBody(body); err == nil {
		t.Fatal("unsupported input item was accepted")
	}
}

func TestRequestMarshalerPreservesNonFiniteTemperatureError(t *testing.T) {
	value := math.Inf(1)
	body := responsesRequest{Model: "m", Input: []any{}, Temperature: &value}
	if _, err := marshalRequestBody(body); err == nil {
		t.Fatal("non-finite temperature was accepted")
	}
}

func TestMessageInputPreservesNilForEmptyProjection(t *testing.T) {
	items, err := MessageInput(protocol.Message{Role: protocol.RoleUser}, "same")
	if err != nil {
		t.Fatal(err)
	}
	if items != nil {
		t.Fatalf("empty projection = %#v, want nil", items)
	}
}

func TestProviderReasoningValidationCompatibility(t *testing.T) {
	tests := []struct {
		name string
		data string
		ok   bool
	}{
		{name: "minimal", data: `{"type":"reasoning","id":"r1","summary":[]}`, ok: true},
		{name: "duplicate last value", data: `{"type":"wrong","type":"reasoning","id":"r1","summary":[]}`, ok: true},
		{name: "summary null", data: `{"type":"reasoning","id":"r1","summary":null}`},
		{name: "content null", data: `{"type":"reasoning","id":"r1","summary":[],"content":null}`},
		{name: "encrypted content null", data: `{"type":"reasoning","id":"r1","summary":[],"encrypted_content":null}`},
		{name: "case variant", data: `{"Type":"reasoning","id":"r1","summary":[]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := MessageInput(protocol.Message{
				Role: protocol.RoleAssistant, Provider: "same", StopReason: protocol.StopStop,
				Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Name: "r1", Data: []byte(test.data)}},
			}, "same")
			if (err == nil) != test.ok {
				t.Fatalf("error = %v, want success %v", err, test.ok)
			}
		})
	}
}

func TestMessageInputClonesProviderContinuity(t *testing.T) {
	data := []byte(`{"type":"reasoning","id":"r1","summary":[]}`)
	items, err := MessageInput(protocol.Message{
		Role: protocol.RoleAssistant, Provider: "same", StopReason: protocol.StopStop,
		Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Name: "r1", Data: data}},
	}, "same")
	if err != nil {
		t.Fatal(err)
	}
	copy(data, []byte(`{"type":"reasoning","id":"xx"`))
	wire, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire), `"id":"r1"`) {
		t.Fatalf("provider continuity alias leaked into MessageInput: %s", wire)
	}
}
