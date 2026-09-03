package opencodego

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestAppendChatJSONStringPreservesContent(t *testing.T) {
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	for _, value := range []string{
		string(allBytes),
		"plain ASCII / path",
		"markup <tag>& and separators \u2028\u2029",
		"Unicode 世界 😀",
	} {
		got := appendChatJSONString(nil, value)
		var decoded string
		if err := json.Unmarshal(got, &decoded); err != nil {
			t.Fatalf("decode quoted string: %v", err)
		}
		if want := string([]rune(value)); decoded != want {
			t.Fatalf("quoted string decoded to %q, want %q", decoded, want)
		}
	}
}

func TestChatRequestMarshalerPreservesJSONSemantics(t *testing.T) {
	temperature := 1e-9
	maxTokens := 1234
	reasoning := "high"
	body := openAIChatRequest{
		Model: "model<&>",
		Messages: []openAIMessage{
			{Role: "system", Content: "system <tag>\u2029 " + string([]byte{0xfe})},
			{Role: "user", Content: []openAIContentPart{
				{Type: "text", Text: "user <text> & quote \" slash \\ line\nseparator \u2028 invalid " + string([]byte{0xff})},
				{Type: "image_url", ImageURL: &openAIImageURLPart{URL: "data:image/png;base64,iVBORw=="}},
			}},
			{Role: "assistant", Content: "", ToolCalls: []openAIToolCall{{ID: "call-1", Type: "function", Function: openAIToolCallFunction{Name: "read", Arguments: `{"path":"README.md"}`}}}},
			{Role: "tool", ToolCallID: "call-1", Content: "tool output"},
		},
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		Tools: []openAITool{{Type: "function", Function: openAIToolFunction{
			Name: "read", Description: "Read <safe> data.", Parameters: json.RawMessage("  {\"type\":\"object\",\"properties\":{}}  "),
		}}},
		Temperature:     &temperature,
		MaxTokens:       &maxTokens,
		ReasoningEffort: &reasoning,
	}
	want, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := marshalChatRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	assertEquivalentChatJSON(t, got, want)
}

func TestChatRequestMarshalerMatchesNilEmptyAndNegativeZero(t *testing.T) {
	negativeZero := math.Copysign(0, -1)
	empty := []openAIMessage{}
	for _, body := range []openAIChatRequest{
		{Model: "nil"},
		{Model: "empty", Messages: empty},
		{Model: "negative-zero", Messages: empty, Temperature: &negativeZero},
		{Model: "typed-nil", Messages: []openAIMessage{{Role: "user", Content: []openAIContentPart(nil)}}},
		{Model: "empty-tool-call", Messages: []openAIMessage{{Role: "assistant", ToolCalls: []openAIToolCall{{}}}}},
		{
			Model: "raw-schema-specials",
			Tools: []openAITool{{Type: "function", Function: openAIToolFunction{
				Name:        "raw",
				Description: "schema",
				Parameters:  json.RawMessage(` { "description": "<&>  ", "escaped": "\u003c\u0026\u003e", "duplicate": 1, "duplicate": 2 } `),
			}}},
		},
	} {
		want, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		got, err := marshalChatRequest(body)
		if err != nil {
			t.Fatal(err)
		}
		assertEquivalentChatJSON(t, got, want)
	}
}

func TestChatRequestMarshalerMatchesInvalidUTF8RawJSONBehavior(t *testing.T) {
	raw := append([]byte(`{"value":"`), byte(0xff))
	raw = append(raw, []byte(`"}`)...)
	body := openAIChatRequest{Model: "invalid-utf8-raw", Tools: []openAITool{{Type: "function", Function: openAIToolFunction{Parameters: raw}}}}
	want, wantErr := json.Marshal(body)
	got, gotErr := marshalChatRequest(body)
	if (gotErr != nil) != (wantErr != nil) {
		t.Fatalf("error mismatch: specialized=%v encoding/json=%v", gotErr, wantErr)
	}
	if wantErr == nil && !bytes.Equal(got, want) {
		t.Fatalf("invalid UTF-8 raw behavior changed\n got: %q\nwant: %q", got, want)
	}
}

func assertEquivalentChatJSON(t *testing.T, got, want []byte) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode specialized request: %v", err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode reference request: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("request JSON changed semantics\n got: %s\nwant: %s", got, want)
	}
}

func TestChatRequestMarshalerRejectsMalformedRawJSON(t *testing.T) {
	body := openAIChatRequest{Model: "m", Tools: []openAITool{{Type: "function", Function: openAIToolFunction{Parameters: json.RawMessage(`{"type":`)}}}}
	if _, err := marshalChatRequest(body); err == nil || strings.Contains(err.Error(), `{"type":`) {
		t.Fatalf("malformed raw JSON error=%v", err)
	}
}

func TestChatRequestMarshalerPreservesNonFiniteTemperatureError(t *testing.T) {
	value := math.Inf(1)
	if _, err := marshalChatRequest(openAIChatRequest{Model: "m", Temperature: &value}); err == nil {
		t.Fatal("non-finite temperature was accepted")
	}
}
