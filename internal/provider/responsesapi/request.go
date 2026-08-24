package responsesapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
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

// The field order in these wire types matches the deterministic order emitted
// by the maps they replace. Keeping concrete shapes avoids map construction and
// sorting during every provider request without changing the JSON object shape.
type responseInputContent struct {
	Detail   string `json:"detail,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Text     string `json:"text,omitempty"`
	Type     string `json:"type"`
}

type responseMessageInput struct {
	Content []responseInputContent `json:"content"`
	Role    string                 `json:"role"`
	Status  string                 `json:"status,omitempty"`
	Type    string                 `json:"type,omitempty"`
}

type responseSingleMessageInput struct {
	Content [1]responseInputContent `json:"content"`
	Role    string                  `json:"role"`
	Status  string                  `json:"status,omitempty"`
	Type    string                  `json:"type,omitempty"`
}

type responseFunctionCallInput struct {
	Arguments string `json:"arguments"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Type      string `json:"type"`
}

type responseFunctionCallOutputText struct {
	CallID string `json:"call_id"`
	Output string `json:"output"`
	Type   string `json:"type"`
}

type responseFunctionCallOutputContent struct {
	CallID string                 `json:"call_id"`
	Output []responseInputContent `json:"output"`
	Type   string                 `json:"type"`
}

type responseLegacyReasoningInput struct {
	EncryptedContent string      `json:"encrypted_content"`
	ID               string      `json:"id"`
	Summary          [0]struct{} `json:"summary"`
	Type             string      `json:"type"`
}

type persistedReasoningEnvelope struct {
	Type             string                   `json:"type"`
	ID               string                   `json:"id"`
	Summary          []persistedReasoningPart `json:"summary"`
	Content          json.RawMessage          `json:"content,omitempty"`
	EncryptedContent json.RawMessage          `json:"encrypted_content,omitempty"`
}

type persistedReasoningPart struct {
	Type string  `json:"type"`
	Text *string `json:"text"`
}

var persistedReasoningDecodeOptions = jsonv2.JoinOptions(
	jsontext.AllowDuplicateNames(true),
	jsontext.AllowInvalidUTF8(true),
)

func BuildRequest(req protocol.ChatRequest, opts RequestOptions) ([]byte, error) {
	body, err := buildRequestBody(req, opts)
	if err != nil {
		return nil, err
	}
	return marshalRequestBody(body)
}

