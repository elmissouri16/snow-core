// Package openaicompat implements a user-configured OpenAI Responses-compatible provider.
package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/auth"
	providerpkg "github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/provider/opencodego"
	"github.com/snow-core/snow/internal/provider/responsesapi"
	"github.com/snow-core/snow/pkg/protocol"
)

const (
	ProviderID = "openai-compatible"
	EnvAPIKey  = "OPENAI_API_KEY"
)

const (
	maxModelsResponseBytes = 4 << 20
	maxErrorResponseBytes  = 1000
)

type Config struct {
	BaseURL string
	// APIKey is an explicit adapter fallback usable for discovery and Chat.
	APIKey string
	// DiscoveryAPIKey is used only by ListModels. App wiring places auth-store
	// keys here so deleting a stored key cannot leave a live Chat fallback.
	DiscoveryAPIKey   string
	DefaultModel      string
	HTTPClient        *http.Client
	DiscoveryTimeout  time.Duration
	StreamIdleTimeout time.Duration
}

type Provider struct {
	responsesURL      string
	chatURL           string
	modelsURL         string
	apiKey            string
	discoveryAPIKey   string
	defaultModel      string
	client            *http.Client
	discoveryTimeout  time.Duration
	streamIdleTimeout time.Duration
	wireMode          atomic.Uint32
}

func New(cfg Config) (*Provider, error) {
	var responsesURL, modelsURL string
	var err error
	if strings.TrimSpace(cfg.BaseURL) != "" {
		responsesURL, modelsURL, err = normalizeEndpoints(cfg.BaseURL)
		if err != nil {
			return nil, err
		}
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	timeout := cfg.DiscoveryTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	streamIdleTimeout := cfg.StreamIdleTimeout
	if streamIdleTimeout == 0 {
		streamIdleTimeout = providerpkg.DefaultStreamIdleTimeout
	} else if streamIdleTimeout < 0 {
		streamIdleTimeout = -1
	}
	return &Provider{responsesURL: responsesURL, chatURL: siblingEndpoint(responsesURL, "chat/completions"), modelsURL: modelsURL, apiKey: cfg.APIKey, discoveryAPIKey: cfg.DiscoveryAPIKey, defaultModel: strings.TrimSpace(cfg.DefaultModel), client: client, discoveryTimeout: timeout, streamIdleTimeout: streamIdleTimeout}, nil
}

func normalizeEndpoints(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("openai-compatible: invalid base URL: %w", err)
	}
	if !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", errors.New("openai-compatible: base URL must be an absolute HTTP(S) URL")
	}
	if u.User != nil {
		return "", "", errors.New("openai-compatible: base URL must not contain userinfo")
	}
	if u.RawQuery != "" || u.ForceQuery {
		return "", "", errors.New("openai-compatible: base URL must not contain a query string")
	}
	if u.Fragment != "" {
		return "", "", errors.New("openai-compatible: base URL must not contain a fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(u.Path, "/responses") {
		responses := u.String()
		u.Path = strings.TrimSuffix(u.Path, "/responses") + "/models"
		return responses, u.String(), nil
	}
	if strings.HasSuffix(u.Path, "/chat/completions") {
		basePath := strings.TrimSuffix(u.Path, "/chat/completions")
		u.Path = basePath + "/responses"
		responses := u.String()
		u.Path = basePath + "/models"
		return responses, u.String(), nil
	}
	basePath := u.Path
	u.Path = basePath + "/responses"
	responses := u.String()
	u.Path = basePath + "/models"
	return responses, u.String(), nil
}

func siblingEndpoint(responsesURL, suffix string) string {
	u, err := url.Parse(responsesURL)
	if err != nil || responsesURL == "" {
		return ""
	}
	u.Path = strings.TrimSuffix(u.Path, "/responses") + "/" + suffix
	return u.String()
}

const (
	wireModeUnknown uint32 = iota
	wireModeResponses
	wireModeChatCompletions
)

func (p *Provider) ID() string       { return ProviderID }
func (p *Provider) Configured() bool { return p.responsesURL != "" }

func (p *Provider) DefaultModel() protocol.Model {
	if p.defaultModel == "" {
		return protocol.Model{}
	}
	return protocol.Model{Provider: ProviderID, ID: p.defaultModel, SupportsTools: true}
}

func (p *Provider) resolveKey(creds auth.Credential) string {
	if creds.Key != "" {
		return creds.Key
	}
	if p.apiKey != "" {
		return p.apiKey
	}
	return os.Getenv(EnvAPIKey)
}

func (p *Provider) resolveDiscoveryKey() string {
	if p.apiKey != "" {
		return p.apiKey
	}
	if p.discoveryAPIKey != "" {
		return p.discoveryAPIKey
	}
	return os.Getenv(EnvAPIKey)
}

