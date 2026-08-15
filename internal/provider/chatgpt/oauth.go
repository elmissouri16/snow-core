package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/snow-core/snow/internal/auth"
)

type LoginMethod string

const (
	LoginBrowser LoginMethod = "browser"
	LoginDevice  LoginMethod = "device"
)

type LoginProgress struct{ Kind, Message, URL, UserCode string }

type LoginOptions struct {
	Method        LoginMethod
	Store         auth.Store
	HTTPClient    *http.Client
	AuthBaseURL   string
	Now           func() time.Time
	OpenBrowser   func(context.Context, string) error
	PasteCallback func(context.Context) (string, error)
	Progress      func(LoginProgress)
	// AllowedWorkspaceIDs restricts login to specific ChatGPT account/workspace
	// IDs using the official Codex allowed_workspace_id OAuth parameter and a
	// post-exchange claim check. Empty allows any account.
	AllowedWorkspaceIDs []string
	BrowserTimeout      time.Duration // tests/embedders; zero uses 10 minutes
	DeviceTimeout       time.Duration // tests/embedders; zero uses 15 minutes
}

type loginClient struct {
	opts   LoginOptions
	base   string
	client *http.Client
	now    func() time.Time
}

func Login(ctx context.Context, opts LoginOptions) (AuthStatus, error) {
	if opts.Store == nil {
		return AuthStatus{}, errors.New("chatgpt: login requires a credential store")
	}
	cred, err := LoginCredential(ctx, opts)
	if err != nil {
		return AuthStatus{}, err
	}
	if err = opts.Store.Put(ProviderID, cred); err != nil {
		return AuthStatus{}, fmt.Errorf("chatgpt: persist OAuth credential: %w", err)
	}
	return CheckAuth(cred)
}

