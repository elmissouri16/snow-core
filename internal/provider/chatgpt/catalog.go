package chatgpt

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	catalogFreshness    = 15 * time.Minute
	catalogTimeout      = 5 * time.Second
	maxCatalogBytes     = 8 << 20
	catalogCacheVersion = 1
)

type reasoningLevelRecord struct {
	Effort string `json:"effort"`
}

type modelUpgradeRecord struct {
	Model             string `json:"model"`
	MigrationMarkdown string `json:"migration_markdown"`
}

type modelRecord struct {
	Slug                     string                 `json:"slug"`
	DisplayName              string                 `json:"display_name"`
	Description              string                 `json:"description"`
	DefaultReasoningLevel    string                 `json:"default_reasoning_level"`
	SupportedReasoningLevels []reasoningLevelRecord `json:"supported_reasoning_levels"`
	Visibility               string                 `json:"visibility"`
	// SupportedInAPI describes public API availability, not whether the model
	// can run through the ChatGPT/Codex subscription backend.
	SupportedInAPI                    bool                `json:"supported_in_api"`
	Priority                          int                 `json:"priority"`
	SupportVerbosity                  bool                `json:"support_verbosity"`
	SupportsReasoningSummaryParameter *bool               `json:"supports_reasoning_summary_parameter"`
	ContextWindow                     int                 `json:"context_window"`
	MaxContextWindow                  int                 `json:"max_context_window"`
	EffectiveContextWindowPercent     int                 `json:"effective_context_window_percent"`
	InputModalities                   []string            `json:"input_modalities"`
	Upgrade                           *modelUpgradeRecord `json:"upgrade"`
}

type catalogCache struct {
	Version       int           `json:"version"`
	BackendOrigin string        `json:"backend_origin"`
	AccountID     string        `json:"account_id"`
	FetchedAt     time.Time     `json:"fetched_at"`
	ETag          string        `json:"etag,omitempty"`
	ClientVersion string        `json:"client_version"`
	Models        []modelRecord `json:"models"`
}

type modelsResponse struct {
	Models []modelRecord `json:"models"`
}

func (p *Provider) DefaultModel() protocol.Model {
	p.modelsMu.RLock()
	records := slices.Clone(p.models)
	p.modelsMu.RUnlock()
	models := mapModelRecords(records)
	if len(models) == 0 {
		models = Models()
	}
	return models[0]
}

// ModelCatalogAuthoritative reports whether ListModels is scoped to a stored
// ChatGPT account. App selection uses this to avoid keeping a configured model
// that the selected account catalog omitted.
func (p *Provider) ModelCatalogAuthoritative() bool {
	_, ok := p.storeCredential()
	return ok
}

func (p *Provider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	return p.listModels(ctx, false)
}

func (p *Provider) RefreshModels(ctx context.Context) ([]protocol.Model, error) {
	return p.listModels(ctx, true)
}

func (p *Provider) ListModelsWithCredential(ctx context.Context, credential auth.Credential) ([]protocol.Model, error) {
	return p.listModelsCredential(ctx, credential, false)
}

func (p *Provider) RefreshModelsWithCredential(ctx context.Context, credential auth.Credential) ([]protocol.Model, error) {
	return p.listModelsCredential(ctx, credential, true)
}

func (p *Provider) listModels(ctx context.Context, force bool) ([]protocol.Model, error) {
	cred, ok := p.storeCredential()
	if !ok {
		return Models(), nil
	}
	resolved, err := p.resolve(ctx, cred, false)
	if err != nil {
		accountID := cred.AccountID
		if status, checkErr := CheckAuth(cred); checkErr == nil && status.AccountID != "" {
			accountID = status.AccountID
		}
		if cached, ok := p.loadCatalogCache(accountID); ok && cached.ClientVersion == p.clientVersion {
			return p.acceptRecords(cached.Models), err
		}
		return nil, err
	}
	return p.listModelsCredential(ctx, resolved, force)
}

func (p *Provider) listModelsCredential(ctx context.Context, credential auth.Credential, force bool) ([]protocol.Model, error) {
	status, err := CheckAuth(credential)
	if err != nil {
		return nil, err
	}
	if status.AccountID == "" {
		return nil, errors.New("chatgpt: OAuth credential has no account ID for model discovery")
	}
	cache, cacheOK := p.loadCatalogCache(status.AccountID)
	compatibleCache := cacheOK && cache.ClientVersion == p.clientVersion
	if compatibleCache && !force && p.now().Sub(cache.FetchedAt) < catalogFreshness {
		return p.acceptRecords(cache.Models), nil
	}
	requestETag := ""
	if compatibleCache {
		requestETag = cache.ETag
	}
	records, etag, notModified, err := p.fetchCatalog(ctx, credential, requestETag, true)
	if err != nil {
		if compatibleCache {
			return p.acceptRecords(cache.Models), err
		}
		return nil, err
	}
	if notModified {
		if !compatibleCache {
			return nil, errors.New("chatgpt: model catalog returned 304 without a local cache")
		}
		cache.FetchedAt = p.now().UTC()
		cache.ClientVersion = p.clientVersion
		_ = p.saveCatalogCache(status.AccountID, cache)
		return p.acceptRecords(cache.Models), nil
	}
	entry := catalogCache{Version: catalogCacheVersion, BackendOrigin: p.backendOrigin(), AccountID: status.AccountID, FetchedAt: p.now().UTC(), ETag: etag, ClientVersion: p.clientVersion, Models: records}
	_ = p.saveCatalogCache(status.AccountID, entry)
	return p.acceptRecords(records), nil
}

