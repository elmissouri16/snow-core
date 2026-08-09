package chatgpt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/snow-core/snow/internal/auth"
	providerpkg "github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/pkg/protocol"
)

// Chat implements the Codex Responses streaming protocol used by ChatGPT
// subscription credentials. The access token is only placed in the request
// header and is never included in errors or stream events.
func (p *Provider) Chat(ctx context.Context, creds auth.Credential, req protocol.ChatRequest) (protocol.EventStream, error) {
	status, err := CheckAuth(creds)
	if err != nil {
		return errorStream(ctx, err), nil
	}
	if status.Expired {
		return errorStream(ctx, errors.New("chatgpt: OAuth access token expired")), nil
	}
	if status.AccountID == "" {
		return errorStream(ctx, errors.New("chatgpt: OAuth token has no ChatGPT account ID")), nil
	}

	body, err := buildResponsesBody(req)
	if err != nil {
		return errorStream(ctx, fmt.Errorf("chatgpt: build request: %w", err)), nil
	}
	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(), bytes.NewReader(body))
		if reqErr != nil {
			return errorStream(ctx, fmt.Errorf("chatgpt: create request: %w", reqErr)), nil
		}
		httpReq.Header.Set("Authorization", "Bearer "+creds.Access)
		httpReq.Header.Set("chatgpt-account-id", status.AccountID)
		httpReq.Header.Set("originator", "snow")
		httpReq.Header.Set("OpenAI-Beta", "responses=experimental")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err = redirectSafeClient(p.client).Do(httpReq)
		if err != nil {
			return errorStream(ctx, fmt.Errorf("chatgpt: request failed: %w", sanitizeNetworkError(err))), nil
		}
		if resp.StatusCode != http.StatusUnauthorized || attempt == 1 {
			break
		}
		resp.Body.Close()
		creds, err = p.resolve(ctx, creds, true)
		if err != nil {
			return errorStream(ctx, err), nil
		}
		status, err = CheckAuth(creds)
		if err != nil {
			return errorStream(ctx, err), nil
		}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1000))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusPaymentRequired {
			return errorStream(ctx, &providerpkg.LimitError{Provider: ProviderID, Status: resp.StatusCode, Message: strings.TrimSpace(string(snippet))}), nil
		}
		return errorStream(ctx, responseError(resp.StatusCode, snippet)), nil
	}

	s := newCodexStream(ctx, resp)
	go s.read()
	return s, nil
}

func (p *Provider) endpoint() string {
	base := strings.TrimRight(p.baseURL, "/")
	if base == "" {
		base = BackendBaseURL
	}
	if strings.HasSuffix(base, "/codex/responses") {
		return base
	}
	if strings.HasSuffix(base, "/codex") {
		return base + "/responses"
	}
	return base + "/codex/responses"
}

func responseError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error.Message != "" {
		message = payload.Error.Message
	}
	if message == "" {
		message = http.StatusText(status)
	}
	if status == http.StatusUnauthorized {
		return errors.New("chatgpt: OAuth credential rejected (HTTP 401)")
	}
	return fmt.Errorf("chatgpt: HTTP %d: %s", status, truncate(message, 500))
}

type responsesRequest struct {
	Model        string          `json:"model"`
	Store        bool            `json:"store"`
	Stream       bool            `json:"stream"`
	Instructions string          `json:"instructions,omitempty"`
	Input        []any           `json:"input"`
	Tools        []responsesTool `json:"tools,omitempty"`
	Reasoning    *reasoning      `json:"reasoning,omitempty"`
	Include      []string        `json:"include,omitempty"`
	Text         *responseText   `json:"text,omitempty"`
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

func buildResponsesBody(req protocol.ChatRequest) ([]byte, error) {
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
		return nil, unsupportedThinkingError(req.Model, model, level)
	}
	body := responsesRequest{
		Model:        model,
		Store:        false,
		Stream:       true,
		Instructions: req.System,
		Input:        make([]any, 0, len(req.Messages)),
		Include:      []string{"reasoning.encrypted_content"},
	}
	// Legacy/custom request models did not carry this capability bit. Keep the
	// historical field for those, while respecting an authenticated ChatGPT
	// catalog record that explicitly does not support verbosity.
	if req.Model.Provider != ProviderID || req.Model.SupportsVerbosity {
		body.Text = &responseText{Verbosity: string(verbosity)}
	}
	if body.Instructions == "" {
		body.Instructions = "You are a helpful assistant."
	}
	for _, msg := range req.Messages {
		input, err := responseInput(msg)
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
	case protocol.ThinkingMinimal, protocol.ThinkingLow:
		return "low", true
	case protocol.ThinkingMedium:
		return "medium", true
	case protocol.ThinkingHigh:
		return "high", true
	default:
		return "", false
	}
}

