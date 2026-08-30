package responsesapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type partialIdleReader struct{ sent bool }

func (r *partialIdleReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, providerpkg.ErrStreamIdle
	}
	r.sent = true
	return copy(p, []byte(`data: {"type":"response.output_text.delta","delta":"private partial"}`)), providerpkg.ErrStreamIdle
}
func (*partialIdleReader) Close() error { return nil }

func TestStreamIdleDoesNotParseOrExposePartialEvent(t *testing.T) {
	stream := NewStreamWithIdleTimeout(context.Background(), &http.Response{Body: &partialIdleReader{}}, "test", -1)
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamError || event.Err == nil {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	responseErr, ok := errors.AsType[*ResponseError](event.Err)
	if !ok || responseErr.Code != "stream_idle" || strings.Contains(event.Err.Error(), "private partial") {
		t.Fatalf("stream error=%v", event.Err)
	}
}

func TestBuildRequestCapabilityOptionsAndStandardLimits(t *testing.T) {
	temperature := 0.2
	model := protocol.Model{Provider: "compatible", ID: "m", SupportsTools: true, SupportsThinking: true, SupportsVerbosity: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingHigh}}
	body, err := BuildRequest(protocol.ChatRequest{Model: model, Thinking: protocol.ThinkingHigh, MaxTokens: 123, Temperature: &temperature, Messages: []protocol.Message{protocol.NewUserMessage("u", "", "hello")}}, RequestOptions{ProviderID: "compatible", IncludeEncryptedReasoning: true})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["stream"] != true || decoded["store"] != false || decoded["max_output_tokens"] != float64(123) || decoded["temperature"] != temperature {
		t.Fatalf("request=%v", decoded)
	}
	if _, ok := decoded["prompt_cache_key"]; ok {
		t.Fatalf("generic request inherited provider affinity: %v", decoded)
	}
	if _, ok := decoded["tool_choice"]; ok {
		t.Fatalf("generic request inherited provider tool policy: %v", decoded)
	}
	if _, ok := decoded["reasoning"]; !ok {
		t.Fatalf("reasoning omitted: %v", decoded)
	}
	if _, ok := decoded["text"]; !ok {
		t.Fatalf("verbosity omitted: %v", decoded)
	}
	include, _ := decoded["include"].([]any)
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include=%v", include)
	}
}

func TestBuildRequestPreservesExtendedReasoningEfforts(t *testing.T) {
	for _, effort := range []protocol.ThinkingLevel{protocol.ThinkingXHigh, protocol.ThinkingMax, protocol.ThinkingUltra} {
		t.Run(string(effort), func(t *testing.T) {
			model := protocol.Model{Provider: "compatible", ID: "m", SupportsThinking: true, ThinkingLevels: []protocol.ThinkingLevel{effort}}
			body, err := BuildRequest(protocol.ChatRequest{Model: model, Thinking: effort}, RequestOptions{ProviderID: "compatible"})
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatal(err)
			}
			reasoning, _ := decoded["reasoning"].(map[string]any)
			if reasoning["effort"] != string(effort) {
				t.Fatalf("reasoning=%v, want effort %q", reasoning, effort)
			}
		})
	}
}

