package responsesapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type responsesRequest struct {
	Model             string          `json:"model"`
	Store             bool            `json:"store"`
	Stream            bool            `json:"stream"`
	Instructions      string          `json:"instructions,omitempty"`
	Input             []any           `json:"input"`
	Tools             []responsesTool `json:"tools,omitempty"`
	Reasoning         *reasoning      `json:"reasoning,omitempty"`
	Include           []string        `json:"include,omitempty"`
	Text              *responseText   `json:"text,omitempty"`
	MaxOutputTokens   int             `json:"max_output_tokens,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	PromptCacheKey    string          `json:"prompt_cache_key,omitempty"`
	ToolChoice        string          `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
}

type responseText struct {
	Verbosity string `json:"verbosity"`
}

type reasoning struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary,omitempty"`
}

type persistedReasoningItem struct {
	Type             string  `json:"type"`
	ID               string  `json:"id"`
	Summary          []any   `json:"summary"`
	Content          *[]any  `json:"content,omitempty"`
	EncryptedContent *string `json:"encrypted_content,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict"`
}

func BuildRequest(req protocol.ChatRequest, opts RequestOptions) ([]byte, error) {
	model := req.Model.ID
	if model == "" {
		return nil, errors.New("model is required")
	}
	level := protocol.NormalizeThinkingLevel(req.Thinking)
	summary, err := protocol.ParseReasoningSummary(string(req.ReasoningSummary))
	if err != nil {
		return nil, err
	}
	verbosity, err := protocol.ParseTextVerbosity(string(req.TextVerbosity))
	if err != nil {
		return nil, err
	}
	if !req.Model.SupportsThinkingLevel(level) {
		return nil, unsupportedThinkingError(opts.ProviderID, req.Model, model, level)
	}
	body := responsesRequest{
		Model:             model,
		Store:             false,
		Stream:            true,
		Instructions:      req.System,
		Input:             make([]any, 0, len(req.Messages)),
		PromptCacheKey:    opts.PromptCacheKey,
		ToolChoice:        opts.ToolChoice,
		ParallelToolCalls: opts.ParallelToolCalls,
	}
	if !opts.OmitMaxOutputTokens {
		body.MaxOutputTokens = req.MaxTokens
	}
	if !opts.OmitTemperature {
		body.Temperature = req.Temperature
	}
	// Legacy/custom request models did not carry this capability bit. Keep the
	// historical field for those, while respecting an authenticated ChatGPT
	// catalog record that explicitly does not support verbosity.
	if req.Model.SupportsVerbosity || opts.AllowLegacyVerbosity {
		body.Text = &responseText{Verbosity: string(verbosity)}
	}
	if opts.IncludeEncryptedReasoning {
		body.Include = []string{"reasoning.encrypted_content"}
	}
	if body.Instructions == "" {
		body.Instructions = "You are a helpful assistant."
	}
	for _, msg := range req.Messages {
		input, err := responseInput(msg, opts.ProviderID)
		if err != nil {
			return nil, err
		}
		body.Input = append(body.Input, input...)
	}
	for _, fragment := range req.InternalContext {
		if err := fragment.Validate(); err != nil {
			return nil, err
		}
		body.Input = append(body.Input, map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": renderInternalFragment(fragment)}}})
	}
	if req.Model.SupportsTools {
		for _, tool := range req.Tools {
			params := tool.Parameters
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			body.Tools = append(body.Tools, responsesTool{
				Type:        "function",
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
				Strict:      false,
			})
		}
	}
	if level != protocol.ThinkingOff {
		if effort, ok := mapThinkingEffort(level); ok {
			body.Reasoning = &reasoning{Effort: effort}
			if summary != protocol.ReasoningSummaryOff && (req.Model.SupportsReasoningSummary == nil || *req.Model.SupportsReasoningSummary) {
				body.Reasoning.Summary = string(summary)
			}
		}
	}
	return json.Marshal(body)
}

func mapThinkingEffort(level protocol.ThinkingLevel) (string, bool) {
	switch level {
	case protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium,
		protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax, protocol.ThinkingUltra:
		return string(level), true
	default:
		return "", false
	}
}

func unsupportedThinkingError(providerID string, model protocol.Model, modelID string, level protocol.ThinkingLevel) error {
	allowed := model.SupportedThinkingLevels()
	parts := make([]string, 0, len(allowed))
	for _, supported := range allowed {
		parts = append(parts, string(supported))
	}
	return fmt.Errorf("%s: model %q does not advertise thinking level %q (supported: %s)", providerLabel(providerID), modelID, level, strings.Join(parts, "|"))
}

func renderInternalFragment(fragment protocol.InternalContextFragment) string {
	return "<snow_internal_context source=\"" + fragment.Source + "\">\n" + fragment.Text + "\n</snow_internal_context>"
}

// MessageInput exposes the provider-neutral history encoder to sibling
// Responses adapters and package regression tests.
func MessageInput(msg protocol.Message, providerID string) ([]any, error) {
	return responseInput(msg, providerID)
}

func responseInput(msg protocol.Message, providerID string) ([]any, error) {
	text := messageText(msg)
	switch msg.Role {
	case protocol.RoleUser, protocol.RoleAgent:
		content, err := userInputContent(msg.Content)
		if err != nil {
			return nil, err
		}
		if len(content) == 0 {
			return nil, nil
		}
		return []any{map[string]any{"role": "user", "content": content}}, nil
	case protocol.RoleAssistant:
		var out []any
		for _, block := range msg.Content {
			switch block.Type {
			case protocol.BlockProviderData:
				// Opaque continuity is scoped to the provider that produced the
				// assistant message. Never forward it to another endpoint, replay
				// unattributed legacy data, or reuse an item from a response that the
				// provider did not complete successfully. Failed/aborted Responses
				// may emit encrypted reasoning before their terminal event; omit it
				// conservatively so a later retry does not depend on failed history.
				if providerID == "" || msg.Provider != providerID || msg.StopReason == protocol.StopError || msg.StopReason == protocol.StopAborted {
					continue
				}
				item, err := replayProviderReasoning(block)
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			case protocol.BlockToolCall:
				args := strings.TrimSpace(string(block.Arguments))
				if args == "" {
					args = "{}"
				}
				out = append(out, map[string]any{
					"type":      "function_call",
					"call_id":   block.ToolCallID,
					"name":      block.Name,
					"arguments": args,
					"status":    "completed",
				})
			}
		}
		if text != "" {
			out = append(out, map[string]any{
				"type": "message", "role": "assistant", "status": "completed",
				"content": []any{map[string]any{"type": "output_text", "text": text}},
			})
		}
		return out, nil
	case protocol.RoleTool:
		output, err := responseToolOutput(msg.Content, text)
		if err != nil {
			return nil, err
		}
		return []any{map[string]any{
			"type": "function_call_output", "call_id": msg.ToolCallID, "output": output,
		}}, nil
	case protocol.RoleCustom:
		if text != "" {
			return []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}}, nil
		}
	}
	return nil, nil
}

