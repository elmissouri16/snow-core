package opencodego

import (
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

var chatRawFormatOptions = []jsontext.Options{
	jsontext.AllowDuplicateNames(true),
	jsontext.AllowInvalidUTF8(true),
	jsontext.EscapeForHTML(true),
	jsontext.EscapeForJS(true),
	jsontext.PreserveRawStrings(true),
}

type chatJSONBuilder struct {
	buf []byte
	err error
}

func marshalChatRequest(body openAIChatRequest) ([]byte, error) {
	if body.Temperature != nil && (math.IsNaN(*body.Temperature) || math.IsInf(*body.Temperature, 0)) {
		return json.Marshal(body)
	}
	builder := chatJSONBuilder{buf: make([]byte, 0, chatRequestJSONCapacity(body))}
	builder.appendRequest(body)
	if builder.err != nil {
		return nil, builder.err
	}
	return builder.buf, nil
}

func (b *chatJSONBuilder) appendRequest(body openAIChatRequest) {
	b.literal(`{"model":`)
	b.quote(body.Model)
	b.literal(`,"messages":`)
	if body.Messages == nil {
		b.literal("null")
	} else {
		b.byte('[')
		for i := range body.Messages {
			if i > 0 {
				b.byte(',')
			}
			b.appendMessage(body.Messages[i])
		}
		b.byte(']')
	}
	b.literal(`,"stream":`)
	b.boolean(body.Stream)
	if body.StreamOptions != nil {
		b.literal(`,"stream_options":{"include_usage":`)
		b.boolean(body.StreamOptions.IncludeUsage)
		b.byte('}')
	}
	if len(body.Tools) > 0 {
		b.literal(`,"tools":[`)
		for i := range body.Tools {
			if i > 0 {
				b.byte(',')
			}
			b.appendTool(body.Tools[i])
		}
		b.byte(']')
	}
	if body.Temperature != nil {
		b.literal(`,"temperature":`)
		b.buf = jsontext.AppendFloat(b.buf, *body.Temperature, 64)
	}
	if body.MaxTokens != nil {
		b.literal(`,"max_tokens":`)
		b.buf = strconv.AppendInt(b.buf, int64(*body.MaxTokens), 10)
	}
	if body.ReasoningEffort != nil {
		b.literal(`,"reasoning_effort":`)
		b.quote(*body.ReasoningEffort)
	}
	b.byte('}')
}

func (b *chatJSONBuilder) appendMessage(message openAIMessage) {
	b.literal(`{"role":`)
	b.quote(message.Role)
	if message.Content != nil {
		b.literal(`,"content":`)
		b.appendContent(message.Content)
	}
	if message.ToolCallID != "" {
		b.literal(`,"tool_call_id":`)
		b.quote(message.ToolCallID)
	}
	if len(message.ToolCalls) > 0 {
		b.literal(`,"tool_calls":[`)
		for i := range message.ToolCalls {
			if i > 0 {
				b.byte(',')
			}
			b.appendToolCall(message.ToolCalls[i])
		}
		b.byte(']')
	}
	b.byte('}')
}

func (b *chatJSONBuilder) appendContent(content any) {
	switch value := content.(type) {
	case string:
		b.quote(value)
	case []openAIContentPart:
		if value == nil {
			b.literal("null")
			return
		}
		b.byte('[')
		for i := range value {
			if i > 0 {
				b.byte(',')
			}
			b.appendContentPart(value[i])
		}
		b.byte(']')
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			b.err = err
			return
		}
		b.buf = append(b.buf, encoded...)
	}
}

func (b *chatJSONBuilder) appendContentPart(part openAIContentPart) {
	b.literal(`{"type":`)
	b.quote(part.Type)
	if part.Text != "" {
		b.literal(`,"text":`)
		b.quote(part.Text)
	}
	if part.ImageURL != nil {
		b.literal(`,"image_url":{"url":`)
		b.quote(part.ImageURL.URL)
		b.literal("}")
	}
	b.byte('}')
}

