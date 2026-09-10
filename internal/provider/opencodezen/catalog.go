package opencodezen

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/provider/modelsdev"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	catalogCacheVersion = 3
	catalogMaxBytes     = 4 << 20
	catalogFreshness    = 15 * time.Minute
	modelsDevProviderID = "opencode"
)

type freeModelSpec struct {
	Model     protocol.Model
	Transport transportKind
}

// freeModels is the bundled offline fallback and local transport/privacy
// policy. Reasoning capability and selectable efforts are deliberately absent:
// ListModels enriches them from OpenCode's current models.dev record instead of
// pinning model-specific values in Snow. Big Pickle advertises a 200k total
// context but a stricter 160k input limit, so Snow uses 160k for safe compaction
// and exposes 200k as the maximum context.
func freeModels() []freeModelSpec {
	return []freeModelSpec{
		{
			Model: protocol.Model{
				Provider: ProviderID, ID: "big-pickle", DisplayName: "Big Pickle",
				Description:   "Privacy warning: collected data may be used to improve the model during its free period. Effective input limit: 160k of 200k total context.",
				ContextWindow: 160000, MaxContextWindow: 200000, MaxOutputTokens: 32000,
				SupportsTools: true,
			},
			Transport: transportChat,
		},
		{
			Model: protocol.Model{
				Provider: ProviderID, ID: "x-preview-f-free", DisplayName: "Ox Alpha Free",
				Description:   "Privacy: the provider documents zero retention and no use of your data for model training.",
				ContextWindow: 1000000, MaxOutputTokens: 131072,
				SupportsTools: true, SupportsVision: true,
			},
			Transport: transportChat,
		},
		{
			Model: protocol.Model{
				Provider: ProviderID, ID: "mimo-v2.5-free", DisplayName: "MiMo-V2.5 Free",
				Description:   "Privacy warning: collected data may be used to improve the model during its free period.",
				ContextWindow: 200000, MaxOutputTokens: 32000,
				SupportsTools: true, SupportsVision: true,
			},
			Transport: transportChat,
		},
		{
			Model: protocol.Model{
				Provider: ProviderID, ID: "hy3-free", DisplayName: "Hy3 Free",
				Description:   "Privacy warning: collected data may be used to improve the model during its free period.",
				ContextWindow: 190000, MaxOutputTokens: 64000,
				SupportsTools: true,
			},
			Transport: transportChat,
		},
		{
			Model: protocol.Model{
				Provider: ProviderID, ID: "nemotron-3-ultra-free", DisplayName: "Nemotron 3 Ultra Free",
				Description:   "Privacy warning: NVIDIA trial endpoint; do not submit personal or confidential data. Use is logged for security and product improvement.",
				ContextWindow: 1000000, MaxOutputTokens: 128000,
				SupportsTools: true,
			},
			Transport: transportChat,
		},
		{
			Model: protocol.Model{
				Provider: ProviderID, ID: "nemotron-3.5-lightning-free", DisplayName: "Nemotron 3.5 Lightning Free",
				Description:   "Privacy warning: NVIDIA trial endpoint; do not submit personal or confidential data. Use is logged for security and product improvement.",
				ContextWindow: 262144, MaxOutputTokens: 262144,
				SupportsTools: true,
			},
			Transport: transportChat,
		},
		{
			Model: protocol.Model{
				Provider: ProviderID, ID: "muse-spark-1.2-contributor-free", DisplayName: "Muse Spark 1.2 Contributor Free",
				Description:   "Privacy warning: prompts and completions may be used to train future Meta models.",
				ContextWindow: 1048576, MaxOutputTokens: 131072,
				SupportsTools: true, SupportsVision: true,
			},
			Transport: transportResponses,
		},
	}
}

func freeModelByID(id string) (freeModelSpec, bool) {
	for _, spec := range freeModels() {
		if spec.Model.ID == id {
			return spec, true
		}
	}
	return freeModelSpec{}, false
}

func cloneModels(models []protocol.Model) []protocol.Model {
	out := make([]protocol.Model, len(models))
	for i := range models {
		out[i] = models[i].Clone()
	}
	return out
}

func staticCatalog() []protocol.Model {
	specs := freeModels()
	models := make([]protocol.Model, 0, len(specs))
	for _, spec := range specs {
		models = append(models, spec.Model.Clone())
	}
	return models
}

