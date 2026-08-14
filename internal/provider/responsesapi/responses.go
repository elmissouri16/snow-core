package responsesapi

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	providerpkg "github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/pkg/protocol"
)

const (
	maxCodexSSELineBytes           = 4 << 20
	maxCodexSSEEventBytes          = 8 << 20
	maxCodexSSEEventFragments      = 4096
	maxCodexToolArgumentBytes      = 1 << 20
	maxCodexTotalToolArgumentBytes = 4 << 20
	maxCodexStreamToolCalls        = 128
	maxCodexIdentityBytes          = 4096
	maxCodexReasoningBytes         = 4 << 20
	maxCodexReasoningItems         = 128
	maxResponseTextBytes           = 16 << 20
	maxStreamErrorBytes            = 500
)

// RequestOptions selects provider-specific optional Responses fields.
type RequestOptions struct {
	ProviderID                string
	IncludeEncryptedReasoning bool
	AllowLegacyVerbosity      bool
	PromptCacheKey            string
	ToolChoice                string
	ParallelToolCalls         *bool
	OmitMaxOutputTokens       bool
	OmitTemperature           bool
}

func providerLabel(id string) string {
	if strings.TrimSpace(id) == "" {
		return "responses"
	}
	return id
}

// ResponseError preserves bounded provider diagnostics without retaining
// request payloads or credentials. Adapters may use the structured fields for
// retry classification while callers receive the same safe Error string.
type ResponseError struct {
	Provider  string
	Message   string
	Code      string
	RequestID string
	Status    int
	Attempts  int
}

