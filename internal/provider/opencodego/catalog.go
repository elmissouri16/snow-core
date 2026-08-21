// Package opencodego implements the OpenCode Go provider adapter: an
// OpenAI-compatible Chat Completions streaming client with bearer-token auth.
//
// The provider id is "opencode-go" (matching the auth.json key and the
// OPENCODE_API_KEY environment variable convention).
package opencodego

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/provider/modelsdev"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

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
	var catalog <-chan map[string]modelsdev.Model
	if p.catalogURL != "" {
		catalogResult := make(chan map[string]modelsdev.Model, 1)
		catalog = catalogResult
		go func() {
			models, _ := modelsdev.FetchProvider(discoveryCtx, p.client, p.catalogURL, ProviderID)
			catalogResult <- models
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
	var metadata map[string]modelsdev.Model
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

// enrichModelRecord applies OpenCode's models.dev metadata only where the
// provider's live /models record omitted a field. Availability and any direct
// gateway metadata therefore remain authoritative.
func enrichModelRecord(record openAIModelRecord, details modelsdev.Model) openAIModelRecord {
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