func (b *chatJSONBuilder) appendToolCall(call openAIToolCall) {
	b.byte('{')
	wrote := false
	if call.ID != "" {
		b.literal(`"id":`)
		b.quote(call.ID)
		wrote = true
	}
	if call.Type != "" {
		if wrote {
			b.byte(',')
		}
		b.literal(`"type":`)
		b.quote(call.Type)
		wrote = true
	}
	if wrote {
		b.byte(',')
	}
	b.literal(`"function":`)
	b.appendToolCallFunction(call.Function)
	b.byte('}')
}

func (b *chatJSONBuilder) appendToolCallFunction(function openAIToolCallFunction) {
	b.byte('{')
	wrote := false
	if function.Name != "" {
		b.literal(`"name":`)
		b.quote(function.Name)
		wrote = true
	}
	if function.Arguments != "" {
		if wrote {
			b.byte(',')
		}
		b.literal(`"arguments":`)
		b.quote(function.Arguments)
	}
	b.byte('}')
}

func (b *chatJSONBuilder) appendTool(tool openAITool) {
	b.literal(`{"type":`)
	b.quote(tool.Type)
	b.literal(`,"function":{"name":`)
	b.quote(tool.Function.Name)
	b.literal(`,"description":`)
	b.quote(tool.Function.Description)
	b.literal(`,"parameters":`)
	b.raw(tool.Function.Parameters)
	b.literal("}}")
}

func (b *chatJSONBuilder) quote(value string) {
	if b.err == nil {
		b.buf = appendChatJSONString(b.buf, value)
	}
}

func appendChatJSONString(dst []byte, src string) []byte {
	start := len(dst)
	quoted, err := jsontext.AppendQuote(dst, src)
	if err == nil && strings.IndexByte(src, '<') < 0 && strings.IndexByte(src, '>') < 0 && strings.IndexByte(src, '&') < 0 &&
		!strings.Contains(src, "\u2028") && !strings.Contains(src, "\u2029") {
		return quoted
	}
	return appendChatJSONStringCompat(dst[:start], src)
}

func appendChatJSONStringCompat(dst []byte, src string) []byte {
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

func (b *chatJSONBuilder) raw(value json.RawMessage) {
	if b.err != nil {
		return
	}
	if value == nil {
		b.literal("null")
		return
	}
	formatted, err := jsontext.AppendFormat(b.buf, value, chatRawFormatOptions...)
	if err != nil {
		b.err = errors.New("chat request contains malformed raw JSON")
		return
	}
	b.buf = formatted
}

func (b *chatJSONBuilder) literal(value string) {
	if b.err == nil {
		b.buf = append(b.buf, value...)
	}
}

func (b *chatJSONBuilder) byte(value byte) {
	if b.err == nil {
		b.buf = append(b.buf, value)
	}
}

func (b *chatJSONBuilder) boolean(value bool) {
	if value {
		b.literal("true")
	} else {
		b.literal("false")
	}
}

func chatRequestJSONCapacity(body openAIChatRequest) int {
	size := 128 + len(body.Model)
	for _, message := range body.Messages {
		size += 48 + len(message.Role) + len(message.ToolCallID)
		switch content := message.Content.(type) {
		case string:
			size += len(content) + len(content)/8
		case []openAIContentPart:
			for _, part := range content {
				size += 48 + len(part.Type) + len(part.Text)
				if part.ImageURL != nil {
					size += len(part.ImageURL.URL)
				}
			}
		}
		for _, call := range message.ToolCalls {
			size += 80 + len(call.ID) + len(call.Type) + len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	for _, tool := range body.Tools {
		size += 96 + len(tool.Type) + len(tool.Function.Name) + len(tool.Function.Description) + len(tool.Function.Parameters)
	}
	return size
}
