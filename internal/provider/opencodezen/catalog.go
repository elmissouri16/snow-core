package opencodezen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	catalogMaxBytes  = 4 << 20
	catalogFreshness = 15 * time.Minute
)

type freeModelSpec struct {
	Model     protocol.Model
	Transport transportKind
}

func freeModels() []freeModelSpec {
	return []freeModelSpec{
		{Model: protocol.Model{Provider: ProviderID, ID: "big-pickle", DisplayName: "Big Pickle", Description: "Privacy warning: collected data may be used to improve the model during its free period.", SupportsTools: true}, Transport: transportChat},
		{Model: protocol.Model{Provider: ProviderID, ID: "x-preview-f-free", DisplayName: "Ox Alpha Free", Description: "Privacy: the provider documents zero retention and no use of your data for model training.", SupportsTools: true}, Transport: transportChat},
		{Model: protocol.Model{Provider: ProviderID, ID: "mimo-v2.5-free", DisplayName: "MiMo-V2.5 Free", Description: "Privacy warning: collected data may be used to improve the model during its free period.", SupportsTools: true}, Transport: transportChat},
		{Model: protocol.Model{Provider: ProviderID, ID: "hy3-free", DisplayName: "Hy3 Free", Description: "Privacy warning: collected data may be used to improve the model during its free period.", SupportsTools: true}, Transport: transportChat},
		{Model: protocol.Model{Provider: ProviderID, ID: "nemotron-3-ultra-free", DisplayName: "Nemotron 3 Ultra Free", Description: "Privacy warning: NVIDIA trial endpoint; do not submit personal or confidential data. Use is logged for security and product improvement.", SupportsTools: true}, Transport: transportChat},
		{Model: protocol.Model{Provider: ProviderID, ID: "nemotron-3.5-lightning-free", DisplayName: "Nemotron 3.5 Lightning Free", Description: "Privacy warning: NVIDIA trial endpoint; do not submit personal or confidential data. Use is logged for security and product improvement.", SupportsTools: true}, Transport: transportChat},
		{Model: protocol.Model{Provider: ProviderID, ID: "muse-spark-1.2-contributor-free", DisplayName: "Muse Spark 1.2 Contributor Free", Description: "Privacy warning: prompts and completions may be used to train future Meta models.", SupportsTools: true}, Transport: transportResponses},
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
	Version   int              `json:"version"`
	BaseURL   string           `json:"base_url"`
	FetchedAt int64            `json:"fetched_at"`
	Models    []protocol.Model `json:"models"`
}

func (p *Provider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	return p.ListModelsWithCredential(ctx, auth.Credential{})
}

func (p *Provider) ListModelsWithCredential(ctx context.Context, credential auth.Credential) ([]protocol.Model, error) {
	p.catalogMu.Lock()
	defer p.catalogMu.Unlock()
	now := time.Now()
	if len(p.cachedModels) > 0 && now.Sub(p.cachedAt) <= catalogFreshness {
		return cloneModels(p.cachedModels), nil
	}
	fallback := staticCatalog()
	if cached, fresh := p.loadCatalogCache(now); len(cached) > 0 {
		fallback = cached
		if fresh {
			p.cachedModels, p.cachedAt = cloneModels(cached), now
			return cloneModels(cached), nil
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, p.discoveryTimeout)
	defer cancel()
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
	data, err := io.ReadAll(io.LimitReader(resp.Body, catalogMaxBytes+1))
	if err != nil || len(data) > catalogMaxBytes {
		return cloneModels(fallback), nil
	}
	var payload modelsPayload
	if json.Unmarshal(data, &payload) != nil {
		return cloneModels(fallback), nil
	}
	available := make(map[string]bool, len(payload.Data))
	for _, record := range payload.Data {
		available[record.ID] = true
	}
	models := make([]protocol.Model, 0, len(available))
	for _, spec := range freeModels() {
		if available[spec.Model.ID] {
			models = append(models, spec.Model.Clone())
		}
	}
	// A valid live response is authoritative even when no maintained free model
	// remains available. Falling back here would resurrect expired promotions.
	if len(models) == 0 {
		return []protocol.Model{}, nil
	}
	p.cachedModels, p.cachedAt = cloneModels(models), now
	p.saveCatalogCache(models, now)
	return cloneModels(models), nil
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
	if json.Unmarshal(data, &cached) != nil || cached.Version != 1 || cached.BaseURL != p.baseURL || len(cached.Models) == 0 {
		return nil, false
	}
	// Treat cached records only as availability IDs. Rehydrate current local
	// metadata so stale or edited cache fields cannot alter transport/privacy
	// policy, and reject unknown or paid IDs completely.
	models := make([]protocol.Model, 0, len(cached.Models))
	seen := make(map[string]bool, len(cached.Models))
	for _, model := range cached.Models {
		spec, ok := freeModelByID(model.ID)
		if !ok || model.Provider != ProviderID || seen[model.ID] {
			return nil, false
		}
		seen[model.ID] = true
		models = append(models, spec.Model.Clone())
	}
	fetched := time.UnixMilli(cached.FetchedAt)
	fresh := !fetched.After(now.Add(time.Minute)) && now.Sub(fetched) <= catalogFreshness
	return models, fresh
}

func (p *Provider) saveCatalogCache(models []protocol.Model, now time.Time) {
	if p.cacheRoot == "" || len(models) == 0 {
		return
	}
	if err := os.MkdirAll(p.cacheRoot, 0o700); err != nil {
		return
	}
	_ = os.Chmod(p.cacheRoot, 0o700)
	data, err := json.Marshal(catalogCache{Version: 1, BaseURL: p.baseURL, FetchedAt: now.UnixMilli(), Models: cloneModels(models)})
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
