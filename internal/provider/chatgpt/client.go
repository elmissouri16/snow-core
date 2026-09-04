package chatgpt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
)

const (
	OAuthClientID               = "app_EMoamEEZ73f0CkXaXp7hrann"
	CatalogCompatibilityVersion = "0.147.0"
	refreshSkew                 = 5 * time.Minute
	maxAuthResponseBytes        = 1 << 20
)

var (
	ErrLoginRequired = errors.New("chatgpt: login required")
	ErrRefreshFailed = errors.New("chatgpt: token refresh temporarily failed")
)

type Config struct {
	BaseURL           string
	AuthBaseURL       string
	HTTPClient        *http.Client
	Store             auth.Store
	CacheRoot         string
	ClientVersion     string
	StreamIdleTimeout time.Duration
	Now               func() time.Time
}

type Provider struct {
	baseURL           string
	authBaseURL       string
	client            *http.Client
	store             auth.Store
	cacheRoot         string
	clientVersion     string
	streamIdleTimeout time.Duration
	now               func() time.Time
	modelsMu          sync.RWMutex
	models            []modelRecord
	authRefreshMu     sync.RWMutex
	authRefresh       func(context.Context, auth.Credential) (auth.Credential, error)
}

func New(configs ...Config) *Provider {
	var cfg Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = BackendBaseURL
	}
	authBase := strings.TrimRight(cfg.AuthBaseURL, "/")
	if authBase == "" {
		authBase = AuthBaseURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	version := cfg.ClientVersion
	if version == "" {
		version = CatalogCompatibilityVersion
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	streamIdleTimeout := cfg.StreamIdleTimeout
	if streamIdleTimeout == 0 {
		streamIdleTimeout = providerpkg.DefaultStreamIdleTimeout
	} else if streamIdleTimeout < 0 {
		streamIdleTimeout = -1
	}
	return &Provider{baseURL: base, authBaseURL: authBase, client: client, store: cfg.Store, cacheRoot: cfg.CacheRoot, clientVersion: version, streamIdleTimeout: streamIdleTimeout, now: now}
}

func (p *Provider) ID() string { return ProviderID }

// SetAuthRefresh binds the generic auth service used by authenticated runtimes.
// Direct transport tests and compatibility callers may leave it unset.
func (p *Provider) SetAuthRefresh(refresh func(context.Context, auth.Credential) (auth.Credential, error)) {
	p.authRefreshMu.Lock()
	p.authRefresh = refresh
	p.authRefreshMu.Unlock()
}

func (p *Provider) refreshRejected(ctx context.Context, rejected auth.Credential) (auth.Credential, error) {
	p.authRefreshMu.RLock()
	refresh := p.authRefresh
	p.authRefreshMu.RUnlock()
	if refresh != nil {
		return refresh(ctx, rejected)
	}
	return p.resolve(ctx, rejected, true)
}

func (p *Provider) resolve(ctx context.Context, supplied auth.Credential, force bool) (auth.Credential, error) {
	if p.store == nil {
		status, err := CheckAuth(supplied)
		if err != nil {
			return supplied, err
		}
		if !force && !needsRefresh(status, p.now()) {
			return supplied, nil
		}
		return supplied, fmt.Errorf("%w: OAuth token needs refresh but no persistent credential store is configured", ErrLoginRequired)
	}
	if coordinator, ok := p.store.(auth.RefreshLockStore); ok {
		return p.resolveCoordinated(ctx, supplied, force, coordinator)
	}
	resolved, _, err := p.store.Update(ProviderID, func(current auth.Credential, exists bool) (auth.Credential, bool, error) {
		if !exists {
			current = supplied
		} else if force && credentialRotatedSince(current, supplied) {
			// Another request/process already rotated the credential after the
			// rejected request was created. Reuse the newer stored value rather
			// than spending (and potentially invalidating) its refresh token.
			return current, false, nil
		}
		status, err := CheckAuth(current)
		if err != nil {
			return current, false, err
		}
		if !force && !needsRefresh(status, p.now()) {
			return current, false, nil
		}
		if strings.TrimSpace(current.Refresh) == "" {
			return current, false, fmt.Errorf("%w: OAuth refresh token is missing; sign in again", ErrLoginRequired)
		}
		next, err := p.refresh(ctx, current)
		if err != nil {
			return current, false, err
		}
		return next, true, nil
	})
	return resolved, err
}

func (p *Provider) resolveCoordinated(ctx context.Context, supplied auth.Credential, force bool, coordinator auth.RefreshLockStore) (auth.Credential, error) {
	var resolved auth.Credential
	err := coordinator.WithRefreshLock(ProviderID, func() error {
		current, exists, err := p.store.Update(ProviderID, func(current auth.Credential, exists bool) (auth.Credential, bool, error) {
			return current, false, nil
		})
		if err != nil {
			return err
		}
		if !exists {
			current = supplied
		} else if force && credentialRotatedSince(current, supplied) {
			resolved = current
			return nil
		}
		status, err := CheckAuth(current)
		if err != nil {
			return err
		}
		if !force && !needsRefresh(status, p.now()) {
			resolved = current
			return nil
		}
		if strings.TrimSpace(current.Refresh) == "" {
			return fmt.Errorf("%w: OAuth refresh token is missing; sign in again", ErrLoginRequired)
		}
		next, err := p.refresh(ctx, current)
		if err != nil {
			return err
		}
		var resolvedExists bool
		resolved, resolvedExists, err = p.store.Update(ProviderID, func(latest auth.Credential, latestExists bool) (auth.Credential, bool, error) {
			if exists && !latestExists {
				// Logout won while the refresh was in flight. Never resurrect it.
				return latest, false, nil
			}
			if latestExists && !sameCredential(latest, current) {
				// A concurrent login/import replaced metadata or tokens while the
				// refresh was in flight. Preserve and reuse that newer value.
				return latest, false, nil
			}
			return next, true, nil
		})
		if err == nil && !resolvedExists {
			return fmt.Errorf("%w: credential was removed during refresh", ErrLoginRequired)
		}
		return err
	})
	return resolved, err
}

func needsRefresh(status AuthStatus, now time.Time) bool {
	return !status.ExpiresAt.IsZero() && !status.ExpiresAt.After(now.Add(refreshSkew))
}

func credentialRotatedSince(current, supplied auth.Credential) bool {
	return supplied.Access != "" && (current.Access != supplied.Access || current.Refresh != supplied.Refresh)
}

func sameCredential(left, right auth.Credential) bool {
	return left.Type == right.Type && left.Key == right.Key && left.Access == right.Access && left.Refresh == right.Refresh && left.Expires == right.Expires && left.AccountID == right.AccountID && reflect.DeepEqual(left.Extra, right.Extra)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
}

func (p *Provider) refresh(ctx context.Context, current auth.Credential) (auth.Credential, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	form := url.Values{
		"client_id": {OAuthClientID}, "grant_type": {"refresh_token"}, "refresh_token": {current.Refresh},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.authBaseURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return current, fmt.Errorf("chatgpt: create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tokens tokenResponse
	if err := doBoundedJSON(p.client, req, &tokens); err != nil {
		if endpointErr, ok := errors.AsType[*oauthEndpointError](err); ok && endpointErr.permanentRefreshFailure() {
			return current, fmt.Errorf("%w: OAuth refresh token was rejected; sign in again", ErrLoginRequired)
		}
		return current, fmt.Errorf("%w: %v", ErrRefreshFailed, err)
	}
	return credentialFromTokens(tokens, current, p.now())
}

func credentialFromTokens(tokens tokenResponse, previous auth.Credential, now time.Time) (auth.Credential, error) {
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return previous, errors.New("chatgpt: token response has no access token")
	}
	next := previous
	next.Provider = ProviderID
	next.Type = auth.CredentialOAuth
	next.Access = tokens.AccessToken
	if tokens.RefreshToken != "" {
		next.Refresh = tokens.RefreshToken
	}
	if tokens.ExpiresIn > 0 {
		next.Expires = now.Add(time.Duration(tokens.ExpiresIn) * time.Second).Unix()
	} else if claims, ok := decodeJWTClaims(tokens.AccessToken); ok {
		if exp, ok := numberClaim(claims, "exp"); ok {
			next.Expires = exp
		}
	}
	status, err := CheckAuth(next)
	if err != nil {
		return previous, err
	}
	accountID, planType := status.AccountID, status.PlanType
	if claims, ok := decodeJWTClaims(tokens.IDToken); ok {
		if accountID == "" {
			accountID = accountIDFromClaims(claims)
		}
		if planType == "" {
			planType = planTypeFromClaims(claims)
		}
	}
	if accountID != "" {
		next.AccountID = accountID
	}
	if planType != "" {
		if next.Extra == nil {
			next.Extra = make(map[string]any)
		}
		next.Extra["plan_type"] = planType
	}
	// id_token is intentionally consumed only for claims and is never stored.
	if next.AccountID == "" {
		return previous, errors.New("chatgpt: OAuth token has no ChatGPT account ID")
	}
	return next, nil
}

func doBoundedJSON(client *http.Client, req *http.Request, dst any) error {
	resp, err := redirectSafeClient(client).Do(req)
	if err != nil {
		return sanitizeNetworkError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAuthResponseBytes+1))
	if err != nil {
		return errors.New("failed to read OAuth response")
	}
	if len(body) > maxAuthResponseBytes {
		return errors.New("OAuth response exceeded size limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// OAuth servers sometimes echo request fields in descriptions. Retain
		// only the standardized error code for classification; descriptions and
		// request fields are deliberately discarded.
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &payload)
		return &oauthEndpointError{status: resp.StatusCode, code: safeOAuthErrorCode(payload.Error)}
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return errors.New("OAuth endpoint returned malformed JSON")
	}
	return nil
}

type oauthEndpointError struct {
	status int
	code   string
}

func (e *oauthEndpointError) Error() string {
	return fmt.Sprintf("OAuth endpoint returned HTTP %d", e.status)
}

func (e *oauthEndpointError) permanentRefreshFailure() bool {
	if e.status != http.StatusBadRequest && e.status != http.StatusUnauthorized {
		return false
	}
	switch e.code {
	case "invalid_grant", "invalid_request", "invalid_client", "unauthorized_client":
		return true
	}
	return e.status == http.StatusUnauthorized
}

func safeOAuthErrorCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, r := range value {
		if (r < 'a' || r > 'z') && r != '_' {
			return ""
		}
	}
	return value
}

type noUserinfoTransport struct{ base http.RoundTripper }

func (t noUserinfoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.User != nil {
		return nil, errors.New("request URL userinfo rejected")
	}
	return t.base.RoundTrip(req)
}

func redirectSafeClient(base *http.Client) *http.Client {
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

func sanitizeNetworkError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New("network request failed")
}
