package responsesapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type codexStreamBounds struct {
	totalToolArgumentBytes  int
	completedReasoningBytes int
	completedReasoningItems int
	responseTextBytes       int
}

func (s *codexStream) processEvent(event map[string]any, calls map[string]*toolAccum, reasoning *reasoningAccum, bounds *codexStreamBounds, finish *protocol.StopReason, sawTool *bool, terminal ...*bool) bool {
	typ, _ := event["type"].(string)
	switch typ {
	case "response.output_text.delta", "response.refusal.delta":
		if delta, _ := event["delta"].(string); delta != "" {
			if len(delta) > maxResponseTextBytes-bounds.responseTextBytes {
				return s.streamLimitError("response text exceeds size limit")
			}
			bounds.responseTextBytes += len(delta)
			s.send(protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: delta})
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if delta, _ := event["delta"].(string); delta != "" {
			key := reasoningIdentity(event, typ)
			if err := reasoning.canAppend(key, delta); err != nil {
				return s.streamLimitError(err.Error())
			}
			s.send(protocol.StreamEvent{Type: protocol.EvStreamThinkingDelta, Text: reasoning.append(key, delta)})
		}
	case "response.reasoning_summary_text.done", "response.reasoning_summary_part.done", "response.reasoning_text.done":
		if text := reasoningDoneText(event, typ); text != "" {
			key := reasoningIdentity(event, typ)
			if suffix := missingReasoningSuffix(reasoning.text(key), text); suffix != "" {
				if err := reasoning.canAppend(key, suffix); err != nil {
					return s.streamLimitError(err.Error())
				}
				s.send(protocol.StreamEvent{Type: protocol.EvStreamThinkingDelta, Text: reasoning.append(key, suffix)})
			}
		}
	case "response.function_call_arguments.delta":
		*sawTool = true
		if codexToolIdentityBytes(event) > maxCodexIdentityBytes {
			return s.streamLimitError("tool-call identity exceeds size limit")
		}
		key, id, name, created := toolIdentity(event, calls)
		if created && len(calls) > maxCodexStreamToolCalls {
			return s.streamLimitError("tool-call count exceeds limit")
		}
		acc := calls[key]
		if delta, _ := event["delta"].(string); delta != "" {
			if err := appendCodexToolArguments(acc, delta, bounds); err != nil {
				return s.streamLimitError(err.Error())
			}
			s.send(protocol.StreamEvent{Type: protocol.EvStreamToolCallDelta, ToolCallID: id, ToolName: name, Arguments: json.RawMessage(delta)})
		}
	case "response.function_call_arguments.done":
		*sawTool = true
		if codexToolIdentityBytes(event) > maxCodexIdentityBytes {
			return s.streamLimitError("tool-call identity exceeds size limit")
		}
		key, id, name, created := toolIdentity(event, calls)
		if created && len(calls) > maxCodexStreamToolCalls {
			return s.streamLimitError("tool-call count exceeds limit")
		}
		acc := calls[key]
		args, _ := event["arguments"].(string)
		if args != "" {
			if err := acceptCodexToolSnapshot(acc, args, bounds); err != nil {
				return s.streamLimitError(err.Error())
			}
		} else {
			args = acc.args.String()
		}
		s.send(protocol.StreamEvent{Type: protocol.EvStreamToolCallDone, ToolCallID: id, ToolName: name, Arguments: persistableToolArguments(args)})
	case "response.output_item.added", "response.output_item.done":
		item, _ := event["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		if itemType == "reasoning" && typ == "response.output_item.done" {
			id, _ := item["id"].(string)
			if len(id) > maxCodexIdentityBytes {
				return s.streamLimitError("reasoning identity exceeds size limit")
			}
			if block := sanitizeCompletedReasoningItem(item); block != nil {
				if bounds.completedReasoningItems >= maxCodexReasoningItems || len(block.Data) > maxCodexReasoningBytes-bounds.completedReasoningBytes {
					return s.streamLimitError("completed reasoning exceeds size limit")
				}
				bounds.completedReasoningItems++
				bounds.completedReasoningBytes += len(block.Data)
				s.send(protocol.StreamEvent{Type: protocol.EvStreamProviderData, ProviderData: block})
			}
		}
		if itemType == "function_call" {
			*sawTool = true
			if codexItemIdentityBytes(item) > maxCodexIdentityBytes {
				return s.streamLimitError("tool-call identity exceeds size limit")
			}
			key, id, name, created := itemIdentity(event, item, calls)
			if created && len(calls) > maxCodexStreamToolCalls {
				return s.streamLimitError("tool-call count exceeds limit")
			}
			acc := calls[key]
			if args, _ := item["arguments"].(string); args != "" {
				wasEmpty := acc.args.Len() == 0
				if typ == "response.output_item.done" {
					if err := acceptCodexToolSnapshot(acc, args, bounds); err != nil {
						return s.streamLimitError(err.Error())
					}
				} else if wasEmpty {
					if err := appendCodexToolArguments(acc, args, bounds); err != nil {
						return s.streamLimitError(err.Error())
					}
				}
				if wasEmpty {
					s.send(protocol.StreamEvent{Type: protocol.EvStreamToolCallDelta, ToolCallID: id, ToolName: name, Arguments: json.RawMessage(args)})
				}
			}
			if typ == "response.output_item.done" {
				args := acc.args.String()
				s.send(protocol.StreamEvent{Type: protocol.EvStreamToolCallDone, ToolCallID: id, ToolName: name, Arguments: persistableToolArguments(args)})
			}
		}
	case "response.completed", "response.done", "response.incomplete":
		if usage := responseUsage(event); usage != nil {
			s.send(protocol.StreamEvent{Type: protocol.EvStreamUsage, Usage: usage})
		}
		if *finish == "" {
			if status, _ := nestedString(event, "response", "status"); status == "incomplete" || typ == "response.incomplete" {
				*finish = protocol.StopLength
			} else if *sawTool {
				*finish = protocol.StopToolUse
			} else {
				*finish = protocol.StopStop
			}
		}
		if len(terminal) > 0 && terminal[0] != nil {
			*terminal[0] = true
		}
		s.send(protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: *finish})
		return true
	case "response.failed", "error":
		message := eventMessage(event)
		if message == "" {
			message = "response failed"
		}
		s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: NewResponseError(s.provider, 0, message, eventCode(event), eventRequestID(event), s.secrets...)})
		return true
	}
	return false
}

