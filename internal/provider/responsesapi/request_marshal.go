package responsesapi

import (
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"math"
	"strconv"
	"unicode/utf8"
)

var responseRawFormatOptions = []jsontext.Options{
	jsontext.AllowDuplicateNames(true),
	jsontext.AllowInvalidUTF8(true),
	jsontext.EscapeForHTML(true),
	jsontext.EscapeForJS(true),
	jsontext.PreserveRawStrings(true),
}

type responseJSONBuilder struct {
	buf []byte
	err error
}

func marshalRequestBody(body responsesRequest) ([]byte, error) {
	// Preserve encoding/json's rejection of non-finite values. Normalized chat
	// requests never contain them, so the compatibility fallback stays cold.
	if body.Temperature != nil && (math.IsNaN(*body.Temperature) || math.IsInf(*body.Temperature, 0)) {
		return json.Marshal(body)
	}
	builder := responseJSONBuilder{buf: make([]byte, 0, requestBodyJSONCapacity(body))}
	builder.appendRequest(body)
	if builder.err != nil {
		return nil, builder.err
	}
	return builder.buf, nil
}

func (b *responseJSONBuilder) appendRequest(body responsesRequest) {
	b.literal(`{"model":`)
	b.quote(body.Model)
	b.literal(`,"store":`)
	b.boolean(body.Store)
	b.literal(`,"stream":`)
	b.boolean(body.Stream)
	if body.Instructions != "" {
		b.literal(`,"instructions":`)
		b.quote(body.Instructions)
	}
	if body.Input == nil {
		b.literal(`,"input":null`)
	} else {
		b.literal(`,"input":[`)
		for i, input := range body.Input {
			if i > 0 {
				b.byte(',')
			}
			b.appendInput(input)
		}
		b.byte(']')
	}
	if len(body.Tools) > 0 {
		b.literal(`,"tools":[`)
		for i, tool := range body.Tools {
			if i > 0 {
				b.byte(',')
			}
			b.appendTool(tool)
		}
		b.byte(']')
	}
	if body.Reasoning != nil {
		b.literal(`,"reasoning":{"effort":`)
		b.quote(body.Reasoning.Effort)
		if body.Reasoning.Summary != "" {
			b.literal(`,"summary":`)
			b.quote(body.Reasoning.Summary)
		}
		b.byte('}')
	}
	if len(body.Include) > 0 {
		b.literal(`,"include":[`)
		for i, include := range body.Include {
			if i > 0 {
				b.byte(',')
			}
			b.quote(include)
		}
		b.byte(']')
	}
	if body.Text != nil {
		b.literal(`,"text":{"verbosity":`)
		b.quote(body.Text.Verbosity)
		b.byte('}')
	}
	if body.MaxOutputTokens != 0 {
		b.literal(`,"max_output_tokens":`)
		b.buf = strconv.AppendInt(b.buf, int64(body.MaxOutputTokens), 10)
	}
	if body.Temperature != nil {
		b.literal(`,"temperature":`)
		b.buf = jsontext.AppendFloat(b.buf, *body.Temperature, 64)
	}
	if body.PromptCacheKey != "" {
		b.literal(`,"prompt_cache_key":`)
		b.quote(body.PromptCacheKey)
	}
	if body.ToolChoice != "" {
		b.literal(`,"tool_choice":`)
		b.quote(body.ToolChoice)
	}
	if body.ParallelToolCalls != nil {
		b.literal(`,"parallel_tool_calls":`)
		b.boolean(*body.ParallelToolCalls)
	}
	b.byte('}')
}

