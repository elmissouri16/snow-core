// Package opencodego implements the OpenCode Go provider adapter: an
// OpenAI-compatible Chat Completions streaming client with bearer-token auth.
//
// The provider id is "opencode-go" (matching the auth.json key and the
// OPENCODE_API_KEY environment variable convention).
package opencodego

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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/snow-core/snow/internal/auth"
	providerpkg "github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/pkg/protocol"
)

// ProviderID is the stable provider identifier.
const ProviderID = "opencode-go"

// EnvAPIKey is the environment variable holding the OpenCode Go API key.
const EnvAPIKey = "OPENCODE_API_KEY"

// DefaultBaseURL is the OpenCode Go API base URL.
//
// Verified 2026 against the opencode model catalog (anomalyco/opencode,
// packages/opencode/test/tool/fixtures/models-api.json) and the live service:
//
//	"opencode-go": { "npm": "@ai-sdk/openai-compatible",
//	                  "api": "https://opencode.ai/zen/go/v1" }
//
// The endpoint is an OpenAI-compatible gateway (GET /models and POST
// /chat/completions both respond with OpenAI wire format; an invalid key
// returns HTTP 401 {"type":"error","error":{"message":"Invalid API key."}}).
// It is overridable via Config.BaseURL.
const DefaultBaseURL = "https://opencode.ai/zen/go/v1"

// DefaultCatalogURL is the public model metadata catalog OpenCode uses to
// enrich provider model availability with capabilities, limits, and variants.
// The OpenCode Go /models endpoint is authoritative for availability but may
// return only the OpenAI-compatible id/object/owned_by fields.
const DefaultCatalogURL = "https://models.dev/api.json"

// DefaultModelID is the fallback model id used when neither the request nor
// config selects one. Verified present in the live GET /zen/go/v1/models
// catalog (kimi-k2.6, context 262144).
const DefaultModelID = "kimi-k2.6"

const (
	maxSSELineBytes           = 4 << 20
	maxToolArgumentBytes      = 1 << 20
	maxTotalToolArgumentBytes = 4 << 20
	maxStreamToolCalls        = 128
	maxResponseTextBytes      = 16 << 20
	maxReasoningBytes         = 4 << 20
	catalogCacheMaxBytes      = 4 << 20
	catalogCacheFreshness     = 15 * time.Minute
)

// Config controls the OpenCode Go adapter.
type Config struct {
	// BaseURL overrides DefaultBaseURL. Empty means default.
	BaseURL string
	// APIKey is a compile-time/config fallback credential (lowest priority).
	APIKey string
	// HTTPClient overrides http.DefaultClient. The client must not impose a
	// total timeout that would kill long-lived streams; callers cancel via ctx.
	HTTPClient *http.Client
	// DefaultModel overrides DefaultModelID.
	DefaultModel string
	// CatalogURL overrides the models.dev metadata endpoint. When BaseURL is
	// customized and CatalogURL is empty, catalog enrichment is disabled so
	// OpenCode-specific metadata is not applied to an unrelated gateway.
	CatalogURL string
	// DiscoveryTimeout bounds startup catalog requests without constraining chat
	// streams. Zero uses five seconds.
	DiscoveryTimeout time.Duration
	// StreamIdleTimeout bounds silence between response bytes. Zero uses the
	// shared provider default; a negative value disables the watchdog.
	StreamIdleTimeout time.Duration
	// CacheRoot enables an atomic 0600 disk cache for successful model catalogs.
	// Empty disables disk caching (used by generic compatible endpoints).
	CacheRoot string
	// ProviderID overrides diagnostic/event attribution when this internal Chat
	// Completions codec is reused by another OpenAI-compatible adapter.
	ProviderID string
	// AllowAnonymous permits endpoints that do not require a bearer key.
	AllowAnonymous bool
	// DisableEnvAPIKey prevents an unrelated OPENCODE_API_KEY from being sent
	// when the codec is reused for another provider.
	DisableEnvAPIKey bool
}

// Provider implements provider.Provider for OpenCode Go.
type Provider struct {
	baseURL           string
	apiKey            string
	client            *http.Client
	defaultModel      string
	catalogURL        string
	discoveryTimeout  time.Duration
	streamIdleTimeout time.Duration
	providerID        string
	allowAnonymous    bool
	useEnvAPIKey      bool
	cacheRoot         string
	catalogMu         sync.Mutex
	cachedModels      []protocol.Model
	cachedAt          time.Time
}