func unsupportedThinkingError(model protocol.Model, modelID string, level protocol.ThinkingLevel) error {
	allowed := model.SupportedThinkingLevels()
	parts := make([]string, 0, len(allowed))
	for _, supported := range allowed {
		parts = append(parts, string(supported))
	}
	return fmt.Errorf("chatgpt: model %q does not advertise thinking level %q (supported: %s)", modelID, level, strings.Join(parts, "|"))
}

func renderInternalFragment(fragment protocol.InternalContextFragment) string {
	return "<snow_internal_context source=\"" + fragment.Source + "\">\n" + fragment.Text + "\n</snow_internal_context>"
}

func responseInput(msg protocol.Message) ([]any, error) {
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

type codexStream struct {
	ch   chan protocol.StreamEvent
	done chan struct{}
	ctx  context.Context
	body io.ReadCloser
	once sync.Once
}

func newCodexStream(ctx context.Context, resp *http.Response) *codexStream {
	return &codexStream{ch: make(chan protocol.StreamEvent, 64), done: make(chan struct{}), ctx: ctx, body: resp.Body}
}

func (s *codexStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	select {
	case ev, ok := <-s.ch:
		if !ok {
			return protocol.StreamEvent{}, io.EOF
		}
		return ev, nil
	case <-ctx.Done():
		return protocol.StreamEvent{}, ctx.Err()
	case <-s.ctx.Done():
		return protocol.StreamEvent{}, s.ctx.Err()
	case <-s.done:
		return protocol.StreamEvent{}, io.EOF
	}
}

func (s *codexStream) Close() error {
	s.once.Do(func() {
		if s.body != nil {
			_ = s.body.Close()
		}
		close(s.done)
	})
	return nil
}

func (s *codexStream) send(ev protocol.StreamEvent) {
	select {
	case s.ch <- ev:
	case <-s.ctx.Done():
	case <-s.done:
	}
}

type toolAccum struct {
	id   string
	name string
	args strings.Builder
}

type reasoningAccum struct {
	items            map[string]string
	emitted          bool
	trailingNewlines int
}

func newReasoningAccum() *reasoningAccum {
	return &reasoningAccum{items: make(map[string]string)}
}

// append preserves raw per-item text for completed-snapshot reconciliation but
// inserts a visible paragraph boundary when the Responses API starts a new
// reasoning summary item. Without this, independently formatted summaries such
// as "Planning tasks" and "Designing workers" render as "tasksDesigning".
func (r *reasoningAccum) append(key, text string) string {
	if r == nil || text == "" {
		return text
	}
	_, seen := r.items[key]
	r.items[key] += text
	out := text
	if !seen && r.emitted {
		needed := max(0, 2-r.trailingNewlines)
		out = strings.Repeat("\n", needed) + strings.TrimLeft(text, "\r\n")
	}
	if out != "" {
		r.emitted = true
		r.trailingNewlines = trailingNewlineCount(out)
	}
	return out
}

func (r *reasoningAccum) text(key string) string {
	if r == nil {
		return ""
	}
	return r.items[key]
}

func trailingNewlineCount(value string) int {
	count := 0
	for i := len(value) - 1; i >= 0 && value[i] == '\n'; i-- {
		count++
	}
	return min(count, 2)
}

func (s *codexStream) read() {
	defer close(s.ch)
	defer s.body.Close()
	scanner := bufio.NewScanner(s.body)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	var data []string
	calls := make(map[string]*toolAccum)
	reasoning := newReasoningAccum()
	var finish protocol.StopReason
	sawTool := false
	flush := func() bool {
		if len(data) == 0 {
			return false
		}
		payload := strings.TrimSpace(strings.Join(data, "\n"))
		data = nil
		if payload == "" || payload == "[DONE]" {
			return false
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("chatgpt: invalid SSE event: %w", err)})
			return true
		}
		return s.processEvent(event, calls, reasoning, &finish, &sawTool)
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if flush() {
				return
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if flush() {
		return
	}
	if err := scanner.Err(); err != nil && s.ctx.Err() == nil {
		s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("chatgpt: stream read failed: %w", err)})
		return
	}
	if finish == "" {
		if sawTool {
			finish = protocol.StopToolUse
		} else {
			finish = protocol.StopStop
		}
	}
	s.send(protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: finish})
}