func (p *Provider) storeCredential() (auth.Credential, bool) {
	if p.store == nil {
		return auth.Credential{}, false
	}
	return p.store.Get(ProviderID)
}

func (p *Provider) fetchCatalog(ctx context.Context, cred auth.Credential, etag string, allowRefresh bool) ([]modelRecord, string, bool, error) {
	status, err := CheckAuth(cred)
	if err != nil {
		return nil, "", false, err
	}
	parentCtx := ctx
	requestCtx, cancel := context.WithTimeout(parentCtx, catalogTimeout)
	defer cancel()
	base := strings.TrimRight(p.baseURL, "/")
	if strings.HasSuffix(base, "/codex/responses") {
		base = strings.TrimSuffix(base, "/responses")
	}
	endpoint := base + "/codex/models"
	if strings.HasSuffix(base, "/codex") {
		endpoint = base + "/models"
	}
	u, _ := url.Parse(endpoint)
	q := u.Query()
	q.Set("client_version", p.clientVersion)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+cred.Access)
	req.Header.Set("chatgpt-account-id", status.AccountID)
	req.Header.Set("originator", "snow")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := redirectSafeClient(p.client).Do(req)
	if err != nil {
		return nil, "", false, sanitizeNetworkError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && allowRefresh {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		fresh, refreshErr := p.refreshRejected(parentCtx, cred)
		if refreshErr != nil {
			return nil, "", false, refreshErr
		}
		return p.fetchCatalog(parentCtx, fresh, etag, false)
	}
	if resp.StatusCode == http.StatusNotModified {
		return nil, resp.Header.Get("ETag"), true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", false, fmt.Errorf("chatgpt: model catalog HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return nil, "", false, errors.New("chatgpt: read model catalog failed")
	}
	if len(body) > maxCatalogBytes {
		return nil, "", false, errors.New("chatgpt: model catalog exceeded size limit")
	}
	var payload modelsResponse
	if json.Unmarshal(body, &payload) != nil || len(payload.Models) == 0 {
		return nil, "", false, errors.New("chatgpt: malformed model catalog")
	}
	return payload.Models, resp.Header.Get("ETag"), false, nil
}

func (p *Provider) acceptRecords(records []modelRecord) []protocol.Model {
	models := mapModelRecords(records)
	// The authenticated response is account-scoped. Do not add bundled models
	// missing from it: the Codex backend can reject a model for one ChatGPT
	// account even when another local OpenCode/Pi account can invoke it.
	p.modelsMu.Lock()
	p.models = slices.Clone(records)
	p.modelsMu.Unlock()
	return models
}

func mapModelRecords(records []modelRecord) []protocol.Model {
	type ranked struct {
		record modelRecord
		order  int
	}
	var visible []ranked
	for i, r := range records {
		if r.Slug != "" && (r.Visibility == "list" || r.Visibility == "") {
			visible = append(visible, ranked{r, i})
		}
	}
	slices.SortStableFunc(visible, func(a, b ranked) int {
		if a.record.Priority == b.record.Priority {
			return cmp.Compare(a.order, b.order)
		}
		if a.record.Priority == 0 {
			return 1
		}
		if b.record.Priority == 0 {
			return -1
		}
		return cmp.Compare(a.record.Priority, b.record.Priority)
	})
	out := make([]protocol.Model, 0, len(visible))
	for _, item := range visible {
		r := item.record
		ctx := r.ContextWindow
		if r.EffectiveContextWindowPercent > 0 && r.EffectiveContextWindowPercent < 100 {
			ctx = ctx * r.EffectiveContextWindowPercent / 100
		}
		levels := make([]protocol.ThinkingLevel, 0, len(r.SupportedReasoningLevels))
		for _, level := range r.SupportedReasoningLevels {
			if parsed, ok := inferenceThinkingLevel(level.Effort); ok {
				levels = appendUniqueThinking(levels, parsed)
			}
		}
		m := protocol.Model{Provider: ProviderID, ID: r.Slug, DisplayName: r.DisplayName, Description: r.Description, ContextWindow: ctx, MaxContextWindow: r.MaxContextWindow, SupportsTools: true, SupportsThinking: len(levels) > 0, ThinkingLevels: levels, SupportsVerbosity: r.SupportVerbosity}
		if r.SupportsReasoningSummaryParameter != nil {
			supported := *r.SupportsReasoningSummaryParameter
			m.SupportsReasoningSummary = &supported
		}
		for _, modality := range r.InputModalities {
			if modality == "image" {
				m.SupportsVision = true
			}
		}
		if defaultThinking, ok := inferenceThinkingLevel(r.DefaultReasoningLevel); ok {
			m.DefaultThinking = defaultThinking
		}
		if r.Upgrade != nil {
			m.Upgrade = &protocol.ModelUpgrade{Model: r.Upgrade.Model, Message: r.Upgrade.MigrationMarkdown}
		}
		out = append(out, m)
	}
	return out
}