// New validates and constructs the provider.
func New(cfg Config) (*Provider, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	model := cfg.DefaultModel
	if model == "" {
		model = DefaultModelID
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	catalogURL := strings.TrimSpace(cfg.CatalogURL)
	if catalogURL == "" && base == DefaultBaseURL {
		catalogURL = DefaultCatalogURL
	}
	discoveryTimeout := cfg.DiscoveryTimeout
	if discoveryTimeout <= 0 {
		discoveryTimeout = 5 * time.Second
	}
	streamIdleTimeout := cfg.StreamIdleTimeout
	if streamIdleTimeout == 0 {
		streamIdleTimeout = providerpkg.DefaultStreamIdleTimeout
	} else if streamIdleTimeout < 0 {
		streamIdleTimeout = 0
	}
	providerID := strings.TrimSpace(cfg.ProviderID)
	if providerID == "" {
		providerID = ProviderID
	}
	return &Provider{baseURL: base, apiKey: cfg.APIKey, client: client, defaultModel: model, catalogURL: catalogURL, discoveryTimeout: discoveryTimeout, streamIdleTimeout: streamIdleTimeout, providerID: providerID, allowAnonymous: cfg.AllowAnonymous, useEnvAPIKey: !cfg.DisableEnvAPIKey, cacheRoot: strings.TrimSpace(cfg.CacheRoot)}, nil
}

// ID implements provider.Provider.
func (p *Provider) ID() string { return p.providerID }

// DefaultModel returns the provider's documented default model (the static
// catalog entry). The app uses this to pin a stable default instead of taking
// whatever the live catalog happens to list first.
func (p *Provider) DefaultModel() protocol.Model {
	return p.staticCatalog()[0]
}

// resolveKey returns the first usable API key from the credential, the
// environment, then config, in that order.
func (p *Provider) resolveKey(creds auth.Credential) string {
	if creds.Key != "" {
		return creds.Key
	}
	if p.useEnvAPIKey {
		if env := os.Getenv(EnvAPIKey); env != "" {
			return env
		}
	}
	return p.apiKey
}

// Resolve implements provider.Provider: the credential is usable when any key
// source is present.
func (p *Provider) Resolve(_ context.Context, creds auth.Credential) (auth.Credential, error) {
	key := p.resolveKey(creds)
	if key != "" {
		creds.Type = auth.CredentialAPIKey
		creds.Key = key
		return creds, nil
	}
	if p.allowAnonymous {
		return creds, nil
	}
	return creds, fmt.Errorf("%s: no API key found: set %s, add a %q entry to the auth file, or pass a credential explicitly", p.providerID, EnvAPIKey, p.providerID)
}

// staticCatalog returns the guaranteed-available fallback catalog.
func (p *Provider) staticCatalog() []protocol.Model {
	return []protocol.Model{{
		Provider:      p.providerID,
		ID:            p.defaultModel,
		DisplayName:   "OpenCode Go Default",
		ContextWindow: 200000,
		SupportsTools: true,
		// The fallback is known to emit reasoning, but it does not claim a
		// selectable effort unless the live catalog advertises one.
		SupportsThinking: true,
	}}
}

// openAIModelCatalog is the OpenAI-compatible shape used by OpenCode Go's
// /models endpoint. The gateway has returned a few different metadata shapes
// over time, so optional aliases are accepted without making any of them
// required beyond id.
type openAIModelCatalog struct {
	Data []openAIModelRecord `json:"data"`
}

type openAIModelRecord struct {
	ID                        string                 `json:"id"`
	Name                      string                 `json:"name"`
	DisplayName               string                 `json:"display_name"`
	ContextWindow             int                    `json:"context_window"`
	ContextLength             int                    `json:"context_length"`
	MaxOutputTokens           int                    `json:"max_output_tokens"`
	MaxCompletionTokens       int                    `json:"max_completion_tokens"`
	MaxTokens                 int                    `json:"max_tokens"`
	Pricing                   *protocol.ModelPricing `json:"pricing,omitempty"`
	SupportsTools             *bool                  `json:"supports_tools"`
	SupportsThinking          *bool                  `json:"supports_thinking"`
	SupportsVision            *bool                  `json:"supports_vision"`
	Reasoning                 *bool                  `json:"reasoning"`
	SupportsReasoning         *bool                  `json:"supports_reasoning"`
	SupportsReasoningEffort   *bool                  `json:"supports_reasoning_effort"`
	Input                     []string               `json:"input"`
	ThinkingLevels            []string               `json:"thinking_levels"`
	ReasoningEfforts          []string               `json:"reasoning_efforts"`
	ReasoningEffortLevels     []string               `json:"reasoning_effort_levels"`
	SupportedReasoningEfforts []string               `json:"supported_reasoning_efforts"`
	SupportedParams           []string               `json:"supported_parameters"`
	Capabilities              *modelCapabilities     `json:"capabilities,omitempty"`
	Architecture              *modelArchitecture     `json:"architecture,omitempty"`
}

type modelCapabilities struct {
	Tools                   *bool    `json:"tools"`
	Thinking                *bool    `json:"thinking"`
	Reasoning               *bool    `json:"reasoning"`
	Vision                  *bool    `json:"vision"`
	ReasoningEffort         *bool    `json:"reasoning_effort"`
	SupportsReasoningEffort *bool    `json:"supports_reasoning_effort"`
	ThinkingLevels          []string `json:"thinking_levels"`
	ReasoningEfforts        []string `json:"reasoning_efforts"`
	SupportedThinkingLevel  []string `json:"supported_thinking_levels"`
}

type modelArchitecture struct {
	InputModalities []string `json:"input_modalities"`
}

type modelsDevCatalog map[string]modelsDevProvider

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	Reasoning        *bool                      `json:"reasoning"`
	ReasoningOptions []modelsDevReasoningOption `json:"reasoning_options"`
	ToolCall         *bool                      `json:"tool_call"`
	Limit            modelsDevLimit             `json:"limit"`
	Cost             *modelsDevCost             `json:"cost,omitempty"`
	Modalities       modelsDevModalities        `json:"modalities"`
}

type modelsDevReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type modelsDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type modelsDevCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type modelsDevModalities struct {
	Input []string `json:"input"`
}

type catalogCacheFile struct {
	Version    int              `json:"version"`
	BaseURL    string           `json:"base_url"`
	CatalogURL string           `json:"catalog_url"`
	FetchedAt  int64            `json:"fetched_at"`
	Models     []protocol.Model `json:"models"`
}

func cloneModels(models []protocol.Model) []protocol.Model {
	out := make([]protocol.Model, len(models))
	for i := range models {
		out[i] = models[i].Clone()
	}
	return out
}

func (p *Provider) loadCatalogCache(now time.Time) ([]protocol.Model, bool) {
	if p.cacheRoot == "" {
		return nil, false
	}
	path := filepath.Join(p.cacheRoot, "catalog.json")
	info, err := os.Stat(path)
	if err != nil || info.Size() > catalogCacheMaxBytes || !info.Mode().IsRegular() {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cached catalogCacheFile
	if json.Unmarshal(data, &cached) != nil || cached.Version != 1 || cached.BaseURL != p.baseURL || cached.CatalogURL != p.catalogURL || len(cached.Models) == 0 {
		return nil, false
	}
	fetched := time.UnixMilli(cached.FetchedAt)
	fresh := !fetched.After(now.Add(time.Minute)) && now.Sub(fetched) <= catalogCacheFreshness
	return cloneModels(cached.Models), fresh
}

func (p *Provider) saveCatalogCache(models []protocol.Model, now time.Time) {
	if p.cacheRoot == "" || len(models) == 0 {
		return
	}
	if err := os.MkdirAll(p.cacheRoot, 0o700); err != nil {
		return
	}
	_ = os.Chmod(p.cacheRoot, 0o700)
	data, err := json.Marshal(catalogCacheFile{Version: 1, BaseURL: p.baseURL, CatalogURL: p.catalogURL, FetchedAt: now.UnixMilli(), Models: cloneModels(models)})
	if err != nil || len(data) > catalogCacheMaxBytes {
		return
	}
	tmp, err := os.CreateTemp(p.cacheRoot, ".models-*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	ok := tmp.Chmod(0o600) == nil
	if ok {
		_, err = tmp.Write(data)
		ok = err == nil && tmp.Sync() == nil
	}
	if closeErr := tmp.Close(); closeErr != nil {
		ok = false
	}
	if ok {
		_ = os.Rename(name, filepath.Join(p.cacheRoot, "catalog.json"))
	}
}

// ListModels implements provider.Transport. Direct callers retain adapter
// fallback behavior; authenticated runtimes use ListModelsWithCredential.
func (p *Provider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	return p.listModels(ctx, auth.Credential{})
}

func (p *Provider) ListModelsWithCredential(ctx context.Context, credential auth.Credential) ([]protocol.Model, error) {
	return p.listModels(ctx, credential)
}

// listModels returns the static catalog when the remote catalog cannot be
// fetched; it never fails on network errors.
func (p *Provider) listModels(ctx context.Context, credential auth.Credential) ([]protocol.Model, error) {
	p.catalogMu.Lock()
	defer p.catalogMu.Unlock()
	now := time.Now()
	if len(p.cachedModels) > 0 && now.Sub(p.cachedAt) <= catalogCacheFreshness {
		return cloneModels(p.cachedModels), nil
	}
	static := p.staticCatalog()
	fallback := static
	if cached, fresh := p.loadCatalogCache(now); len(cached) > 0 {
		fallback = cached
		if fresh {
			p.cachedModels, p.cachedAt = cloneModels(cached), now
			return cached, nil
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, p.discoveryTimeout)
	defer cancel()
	var catalog <-chan map[string]modelsDevModel
	if p.catalogURL != "" {
		catalogResult := make(chan map[string]modelsDevModel, 1)
		catalog = catalogResult
		go func() {
			catalogResult <- p.fetchModelsDev(discoveryCtx)
		}()
	}
	req, err := http.NewRequestWithContext(discoveryCtx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return cloneModels(fallback), nil
	}
	if key := p.resolveKey(credential); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return cloneModels(fallback), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cloneModels(fallback), nil
	}
	var payload openAIModelCatalog
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return cloneModels(fallback), nil
	}
	var metadata map[string]modelsDevModel
	if catalog != nil {
		metadata = <-catalog
	}
	out := make([]protocol.Model, 0, len(payload.Data))
	for _, record := range payload.Data {
		if details, ok := metadata[record.ID]; ok {
			record = enrichModelRecord(record, details)
		}
		model, ok := normalizeModelRecord(record)
		if ok {
			out = append(out, model)
		}
	}
	if len(out) == 0 {
		return cloneModels(fallback), nil
	}
	p.cachedModels, p.cachedAt = cloneModels(out), now
	p.saveCatalogCache(out, now)
	return cloneModels(out), nil
}

func (p *Provider) fetchModelsDev(ctx context.Context) map[string]modelsDevModel {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.catalogURL, nil)
	if err != nil {
		return nil
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var payload modelsDevCatalog
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&payload); err != nil {
		return nil
	}
	return payload[ProviderID].Models
}

