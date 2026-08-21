// Package opencodego implements the OpenCode Go provider adapter: an
// OpenAI-compatible Chat Completions streaming client with bearer-token auth.
//
// The provider id is "opencode-go" (matching the auth.json key and the
// OPENCODE_API_KEY environment variable convention).
package opencodego

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/modelsdev"
	"github.com/elmissouri16/snow-core/pkg/protocol"
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
const DefaultCatalogURL = modelsdev.DefaultURL

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