type modelsPayload struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type catalogCache struct {
	Version    int                        `json:"version"`
	BaseURL    string                     `json:"base_url"`
	CatalogURL string                     `json:"catalog_url"`
	FetchedAt  int64                      `json:"fetched_at"`
	Models     []protocol.Model           `json:"models"`
	Metadata   map[string]modelsdev.Model `json:"metadata,omitempty"`
}

type metadataResult struct {
	models map[string]modelsdev.Model
	ok     bool
}

func (p *Provider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	return p.ListModelsWithCredential(ctx, auth.Credential{})
}

func (p *Provider) ListModelsWithCredential(ctx context.Context, credential auth.Credential) ([]protocol.Model, error) {
	return p.listModels(ctx, credential, false)
}

func (p *Provider) RefreshModelsWithCredential(ctx context.Context, credential auth.Credential) ([]protocol.Model, error) {
	return p.listModels(ctx, credential, true)
}

func (p *Provider) RefreshModels(ctx context.Context) ([]protocol.Model, error) {
	return p.RefreshModelsWithCredential(ctx, auth.Credential{})
}

// ModelCatalogStale lets the app expire its snapshot without holding the
// catalog mutex or starting a network request on the UI goroutine.
func (p *Provider) ModelCatalogStale() bool {
	return time.Now().UnixMilli() >= p.catalogExpiresAt.Load()
}

// ModelCatalogRevision changes when discovery publishes a snapshot, including
// discovery performed by Chat before the app next opens its picker.
func (p *Provider) ModelCatalogRevision() uint64 { return p.catalogRevision.Load() }

func (p *Provider) listModels(ctx context.Context, credential auth.Credential, force bool) ([]protocol.Model, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.catalogMu.Lock()
	defer p.catalogMu.Unlock()
	now := time.Now()
	if !force && !p.cachedAt.IsZero() && !p.ModelCatalogStale() {
		return cloneModels(p.cachedModels), nil
	}
	fallback := staticCatalog()
	var cachedModels []protocol.Model
	if !p.cachedAt.IsZero() {
		cachedModels = p.cachedModels
		fallback = cachedModels
	} else if cached, fresh := p.loadCatalogCache(now); cached != nil {
		cachedModels = cached
		fallback = cached
		p.cachedModels = cloneModels(cached)
		if fresh && !force {
			p.cachedModels = cloneModels(cached)
			p.catalogExpiresAt.Store(p.cachedAt.Add(catalogFreshness).UnixMilli())
			p.catalogRevision.Add(1)
			return cloneModels(cached), nil
		}
	}
	fallbackMetadata := p.cachedMetadata
	failed := func() ([]protocol.Model, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p.cachedModels, p.cachedMetadata = cloneModels(fallback), fallbackMetadata
		p.cachedAt = now
		// Avoid repeated offline lookups on every picker opening, but retry sooner
		// than a successfully verified catalog. Never renew the disk snapshot.
		p.catalogExpiresAt.Store(now.Add(time.Minute).UnixMilli())
		p.catalogRevision.Add(1)
		return cloneModels(fallback), nil
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, p.discoveryTimeout)
	defer cancel()
	var metadata <-chan metadataResult
	if p.catalogURL != "" {
		result := make(chan metadataResult, 1)
		metadata = result
		go func() {
			models, ok := modelsdev.FetchProvider(discoveryCtx, p.client, p.catalogURL, modelsDevProviderID)
			result <- metadataResult{models: models, ok: ok}
		}()
	}
	req, err := http.NewRequestWithContext(discoveryCtx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return failed()
	}
	if key := p.resolveKey(credential); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return failed()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return failed()
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, catalogMaxBytes+1))
	if err != nil || len(data) > catalogMaxBytes {
		return failed()
	}
	var payload modelsPayload
	if json.Unmarshal(data, &payload) != nil || payload.Data == nil {
		return failed()
	}
	var dynamic metadataResult
	if metadata != nil {
		dynamic = <-metadata
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !dynamic.ok {
		dynamic.models = fallbackMetadata
	}
	cachedByID := make(map[string]protocol.Model, len(cachedModels))
	for _, model := range cachedModels {
		cachedByID[model.ID] = model
	}
	available := make(map[string]bool, len(payload.Data))
	for _, record := range payload.Data {
		available[record.ID] = true
	}
	models := make([]protocol.Model, 0, len(available))
	ids := make([]string, 0, len(available))
	for _, spec := range freeModels() {
		if available[spec.Model.ID] {
			ids = append(ids, spec.Model.ID)
			delete(available, spec.Model.ID)
		}
	}
	ids = append(ids, slices.Sorted(maps.Keys(available))...)
	verifiedMetadata := make(map[string]modelsdev.Model)
	for _, id := range ids {
		details, hasMetadata := dynamic.models[id]
		spec, ok := catalogSpec(id, details, hasMetadata)
		if !ok {
			continue
		}
		model := spec.Model.Clone()
		if hasMetadata {
			verifiedMetadata[id] = details
		} else if !dynamic.ok {
			applyCachedReasoning(&model, cachedByID[model.ID])
		}
		models = append(models, model)
	}
	// Persist successful empty catalogs too, so an offline restart cannot
	// resurrect withdrawn promotions from an older snapshot.
	p.cachedModels, p.cachedMetadata, p.cachedAt = cloneModels(models), verifiedMetadata, now
	p.catalogExpiresAt.Store(now.Add(catalogFreshness).UnixMilli())
	p.catalogRevision.Add(1)
	// A configured metadata endpoint must succeed before replacing a previously
	// verified disk snapshot. A custom gateway with enrichment disabled still
	// caches its authoritative availability intersection.
	if dynamic.ok || p.catalogURL == "" || len(models) == 0 {
		p.saveCatalogCache(models, now)
	}
	return cloneModels(models), nil
}