// enrichModelRecord applies OpenCode's models.dev metadata only where the
// provider's live /models record omitted a field. Availability and any direct
// gateway metadata therefore remain authoritative.
func enrichModelRecord(record openAIModelRecord, details modelsDevModel) openAIModelRecord {
	if record.Name == "" && record.DisplayName == "" {
		record.Name = details.Name
	}
	if record.ContextWindow <= 0 && record.ContextLength <= 0 {
		record.ContextWindow = details.Limit.Context
	}
	if record.MaxOutputTokens <= 0 && record.MaxCompletionTokens <= 0 && record.MaxTokens <= 0 {
		record.MaxOutputTokens = details.Limit.Output
	}
	if record.SupportsTools == nil && (record.Capabilities == nil || record.Capabilities.Tools == nil) {
		record.SupportsTools = details.ToolCall
	}
	if record.SupportsThinking == nil && record.Reasoning == nil && record.SupportsReasoning == nil && (record.Capabilities == nil || (record.Capabilities.Thinking == nil && record.Capabilities.Reasoning == nil)) {
		record.Reasoning = details.Reasoning
	}
	if record.SupportsVision == nil && (record.Capabilities == nil || record.Capabilities.Vision == nil) && len(record.Input) == 0 && record.Architecture == nil {
		record.Input = append([]string(nil), details.Modalities.Input...)
	}
	if record.Pricing == nil && details.Cost != nil {
		record.Pricing = &protocol.ModelPricing{
			Currency:             "USD",
			InputPerMillion:      details.Cost.Input,
			OutputPerMillion:     details.Cost.Output,
			CacheReadPerMillion:  details.Cost.CacheRead,
			CacheWritePerMillion: details.Cost.CacheWrite,
		}
	}
	if record.SupportsReasoningEffort == nil && (record.Capabilities == nil || (record.Capabilities.ReasoningEffort == nil && record.Capabilities.SupportsReasoningEffort == nil)) && len(record.ThinkingLevels) == 0 && len(record.ReasoningEfforts) == 0 && len(record.ReasoningEffortLevels) == 0 && len(record.SupportedReasoningEfforts) == 0 {
		for _, option := range details.ReasoningOptions {
			if option.Type != "effort" || len(option.Values) == 0 {
				continue
			}
			supported := true
			record.SupportsReasoningEffort = &supported
			record.ReasoningEfforts = append([]string(nil), option.Values...)
			break
		}
	}
	return record
}

