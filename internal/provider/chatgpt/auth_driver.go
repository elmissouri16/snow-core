package chatgpt

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
)

// AuthDriver adapts ChatGPT OAuth to the provider-independent auth lifecycle.
// OAuth endpoints, scopes, claims, and workspace checks remain in this package.
type AuthDriver struct {
	provider      *Provider
	providerBuild func() (*Provider, error)
	providerOnce  sync.Once
	providerErr   error
}

func NewAuthDriver(provider *Provider) *AuthDriver { return &AuthDriver{provider: provider} }

// NewLazyAuthDriver defers construction of the ChatGPT HTTP adapter until an
// OAuth login or refresh actually needs it. Descriptor and credential
// inspection remain allocation-only local operations.
func NewLazyAuthDriver(build func() (*Provider, error)) *AuthDriver {
	return &AuthDriver{providerBuild: build}
}

func (d *AuthDriver) configuredProvider() (*Provider, error) {
	if d == nil {
		return nil, fmt.Errorf("chatgpt: OAuth driver is not configured")
	}
	if d.providerBuild == nil {
		if d.provider == nil {
			return nil, fmt.Errorf("chatgpt: OAuth driver is not configured")
		}
		return d.provider, nil
	}
	d.providerOnce.Do(func() {
		d.provider, d.providerErr = d.providerBuild()
		if d.providerErr == nil && d.provider == nil {
			d.providerErr = fmt.Errorf("chatgpt: OAuth driver is not configured")
		}
	})
	if d.providerErr != nil {
		return nil, fmt.Errorf("chatgpt: initialize OAuth client: %w", d.providerErr)
	}
	return d.provider, nil
}

func (*AuthDriver) Descriptor() auth.Descriptor {
	return auth.Descriptor{
		ProviderID: ProviderID, DisplayName: "ChatGPT/Codex", Required: true,
		Kinds: []auth.CredentialType{auth.CredentialOAuth},
		Methods: []auth.LoginMethod{
			{ID: string(LoginBrowser), DisplayName: "Browser OAuth", Kind: auth.CredentialOAuth},
			{ID: string(LoginDevice), DisplayName: "Device code", Kind: auth.CredentialOAuth},
		},
	}
}

func (*AuthDriver) Inspect(credential auth.Credential) (auth.Status, error) {
	checked, err := CheckAuth(credential)
	status := auth.Status{ProviderID: ProviderID, Method: auth.CredentialOAuth, Refreshable: checked.Refreshable, ExpiresAt: checked.ExpiresAt, AccountID: checked.AccountID}
	if err != nil {
		status.State = auth.StateInvalid
		status.Summary = err.Error()
		return status, err
	}
	if checked.Expired {
		status.State = auth.StateExpired
	} else {
		status.State = auth.StateConfigured
	}
	status.Summary = FormatStatus(checked)
	return status, nil
}

func (*AuthDriver) Validate(credential auth.Credential) error { return Validate(credential) }

func (*AuthDriver) NeedsRefresh(credential auth.Credential, now time.Time) bool {
	checked, err := CheckAuth(credential)
	if err != nil || checked.ExpiresAt.IsZero() {
		return false
	}
	return !checked.ExpiresAt.After(now.Add(refreshSkew))
}

func (d *AuthDriver) Refresh(ctx context.Context, current auth.Credential, _ auth.RefreshReason) (auth.Credential, error) {
	if strings.TrimSpace(current.Refresh) == "" {
		return auth.Credential{}, fmt.Errorf("%w: OAuth refresh token is missing; sign in again", ErrLoginRequired)
	}
	provider, err := d.configuredProvider()
	if err != nil {
		return auth.Credential{}, err
	}
	return provider.refresh(ctx, current)
}

func (d *AuthDriver) Login(ctx context.Context, request auth.LoginRequest, interaction auth.Interaction) (auth.Credential, error) {
	method := LoginMethod(request.Method)
	if method == "" {
		method = LoginBrowser
	}
	if method != LoginBrowser && method != LoginDevice {
		return auth.Credential{}, fmt.Errorf("chatgpt: unsupported login method %q", request.Method)
	}
	provider, err := d.configuredProvider()
	if err != nil {
		return auth.Credential{}, err
	}
	if interaction == nil {
		interaction = auth.NopInteraction{}
	}
	var pasteCallback func(context.Context) (string, error)
	if availability, ok := interaction.(auth.PromptAvailability); !ok || availability.PromptAvailable() {
		pasteCallback = func(ctx context.Context) (string, error) {
			response, err := interaction.Prompt(ctx, auth.Prompt{ID: "callback_url", Kind: auth.PromptText, Title: "Paste the complete OAuth callback URL", Optional: true})
			return response.Value, err
		}
	}
	return LoginCredential(ctx, LoginOptions{
		Method: method, HTTPClient: provider.client, AuthBaseURL: provider.authBaseURL, Now: provider.now,
		AllowedWorkspaceIDs: slices.Clone(request.Params["allowed_workspace_id"]),
		OpenBrowser:         interaction.OpenURL,
		PasteCallback:       pasteCallback,
		Progress: func(progress LoginProgress) {
			interaction.Progress(auth.Progress{Kind: progress.Kind, Message: progress.Message, URL: progress.URL, UserCode: progress.UserCode})
		},
	})
}