// LoginCredential runs a ChatGPT OAuth flow without persisting its result.
// Generic auth.Service callers use this to keep storage and provider wire logic
// separated; Login remains as a compatibility wrapper for existing embedders.
func LoginCredential(ctx context.Context, opts LoginOptions) (auth.Credential, error) {
	base := strings.TrimRight(opts.AuthBaseURL, "/")
	if base == "" {
		base = AuthBaseURL
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	c := &loginClient{opts: opts, base: base, client: client, now: now}
	var cred auth.Credential
	var err error
	if opts.Method == LoginDevice {
		cred, err = c.device(ctx)
	} else {
		cred, err = c.browser(ctx)
	}
	if err != nil {
		return auth.Credential{}, err
	}
	if err = ensureAllowedWorkspace(cred, opts.AllowedWorkspaceIDs); err != nil {
		return auth.Credential{}, err
	}
	return cred, nil
}

func normalizedWorkspaceIDs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func ensureAllowedWorkspace(cred auth.Credential, allowed []string) error {
	allowed = normalizedWorkspaceIDs(allowed)
	if len(allowed) == 0 {
		return nil
	}
	status, err := CheckAuth(cred)
	if err != nil {
		return err
	}
	for _, expected := range allowed {
		if status.AccountID == expected {
			return nil
		}
	}
	return fmt.Errorf("chatgpt: selected account %q is not the requested workspace; login was not saved", status.AccountID)
}

func (c *loginClient) progress(p LoginProgress) {
	if c.opts.Progress != nil {
		c.opts.Progress(p)
	}
}

func randomURLString(bytesN int) (string, error) {
	b := make([]byte, bytesN)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (c *loginClient) browser(ctx context.Context) (auth.Credential, error) {
	state, err := randomURLString(32)
	if err != nil {
		return auth.Credential{}, err
	}
	verifier, err := randomURLString(64)
	if err != nil {
		return auth.Credential{}, err
	}
	listener, listenErr := net.Listen("tcp", "127.0.0.1:1455")
	if listenErr != nil && c.opts.PasteCallback == nil {
		return auth.Credential{}, errors.New("chatgpt: OAuth callback port 1455 is unavailable; use device-code login")
	}
	if listener != nil {
		defer listener.Close()
	}
	redirectURI := "http://localhost:1455/auth/callback"
	callback := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if len(r.URL.RawQuery) > 8192 {
			http.Error(w, "query too large", http.StatusRequestURITooLong)
			return
		}
		result, ok := parseCallbackURL(r.URL.String(), state)
		if !ok {
			http.Error(w, "invalid OAuth state", http.StatusBadRequest)
			return
		}
		select {
		case callback <- result:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><body>Snow login complete. You may close this window.</body></html>")
	})
	var server *http.Server
	if listener != nil {
		server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = server.Serve(listener) }()
		defer server.Shutdown(context.Background())
	} else {
		c.progress(LoginProgress{Kind: "paste_callback", Message: "Callback port unavailable; paste the complete callback URL to finish sign-in"})
	}
	authURL, err := url.Parse(c.base + "/oauth/authorize")
	if err != nil {
		return auth.Credential{}, err
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", OAuthClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "openid profile email offline_access api.connectors.read api.connectors.invoke")
	q.Set("code_challenge", pkceChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", "snow")
	if allowed := normalizedWorkspaceIDs(c.opts.AllowedWorkspaceIDs); len(allowed) > 0 {
		q.Set("allowed_workspace_id", strings.Join(allowed, ","))
	}
	authURL.RawQuery = q.Encode()
	c.progress(LoginProgress{Kind: "authorization_url", URL: authURL.String(), Message: "Open this URL to sign in with ChatGPT"})
	if c.opts.OpenBrowser != nil {
		if err := c.opts.OpenBrowser(ctx, authURL.String()); err != nil {
			c.progress(LoginProgress{Kind: "browser_error", Message: "Could not open the browser; open the URL manually"})
		}
	}
	if c.opts.PasteCallback != nil {
		go func() {
			raw, err := c.opts.PasteCallback(ctx)
			if err != nil {
				return
			}
			u, err := url.Parse(strings.TrimSpace(raw))
			if err != nil {
				return
			}
			result, ok := parseCallbackURL(u.String(), state)
			if ok {
				select {
				case callback <- result:
				case <-ctx.Done():
				}
			}
		}()
	}
	timeout := c.opts.BrowserTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return auth.Credential{}, ctx.Err()
	case <-timer.C:
		return auth.Credential{}, errors.New("chatgpt: browser login timed out")
	case result := <-callback:
		if result.err != "" {
			return auth.Credential{}, fmt.Errorf("chatgpt: OAuth authorization failed: %s", result.err)
		}
		return c.exchange(ctx, result.code, verifier, redirectURI)
	}
}

type callbackResult struct{ code, err string }

func parseCallbackURL(raw, state string) (callbackResult, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Path != "/auth/callback" || u.Query().Get("state") != state {
		return callbackResult{}, false
	}
	if e := u.Query().Get("error"); e != "" {
		return callbackResult{err: safeText(e)}, true
	}
	code := u.Query().Get("code")
	if code == "" {
		return callbackResult{err: "authorization code missing"}, true
	}
	return callbackResult{code: code}, true
}

func (c *loginClient) exchange(ctx context.Context, code, verifier, redirectURI string) (auth.Credential, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {OAuthClientID}, "code": {code}, "redirect_uri": {redirectURI}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return auth.Credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tokens tokenResponse
	if err = doBoundedJSON(c.client, req, &tokens); err != nil {
		return auth.Credential{}, fmt.Errorf("chatgpt: token exchange failed: %w", err)
	}
	return credentialFromTokens(tokens, auth.Credential{}, c.now())
}

type flexibleSeconds int64

const maxDevicePollInterval = 60 * time.Second

func (s *flexibleSeconds) UnmarshalJSON(data []byte) error {
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		value, err := number.Int64()
		if err != nil {
			return errors.New("interval must be an integer")
		}
		*s = flexibleSeconds(value)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return errors.New("interval must be a JSON string or number")
	}
	value, err := json.Number(strings.TrimSpace(text)).Int64()
	if err != nil {
		return errors.New("interval string must contain an integer")
	}
	*s = flexibleSeconds(value)
	return nil
}