func normalizeModelRecord(record openAIModelRecord) (protocol.Model, bool) {
	if strings.TrimSpace(record.ID) == "" {
		return protocol.Model{}, false
	}
	name := strings.TrimSpace(record.Name)
	if name == "" {
		name = strings.TrimSpace(record.DisplayName)
	}
	if name == "" {
		name = "OpenCode Go " + record.ID
	}
	contextWindow := record.ContextWindow
	if contextWindow <= 0 {
		contextWindow = record.ContextLength
	}
	if contextWindow <= 0 {
		contextWindow = 200000
	}
	maxOutput := record.MaxOutputTokens
	if maxOutput <= 0 {
		maxOutput = record.MaxCompletionTokens
	}
	if maxOutput <= 0 {
		maxOutput = record.MaxTokens
	}

	model := protocol.Model{
		Provider:        ProviderID,
		ID:              record.ID,
		DisplayName:     name,
		ContextWindow:   contextWindow,
		MaxOutputTokens: maxOutput,
		SupportsTools:   true, // preserve the gateway's established default
		Pricing:         record.Pricing,
	}
	if record.SupportsTools != nil {
		model.SupportsTools = *record.SupportsTools
	} else if record.Capabilities != nil && record.Capabilities.Tools != nil {
		model.SupportsTools = *record.Capabilities.Tools
	}

	if record.SupportsVision != nil {
		model.SupportsVision = *record.SupportsVision
	} else if record.Capabilities != nil && record.Capabilities.Vision != nil {
		model.SupportsVision = *record.Capabilities.Vision
	} else {
		for _, modality := range record.Input {
			if strings.EqualFold(modality, "image") {
				model.SupportsVision = true
				break
			}
		}
		if record.Architecture != nil && !model.SupportsVision {
			for _, modality := range record.Architecture.InputModalities {
				if strings.EqualFold(modality, "image") {
					model.SupportsVision = true
					break
				}
			}
		}
	}

	levels := append([]string(nil), record.ThinkingLevels...)
	levels = append(levels, record.ReasoningEfforts...)
	levels = append(levels, record.ReasoningEffortLevels...)
	levels = append(levels, record.SupportedReasoningEfforts...)
	reasoningAdvertised := false
	effortCapabilityKnown := false
	if record.SupportsReasoningEffort != nil {
		effortCapabilityKnown = true
		reasoningAdvertised = *record.SupportsReasoningEffort
	}
	if record.Capabilities != nil {
		levels = append(levels, record.Capabilities.ThinkingLevels...)
		levels = append(levels, record.Capabilities.ReasoningEfforts...)
		levels = append(levels, record.Capabilities.SupportedThinkingLevel...)
		if !effortCapabilityKnown && record.Capabilities.ReasoningEffort != nil {
			effortCapabilityKnown = true
			reasoningAdvertised = *record.Capabilities.ReasoningEffort
		}
		if !effortCapabilityKnown && record.Capabilities.SupportsReasoningEffort != nil {
			effortCapabilityKnown = true
			reasoningAdvertised = *record.Capabilities.SupportsReasoningEffort
		}
	}
	parsedLevels := normalizeThinkingLevels(levels)
	if len(parsedLevels) > 0 && !effortCapabilityKnown {
		reasoningAdvertised = true
	}
	if !effortCapabilityKnown {
		for _, param := range record.SupportedParams {
			if strings.EqualFold(param, "reasoning_effort") {
				reasoningAdvertised = true
				break
			}
		}
	}
	if effortCapabilityKnown && !reasoningAdvertised {
		// An explicit false capability wins over stale/extra level metadata.
		parsedLevels = nil
	}
	if record.SupportsThinking != nil {
		model.SupportsThinking = *record.SupportsThinking
	} else if record.Reasoning != nil {
		model.SupportsThinking = *record.Reasoning
	} else if record.SupportsReasoning != nil {
		model.SupportsThinking = *record.SupportsReasoning
	} else if record.Capabilities != nil {
		if record.Capabilities.Thinking != nil {
			model.SupportsThinking = *record.Capabilities.Thinking
		} else if record.Capabilities.Reasoning != nil {
			model.SupportsThinking = *record.Capabilities.Reasoning
		} else {
			model.SupportsThinking = reasoningAdvertised
		}
	} else {
		model.SupportsThinking = reasoningAdvertised
	}
	if reasoningAdvertised && len(parsedLevels) == 0 {
		// A generic reasoning_effort parameter documents only the standard
		// low/medium/high values. More specific levels require explicit catalog
		// advertisement so Snow never sends a guessed native value.
		parsedLevels = []protocol.ThinkingLevel{
			protocol.ThinkingLow,
			protocol.ThinkingMedium,
			protocol.ThinkingHigh,
		}
	}
	if !model.SupportsThinking {
		parsedLevels = nil
	}
	model.ThinkingLevels = parsedLevels
	return model, true
}