func (p *Provider) Resolve(_ context.Context, creds auth.Credential) (auth.Credential, error) {
	if key := p.resolveKey(creds); key != "" {
		creds.Type = auth.CredentialAPIKey
		creds.Key = key
	}
	return creds, nil
}

func (p *Provider) staticCatalog() []protocol.Model {
	if p.defaultModel == "" {
		return nil
	}
	return []protocol.Model{{Provider: ProviderID, ID: p.defaultModel, SupportsTools: true}}
}

type modelList struct {
	Data []modelRecord `json:"data"`
}
type modelRecord struct {
	ID                        string                 `json:"id"`
	Name                      string                 `json:"name"`
	DisplayName               string                 `json:"display_name"`
	ContextWindow             int                    `json:"context_window"`
	ContextLength             int                    `json:"context_length"`
	MaxOutputTokens           int                    `json:"max_output_tokens"`
	MaxCompletionTokens       int                    `json:"max_completion_tokens"`
	Pricing                   *protocol.ModelPricing `json:"pricing,omitempty"`
	SupportsTools             *bool                  `json:"supports_tools"`
	SupportsThinking          *bool                  `json:"supports_thinking"`
	SupportsVision            *bool                  `json:"supports_vision"`
	SupportsVerbosity         *bool                  `json:"supports_verbosity"`
	SupportsReasoningSummary  *bool                  `json:"supports_reasoning_summary"`
	Reasoning                 *bool                  `json:"reasoning"`
	ThinkingLevels            []string               `json:"thinking_levels"`
	ReasoningEfforts          []string               `json:"reasoning_efforts"`
	SupportedReasoningEfforts []string               `json:"supported_reasoning_efforts"`
	Input                     []string               `json:"input"`
	Capabilities              *modelCapabilities     `json:"capabilities,omitempty"`
	Architecture              *modelArchitecture     `json:"architecture,omitempty"`
}
type modelCapabilities struct {
	Tools            *bool    `json:"tools"`
	Thinking         *bool    `json:"thinking"`
	Reasoning        *bool    `json:"reasoning"`
	Vision           *bool    `json:"vision"`
	Verbosity        *bool    `json:"verbosity"`
	ReasoningSummary *bool    `json:"reasoning_summary"`
	ThinkingLevels   []string `json:"thinking_levels"`
	ReasoningEfforts []string `json:"reasoning_efforts"`
}
type modelArchitecture struct {
	InputModalities []string `json:"input_modalities"`
}

