// Package opencodego implements the OpenCode Go provider adapter: an
// OpenAI-compatible Chat Completions streaming client with bearer-token auth.
//
// The provider id is "opencode-go" (matching the auth.json key and the
// OPENCODE_API_KEY environment variable convention).
package opencodego

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type openAIChatRequest struct {
	Model           string          `json:"model"`
	Messages        []openAIMessage `json:"messages"`
	Stream          bool            `json:"stream"`
	StreamOptions   *streamOptions  `json:"stream_options,omitempty"`
	Tools           []openAITool    `json:"tools,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxTokens       *int            `json:"max_tokens,omitempty"`
	ReasoningEffort *string         `json:"reasoning_effort,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function openAIToolCallFunction `json:"function,omitempty"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// buildBody maps a ChatRequest into the OpenAI wire format.
func (p *Provider) buildBody(req protocol.ChatRequest) ([]byte, error) {
	model := req.Model.ID
	if model == "" {
		model = p.defaultModel
	}
	level := protocol.NormalizeThinkingLevel(req.Thinking)
	if !req.Model.SupportsThinkingLevel(level) {
		return nil, unsupportedThinkingError(p.providerID, req.Model, model, level)
	}
	messageCapacity := len(req.Messages) + len(req.InternalContext)
	if req.System != "" {
		messageCapacity++
	}
	oreq := openAIChatRequest{
		Model:         model,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}
	if messageCapacity > 0 {
		oreq.Messages = make([]openAIMessage, 0, messageCapacity)
	}
	if len(req.Tools) > 0 {
		oreq.Tools = make([]openAITool, 0, len(req.Tools))
	}
	if req.System != "" {
		oreq.Messages = append(oreq.Messages, openAIMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		om, ok := mapMessage(m)
		if !ok {
			continue
		}
		oreq.Messages = append(oreq.Messages, om)
	}
	for _, fragment := range req.InternalContext {
		if err := fragment.Validate(); err != nil {
			return nil, err
		}
		oreq.Messages = append(oreq.Messages, openAIMessage{Role: "user", Content: renderInternalFragment(fragment)})
	}
	for _, t := range req.Tools {
		params := t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		oreq.Tools = append(oreq.Tools, openAITool{
			Type: "function",
			Function: openAIToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	if req.MaxTokens > 0 {
		v := req.MaxTokens
		oreq.MaxTokens = &v
	}
	if req.Temperature != nil {
		oreq.Temperature = req.Temperature
	}
	// Map Snow thinking levels to the model-advertised OpenAI reasoning_effort
	// value when set. Off omits the field entirely.
	if level != protocol.ThinkingOff {
		if effort, ok := mapThinkingEffort(level); ok {
			v := effort
			oreq.ReasoningEffort = &v
		}
	}
	return marshalChatRequest(oreq)
}

// mapThinkingEffort maps Snow's normalized levels to OpenAI-compatible
// reasoning_effort values.
func mapThinkingEffort(l protocol.ThinkingLevel) (string, bool) {
	switch l {
	case protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium,
		protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax, protocol.ThinkingUltra:
		return string(l), true
	}
	return "", false
}

func unsupportedThinkingError(providerID string, model protocol.Model, modelID string, level protocol.ThinkingLevel) error {
	allowed := model.SupportedThinkingLevels()
	return fmt.Errorf("%s: model %q does not advertise thinking level %q (supported: %s)", providerID, modelID, level, joinThinkingLevels(allowed))
}

func joinThinkingLevels(levels []protocol.ThinkingLevel) string {
	parts := make([]string, 0, len(levels))
	for _, level := range levels {
		parts = append(parts, string(level))
	}
	return strings.Join(parts, "|")
}

func renderInternalFragment(fragment protocol.InternalContextFragment) string {
	return "<snow_internal_context source=\"" + fragment.Source + "\">\n" + fragment.Text + "\n</snow_internal_context>"
}

// mapMessage converts a protocol message to the OpenAI wire format. The bool
// result is false for message roles that cannot be represented.
func mapMessage(m protocol.Message) (openAIMessage, bool) {
	switch m.Role {
	case protocol.RoleUser:
		return openAIMessage{Role: "user", Content: messageContent(m)}, true
	case protocol.RoleAgent:
		// OpenAI Chat Completions has no portable agent-message role. The core
		// stores a sealed, attributed envelope so rendering it as user input does
		// not lose sender/recipient/type metadata.
		return openAIMessage{Role: "user", Content: messageContent(m)}, true
	case protocol.RoleAssistant:
		toolCalls := 0
		for _, block := range m.Content {
			if block.Type == protocol.BlockToolCall {
				toolCalls++
			}
		}
		om := openAIMessage{Role: "assistant", Content: textContent(m)}
		if toolCalls > 0 {
			om.ToolCalls = make([]openAIToolCall, 0, toolCalls)
		}
		for _, b := range m.Content {
			if b.Type != protocol.BlockToolCall {
				continue
			}
			args := string(b.Arguments)
			if args == "" {
				args = "{}"
			}
			om.ToolCalls = append(om.ToolCalls, openAIToolCall{
				ID:   b.ToolCallID,
				Type: "function",
				Function: openAIToolCallFunction{
					Name:      b.Name,
					Arguments: args,
				},
			})
		}
		return om, true
	case protocol.RoleTool:
		return openAIMessage{Role: "tool", ToolCallID: m.ToolCallID, Content: textContent(m)}, true
	case protocol.RoleCustom:
		// Harness-owned checkpoints are authoritative input, not prior model
		// output. Rendering them as assistant text lets chat-completions models
		// discount or contradict the newest compaction state, especially after
		// repeated compaction. Responses adapters already render this role as
		// user input; keep both protocol paths consistent.
		return openAIMessage{Role: "user", Content: textContent(m)}, true
	default:
		return openAIMessage{}, false
	}
}

type openAIContentPart struct {
	Type     string              `json:"type"`
	Text     string              `json:"text,omitempty"`
	ImageURL *openAIImageURLPart `json:"image_url,omitempty"`
}

type openAIImageURLPart struct {
	URL string `json:"url"`
}

func messageContent(m protocol.Message) any {
	hasImages := false
	for _, block := range m.Content {
		if block.Type == protocol.BlockImage {
			hasImages = true
			break
		}
	}
	if !hasImages {
		return textContent(m)
	}
	parts := make([]openAIContentPart, 0, len(m.Content))
	for _, block := range m.Content {
		switch block.Type {
		case protocol.BlockText:
			if block.Text != "" {
				parts = append(parts, openAIContentPart{Type: "text", Text: block.Text})
			}
		case protocol.BlockPlan:
			if text := planBlockText(block); text != "" {
				parts = append(parts, openAIContentPart{Type: "text", Text: text})
			}
		case protocol.BlockImage:
			mime := strings.ToLower(strings.TrimSpace(block.MIMEType))
			if mime == "" {
				mime = "image/png"
			}
			parts = append(parts, openAIContentPart{Type: "image_url", ImageURL: &openAIImageURLPart{URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(block.Data)}})
		}
	}
	return parts
}

// textContent joins the representable text of a message. Thinking blocks are
// skipped for OpenAI-compatible Chat Completions.
func textContent(m protocol.Message) string {
	if len(m.Content) == 1 {
		switch block := m.Content[0]; block.Type {
		case protocol.BlockText:
			return block.Text
		case protocol.BlockPlan:
			return planBlockText(block)
		default:
			return ""
		}
	}
	var sb strings.Builder
	for _, block := range m.Content {
		switch block.Type {
		case protocol.BlockText:
			sb.WriteString(block.Text)
		case protocol.BlockPlan:
			if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
				sb.WriteByte('\n')
			}
			sb.WriteString(planBlockText(block))
		case protocol.BlockThinking:
			// Skipped.
		default:
			// Tool calls and images contribute no plain text.
		}
	}
	return sb.String()
}

func planBlockText(block protocol.ContentBlock) string {
	if !block.PlanComplete {
		return block.Text
	}
	text := "<proposed_plan>\n" + block.Text
	if !strings.HasSuffix(block.Text, "\n") {
		text += "\n"
	}
	return text + "</proposed_plan>\n"
}

// ---------------------------------------------------------------------------
// SSE response parsing
// ---------------------------------------------------------------------------