func sanitizeCompletedReasoningItem(item map[string]any) *protocol.ContentBlock {
	id, _ := item["id"].(string)
	if id == "" {
		return nil
	}
	summary := []any{}
	if value, ok := item["summary"]; ok {
		var valid bool
		summary, valid = sanitizeReasoningParts(value, "summary_text")
		if !valid {
			return nil
		}
	}
	wire := persistedReasoningItem{Type: "reasoning", ID: id, Summary: summary}
	if value, ok := item["content"]; ok {
		content, valid := sanitizeReasoningParts(value, "reasoning_text")
		if !valid {
			return nil
		}
		wire.Content = &content
	}
	if value, ok := item["encrypted_content"]; ok {
		encrypted, valid := value.(string)
		if !valid {
			return nil
		}
		wire.EncryptedContent = &encrypted
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return nil
	}
	return &protocol.ContentBlock{Type: protocol.BlockProviderData, Name: id, Data: data}
}

func sanitizeReasoningParts(value any, expectedType string) ([]any, bool) {
	parts, ok := value.([]any)
	if !ok {
		return nil, false
	}
	sanitized := make([]any, 0, len(parts))
	for _, value := range parts {
		part, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		typ, typeOK := part["type"].(string)
		text, textOK := part["text"].(string)
		if !typeOK || typ != expectedType || !textOK {
			return nil, false
		}
		sanitized = append(sanitized, map[string]any{"type": typ, "text": text})
	}
	return sanitized, true
}

func reasoningIdentity(event map[string]any, typ string) string {
	family := "summary"
	indexField := "summary_index"
	if strings.Contains(typ, "reasoning_text") && !strings.Contains(typ, "summary") {
		family = "text"
		indexField = "content_index"
	}
	itemID, _ := event["item_id"].(string)
	if itemID == "" {
		itemID = fmt.Sprintf("output-%d", intNumber(event["output_index"]))
	}
	return fmt.Sprintf("%s:%s:%d", family, itemID, intNumber(event[indexField]))
}

func reasoningDoneText(event map[string]any, typ string) string {
	if typ == "response.reasoning_summary_part.done" {
		part, _ := event["part"].(map[string]any)
		text, _ := part["text"].(string)
		return text
	}
	text, _ := event["text"].(string)
	return text
}