type deviceCodeResponse struct {
	DeviceAuthID    string          `json:"device_auth_id"`
	UserCode        string          `json:"user_code"`
	VerificationURI string          `json:"verification_uri"`
	Interval        flexibleSeconds `json:"interval"`
}
type deviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

func (c *loginClient) device(ctx context.Context) (auth.Credential, error) {
	payload, _ := json.Marshal(map[string]string{"client_id": OAuthClientID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/accounts/deviceauth/usercode", strings.NewReader(string(payload)))
	if err != nil {
		return auth.Credential{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	var code deviceCodeResponse
	if err = doBoundedJSON(c.client, req, &code); err != nil {
		return auth.Credential{}, fmt.Errorf("chatgpt: device code request failed: %w", err)
	}
	if code.DeviceAuthID == "" || code.UserCode == "" {
		return auth.Credential{}, errors.New("chatgpt: malformed device code response")
	}
	if code.VerificationURI == "" {
		code.VerificationURI = c.base + "/codex/device"
	}
	interval := devicePollInterval(code.Interval)
	c.progress(LoginProgress{Kind: "device_code", URL: code.VerificationURI, UserCode: code.UserCode, Message: "Enter this code to sign in with ChatGPT"})
	if c.opts.OpenBrowser != nil {
		_ = c.opts.OpenBrowser(ctx, code.VerificationURI)
	}
	timeout := c.opts.DeviceTimeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return auth.Credential{}, ctx.Err()
		case <-deadline.C:
			timer.Stop()
			return auth.Credential{}, errors.New("chatgpt: device login timed out after 15 minutes")
		case <-timer.C:
		}
		result, pending, slow, err := c.pollDevice(ctx, code)
		if err != nil {
			return auth.Credential{}, err
		}
		if slow {
			interval = min(interval+5*time.Second, maxDevicePollInterval)
		}
		if pending {
			continue
		}
		return c.exchange(ctx, result.AuthorizationCode, result.CodeVerifier, c.base+"/deviceauth/callback")
	}
}

func devicePollInterval(seconds flexibleSeconds) time.Duration {
	if seconds < 1 {
		seconds = 5
	}
	if seconds > flexibleSeconds(maxDevicePollInterval/time.Second) {
		return maxDevicePollInterval
	}
	return time.Duration(seconds) * time.Second
}

func (c *loginClient) pollDevice(ctx context.Context, code deviceCodeResponse) (deviceTokenResponse, bool, bool, error) {
	payload, _ := json.Marshal(map[string]string{"device_auth_id": code.DeviceAuthID, "user_code": code.UserCode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/accounts/deviceauth/token", strings.NewReader(string(payload)))
	if err != nil {
		return deviceTokenResponse{}, false, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := redirectSafeClient(c.client).Do(req)
	if err != nil {
		return deviceTokenResponse{}, false, false, sanitizeNetworkError(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAuthResponseBytes+1))
	if len(body) > maxAuthResponseBytes {
		return deviceTokenResponse{}, false, false, errors.New("chatgpt: device response exceeded size limit")
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		text := strings.ToLower(string(body))
		if strings.Contains(text, "access_denied") || strings.Contains(text, "denied") {
			return deviceTokenResponse{}, false, false, errors.New("chatgpt: device authorization was denied")
		}
		if strings.Contains(text, "expired") {
			return deviceTokenResponse{}, false, false, errors.New("chatgpt: device authorization expired")
		}
		slow := strings.Contains(text, "slow_down")
		return deviceTokenResponse{}, true, slow, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deviceTokenResponse{}, false, false, fmt.Errorf("chatgpt: device authorization failed (HTTP %d)", resp.StatusCode)
	}
	var result deviceTokenResponse
	if json.Unmarshal(body, &result) != nil || result.AuthorizationCode == "" || result.CodeVerifier == "" {
		return result, false, false, errors.New("chatgpt: malformed device authorization response")
	}
	return result, false, false, nil
}
func safeText(v string) string {
	return truncate(strings.Map(func(r rune) rune {
		if r < 0x20 {
			return -1
		}
		return r
	}, v), 200)
}