func normalizeThinkingLevels(values []string) []protocol.ThinkingLevel {
	var out []protocol.ThinkingLevel
	seen := make(map[protocol.ThinkingLevel]bool)
	for _, raw := range values {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "minimal":
			if !seen[protocol.ThinkingMinimal] {
				seen[protocol.ThinkingMinimal] = true
				out = append(out, protocol.ThinkingMinimal)
			}
		case "low":
			if !seen[protocol.ThinkingLow] {
				seen[protocol.ThinkingLow] = true
				out = append(out, protocol.ThinkingLow)
			}
		case "medium":
			if !seen[protocol.ThinkingMedium] {
				seen[protocol.ThinkingMedium] = true
				out = append(out, protocol.ThinkingMedium)
			}
		case "high":
			if !seen[protocol.ThinkingHigh] {
				seen[protocol.ThinkingHigh] = true
				out = append(out, protocol.ThinkingHigh)
			}
		case "xhigh":
			if !seen[protocol.ThinkingXHigh] {
				seen[protocol.ThinkingXHigh] = true
				out = append(out, protocol.ThinkingXHigh)
			}
		case "max":
			if !seen[protocol.ThinkingMax] {
				seen[protocol.ThinkingMax] = true
				out = append(out, protocol.ThinkingMax)
			}
		case "ultra":
			if !seen[protocol.ThinkingUltra] {
				seen[protocol.ThinkingUltra] = true
				out = append(out, protocol.ThinkingUltra)
			}
		case "off", "none":
			// Off is implicit and is never sent as a native effort value.
		}
	}
	return out
}

