// Package opencodezen implements OpenCode Zen's promotional free-model API.
// Authentication is optional: when no key is resolved requests omit the
// Authorization header and use Zen's anonymous allowance.
package opencodezen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/modelsdev"
	"github.com/elmissouri16/snow-core/internal/provider/opencodego"
	"github.com/elmissouri16/snow-core/internal/provider/responsesapi"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	ProviderID        = "opencode-zen"
	EnvAPIKey         = "OPENCODE_API_KEY"
	DefaultBaseURL    = "https://opencode.ai/zen/v1"
	DefaultCatalogURL = modelsdev.DefaultURL
	DefaultModelID    = "big-pickle"
	maxErrorBodySize  = 1000
)

type transportKind uint8

const (
	transportChat transportKind = iota
	transportResponses
)

// Config controls the OpenCode Zen adapter.
type Config struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	HTTPClient   *http.Client
	// CatalogURL overrides the public models.dev metadata endpoint. When
	// BaseURL is customized and CatalogURL is empty, metadata enrichment is
	// disabled so OpenCode metadata is not applied to an unrelated compatible
	// gateway.
	CatalogURL        string
	DiscoveryTimeout  time.Duration
	StreamIdleTimeout time.Duration
	CacheRoot         string
}

// Provider implements provider.Transport for OpenCode Zen.
type Provider struct {
	baseURL           string
	apiKey            string
	defaultModel      string
	client            *http.Client
	catalogURL        string
	discoveryTimeout  time.Duration
	streamIdleTimeout time.Duration
	cacheRoot         string

	catalogMu    sync.Mutex
	cachedModels []protocol.Model
	cachedAt     time.Time
}