func (b *responseJSONBuilder) appendInput(input any) {
	if b.err != nil {
		return
	}
	switch input := input.(type) {
	case *responseMessageInput:
		b.appendMessage(input.Content, input.Role, input.Status, input.Type)
	case *responseSingleMessageInput:
		b.appendMessage(input.Content[:], input.Role, input.Status, input.Type)
	case *responseFunctionCallInput:
		b.literal(`{"arguments":`)
		b.quote(input.Arguments)
		b.literal(`,"call_id":`)
		b.quote(input.CallID)
		b.literal(`,"name":`)
		b.quote(input.Name)
		b.literal(`,"status":`)
		b.quote(input.Status)
		b.literal(`,"type":`)
		b.quote(input.Type)
		b.byte('}')
	case *responseFunctionCallOutputText:
		b.literal(`{"call_id":`)
		b.quote(input.CallID)
		b.literal(`,"output":`)
		b.quote(input.Output)
		b.literal(`,"type":`)
		b.quote(input.Type)
		b.byte('}')
	case *responseFunctionCallOutputContent:
		b.literal(`{"call_id":`)
		b.quote(input.CallID)
		b.literal(`,"output":`)
		b.appendContent(input.Output)
		b.literal(`,"type":`)
		b.quote(input.Type)
		b.byte('}')
	case *responseLegacyReasoningInput:
		b.literal(`{"encrypted_content":`)
		b.quote(input.EncryptedContent)
		b.literal(`,"id":`)
		b.quote(input.ID)
		b.literal(`,"summary":[],"type":`)
		b.quote(input.Type)
		b.byte('}')
	case json.RawMessage:
		b.raw(input)
	default:
		b.err = errors.New("responses request contains an unsupported input item")
	}
}

func (b *responseJSONBuilder) appendMessage(content []responseInputContent, role, status, messageType string) {
	b.literal(`{"content":`)
	b.appendContent(content)
	b.literal(`,"role":`)
	b.quote(role)
	if status != "" {
		b.literal(`,"status":`)
		b.quote(status)
	}
	if messageType != "" {
		b.literal(`,"type":`)
		b.quote(messageType)
	}
	b.byte('}')
}

func (b *responseJSONBuilder) appendContent(content []responseInputContent) {
	if content == nil {
		b.literal("null")
		return
	}
	b.byte('[')
	for i, item := range content {
		if i > 0 {
			b.byte(',')
		}
		b.byte('{')
		wrote := false
		if item.Detail != "" {
			b.literal(`"detail":`)
			b.quote(item.Detail)
			wrote = true
		}
		if item.ImageURL != "" {
			if wrote {
				b.byte(',')
			}
			b.literal(`"image_url":`)
			b.quoteSafeASCII(item.ImageURL)
			wrote = true
		}
		if item.Text != "" {
			if wrote {
				b.byte(',')
			}
			b.literal(`"text":`)
			b.quote(item.Text)
			wrote = true
		}
		if wrote {
			b.byte(',')
		}
		b.literal(`"type":`)
		b.quote(item.Type)
		b.byte('}')
	}
	b.byte(']')
}

func (b *responseJSONBuilder) appendTool(tool responsesTool) {
	b.literal(`{"type":`)
	b.quote(tool.Type)
	b.literal(`,"name":`)
	b.quote(tool.Name)
	if tool.Description != "" {
		b.literal(`,"description":`)
		b.quote(tool.Description)
	}
	if len(tool.Parameters) > 0 {
		b.literal(`,"parameters":`)
		b.raw(tool.Parameters)
	}
	b.literal(`,"strict":`)
	b.boolean(tool.Strict)
	b.byte('}')
}

func (b *responseJSONBuilder) quote(value string) {
	if b.err == nil {
		b.buf = appendResponseJSONString(b.buf, value)
	}
}

func (b *responseJSONBuilder) quoteSafeASCII(value string) {
	if b.err != nil {
		return
	}
	b.byte('"')
	b.literal(value)
	b.byte('"')
}