func TestBuildRequestScopesContinuityAndToolCapabilities(t *testing.T) {
	wire := []byte(`{"type":"reasoning","id":"r1","summary":[],"encrypted_content":"opaque"}`)
	messages := []protocol.Message{
		{Role: protocol.RoleAssistant, Provider: "same", Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Name: "r1", Data: wire}}},
		{Role: protocol.RoleAssistant, Provider: "other", Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Name: "r1", Data: wire}}},
		{Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Name: "r1", Data: wire}}},
		{Role: protocol.RoleAssistant, Provider: "same", StopReason: protocol.StopError, Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Name: "failed", Data: []byte(`{"type":"reasoning","id":"failed","summary":[],"encrypted_content":"failed-opaque"}`)}}},
		{Role: protocol.RoleAssistant, Provider: "same", StopReason: protocol.StopAborted, Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Name: "aborted", Data: []byte(`{"type":"reasoning","id":"aborted","summary":[],"encrypted_content":"aborted-opaque"}`)}}},
	}
	body, err := BuildRequest(protocol.ChatRequest{
		Model: protocol.Model{Provider: "same", ID: "m", SupportsTools: false}, Messages: messages,
		Tools: []protocol.ToolSchema{{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}, RequestOptions{ProviderID: "same"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	input, _ := decoded["input"].([]any)
	if len(input) != 1 || strings.Count(string(body), "opaque") != 1 {
		t.Fatalf("provider continuity was not exactly scoped: %s", body)
	}
	if _, ok := decoded["tools"]; ok {
		t.Fatalf("tools sent to non-tool model: %s", body)
	}
}

func TestStreamStopsAtDoneSentinel(t *testing.T) {
	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: [DONE]\n\ndata: not-json\n\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	stream := NewStream(context.Background(), resp, "compatible")
	defer stream.Close()
	var events []protocol.StreamEvent
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			break
		}
		events = append(events, event)
	}
	if len(events) != 2 || events[0].Type != protocol.EvStreamTextDelta || events[1].Type != protocol.EvStreamDone {
		t.Fatalf("events=%+v", events)
	}
}

func TestBuildRequestProviderOptionsCanSuppressStandardFields(t *testing.T) {
	temperature := 0.2
	parallel := true
	body, err := BuildRequest(protocol.ChatRequest{
		Model: protocol.Model{Provider: "chatgpt", ID: "m"}, MaxTokens: 123, Temperature: &temperature,
	}, RequestOptions{
		ProviderID: "chatgpt", PromptCacheKey: "cache", ToolChoice: "auto", ParallelToolCalls: &parallel,
		OmitMaxOutputTokens: true, OmitTemperature: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["prompt_cache_key"] != "cache" || decoded["tool_choice"] != "auto" || decoded["parallel_tool_calls"] != true {
		t.Fatalf("provider controls missing: %v", decoded)
	}
	if _, ok := decoded["max_output_tokens"]; ok {
		t.Fatalf("max_output_tokens not suppressed: %v", decoded)
	}
	if _, ok := decoded["temperature"]; ok {
		t.Fatalf("temperature not suppressed: %v", decoded)
	}
}

func TestStreamRejectsEOFWithoutTerminalEvent(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"))}
	stream := NewStream(context.Background(), resp, "compatible")
	defer stream.Close()
	first, err := stream.Next(context.Background())
	if err != nil || first.Type != protocol.EvStreamTextDelta {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := stream.Next(context.Background())
	if err != nil || second.Type != protocol.EvStreamError || second.Err == nil || !strings.Contains(second.Err.Error(), "stream_truncated") {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if third, err := stream.Next(context.Background()); err != io.EOF || third.Type != "" {
		t.Fatalf("third=%+v err=%v", third, err)
	}
}

func TestSanitizeErrorTextRedactsCutoffPrefixesAndControls(t *testing.T) {
	secret := strings.Repeat("s", 300)
	unsafe := "line one\n\x1b\u009b[31m " + strings.Repeat("x", 470) + secret[:40]
	safe := SanitizeErrorText(unsafe, 500, secret)
	if strings.Contains(safe, secret[:8]) || strings.ContainsAny(safe, "\n\r\x1b\u009b") || !strings.Contains(safe, "[redacted]") {
		t.Fatalf("unsafe diagnostic survived: %q", safe)
	}
}

func TestResponseErrorTransientClassification(t *testing.T) {
	for _, candidate := range []*ResponseError{
		NewResponseError("test", http.StatusServiceUnavailable, "unavailable", "", ""),
		NewResponseError("test", 0, "network failed", "network_error", ""),
		NewResponseError("test", 0, "idle", "stream_idle", ""),
	} {
		if !candidate.Transient() {
			t.Fatalf("not transient: %+v", candidate)
		}
	}
	for _, candidate := range []*ResponseError{
		NewResponseError("test", http.StatusTooManyRequests, "limited", "rate_limit", ""),
		NewResponseError("test", http.StatusBadRequest, "invalid", "invalid_request", ""),
		NewResponseError("test", 0, "permanent failure", "", ""),
	} {
		if candidate.Transient() {
			t.Fatalf("unexpected transient: %+v", candidate)
		}
	}
}

func TestResponseErrorMarksContextWindowExceeded(t *testing.T) {
	for _, candidate := range []*ResponseError{
		NewResponseError("test", 400, "too large", "context_length_exceeded", ""),
		NewResponseError("test", 400, "maximum context length exceeded", "", ""),
	} {
		if !candidate.ContextWindowExceeded() {
			t.Fatalf("not marked: %+v", candidate)
		}
	}
	if NewResponseError("test", 400, "max_tokens invalid", "invalid_request", "").ContextWindowExceeded() {
		t.Fatal("output token configuration misclassified")
	}
}

func TestStreamPreservesBoundedErrorMetadata(t *testing.T) {
	const secret = "secret-token"
	body := "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"overloaded secret-token\",\"code\":\"server_overloaded\"},\"request_id\":\"req-123\"}}\n\n"
	stream := NewStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(body))}, "chatgpt", secret)
	defer stream.Close()
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamError || event.Err == nil {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	responseErr, ok := errors.AsType[*ResponseError](event.Err)
	if !ok || responseErr.Code != "server_overloaded" || responseErr.RequestID != "req-123" {
		t.Fatalf("error=%T %v", event.Err, event.Err)
	}
	if strings.Contains(event.Err.Error(), secret) || !strings.Contains(event.Err.Error(), "[redacted]") {
		t.Fatalf("secret handling failed: %v", event.Err)
	}
	if strings.ContainsAny(event.Err.Error(), "\n\r\x1b") {
		t.Fatalf("control character survived: %q", event.Err.Error())
	}
}

func TestResponseUsageDistinguishesExplicitZeroFromOmittedCacheRead(t *testing.T) {
	explicit := responseUsage(map[string]any{"response": map[string]any{"usage": map[string]any{
		"input_tokens": float64(4), "input_tokens_details": map[string]any{"cached_tokens": float64(0)},
	}}})
	if explicit == nil || !explicit.CacheReadKnown || explicit.CacheRead != 0 {
		t.Fatalf("explicit usage = %+v", explicit)
	}
	omitted := responseUsage(map[string]any{"response": map[string]any{"usage": map[string]any{
		"input_tokens": float64(4), "input_tokens_details": map[string]any{},
	}}})
	if omitted == nil || omitted.CacheReadKnown {
		t.Fatalf("omitted usage = %+v", omitted)
	}
}

func TestStreamNormalizesRefusalUsageAndIncomplete(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.refusal.delta","delta":"cannot"}`,
		``,
		`data: {"type":"response.incomplete","response":{"status":"incomplete","usage":{"input_tokens":4,"output_tokens":2,"input_tokens_details":{"cached_tokens":1},"output_tokens_details":{"reasoning_tokens":1}}}}`,
		``,
	}, "\n")
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	stream := NewStream(context.Background(), resp, "compatible")
	defer stream.Close()
	var text string
	var usage *protocol.Usage
	var stop protocol.StopReason
	for {
		ev, err := stream.Next(context.Background())
		if err != nil {
			break
		}
		switch ev.Type {
		case protocol.EvStreamTextDelta:
			text += ev.Text
		case protocol.EvStreamUsage:
			usage = ev.Usage
		case protocol.EvStreamDone:
			stop = ev.StopReason
		}
	}
	if text != "cannot" || stop != protocol.StopLength || usage == nil || usage.Total != 6 || usage.CacheRead != 1 || !usage.CacheReadKnown || usage.Reasoning != 1 {
		t.Fatalf("text=%q stop=%q usage=%+v", text, stop, usage)
	}
}
