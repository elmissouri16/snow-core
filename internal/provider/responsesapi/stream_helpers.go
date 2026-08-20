package responsesapi

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *codexStream) streamLimitError(message string) bool {
	s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: s.prefix(message)})
	return true
}

func responseUsage(event map[string]any) *protocol.Usage {
	response, _ := event["response"].(map[string]any)
	usage, _ := response["usage"].(map[string]any)
	if usage == nil {
		return nil
	}
	out := &protocol.Usage{
		Input:  intNumber(usage["input_tokens"]),
		Output: intNumber(usage["output_tokens"]),
		Total:  intNumber(usage["total_tokens"]),
	}
	if details, _ := usage["input_tokens_details"].(map[string]any); details != nil {
		if cached, ok := intNumberPresent(details["cached_tokens"]); ok {
			out.CacheRead = cached
			out.CacheReadKnown = true
		}
		out.CacheWrite = intNumber(details["cache_creation_input_tokens"])
	}
	if details, _ := usage["output_tokens_details"].(map[string]any); details != nil {
		out.Reasoning = intNumber(details["reasoning_tokens"])
	}
	if out.Total == 0 {
		out.Total = out.Input + out.Output
	}
	return out
}

func nestedString(event map[string]any, parent, key string) (string, bool) {
	obj, _ := event[parent].(map[string]any)
	value, ok := obj[key].(string)
	return value, ok
}

func eventMessage(event map[string]any) string {
	if message, _ := event["message"].(string); message != "" {
		return message
	}
	if errObj, _ := event["error"].(map[string]any); errObj != nil {
		message, _ := errObj["message"].(string)
		return message
	}
	if response, _ := event["response"].(map[string]any); response != nil {
		if errObj, _ := response["error"].(map[string]any); errObj != nil {
			message, _ := errObj["message"].(string)
			return message
		}
	}
	return ""
}

func eventCode(event map[string]any) string {
	if code, _ := event["code"].(string); code != "" {
		return code
	}
	if errObj, _ := event["error"].(map[string]any); errObj != nil {
		if code, _ := errObj["code"].(string); code != "" {
			return code
		}
	}
	if response, _ := event["response"].(map[string]any); response != nil {
		if errObj, _ := response["error"].(map[string]any); errObj != nil {
			code, _ := errObj["code"].(string)
			return code
		}
	}
	return ""
}

func eventRequestID(event map[string]any) string {
	for _, key := range []string{"request_id", "requestId"} {
		if value, _ := event[key].(string); value != "" {
			return value
		}
	}
	if errObj, _ := event["error"].(map[string]any); errObj != nil {
		for _, key := range []string{"request_id", "requestId"} {
			if value, _ := errObj[key].(string); value != "" {
				return value
			}
		}
	}
	if response, _ := event["response"].(map[string]any); response != nil {
		for _, key := range []string{"request_id", "requestId"} {
			if value, _ := response[key].(string); value != "" {
				return value
			}
		}
	}
	return ""
}

func intNumber(v any) int {
	n, _ := intNumberPresent(v)
	return n
}

func intNumberPresent(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func truncateUTF8(value string, maxBytes int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= maxBytes {
		return value
	}
	body := []byte(value)[:maxBytes]
	for len(body) > 0 && !utf8.Valid(body) {
		body = body[:len(body)-1]
	}
	return string(body) + "…"
}

// ErrorStream returns a stream that emits one normalized provider error.
func ErrorStream(ctx context.Context, err error) protocol.EventStream {
	s := &codexStream{ch: make(chan protocol.StreamEvent, 1), done: make(chan struct{}), ctx: ctx}
	s.ch <- protocol.StreamEvent{Type: protocol.EvStreamError, Err: err}
	close(s.ch)
	return s
}