func buildRequestBody(req protocol.ChatRequest, opts RequestOptions) (responsesRequest, error) {
	model := req.Model.ID
	if model == "" {
		return responsesRequest{}, errors.New("model is required")
	}
	level := protocol.NormalizeThinkingLevel(req.Thinking)
	summary, err := protocol.ParseReasoningSummary(string(req.ReasoningSummary))
	if err != nil {
		return responsesRequest{}, err
	}
	verbosity, err := protocol.ParseTextVerbosity(string(req.TextVerbosity))
	if err != nil {
		return responsesRequest{}, err
	}
	if !req.Model.SupportsThinkingLevel(level) {
		return responsesRequest{}, unsupportedThinkingError(opts.ProviderID, req.Model, model, level)
	}
	body := responsesRequest{
		Model:             model,
		Store:             false,
		Stream:            true,
		Instructions:      req.System,
		Input:             make([]any, 0, requestInputCount(req.Messages, len(req.InternalContext), opts.ProviderID)),
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
		body.Input, err = appendResponseInput(body.Input, msg, opts.ProviderID, false)
		if err != nil {
			return responsesRequest{}, err
		}
	}
	for _, fragment := range req.InternalContext {
		if err := fragment.Validate(); err != nil {
			return responsesRequest{}, err
		}
		body.Input = append(body.Input, &responseSingleMessageInput{
			Content: [1]responseInputContent{{Text: renderInternalFragment(fragment), Type: "input_text"}},
			Role:    "user",
		})
	}
	if req.Model.SupportsTools && len(req.Tools) > 0 {
		body.Tools = make([]responsesTool, 0, len(req.Tools))
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
	return body, nil
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
// Responses adapters and package regression tests. It retains defensive
// ownership of provider-private bytes because callers may retain the result.
func MessageInput(msg protocol.Message, providerID string) ([]any, error) {
	return responseInput(msg, providerID)
}

func responseInput(msg protocol.Message, providerID string) ([]any, error) {
	count := responseInputItemCount(msg, providerID)
	if count == 0 {
		return nil, nil
	}
	out := make([]any, 0, count)
	return appendResponseInput(out, msg, providerID, true)
}

func appendResponseInput(out []any, msg protocol.Message, providerID string, cloneProviderData bool) ([]any, error) {
	switch msg.Role {
	case protocol.RoleUser, protocol.RoleAgent:
		contentCount := userInputContentCount(msg.Content)
		if contentCount == 1 {
			content, err := singleUserInputContent(msg.Content)
			if err != nil {
				return nil, err
			}
			out = append(out, &responseSingleMessageInput{
				Content: [1]responseInputContent{content},
				Role:    "user",
			})
		} else if contentCount > 1 {
			content, err := userInputContent(msg.Content)
			if err != nil {
				return nil, err
			}
			out = append(out, &responseMessageInput{Content: content, Role: "user"})
		}
	case protocol.RoleAssistant:
		for _, block := range msg.Content {
			switch block.Type {
			case protocol.BlockProviderData:
				// Opaque continuity is scoped to the provider that produced the
				// assistant message. Never forward it to another endpoint, replay
				// unattributed legacy data, or reuse an item from a response that the
				// provider did not complete successfully. Failed/aborted Responses
				// may emit encrypted reasoning before their terminal event; omit it
				// conservatively so a later retry does not depend on failed history.
				if !canReplayProviderData(msg, providerID) {
					continue
				}
				item, err := replayProviderReasoning(block, cloneProviderData)
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			case protocol.BlockToolCall:
				args := bytes.TrimSpace(block.Arguments)
				argumentText := "{}"
				if len(args) > 0 {
					argumentText = string(args)
				}
				out = append(out, &responseFunctionCallInput{
					Arguments: argumentText,
					CallID:    block.ToolCallID,
					Name:      block.Name,
					Status:    "completed",
					Type:      "function_call",
				})
			}
		}
		if text := messageText(msg); text != "" {
			out = append(out, &responseSingleMessageInput{
				Content: [1]responseInputContent{{Text: text, Type: "output_text"}},
				Role:    "assistant",
				Status:  "completed",
				Type:    "message",
			})
		}
	case protocol.RoleTool:
		hasImage := false
		for _, block := range msg.Content {
			if block.Type == protocol.BlockImage {
				hasImage = true
				break
			}
		}
		if hasImage {
			content, err := userInputContent(msg.Content)
			if err != nil {
				return nil, err
			}
			out = append(out, &responseFunctionCallOutputContent{
				CallID: msg.ToolCallID,
				Output: content,
				Type:   "function_call_output",
			})
		} else {
			text := messageText(msg)
			if text == "" {
				text = "(no tool output)"
			}
			out = append(out, &responseFunctionCallOutputText{
				CallID: msg.ToolCallID,
				Output: text,
				Type:   "function_call_output",
			})
		}
	case protocol.RoleCustom:
		if text := messageText(msg); text != "" {
			out = append(out, &responseSingleMessageInput{
				Content: [1]responseInputContent{{Text: text, Type: "input_text"}},
				Role:    "user",
			})
		}
	}
	return out, nil
}

func requestInputCount(messages []protocol.Message, internalCount int, providerID string) int {
	count := internalCount
	for _, msg := range messages {
		count += responseInputItemCount(msg, providerID)
	}
	return count
}

func responseInputItemCount(msg protocol.Message, providerID string) int {
	switch msg.Role {
	case protocol.RoleUser, protocol.RoleAgent:
		if userInputContentCount(msg.Content) > 0 {
			return 1
		}
	case protocol.RoleAssistant:
		count := 0
		replayProviderData := canReplayProviderData(msg, providerID)
		for _, block := range msg.Content {
			switch block.Type {
			case protocol.BlockProviderData:
				if replayProviderData {
					count++
				}
			case protocol.BlockToolCall:
				count++
			}
		}
		if messageTextPresent(msg.Content) {
			count++
		}
		return count
	case protocol.RoleTool:
		return 1
	case protocol.RoleCustom:
		if messageTextPresent(msg.Content) {
			return 1
		}
	}
	return 0
}

func canReplayProviderData(msg protocol.Message, providerID string) bool {
	return providerID != "" && msg.Provider == providerID &&
		msg.StopReason != protocol.StopError && msg.StopReason != protocol.StopAborted
}

func userInputContentCount(blocks []protocol.ContentBlock) int {
	count := 0
	for _, block := range blocks {
		switch block.Type {
		case protocol.BlockText:
			if block.Text != "" {
				count++
			}
		case protocol.BlockPlan:
			if block.PlanComplete || block.Text != "" {
				count++
			}
		case protocol.BlockImage:
			count++
		}
	}
	return count
}

func messageTextPresent(blocks []protocol.ContentBlock) bool {
	for _, block := range blocks {
		switch block.Type {
		case protocol.BlockText:
			if block.Text != "" {
				return true
			}
		case protocol.BlockPlan:
			if block.PlanComplete || block.Text != "" {
				return true
			}
		}
	}
	return false
}

func replayProviderReasoning(block protocol.ContentBlock, cloneData bool) (any, error) {
	if block.Name == "" || len(block.Data) == 0 {
		return nil, errors.New("persisted provider reasoning data is malformed")
	}
	var item persistedReasoningEnvelope
	if err := jsonv2.Unmarshal(block.Data, &item, persistedReasoningDecodeOptions, jsonv2.RejectUnknownMembers(true)); err != nil {
		if !json.Valid(block.Data) {
			// Backward compatibility for the original persistence format, where
			// Data held only encrypted_content and Name held the reasoning item ID.
			// Upgrade it because summary is required by Responses.
			return &responseLegacyReasoningInput{
				EncryptedContent: string(block.Data),
				ID:               block.Name,
				Type:             "reasoning",
			}, nil
		}
		return nil, errors.New("persisted provider reasoning data is malformed")
	}
	if item.Type != "reasoning" || item.ID == "" || item.ID != block.Name ||
		item.Summary == nil || !validReasoningPartValues(item.Summary, "summary_text") ||
		(len(item.Content) > 0 && !validReasoningParts(item.Content, "reasoning_text")) ||
		(len(item.EncryptedContent) > 0 && !validJSONString(item.EncryptedContent)) {
		return nil, errors.New("persisted provider reasoning data is malformed")
	}
	raw := json.RawMessage(block.Data)
	if cloneData {
		raw = append(json.RawMessage(nil), block.Data...)
	}
	return raw, nil
}

func validReasoningParts(raw json.RawMessage, expectedType string) bool {
	if len(raw) == 0 {
		return false
	}
	var parts []persistedReasoningPart
	if jsonv2.Unmarshal(raw, &parts, persistedReasoningDecodeOptions) != nil || parts == nil {
		return false
	}
	return validReasoningPartValues(parts, expectedType)
}

// Nested reasoning parts intentionally allow additional provider fields for
// forward compatibility, matching the prior sanitizer; required type/text
// fields remain strict while the root reasoning envelope rejects unknown fields.
func validReasoningPartValues(parts []persistedReasoningPart, expectedType string) bool {
	for _, part := range parts {
		if part.Type != expectedType || part.Text == nil {
			return false
		}
	}
	return true
}

func validJSONString(raw json.RawMessage) bool {
	// The successful envelope decode already established syntactic validity;
	// checking the value kind avoids decoding opaque encrypted content again.
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) >= 2 && trimmed[0] == '"'
}

func userInputContent(blocks []protocol.ContentBlock) ([]responseInputContent, error) {
	content := make([]responseInputContent, 0, userInputContentCount(blocks))
	for _, block := range blocks {
		item, include, err := userInputContentBlock(block)
		if err != nil {
			return nil, err
		}
		if include {
			content = append(content, item)
		}
	}
	return content, nil
}

func singleUserInputContent(blocks []protocol.ContentBlock) (responseInputContent, error) {
	for _, block := range blocks {
		item, include, err := userInputContentBlock(block)
		if err != nil {
			return responseInputContent{}, err
		}
		if include {
			return item, nil
		}
	}
	return responseInputContent{}, errors.New("responses input content count changed")
}

func userInputContentBlock(block protocol.ContentBlock) (responseInputContent, bool, error) {
	switch block.Type {
	case protocol.BlockText:
		if block.Text != "" {
			return responseInputContent{Text: block.Text, Type: "input_text"}, true, nil
		}
	case protocol.BlockPlan:
		if text := planBlockText(block); text != "" {
			return responseInputContent{Text: text, Type: "input_text"}, true, nil
		}
	case protocol.BlockImage:
		image, err := responseImageInput(block)
		return image, err == nil, err
	}
	return responseInputContent{}, false, nil
}

func responseImageInput(block protocol.ContentBlock) (responseInputContent, error) {
	mime := strings.ToLower(strings.TrimSpace(block.MIMEType))
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return responseInputContent{}, fmt.Errorf("unsupported image MIME type %q", block.MIMEType)
	}
	if len(block.Data) == 0 {
		return responseInputContent{}, errors.New("image content is empty")
	}
	if len(block.Data) > 20<<20 {
		return responseInputContent{}, errors.New("image content exceeds 20 MiB limit")
	}
	return responseInputContent{Detail: "high", ImageURL: encodeImageDataURI(mime, block.Data), Type: "input_image"}, nil
}