func (p *Provider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	fallback := p.staticCatalog()
	if p.modelsURL == "" {
		return fallback, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, p.discoveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(discoveryCtx, http.MethodGet, p.modelsURL, nil)
	if err != nil {
		return fallbackOrError(fallback, err)
	}
	if key := p.resolveDiscoveryKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := secureClient(p.client).Do(req)
	if err != nil {
		return fallbackOrError(fallback, errors.New("model discovery request failed"))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fallbackOrError(fallback, fmt.Errorf("model discovery returned HTTP %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxModelsResponseBytes+1))
	if err != nil || len(body) > maxModelsResponseBytes {
		return fallbackOrError(fallback, errors.New("model discovery response is invalid or too large"))
	}
	var payload modelList
	if json.Unmarshal(body, &payload) != nil {
		return fallbackOrError(fallback, errors.New("model discovery response is invalid"))
	}
	models := make([]protocol.Model, 0, len(payload.Data))
	seen := map[string]bool{}
	for _, record := range payload.Data {
		model, ok := normalizeModel(record)
		if !ok || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		models = append(models, model)
	}
	if len(models) == 0 {
		return fallbackOrError(fallback, errors.New("model discovery returned no valid models"))
	}
	if p.defaultModel != "" && !seen[p.defaultModel] {
		models = append(models, protocol.Model{Provider: ProviderID, ID: p.defaultModel, SupportsTools: true})
	}
	return models, nil
}

func fallbackOrError(fallback []protocol.Model, err error) ([]protocol.Model, error) {
	if len(fallback) > 0 {
		return fallback, nil
	}
	return nil, fmt.Errorf("openai-compatible: %w", err)
}

func normalizeModel(r modelRecord) (protocol.Model, bool) {
	r.ID = strings.TrimSpace(r.ID)
	if r.ID == "" {
		return protocol.Model{}, false
	}
	m := protocol.Model{Provider: ProviderID, ID: r.ID, DisplayName: firstNonempty(r.DisplayName, r.Name), SupportsTools: true, Pricing: r.Pricing}
	m.ContextWindow = max(r.ContextWindow, r.ContextLength)
	m.MaxOutputTokens = max(r.MaxOutputTokens, r.MaxCompletionTokens)
	if r.SupportsTools != nil {
		m.SupportsTools = *r.SupportsTools
	} else if r.Capabilities != nil && r.Capabilities.Tools != nil {
		m.SupportsTools = *r.Capabilities.Tools
	}
	visionExplicit := false
	if r.SupportsVision != nil {
		m.SupportsVision = *r.SupportsVision
		visionExplicit = true
	} else if r.Capabilities != nil && r.Capabilities.Vision != nil {
		m.SupportsVision = *r.Capabilities.Vision
		visionExplicit = true
	}
	if !visionExplicit {
		inputs := append([]string(nil), r.Input...)
		if r.Architecture != nil {
			inputs = append(inputs, r.Architecture.InputModalities...)
		}
		for _, input := range inputs {
			if strings.EqualFold(input, "image") {
				m.SupportsVision = true
			}
		}
	}
	if r.SupportsVerbosity != nil {
		m.SupportsVerbosity = *r.SupportsVerbosity
	} else if r.Capabilities != nil && r.Capabilities.Verbosity != nil {
		m.SupportsVerbosity = *r.Capabilities.Verbosity
	}
	if r.SupportsReasoningSummary != nil {
		value := *r.SupportsReasoningSummary
		m.SupportsReasoningSummary = &value
	} else if r.Capabilities != nil && r.Capabilities.ReasoningSummary != nil {
		value := *r.Capabilities.ReasoningSummary
		m.SupportsReasoningSummary = &value
	}
	thinking, thinkingExplicit := false, false
	switch {
	case r.SupportsThinking != nil:
		thinking, thinkingExplicit = *r.SupportsThinking, true
	case r.Reasoning != nil:
		thinking, thinkingExplicit = *r.Reasoning, true
	case r.Capabilities != nil && r.Capabilities.Thinking != nil:
		thinking, thinkingExplicit = *r.Capabilities.Thinking, true
	case r.Capabilities != nil && r.Capabilities.Reasoning != nil:
		thinking, thinkingExplicit = *r.Capabilities.Reasoning, true
	}
	var levels []string
	levels = append(levels, r.ThinkingLevels...)
	levels = append(levels, r.ReasoningEfforts...)
	levels = append(levels, r.SupportedReasoningEfforts...)
	if r.Capabilities != nil {
		levels = append(levels, r.Capabilities.ThinkingLevels...)
		levels = append(levels, r.Capabilities.ReasoningEfforts...)
	}
	if !thinkingExplicit || thinking {
		m.ThinkingLevels = normalizeLevels(levels)
	}
	m.SupportsThinking = thinking || (!thinkingExplicit && len(m.ThinkingLevels) > 0)
	return m, true
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
func normalizeLevels(values []string) []protocol.ThinkingLevel {
	seen := map[protocol.ThinkingLevel]bool{}
	var out []protocol.ThinkingLevel
	for _, value := range values {
		level := protocol.ThinkingLevel(strings.ToLower(strings.TrimSpace(value)))
		if level == "none" {
			level = protocol.ThinkingOff
		}
		if level == protocol.ThinkingOff || level == "" || seen[level] {
			continue
		}
		switch level {
		case protocol.ThinkingMinimal, protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax, protocol.ThinkingUltra:
			seen[level] = true
			out = append(out, level)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return levelRank(out[i]) < levelRank(out[j]) })
	return out
}
func levelRank(level protocol.ThinkingLevel) int {
	for i, candidate := range protocol.KnownThinkingLevels() {
		if candidate == level {
			return i
		}
	}
	return 99
}

func (p *Provider) Chat(ctx context.Context, creds auth.Credential, request protocol.ChatRequest) (protocol.EventStream, error) {
	if p.responsesURL == "" {
		return newErrorStream(ctx, errors.New("openai-compatible: base URL is required; pass --base-url or configure providers.openai-compatible.base_url")), nil
	}
	if !request.Model.SupportsVision && requestHasImages(request) {
		return newErrorStream(ctx, fmt.Errorf("openai-compatible: model %q does not advertise image input support", request.Model.ID)), nil
	}
	if !request.Model.SupportsThinking {
		request = withoutProviderReasoning(request)
	}
	key := p.resolveKey(creds)
	if p.wireMode.Load() == wireModeChatCompletions {
		return p.chatCompletions(ctx, key, request)
	}
	body, err := responsesapi.BuildRequest(request, responsesapi.RequestOptions{ProviderID: ProviderID, IncludeEncryptedReasoning: request.Model.SupportsThinking && protocol.NormalizeThinkingLevel(request.Thinking) != protocol.ThinkingOff})
	if err != nil {
		return newErrorStream(ctx, fmt.Errorf("openai-compatible: build request: %w", err)), nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.responsesURL, bytes.NewReader(body))
	if err != nil {
		return newErrorStream(ctx, errors.New("openai-compatible: create request failed")), nil
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := secureClient(p.client).Do(req)
	if err != nil {
		return newErrorStream(ctx, errors.New("openai-compatible: network request failed")), nil
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorResponseBytes))
		_ = resp.Body.Close()
		p.wireMode.Store(wireModeChatCompletions)
		return p.chatCompletions(ctx, key, request)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBytes))
		message := providerpkg.RedactSecrets(responseMessage(snippet), key)
		if resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusTooManyRequests {
			return newErrorStream(ctx, &providerpkg.LimitError{Provider: ProviderID, Status: resp.StatusCode, Message: message}), nil
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return newErrorStream(ctx, errors.New("openai-compatible: credential rejected (HTTP 401)")), nil
		}
		return newErrorStream(ctx, fmt.Errorf("openai-compatible: HTTP %d: %s", resp.StatusCode, truncate(message, 500))), nil
	}
	p.wireMode.Store(wireModeResponses)
	mediaType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if !strings.EqualFold(mediaType, "text/event-stream") {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBytes))
		message := providerpkg.RedactSecrets(responseMessage(snippet), key)
		message = strings.ToValidUTF8(message, "�")
		if message == "" {
			message = "expected text/event-stream response"
		}
		return newErrorStream(ctx, fmt.Errorf("openai-compatible: incompatible response content type %q: %s", mediaType, truncate(message, 500))), nil
	}
	return responsesapi.NewStreamWithIdleTimeout(ctx, resp, ProviderID, p.streamIdleTimeout, key), nil
}