func appendResponseJSONString(dst []byte, src string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(src); {
		if char := src[i]; char < utf8.RuneSelf {
			if char >= 0x20 && char != '\\' && char != '"' && char != '<' && char != '>' && char != '&' {
				i++
				continue
			}
			dst = append(dst, src[start:i]...)
			switch char {
			case '\\', '"':
				dst = append(dst, '\\', char)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hex[char>>4], hex[char&0xf])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(src[i:])
		if r == utf8.RuneError && size == 1 {
			dst = append(dst, src[start:i]...)
			dst = append(dst, '\xef', '\xbf', '\xbd')
			i++
			start = i
			continue
		}
		if r == '\u2028' || r == '\u2029' {
			dst = append(dst, src[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', hex[r&0xf])
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, src[start:]...)
	return append(dst, '"')
}

func (b *responseJSONBuilder) raw(value json.RawMessage) {
	if b.err != nil {
		return
	}
	if value == nil {
		b.literal("null")
		return
	}
	formatted, err := jsontext.AppendFormat(b.buf, value, responseRawFormatOptions...)
	if err != nil {
		b.err = errors.New("responses request contains malformed raw JSON")
		return
	}
	b.buf = formatted
}

func (b *responseJSONBuilder) literal(value string) {
	if b.err == nil {
		b.buf = append(b.buf, value...)
	}
}

func (b *responseJSONBuilder) byte(value byte) {
	if b.err == nil {
		b.buf = append(b.buf, value)
	}
}

func (b *responseJSONBuilder) boolean(value bool) {
	if value {
		b.literal("true")
	} else {
		b.literal("false")
	}
}

func requestBodyJSONCapacity(body responsesRequest) int {
	fixedBytes := len(`{"model":"","store":false,"stream":false,"input":[]}`)
	if body.Input == nil {
		fixedBytes += len("null") - len("[]")
	}
	stringBytes := len(body.Model)
	quoteRiskBytes := len(body.Model)
	if body.Instructions != "" {
		fixedBytes += len(`,"instructions":""`)
		stringBytes += len(body.Instructions)
		quoteRiskBytes += len(body.Instructions)
	}
	if len(body.Input) > 0 {
		fixedBytes += len(body.Input) - 1
	}
	for _, input := range body.Input {
		switch input := input.(type) {
		case *responseMessageInput:
			fixedBytes, stringBytes, quoteRiskBytes = responseMessageJSONCapacity(
				input.Content, input.Role, input.Status, input.Type, fixedBytes, stringBytes, quoteRiskBytes,
			)
		case *responseSingleMessageInput:
			fixedBytes, stringBytes, quoteRiskBytes = responseMessageJSONCapacity(
				input.Content[:], input.Role, input.Status, input.Type, fixedBytes, stringBytes, quoteRiskBytes,
			)
		case *responseFunctionCallInput:
			fixedBytes += len(`{"arguments":"","call_id":"","name":"","status":"","type":""}`)
			values := len(input.Arguments) + len(input.CallID) + len(input.Name) + len(input.Status) + len(input.Type)
			stringBytes += values
			quoteRiskBytes += values
		case *responseFunctionCallOutputText:
			fixedBytes += len(`{"call_id":"","output":"","type":""}`)
			values := len(input.CallID) + len(input.Output) + len(input.Type)
			stringBytes += values
			quoteRiskBytes += values
		case *responseFunctionCallOutputContent:
			fixedBytes += len(`{"call_id":"","output":[],"type":""}`)
			values := len(input.CallID) + len(input.Type)
			stringBytes += values
			quoteRiskBytes += values
			fixedBytes, stringBytes, quoteRiskBytes = responseContentJSONCapacity(input.Output, fixedBytes, stringBytes, quoteRiskBytes)
		case *responseLegacyReasoningInput:
			fixedBytes += len(`{"encrypted_content":"","id":"","summary":[],"type":""}`)
			values := len(input.EncryptedContent) + len(input.ID) + len(input.Type)
			stringBytes += values
			quoteRiskBytes += values
		case json.RawMessage:
			if input == nil {
				fixedBytes += len("null")
			} else {
				fixedBytes += len(input)
			}
		}
	}
	if len(body.Tools) > 0 {
		fixedBytes += len(`,"tools":[]`) + len(body.Tools) - 1
		for _, tool := range body.Tools {
			fixedBytes += len(`{"type":"","name":"","strict":false}`)
			values := len(tool.Type) + len(tool.Name)
			if tool.Description != "" {
				fixedBytes += len(`,"description":""`)
				values += len(tool.Description)
			}
			if len(tool.Parameters) > 0 {
				fixedBytes += len(`,"parameters":`) + len(tool.Parameters)
			}
			stringBytes += values
			quoteRiskBytes += values
		}
	}
	if body.Reasoning != nil {
		fixedBytes += len(`,"reasoning":{"effort":""}`)
		stringBytes += len(body.Reasoning.Effort)
		quoteRiskBytes += len(body.Reasoning.Effort)
		if body.Reasoning.Summary != "" {
			fixedBytes += len(`,"summary":""`)
			stringBytes += len(body.Reasoning.Summary)
			quoteRiskBytes += len(body.Reasoning.Summary)
		}
	}
	if len(body.Include) > 0 {
		fixedBytes += len(`,"include":[]`) + len(body.Include) - 1
		for _, include := range body.Include {
			fixedBytes += 2
			stringBytes += len(include)
			quoteRiskBytes += len(include)
		}
	}
	if body.Text != nil {
		fixedBytes += len(`,"text":{"verbosity":""}`)
		stringBytes += len(body.Text.Verbosity)
		quoteRiskBytes += len(body.Text.Verbosity)
	}
	if body.MaxOutputTokens != 0 {
		fixedBytes += len(`,"max_output_tokens":`) + 20
	}
	if body.Temperature != nil {
		fixedBytes += len(`,"temperature":`) + 24
	}
	if body.PromptCacheKey != "" {
		fixedBytes += len(`,"prompt_cache_key":""`)
		stringBytes += len(body.PromptCacheKey)
		quoteRiskBytes += len(body.PromptCacheKey)
	}
	if body.ToolChoice != "" {
		fixedBytes += len(`,"tool_choice":""`)
		stringBytes += len(body.ToolChoice)
		quoteRiskBytes += len(body.ToolChoice)
	}
	if body.ParallelToolCalls != nil {
		fixedBytes += len(`,"parallel_tool_calls":false`)
	}
	// Most model text has sparse escaping. Reserve a small allowance without
	// rescanning every string before the encoder performs its authoritative pass.
	return fixedBytes + stringBytes + quoteRiskBytes/32
}

func responseMessageJSONCapacity(content []responseInputContent, role, status, messageType string, fixedBytes, stringBytes, quoteRiskBytes int) (int, int, int) {
	fixedBytes += len(`{"content":[],"role":""}`)
	stringBytes += len(role)
	quoteRiskBytes += len(role)
	if status != "" {
		fixedBytes += len(`,"status":""`)
		stringBytes += len(status)
		quoteRiskBytes += len(status)
	}
	if messageType != "" {
		fixedBytes += len(`,"type":""`)
		stringBytes += len(messageType)
		quoteRiskBytes += len(messageType)
	}
	return responseContentJSONCapacity(content, fixedBytes, stringBytes, quoteRiskBytes)
}

func responseContentJSONCapacity(content []responseInputContent, fixedBytes, stringBytes, quoteRiskBytes int) (int, int, int) {
	if len(content) > 0 {
		fixedBytes += len(content) - 1
	}
	for _, item := range content {
		fixedBytes += len(`{"type":""}`)
		stringBytes += len(item.Type)
		quoteRiskBytes += len(item.Type)
		if item.Detail != "" {
			fixedBytes += len(`,"detail":""`)
			stringBytes += len(item.Detail)
			quoteRiskBytes += len(item.Detail)
		}
		if item.ImageURL != "" {
			fixedBytes += len(`,"image_url":""`)
			stringBytes += len(item.ImageURL)
			// Data URIs contain only JSON-safe ASCII and need no escape allowance.
		}
		if item.Text != "" {
			fixedBytes += len(`,"text":""`)
			stringBytes += len(item.Text)
			quoteRiskBytes += len(item.Text)
		}
	}
	return fixedBytes, stringBytes, quoteRiskBytes
}