func applyReasoningMetadata(model *protocol.Model, details modelsdev.Model) {
	model.SupportsThinking, model.ThinkingLevels = modelsdev.ReasoningMetadata(details)
}

func applyCachedReasoning(model *protocol.Model, cached protocol.Model) {
	if !cached.SupportsThinking {
		return
	}
	model.SupportsThinking = true
	levels := cached.SupportedThinkingLevels()
	if len(levels) > 1 {
		model.ThinkingLevels = slices.Clone(levels[1:])
	}
}

func (p *Provider) loadCatalogCache(now time.Time) ([]protocol.Model, bool) {
	if p.cacheRoot == "" {
		return nil, false
	}
	path := filepath.Join(p.cacheRoot, "catalog.json")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > catalogMaxBytes {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cached catalogCache
	if json.Unmarshal(data, &cached) != nil || cached.Version != catalogCacheVersion || cached.BaseURL != p.baseURL || cached.CatalogURL != p.catalogURL || cached.Models == nil {
		return nil, false
	}
	// Revalidate new IDs from cached pricing/protocol evidence rather than
	// trusting cached protocol.Model capabilities or an arbitrary model ID.
	models := make([]protocol.Model, 0, len(cached.Models))
	seen := make(map[string]bool, len(cached.Models))
	for _, cachedModel := range cached.Models {
		details, hasMetadata := cached.Metadata[cachedModel.ID]
		spec, ok := catalogSpec(cachedModel.ID, details, hasMetadata)
		if !ok || cachedModel.Provider != ProviderID || seen[cachedModel.ID] {
			return nil, false
		}
		seen[cachedModel.ID] = true
		model := spec.Model.Clone()
		if !hasMetadata {
			applyCachedReasoning(&model, cachedModel)
		}
		models = append(models, model)
	}
	fetched := time.UnixMilli(cached.FetchedAt)
	if fetched.After(now.Add(time.Minute)) || cached.FetchedAt <= 0 {
		return nil, false
	}
	p.cachedAt, p.cachedMetadata = fetched, cached.Metadata
	fresh := now.Sub(fetched) < catalogFreshness
	return models, fresh
}

func (p *Provider) saveCatalogCache(models []protocol.Model, now time.Time) {
	if p.cacheRoot == "" {
		return
	}
	if err := os.MkdirAll(p.cacheRoot, 0o700); err != nil {
		return
	}
	_ = os.Chmod(p.cacheRoot, 0o700)
	data, err := json.Marshal(catalogCache{
		Version: catalogCacheVersion, BaseURL: p.baseURL, CatalogURL: p.catalogURL,
		FetchedAt: now.UnixMilli(), Models: cloneModels(models),
		Metadata: p.cachedMetadata,
	})
	if err != nil || len(data) > catalogMaxBytes {
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