// missingReasoningSuffix merges a completed snapshot into an append-only
// delta stream. Completed events commonly repeat all text already delivered by
// deltas; only a genuinely missing suffix is safe to publish. A shorter or
// divergent snapshot must not overwrite or duplicate visible reasoning.
func missingReasoningSuffix(streamed, completed string) string {
	if streamed == "" {
		return completed
	}
	if after, ok := strings.CutPrefix(completed, streamed); ok {
		return after
	}
	return ""
}

func codexToolIdentityBytes(event map[string]any) int {
	total := 0
	for _, field := range []string{"item_id", "call_id", "name"} {
		if value, _ := event[field].(string); value != "" {
			total += len(value)
		}
	}
	return total
}

func codexItemIdentityBytes(item map[string]any) int {
	total := 0
	for _, field := range []string{"id", "call_id", "name"} {
		if value, _ := item[field].(string); value != "" {
			total += len(value)
		}
	}
	return total
}

func toolIdentity(event map[string]any, calls map[string]*toolAccum) (string, string, string, bool) {
	key, _ := event["item_id"].(string)
	if key == "" {
		if n, ok := event["output_index"].(float64); ok {
			key = fmt.Sprintf("output-%d", int(n))
		}
	}
	id, _ := event["call_id"].(string)
	name, _ := event["name"].(string)
	if key == "" {
		key = id
	}
	if key == "" {
		key = "call-0"
	}
	acc := calls[key]
	created := acc == nil
	if created {
		acc = &toolAccum{id: id, name: name}
		calls[key] = acc
	}
	if id == "" {
		id = acc.id
	}
	if name == "" {
		name = acc.name
	}
	if id == "" {
		id = key
		acc.id = id
	}
	if name != "" {
		acc.name = name
	}
	return key, id, name, created
}

func itemIdentity(event, item map[string]any, calls map[string]*toolAccum) (string, string, string, bool) {
	key, _ := item["id"].(string)
	id, _ := item["call_id"].(string)
	name, _ := item["name"].(string)
	if id == "" {
		id = key
	}
	if key == "" {
		key = id
	}
	if key == "" {
		if n, ok := event["output_index"].(float64); ok {
			key = fmt.Sprintf("output-%d", int(n))
		}
	}
	if key == "" {
		// With no protocol identity there is no sound way to correlate items.
		// Treat each event as a distinct anonymous call so malformed streams
		// remain count-bounded rather than collapsing forever into call-0.
		key = fmt.Sprintf("anonymous-%d", len(calls))
	}
	if id == "" {
		id = key
	}
	acc := calls[key]
	created := acc == nil
	if created {
		acc = &toolAccum{id: id, name: name}
		calls[key] = acc
	}
	return key, id, name, created
}

func persistableToolArguments(arguments string) json.RawMessage {
	if arguments == "" {
		return json.RawMessage("{}")
	}
	if json.Valid([]byte(arguments)) {
		return json.RawMessage(arguments)
	}
	encoded, _ := json.Marshal(arguments)
	return json.RawMessage(encoded)
}

func appendCodexToolArguments(acc *toolAccum, fragment string, bounds *codexStreamBounds) error {
	if len(fragment) > maxCodexToolArgumentBytes-acc.args.Len() {
		return errors.New("tool arguments exceed per-call size limit")
	}
	if len(fragment) > maxCodexTotalToolArgumentBytes-bounds.totalToolArgumentBytes {
		return errors.New("tool arguments exceed total size limit")
	}
	acc.args.WriteString(fragment)
	bounds.totalToolArgumentBytes += len(fragment)
	return nil
}

func acceptCodexToolSnapshot(acc *toolAccum, arguments string, bounds *codexStreamBounds) error {
	if len(arguments) > maxCodexToolArgumentBytes {
		return errors.New("tool arguments exceed per-call size limit")
	}
	if acc.args.Len() == 0 {
		return appendCodexToolArguments(acc, arguments, bounds)
	}
	// Deltas are already charged. A completed snapshot may repeat those bytes;
	// charge only growth beyond the accumulated form so distinct large
	// snapshots cannot bypass the aggregate bound without double-counting the
	// normal repeated snapshot.
	additional := max(0, len(arguments)-acc.args.Len())
	if additional > maxCodexTotalToolArgumentBytes-bounds.totalToolArgumentBytes {
		return errors.New("tool arguments exceed total size limit")
	}
	bounds.totalToolArgumentBytes += additional
	// The completed snapshot is authoritative. Preserve it for any later
	// output_item.done reconciliation instead of re-emitting stale deltas.
	acc.args.Reset()
	acc.args.WriteString(arguments)
	return nil
}