func encodeImageDataURI(mime string, data []byte) string {
	const inputChunkBytes = 24 << 10
	var encoded [32 << 10]byte
	var imageURL strings.Builder
	imageURL.Grow(len("data:") + len(mime) + len(";base64,") + base64.StdEncoding.EncodedLen(len(data)))
	imageURL.WriteString("data:")
	imageURL.WriteString(mime)
	imageURL.WriteString(";base64,")
	for len(data) > inputChunkBytes {
		base64.StdEncoding.Encode(encoded[:], data[:inputChunkBytes])
		imageURL.Write(encoded[:])
		data = data[inputChunkBytes:]
	}
	encodedBytes := base64.StdEncoding.EncodedLen(len(data))
	base64.StdEncoding.Encode(encoded[:encodedBytes], data)
	imageURL.Write(encoded[:encodedBytes])
	return imageURL.String()
}

// MessageText returns the text/plan representation used by Responses input.
func MessageText(msg protocol.Message) string { return messageText(msg) }

func messageText(msg protocol.Message) string {
	if text, ok := singleMessageText(msg.Content); ok {
		return text
	}
	var b strings.Builder
	b.Grow(messageTextGrowSize(msg.Content))
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

func singleMessageText(blocks []protocol.ContentBlock) (string, bool) {
	var text string
	found := false
	for _, block := range blocks {
		switch block.Type {
		case protocol.BlockText:
		case protocol.BlockPlan:
			if block.PlanComplete {
				return "", false
			}
		default:
			continue
		}
		if block.Text == "" {
			continue
		}
		if found {
			return "", false
		}
		text = block.Text
		found = true
	}
	return text, true
}

func messageTextGrowSize(blocks []protocol.ContentBlock) int {
	const planMarkupBytes = len("<proposed_plan>\n") + len("</proposed_plan>\n") + 2
	size := 0
	for _, block := range blocks {
		switch block.Type {
		case protocol.BlockText:
			size += len(block.Text)
		case protocol.BlockPlan:
			size += len(block.Text)
			if block.PlanComplete {
				size += planMarkupBytes
			}
		}
	}
	return size
}

func planBlockText(block protocol.ContentBlock) string {
	if !block.PlanComplete {
		return block.Text
	}
	var b strings.Builder
	b.Grow(len(block.Text) + len("<proposed_plan>\n") + len("</proposed_plan>\n") + 1)
	b.WriteString("<proposed_plan>\n")
	b.WriteString(block.Text)
	if !strings.HasSuffix(block.Text, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("</proposed_plan>\n")
	return b.String()
}