// inferenceThinkingLevel intersects catalog UI presets with values accepted by
// the Codex Responses reasoning.effort field. The catalog's "ultra" preset is a
// Codex host mode that enables proactive multi-agent behavior; sending it as a
// reasoning effort is rejected by the inference backend.
func inferenceThinkingLevel(value string) (protocol.ThinkingLevel, bool) {
	level, err := protocol.ParseThinkingLevel(value)
	if err != nil || level == protocol.ThinkingOff || level == protocol.ThinkingUltra {
		return protocol.ThinkingOff, false
	}
	return level, true
}

func appendUniqueThinking(levels []protocol.ThinkingLevel, level protocol.ThinkingLevel) []protocol.ThinkingLevel {
	if slices.Contains(levels, level) {
		return levels
	}
	return append(levels, level)
}

func Models() []protocol.Model { return mapModelRecords(bundledModels()) }
func bundledModels() []modelRecord {
	return []modelRecord{
		{Slug: "gpt-5.6-sol", DisplayName: "GPT-5.6-Sol", Description: "Latest frontier agentic coding model.", Visibility: "list", Priority: 1, SupportVerbosity: true, ContextWindow: 272000, MaxContextWindow: 272000, EffectiveContextWindowPercent: 95, InputModalities: []string{"text", "image"}},
		{Slug: "gpt-5.6-terra", DisplayName: "GPT-5.6-Terra", Description: "Balanced agentic coding model for everyday work.", Visibility: "list", Priority: 2, SupportVerbosity: true, ContextWindow: 272000, MaxContextWindow: 272000, EffectiveContextWindowPercent: 95, InputModalities: []string{"text", "image"}},
		{Slug: "gpt-5.6-luna", DisplayName: "GPT-5.6-Luna", Description: "Fast and affordable agentic coding model.", Visibility: "list", Priority: 3, SupportVerbosity: true, ContextWindow: 272000, MaxContextWindow: 272000, EffectiveContextWindowPercent: 95, InputModalities: []string{"text", "image"}},
		{Slug: "gpt-5.3-codex-spark", DisplayName: "GPT-5.3-Codex-Spark", Description: "Ultra-fast coding model.", Visibility: "list", Priority: 26, SupportVerbosity: true, ContextWindow: 128000, MaxContextWindow: 128000, EffectiveContextWindowPercent: 95, InputModalities: []string{"text"}},
	}
}

func (p *Provider) backendOrigin() string {
	u, err := url.Parse(p.baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func (p *Provider) catalogCachePath(accountID string) string {
	sum := sha256.Sum256([]byte(p.backendOrigin() + "\x00" + accountID))
	return filepath.Join(p.cacheRoot, hex.EncodeToString(sum[:])+".json")
}
func (p *Provider) loadCatalogCache(accountID string) (catalogCache, bool) {
	if p.cacheRoot == "" || accountID == "" {
		return catalogCache{}, false
	}
	data, err := os.ReadFile(p.catalogCachePath(accountID))
	if err != nil || len(data) > maxCatalogBytes {
		return catalogCache{}, false
	}
	var c catalogCache
	if json.Unmarshal(data, &c) != nil || len(c.Models) == 0 ||
		c.Version != catalogCacheVersion || c.BackendOrigin != p.backendOrigin() ||
		c.AccountID != accountID || c.ClientVersion != p.clientVersion || c.FetchedAt.IsZero() ||
		c.FetchedAt.After(p.now().Add(time.Minute)) {
		return catalogCache{}, false
	}
	return c, true
}
func (p *Provider) saveCatalogCache(accountID string, c catalogCache) error {
	if p.cacheRoot == "" || accountID == "" {
		return nil
	}
	if err := os.MkdirAll(p.cacheRoot, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(p.cacheRoot, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(p.cacheRoot, ".models-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, p.catalogCachePath(accountID)); err != nil {
		return err
	}
	return os.Chmod(p.catalogCachePath(accountID), 0o600)
}

var _ providerpkg.Transport = (*Provider)(nil)