func New(cfg Config) (*Provider, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("opencode-zen: base URL must be an absolute HTTP(S) URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("opencode-zen: base URL must not contain userinfo, query, or fragment")
	}
	defaultModel := strings.TrimSpace(cfg.DefaultModel)
	if defaultModel == "" {
		defaultModel = DefaultModelID
	}
	if _, ok := freeModelByID(defaultModel); !ok {
		return nil, fmt.Errorf("opencode-zen: default model %q is not in the maintained free catalog", defaultModel)
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
	idleTimeout := cfg.StreamIdleTimeout
	if idleTimeout == 0 {
		idleTimeout = providerpkg.DefaultStreamIdleTimeout
	} else if idleTimeout < 0 {
		idleTimeout = -1
	}
	return &Provider{
		baseURL: base, apiKey: strings.TrimSpace(cfg.APIKey), defaultModel: defaultModel, client: client,
		catalogURL: catalogURL, discoveryTimeout: discoveryTimeout, streamIdleTimeout: idleTimeout,
		cacheRoot: strings.TrimSpace(cfg.CacheRoot),
	}, nil
}

func (p *Provider) ID() string { return ProviderID }

func (p *Provider) DefaultModel() protocol.Model {
	spec, _ := freeModelByID(p.defaultModel)
	return spec.Model.Clone()
}

// ModelCatalogAuthoritative rejects model IDs outside the maintained free
// catalog instead of allowing accidental paid Zen requests.
func (*Provider) ModelCatalogAuthoritative() bool { return true }

// RejectUnknownModels prevents explicit custom IDs from bypassing the free
// catalog and reaching paid Zen routes.
func (*Provider) RejectUnknownModels() bool { return true }

func (p *Provider) resolveKey(credential auth.Credential) string {
	if key := strings.TrimSpace(credential.Key); key != "" {
		return key
	}
	return p.apiKey
}

func (p *Provider) Chat(ctx context.Context, credential auth.Credential, request protocol.ChatRequest) (protocol.EventStream, error) {
	spec, ok := freeModelByID(request.Model.ID)
	if !ok {
		return eventErrorStream(fmt.Errorf("opencode-zen: model %q is not in the maintained free catalog", request.Model.ID)), nil
	}
	key := p.resolveKey(credential)
	stream, err := p.chatAttempt(ctx, key, spec.Transport, request)
	if err != nil {
		return eventErrorStream(err), nil
	}
	return &validatedResponseStream{stream: stream, model: request.Model.ID}, nil
}

func (p *Provider) chatAttempt(ctx context.Context, key string, transport transportKind, request protocol.ChatRequest) (protocol.EventStream, error) {
	if transport == transportChat {
		compatible, err := opencodego.New(opencodego.Config{
			BaseURL: p.baseURL, APIKey: key, HTTPClient: p.client,
			DefaultModel: request.Model.ID, ProviderID: ProviderID,
			AllowAnonymous: true, DisableEnvAPIKey: true, StreamIdleTimeout: p.streamIdleTimeout,
		})
		if err != nil {
			return nil, fmt.Errorf("opencode-zen: initialize Chat Completions: %w", err)
		}
		return compatible.Chat(ctx, auth.Credential{Type: auth.CredentialAPIKey, Key: key}, request)
	}
	body, err := responsesapi.BuildRequest(request, responsesapi.RequestOptions{
		ProviderID:                ProviderID,
		IncludeEncryptedReasoning: request.Model.SupportsThinking && protocol.NormalizeThinkingLevel(request.Thinking) != protocol.ThinkingOff,
	})
	if err != nil {
		return nil, fmt.Errorf("opencode-zen: build Responses request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("opencode-zen: create Responses request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		message := providerpkg.RedactSecrets("opencode-zen: Responses request failed: "+err.Error(), key)
		cause := &providerpkg.CauseError{Message: message, Cause: err}
		return nil, &providerpkg.AdvisedError{Err: cause, Advice: providerpkg.RetryAdvice{Kind: providerpkg.RetryTransient}}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		message := providerpkg.RedactSecrets(strings.TrimSpace(string(snippet)), key)
		if resp.StatusCode == http.StatusPaymentRequired {
			return nil, &providerpkg.LimitError{Provider: ProviderID, Status: resp.StatusCode, Message: message}
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, errors.New("opencode-zen: credential rejected (HTTP 401)")
		}
		cause := fmt.Errorf("opencode-zen: HTTP %d: %s", resp.StatusCode, truncate(message, 500))
		if advice, retryable := providerpkg.HTTPRetryAdvice(resp.StatusCode, resp.Header, time.Now(), 24*time.Hour); retryable {
			if advice.Kind == providerpkg.RetryRateLimit {
				return nil, &providerpkg.RateLimitError{Provider: ProviderID, Status: resp.StatusCode, Message: message, RetryAfter: advice.RetryAfter}
			}
			return nil, &providerpkg.AdvisedError{Err: cause, Advice: advice}
		}
		return nil, cause
	}
	mediaType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if !strings.EqualFold(mediaType, "text/event-stream") {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return nil, fmt.Errorf("opencode-zen: incompatible response content type %q: %s", mediaType, truncate(providerpkg.RedactSecrets(strings.TrimSpace(string(snippet)), key), 500))
	}
	return responsesapi.NewStreamWithIdleTimeout(ctx, resp, ProviderID, p.streamIdleTimeout, key), nil
}

// validatedResponseStream prevents successful-but-empty Zen completions from
// disappearing in the UI. Some upstream routes can terminate cleanly without
// text or a tool call; that is not a usable assistant response and must remain
// a visible diagnostic rather than an apparently successful blank turn.
type validatedResponseStream struct {
	stream   protocol.EventStream
	model    string
	hasText  bool
	hasTool  bool
	thinking bool
}

func (s *validatedResponseStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	event, err := s.stream.Next(ctx)
	if err != nil {
		return event, err
	}
	switch event.Type {
	case protocol.EvStreamTextDelta:
		if strings.TrimSpace(event.Text) != "" {
			s.hasText = true
		}
	case protocol.EvStreamThinkingDelta:
		if strings.TrimSpace(event.Text) != "" {
			s.thinking = true
		}
	case protocol.EvStreamToolCallDone:
		s.hasTool = true
	case protocol.EvStreamDone:
		if !s.hasText && !s.hasTool {
			message := fmt.Sprintf("opencode-zen: model %q returned an empty completion", s.model)
			if event.StopReason == protocol.StopLength {
				message = fmt.Sprintf("opencode-zen: model %q reached its output limit before producing an answer", s.model)
			} else if event.StopReason == protocol.StopToolUse {
				message = fmt.Sprintf("opencode-zen: model %q reported tool use without a tool call", s.model)
			} else if s.thinking {
				message += " after reasoning without producing a final answer"
			}
			message += "; retry the prompt or switch Zen models"
			return protocol.StreamEvent{Type: protocol.EvStreamError, Err: errors.New(message)}, nil
		}
	}
	return event, nil
}

func (s *validatedResponseStream) Close() error { return s.stream.Close() }

type singleErrorStream struct {
	err  error
	once bool
}

func eventErrorStream(err error) protocol.EventStream { return &singleErrorStream{err: err} }
func (s *singleErrorStream) Next(context.Context) (protocol.StreamEvent, error) {
	if s.once {
		return protocol.StreamEvent{}, io.EOF
	}
	s.once = true
	return protocol.StreamEvent{Type: protocol.EvStreamError, Err: s.err}, nil
}
func (*singleErrorStream) Close() error { return nil }

func truncate(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "…"
}