func (s *codexStream) processEvent(event map[string]any, calls map[string]*toolAccum, reasoning *reasoningAccum, finish *protocol.StopReason, sawTool *bool) bool {
	typ, _ := event["type"].(string)
	switch typ {
	case "response.output_text.delta":
		if delta, _ := event["delta"].(string); delta != "" {
			s.send(protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: delta})
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if delta, _ := event["delta"].(string); delta != "" {
			key := reasoningIdentity(event, typ)
			s.send(protocol.StreamEvent{Type: protocol.EvStreamThinkingDelta, Text: reasoning.append(key, delta)})
		}
	case "response.reasoning_summary_text.done", "response.reasoning_summary_part.done", "response.reasoning_text.done":
		if text := reasoningDoneText(event, typ); text != "" {
			key := reasoningIdentity(event, typ)
			if suffix := missingReasoningSuffix(reasoning.text(key), text); suffix != "" {
				s.send(protocol.StreamEvent{Type: protocol.EvStreamThinkingDelta, Text: reasoning.append(key, suffix)})
			}
		}
	case "response.function_call_arguments.delta":
		*sawTool = true
		key, id, name := toolIdentity(event, calls)
		acc := calls[key]
		if delta, _ := event["delta"].(string); delta != "" {
			acc.args.WriteString(delta)
			s.send(protocol.StreamEvent{Type: protocol.EvStreamToolCallDelta, ToolCallID: id, ToolName: name, Arguments: json.RawMessage(delta)})
		}
	case "response.function_call_arguments.done":
		*sawTool = true
		key, id, name := toolIdentity(event, calls)
		acc := calls[key]
		args, _ := event["arguments"].(string)
		if args == "" {
			args = acc.args.String()
		}
		if args == "" || !json.Valid([]byte(args)) {
			args = "{}"
		}
		s.send(protocol.StreamEvent{Type: protocol.EvStreamToolCallDone, ToolCallID: id, ToolName: name, Arguments: json.RawMessage(args)})
	case "response.output_item.added", "response.output_item.done":
		item, _ := event["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		if itemType == "reasoning" && typ == "response.output_item.done" {
			if block := sanitizeCompletedReasoningItem(item); block != nil {
				s.send(protocol.StreamEvent{Type: protocol.EvStreamProviderData, ProviderData: block})
			}
		}
		if itemType == "function_call" {
			*sawTool = true
			key, id, name := itemIdentity(item, calls)
			acc := calls[key]
			if args, _ := item["arguments"].(string); args != "" && acc.args.Len() == 0 {
				acc.args.WriteString(args)
				s.send(protocol.StreamEvent{Type: protocol.EvStreamToolCallDelta, ToolCallID: id, ToolName: name, Arguments: json.RawMessage(args)})
			}
			if typ == "response.output_item.done" {
				args := acc.args.String()
				if args == "" || !json.Valid([]byte(args)) {
					args = "{}"
				}
				s.send(protocol.StreamEvent{Type: protocol.EvStreamToolCallDone, ToolCallID: id, ToolName: name, Arguments: json.RawMessage(args)})
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
	case "response.failed", "error":
		message := eventMessage(event)
		if message == "" {
			message = "Codex response failed"
		}
		s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: errors.New("chatgpt: " + message)})
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
	if strings.HasPrefix(completed, streamed) {
		return strings.TrimPrefix(completed, streamed)
	}
	return ""
}

func toolIdentity(event map[string]any, calls map[string]*toolAccum) (string, string, string) {
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
	if acc == nil {
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
	return key, id, name
}

func itemIdentity(item map[string]any, calls map[string]*toolAccum) (string, string, string) {
	key, _ := item["id"].(string)
	id, _ := item["call_id"].(string)
	name, _ := item["name"].(string)
	if id == "" {
		id = key
	}
	acc := calls[key]
	if acc == nil {
		acc = &toolAccum{id: id, name: name}
		calls[key] = acc
	}
	return key, id, name
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
		out.CacheRead = intNumber(details["cached_tokens"])
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

func intNumber(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "…"
}

func errorStream(ctx context.Context, err error) protocol.EventStream {
	s := &codexStream{ch: make(chan protocol.StreamEvent, 1), done: make(chan struct{}), ctx: ctx}
	s.ch <- protocol.StreamEvent{Type: protocol.EvStreamError, Err: err}
	close(s.ch)
	return s
}