func (p *Provider) chatCompletions(ctx context.Context, key string, request protocol.ChatRequest) (protocol.EventStream, error) {
	compatible, err := opencodego.New(opencodego.Config{
		BaseURL:           strings.TrimSuffix(p.chatURL, "/chat/completions"),
		APIKey:            key,
		HTTPClient:        secureClient(p.client),
		DefaultModel:      request.Model.ID,
		ProviderID:        ProviderID,
		AllowAnonymous:    true,
		DisableEnvAPIKey:  true,
		StreamIdleTimeout: p.streamIdleTimeout,
	})
	if err != nil {
		return newErrorStream(ctx, fmt.Errorf("openai-compatible: initialize Chat Completions fallback: %w", err)), nil
	}
	return compatible.Chat(ctx, auth.Credential{Type: auth.CredentialAPIKey, Key: key}, request)
}

func requestHasImages(request protocol.ChatRequest) bool {
	for _, message := range request.Messages {
		for _, block := range message.Content {
			if block.Type == protocol.BlockImage {
				return true
			}
		}
	}
	return false
}

func withoutProviderReasoning(request protocol.ChatRequest) protocol.ChatRequest {
	request.Messages = append([]protocol.Message(nil), request.Messages...)
	for i, message := range request.Messages {
		message = message.Clone()
		content := message.Content[:0]
		for _, block := range message.Content {
			if block.Type != protocol.BlockProviderData {
				content = append(content, block)
			}
		}
		message.Content = content
		request.Messages[i] = message
	}
	return request
}

func responseMessage(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error.Message != "" {
		return payload.Error.Message
	}
	return strings.TrimSpace(string(body))
}
func truncate(value string, n int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= n {
		return value
	}
	body := []byte(value)[:n]
	for len(body) > 0 && !utf8.Valid(body) {
		body = body[:len(body)-1]
	}
	return string(body) + "…"
}

type noUserinfoTransport struct{ base http.RoundTripper }

func (t noUserinfoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.User != nil {
		return nil, errors.New("request URL userinfo rejected")
	}
	return t.base.RoundTrip(req)
}
func secureClient(base *http.Client) *http.Client {
	copy := *base
	transport := copy.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	copy.Transport = noUserinfoTransport{base: transport}
	previous := copy.CheckRedirect
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.User != nil {
			return errors.New("redirect URL userinfo rejected")
		}
		if len(via) > 0 {
			origin := via[0].URL
			if origin.User != nil || req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host {
				return errors.New("cross-origin redirect rejected")
			}
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	return &copy
}

type errorStream struct {
	ctx  context.Context
	err  error
	once sync.Once
}

func newErrorStream(ctx context.Context, err error) *errorStream {
	return &errorStream{ctx: ctx, err: err}
}
func (s *errorStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	var event protocol.StreamEvent
	emitted := false
	s.once.Do(func() { event = protocol.StreamEvent{Type: protocol.EvStreamError, Err: s.err}; emitted = true })
	if emitted {
		return event, nil
	}
	select {
	case <-ctx.Done():
		return protocol.StreamEvent{}, ctx.Err()
	case <-s.ctx.Done():
		return protocol.StreamEvent{}, s.ctx.Err()
	default:
		return protocol.StreamEvent{}, io.EOF
	}
}
func (*errorStream) Close() error { return nil }