func replayProviderReasoning(block protocol.ContentBlock) (any, error) {
	if block.Name == "" || len(block.Data) == 0 {
		return nil, errors.New("persisted provider reasoning data is malformed")
	}
	if !json.Valid(block.Data) {
		// Backward compatibility for the original persistence format, where Data
		// held only encrypted_content and Name held the reasoning item ID. Upgrade
		// it to the current wire shape because summary is required by Responses.
		return map[string]any{"type": "reasoning", "id": block.Name, "summary": []any{}, "encrypted_content": string(block.Data)}, nil
	}
	var item persistedReasoningItem
	var fields map[string]json.RawMessage
	if json.Unmarshal(block.Data, &item) != nil || json.Unmarshal(block.Data, &fields) != nil ||
		item.Type != "reasoning" || item.ID == "" || item.ID != block.Name || item.Summary == nil {
		return nil, errors.New("persisted provider reasoning data is malformed")
	}
	for name := range fields {
		switch name {
		case "type", "id", "summary", "content", "encrypted_content":
		default:
			return nil, errors.New("persisted provider reasoning data is malformed")
		}
	}
	if _, ok := fields["summary"]; !ok {
		return nil, errors.New("persisted provider reasoning data is malformed")
	}
	if _, valid := sanitizeReasoningParts(item.Summary, "summary_text"); !valid {
		return nil, errors.New("persisted provider reasoning data is malformed")
	}
	if raw, ok := fields["content"]; ok {
		if item.Content == nil || string(raw) == "null" {
			return nil, errors.New("persisted provider reasoning data is malformed")
		}
		if _, valid := sanitizeReasoningParts(*item.Content, "reasoning_text"); !valid {
			return nil, errors.New("persisted provider reasoning data is malformed")
		}
	}
	if raw, ok := fields["encrypted_content"]; ok && (item.EncryptedContent == nil || string(raw) == "null") {
		return nil, errors.New("persisted provider reasoning data is malformed")
	}
	return json.RawMessage(append([]byte(nil), block.Data...)), nil
}

func responseToolOutput(blocks []protocol.ContentBlock, text string) (any, error) {
	hasImage := false
	for _, block := range blocks {
		if block.Type == protocol.BlockImage {
			hasImage = true
			break
		}
	}
	if !hasImage {
		if text == "" {
			text = "(no tool output)"
		}
		return text, nil
	}
	return userInputContent(blocks)
}

func userInputContent(blocks []protocol.ContentBlock) ([]any, error) {
	var content []any
	for _, block := range blocks {
		switch block.Type {
		case protocol.BlockText:
			if block.Text != "" {
				content = append(content, map[string]any{"type": "input_text", "text": block.Text})
			}
		case protocol.BlockPlan:
			text := messageText(protocol.Message{Content: []protocol.ContentBlock{block}})
			if text != "" {
				content = append(content, map[string]any{"type": "input_text", "text": text})
			}
		case protocol.BlockImage:
			image, err := responseImageInput(block)
			if err != nil {
				return nil, err
			}
			content = append(content, image)
		}
	}
	return content, nil
}

func responseImageInput(block protocol.ContentBlock) (map[string]any, error) {
	mime := strings.ToLower(strings.TrimSpace(block.MIMEType))
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return nil, fmt.Errorf("unsupported image MIME type %q", block.MIMEType)
	}
	if len(block.Data) == 0 {
		return nil, errors.New("image content is empty")
	}
	if len(block.Data) > 20<<20 {
		return nil, errors.New("image content exceeds 20 MiB limit")
	}
	return map[string]any{
		"type": "input_image", "image_url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(block.Data), "detail": "high",
	}, nil
}

// MessageText returns the text/plan representation used by Responses input.
func MessageText(msg protocol.Message) string { return messageText(msg) }

func messageText(msg protocol.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		switch block.Type {
		case protocol.BlockText:
			b.WriteString(block.Text)
		case protocol.BlockPlan:
			if !block.PlanComplete {
				b.WriteString(block.Text)
				continue
			}
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
				b.WriteByte('\n')
			}
			b.WriteString("<proposed_plan>\n")
			b.WriteString(block.Text)
			if !strings.HasSuffix(block.Text, "\n") {
				b.WriteByte('\n')
			}
			b.WriteString("</proposed_plan>\n")
		}
	}
	return b.String()
}