// Chat implements provider.Provider: starts an SSE streaming chat request and
// returns a normalized EventStream.
func (p *Provider) Chat(ctx context.Context, creds auth.Credential, req protocol.ChatRequest) (protocol.EventStream, error) {
	key := p.resolveKey(creds)
	if key == "" && !p.allowAnonymous {
		return errorStream(ctx, fmt.Errorf("%s: no API key found: set %s, add a %q entry to the auth file, or pass a credential explicitly", p.providerID, EnvAPIKey, p.providerID)), nil
	}

	body, err := p.buildBody(req)
	if err != nil {
		return errorStream(ctx, fmt.Errorf("%s: build request: %w", p.providerID, err)), nil
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return errorStream(ctx, fmt.Errorf("%s: create request: %w", p.providerID, err)), nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return errorStream(ctx, fmt.Errorf("%s: request failed: %w", p.providerID, err)), nil
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		redactedSnippet := providerpkg.RedactSecrets(strings.TrimSpace(string(snippet)), key)
		msg := fmt.Sprintf("%s: HTTP %d: %s", p.providerID, resp.StatusCode, redactedSnippet)
		if resp.StatusCode == http.StatusUnauthorized {
			msg = p.providerID + ": invalid API key (HTTP 401)"
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusPaymentRequired {
			return errorStream(ctx, &providerpkg.LimitError{Provider: p.providerID, Status: resp.StatusCode, Message: redactedSnippet}), nil
		}
		return errorStream(ctx, errors.New(msg)), nil
	}
	mediaType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if !strings.EqualFold(mediaType, "text/event-stream") {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		message := providerpkg.RedactSecrets(strings.TrimSpace(string(snippet)), key)
		if message == "" {
			message = "expected text/event-stream response"
		}
		return errorStream(ctx, fmt.Errorf("%s: incompatible response content type %q: %s", p.providerID, mediaType, truncateStr(message, 500))), nil
	}

	resp.Body = providerpkg.WrapIdleReadCloser(resp.Body, p.streamIdleTimeout)
	s := newStream(ctx, 64, func() { _ = resp.Body.Close() }, p.providerID, key)
	go s.readSSE(resp)
	return s, nil
}

// ---------------------------------------------------------------------------
// Request building
// ---------------------------------------------------------------------------

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
	oreq := openAIChatRequest{
		Model:         model,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
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
	return json.Marshal(oreq)
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
		om := openAIMessage{Role: "assistant", Content: textContent(m)}
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
		// Harness notes surface as assistant text.
		return openAIMessage{Role: "assistant", Content: textContent(m)}, true
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

type openAIChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

type openAIUsage struct {
	PromptTokens         int  `json:"prompt_tokens"`
	CompletionTokens     int  `json:"completion_tokens"`
	TotalTokens          int  `json:"total_tokens"`
	PromptCacheHitTokens *int `json:"prompt_cache_hit_tokens"`
	PromptTokensDetails  struct {
		CachedTokens        *int `json:"cached_tokens"`
		CacheCreationTokens int  `json:"cache_creation_input_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func mapUsage(u openAIUsage) *protocol.Usage {
	out := &protocol.Usage{
		Input:      u.PromptTokens,
		Output:     u.CompletionTokens,
		Reasoning:  u.CompletionTokensDetails.ReasoningTokens,
		CacheWrite: u.PromptTokensDetails.CacheCreationTokens,
	}
	cachedTokens := u.PromptTokensDetails.CachedTokens
	if cachedTokens == nil {
		cachedTokens = u.PromptCacheHitTokens
	}
	if cachedTokens != nil {
		out.CacheRead = *cachedTokens
		out.CacheReadKnown = true
	}
	out.Total = u.TotalTokens
	if out.Total == 0 {
		out.Total = out.Input + out.Output
	}
	return out
}

type toolCallAccum struct {
	index   int
	id      string
	name    string
	argsBuf strings.Builder
}

// finalArgs returns the complete arguments, wrapping malformed fragments.
func (a *toolCallAccum) finalArgs() json.RawMessage {
	raw := strings.TrimSpace(a.argsBuf.String())
	if raw == "" {
		raw = "{}"
	}
	if !json.Valid([]byte(raw)) {
		wrapped, _ := json.Marshal(map[string]string{"_raw": raw})
		return wrapped
	}
	return json.RawMessage(raw)
}

// stream is the channel-backed EventStream returned by Chat.
type stream struct {
	ch                 chan protocol.StreamEvent
	done               chan struct{}
	reqCtx             context.Context
	closeFn            func()
	once               sync.Once
	totalArgs          int
	responseTextBytes  int
	reasoningTextBytes int
	provider           string
	secret             string
}

func newStream(ctx context.Context, buf int, closeFn func(), provider, secret string) *stream {
	return &stream{
		ch:       make(chan protocol.StreamEvent, buf),
		done:     make(chan struct{}),
		reqCtx:   ctx,
		closeFn:  closeFn,
		provider: provider,
		secret:   secret,
	}
}

// Next implements protocol.EventStream.
func (s *stream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	select {
	case ev, ok := <-s.ch:
		if !ok {
			return protocol.StreamEvent{}, io.EOF
		}
		return ev, nil
	case <-ctx.Done():
		return protocol.StreamEvent{}, ctx.Err()
	case <-s.reqCtx.Done():
		return protocol.StreamEvent{}, s.reqCtx.Err()
	case <-s.done:
		return protocol.StreamEvent{}, io.EOF
	}
}

// Close implements protocol.EventStream.
func (s *stream) Close() error {
	s.once.Do(func() {
		if s.closeFn != nil {
			s.closeFn()
		}
		close(s.done)
	})
	return nil
}

// send delivers an event unless the stream is being torn down.
func (s *stream) send(ev protocol.StreamEvent) {
	select {
	case s.ch <- ev:
	case <-s.reqCtx.Done():
	case <-s.done:
	}
}

// errorStream returns a stream whose first event is EvStreamError, then EOF.
func errorStream(ctx context.Context, err error) protocol.EventStream {
	s := newStream(ctx, 1, nil, ProviderID, "")
	s.ch <- protocol.StreamEvent{Type: protocol.EvStreamError, Err: err}
	close(s.ch)
	return s
}

// readSSE consumes the response body and normalizes events into the stream.
// Only the user's Close() closes s.done; this goroutine only closes s.ch so
// buffered events are always drained before EOF is reported.
func (s *stream) readSSE(resp *http.Response) {
	defer close(s.ch)
	defer func() { _ = resp.Body.Close() }()

	r := bufio.NewReader(resp.Body)
	var (
		eventName        string
		accums           = make(map[int]*toolCallAccum)
		order            []int // insertion order of tool call indices
		finish           protocol.StopReason
		doneSent         bool
		errored          bool
		terminalObserved bool
	)
	markError := func(ev protocol.StreamEvent) {
		errored = true
		s.send(ev)
	}
	sendDone := func() {
		if doneSent || errored {
			return
		}
		doneSent = true
		if finish == "" {
			finish = protocol.StopStop
		}
		s.send(protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: finish})
	}

	for {
		line, err := readBoundedSSELine(r, maxSSELineBytes)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, providerpkg.ErrStreamIdle) {
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("%s: SSE line exceeds limit or is unreadable: %w", s.provider, err)})
			return
		}
		if line != "" {
			stop := s.handleLine(strings.TrimSpace(line), &eventName, accums, &order, &finish, &terminalObserved, markError)
			if stop {
				sendDone()
				return
			}
		}
		if errors.Is(err, providerpkg.ErrStreamIdle) {
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("%s: stream idle timeout: %w", s.provider, err)})
			return
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if terminalObserved {
					sendDone()
				} else {
					markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("%s: stream truncated before terminal event", s.provider)})
				}
				return
			}
			select {
			case <-s.done:
				return // user closed the stream; not an error
			default:
			}
			if s.reqCtx.Err() != nil {
				return // cancellation; Next() surfaces ctx.Err()
			}
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("%s: stream read failed: %w", s.provider, err)})
			return
		}
	}
}

// handleLine returns true when the stream should stop (e.g. after [DONE]).
func (s *stream) handleLine(line string, eventName *string, accums map[int]*toolCallAccum, order *[]int, finish *protocol.StopReason, terminalObserved *bool, markError func(protocol.StreamEvent)) bool {
	switch {
	case strings.HasPrefix(line, "event:"):
		*eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
	case strings.HasPrefix(line, "data:"):
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			return false
		}
		if *eventName == "error" {
			*eventName = ""
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: errors.New(s.provider + ": " + providerpkg.RedactSecrets(data, s.secret))})
			return false
		}
		*eventName = ""
		if data == "[DONE]" {
			// [DONE] terminates the stream; some servers keep the connection
			// open afterwards, so signal the caller to stop reading.
			*terminalObserved = true
			return true
		}
		var chunk openAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			var errPayload struct {
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if uerr := json.Unmarshal([]byte(data), &errPayload); uerr == nil && errPayload.Error != nil && errPayload.Error.Message != "" {
				markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: errors.New(s.provider + ": " + providerpkg.RedactSecrets(errPayload.Error.Message, s.secret))})
			} else {
				// Malformed chunk: surface it rather than silently dropping.
				markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: fmt.Errorf("%s: unparseable SSE data: %s", s.provider, truncateStr(providerpkg.RedactSecrets(data, s.secret), 500))})
			}
			return false
		}
		if err := s.processChunk(chunk, accums, order, finish); err != nil {
			markError(protocol.StreamEvent{Type: protocol.EvStreamError, Err: err})
			return true
		}
		if *finish != "" {
			*terminalObserved = true
		}
	}
	return false
}

func (s *stream) processChunk(chunk openAIChunk, accums map[int]*toolCallAccum, order *[]int, finish *protocol.StopReason) error {
	if chunk.Usage != nil {
		s.send(protocol.StreamEvent{Type: protocol.EvStreamUsage, Usage: mapUsage(*chunk.Usage)})
	}
	for _, ch := range chunk.Choices {
		d := ch.Delta
		if d.Content != "" {
			if len(d.Content) > maxResponseTextBytes-s.responseTextBytes {
				return fmt.Errorf("%s: response text exceeds size limit", s.provider)
			}
			s.responseTextBytes += len(d.Content)
			s.send(protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: d.Content})
		}
		if d.ReasoningContent != "" {
			if len(d.ReasoningContent) > maxReasoningBytes-s.reasoningTextBytes {
				return fmt.Errorf("%s: reasoning text exceeds size limit", s.provider)
			}
			s.reasoningTextBytes += len(d.ReasoningContent)
			s.send(protocol.StreamEvent{Type: protocol.EvStreamThinkingDelta, Text: d.ReasoningContent})
		}
		for _, tc := range d.ToolCalls {
			acc, ok := accums[tc.Index]
			if !ok {
				if len(accums) >= maxStreamToolCalls {
					return fmt.Errorf("%s: too many streamed tool calls", s.provider)
				}
				acc = &toolCallAccum{index: tc.Index}
				accums[tc.Index] = acc
				*order = append(*order, tc.Index)
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			// Sticky fallback id: some compatible servers defer/omit ids.
			if acc.id == "" {
				acc.id = fmt.Sprintf("tc-%d", acc.index)
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			fragmentBytes := len(tc.Function.Arguments)
			if acc.argsBuf.Len()+fragmentBytes > maxToolArgumentBytes || s.totalArgs+fragmentBytes > maxTotalToolArgumentBytes {
				return fmt.Errorf("%s: streamed tool arguments exceed limit", s.provider)
			}
			// Deltas carry only the fragment; the agent appends fragments.
			s.send(protocol.StreamEvent{
				Type:       protocol.EvStreamToolCallDelta,
				ToolCallID: acc.id,
				ToolName:   acc.name,
				Arguments:  json.RawMessage(tc.Function.Arguments),
			})
			if tc.Function.Arguments != "" {
				acc.argsBuf.WriteString(tc.Function.Arguments)
				s.totalArgs += fragmentBytes
			}
		}
		// finish_reason is first-wins: a trailing chunk must not overwrite an
		// earlier tool_calls/stop decision.
		switch ch.FinishReason {
		case "stop":
			if *finish == "" {
				*finish = protocol.StopStop
			}
		case "length":
			if *finish == "" {
				*finish = protocol.StopLength
			}
		case "tool_calls":
			if *finish == "" {
				*finish = protocol.StopToolUse
			}
			if *finish == protocol.StopToolUse {
				for _, idx := range *order {
					acc := accums[idx]
					if acc == nil {
						continue
					}
					s.send(protocol.StreamEvent{
						Type:       protocol.EvStreamToolCallDone,
						ToolCallID: acc.id,
						ToolName:   acc.name,
						Arguments:  acc.finalArgs(),
					})
				}
			}
		}
	}
	return nil
}

func readBoundedSSELine(reader *bufio.Reader, maxBytes int) (string, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > maxBytes {
			return "", fmt.Errorf("line exceeds %d bytes", maxBytes)
		}
		line = append(line, part...)
		if err == nil {
			return string(line), nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return string(line), err
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