func (e *ResponseError) ContextWindowExceeded() bool {
	if e == nil {
		return false
	}
	code := strings.ToLower(strings.TrimSpace(e.Code))
	switch code {
	case "context_length_exceeded", "context_window_exceeded", "prompt_too_long", "input_too_long":
		return true
	}
	message := strings.ToLower(e.Message)
	for _, phrase := range []string{"maximum context length", "context length exceeded", "context window exceeded", "prompt is too long", "prompt too long", "input is too long", "input too long", "too many tokens in prompt"} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

func (e *ResponseError) Error() string {
	if e == nil {
		return "responses: request failed"
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "request failed"
	}
	var details []string
	if e.Status > 0 {
		details = append(details, fmt.Sprintf("HTTP %d", e.Status))
	}
	if e.Code != "" {
		details = append(details, "code "+e.Code)
	}
	if e.RequestID != "" {
		details = append(details, "request ID "+e.RequestID)
	}
	if e.Attempts > 1 {
		details = append(details, fmt.Sprintf("%d attempts", e.Attempts))
	}
	if len(details) > 0 {
		message += " (" + strings.Join(details, ", ") + ")"
	}
	return providerLabel(e.Provider) + ": " + message
}

// NewResponseError bounds and redacts untrusted provider diagnostics.
func NewResponseError(providerID string, status int, message, code, requestID string, secrets ...string) *ResponseError {
	return &ResponseError{
		Provider:  providerLabel(providerID),
		Message:   SanitizeErrorText(message, maxStreamErrorBytes, secrets...),
		Code:      safeErrorMetadata(providerpkg.RedactSecrets(code, secrets...)),
		RequestID: safeErrorMetadata(providerpkg.RedactSecrets(requestID, secrets...)),
		Status:    status,
	}
}

// SanitizeErrorText redacts credentials before truncation, removes terminal
// control characters, and bounds untrusted provider text. A trailing credential
// prefix is also removed so a bounded read cannot expose the start of a secret
// that crossed its cutoff.
func SanitizeErrorText(value string, maxBytes int, secrets ...string) string {
	value = providerpkg.RedactSecrets(value, secrets...)
	if maxBytes > 0 && len(value) > maxBytes {
		value = value[:maxBytes]
	}
	for _, secret := range secrets {
		limit := min(len(secret), len(value))
		for size := limit; size >= 1; size-- {
			if strings.HasSuffix(value, secret[:size]) {
				value = strings.TrimSuffix(value, secret[:size]) + "[redacted]"
				break
			}
		}
	}
	value = strings.ToValidUTF8(value, "�")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	return truncateUTF8(value, maxBytes)
}

func safeErrorMetadata(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	return truncateUTF8(value, 200)
}

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

type codexStream struct {
	ch       chan protocol.StreamEvent
	done     chan struct{}
	ctx      context.Context
	body     io.ReadCloser
	once     sync.Once
	secrets  []string
	provider string
}

func NewStream(ctx context.Context, resp *http.Response, providerID string, secrets ...string) protocol.EventStream {
	return NewStreamWithIdleTimeout(ctx, resp, providerID, providerpkg.DefaultStreamIdleTimeout, secrets...)
}

func NewStreamWithIdleTimeout(ctx context.Context, resp *http.Response, providerID string, idleTimeout time.Duration, secrets ...string) protocol.EventStream {
	body := providerpkg.WrapIdleReadCloser(resp.Body, idleTimeout)
	s := &codexStream{ch: make(chan protocol.StreamEvent, 64), done: make(chan struct{}), ctx: ctx, body: body, secrets: append([]string(nil), secrets...), provider: providerLabel(providerID)}
	go s.read()
	return s
}

func (s *codexStream) prefix(message string) error { return errors.New(s.provider + ": " + message) }

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
	totalBytes       int
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
	r.totalBytes += len(text)
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

func (r *reasoningAccum) canAppend(key, text string) error {
	if r == nil || text == "" {
		return nil
	}
	if len(key) > maxCodexIdentityBytes {
		return errors.New("reasoning identity exceeds size limit")
	}
	if _, exists := r.items[key]; !exists && len(r.items) >= maxCodexReasoningItems {
		return errors.New("reasoning item count exceeds limit")
	}
	if len(text) > maxCodexReasoningBytes-r.totalBytes {
		return errors.New("reasoning text exceeds total size limit")
	}
	return nil
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
	scanner.Buffer(make([]byte, 4096), maxCodexSSELineBytes)
	var data []string
	dataBytes := 0
	dataFragments := 0
	calls := make(map[string]*toolAccum)
	bounds := &codexStreamBounds{}
	reasoning := newReasoningAccum()
	var finish protocol.StopReason
	sawTool := false
	terminal := false
	sendDone := func() {
		if finish == "" {
			if sawTool {
				finish = protocol.StopToolUse
			} else {
				finish = protocol.StopStop
			}
		}
		s.send(protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: finish})
	}
	flush := func() bool {
		if len(data) == 0 {
			return false
		}
		payload := strings.TrimSpace(strings.Join(data, "\n"))
		data = nil
		dataBytes = 0
		dataFragments = 0
		if payload == "" {
			return false
		}
		if payload == "[DONE]" {
			terminal = true
			sendDone()
			return true
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("%s: invalid SSE event: %w", s.provider, err)})
			return true
		}
		return s.processEvent(event, calls, reasoning, bounds, &finish, &sawTool, &terminal)
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
			fragment := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			// Count a separator byte even for an empty fragment and independently
			// cap fragment count so slice overhead cannot bypass the byte bound.
			if dataFragments >= maxCodexSSEEventFragments || len(fragment)+1 > maxCodexSSEEventBytes-dataBytes {
				s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: s.prefix("SSE event exceeds size limit")})
				return
			}
			dataFragments++
			dataBytes += len(fragment) + 1
			data = append(data, fragment)
		}
	}
	scanErr := scanner.Err()
	if errors.Is(scanErr, providerpkg.ErrStreamIdle) && s.ctx.Err() == nil {
		s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: NewResponseError(s.provider, 0, "stream idle timeout", "stream_idle", "", s.secrets...)})
		return
	}
	if flush() {
		return
	}
	if scanErr != nil && s.ctx.Err() == nil {
		s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: NewResponseError(s.provider, 0, "stream read failed", "network_error", "", s.secrets...)})
		return
	}
	if !terminal && s.ctx.Err() == nil {
		s.send(protocol.StreamEvent{Type: protocol.EvStreamError, Err: NewResponseError(s.provider, 0, "stream ended before a terminal response event", "stream_truncated", "", s.secrets...)})
	}
}

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
	if strings.HasPrefix(completed, streamed) {
		return strings.TrimPrefix(completed, streamed)
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
