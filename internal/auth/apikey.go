package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// APIKeyOptions configures the reusable API-key authentication driver.
type APIKeyOptions struct {
	ProviderID  string
	DisplayName string
	Required    bool
	Environment []string
}

type APIKeyDriver struct{ descriptor Descriptor }

func NewAPIKeyDriver(options APIKeyOptions) *APIKeyDriver {
	return &APIKeyDriver{descriptor: Descriptor{
		ProviderID: options.ProviderID, DisplayName: options.DisplayName,
		Required: options.Required, Kinds: []CredentialType{CredentialAPIKey},
		Environment: slices.Clone(options.Environment),
		Methods:     []LoginMethod{{ID: "api_key", DisplayName: "API key", Kind: CredentialAPIKey}},
	}}
}

func (d *APIKeyDriver) Descriptor() Descriptor { return d.descriptor }

func (d *APIKeyDriver) Inspect(credential Credential) (Status, error) {
	status := Status{ProviderID: d.descriptor.ProviderID, Method: CredentialAPIKey}
	if strings.TrimSpace(credential.Key) == "" {
		status.State = StateMissing
		status.Summary = "no key"
		if d.descriptor.Required {
			return status, errors.New("API key is missing")
		}
		return status, nil
	}
	if credential.Type != "" && credential.Type != CredentialAPIKey {
		status.State = StateInvalid
		status.Summary = "wrong credential type"
		return status, fmt.Errorf("auth: %s requires an API-key credential", d.descriptor.ProviderID)
	}
	status.State = StateConfigured
	status.Summary = "API key configured"
	return status, nil
}

func (d *APIKeyDriver) Login(ctx context.Context, request LoginRequest, interaction Interaction) (Credential, error) {
	if request.Method != "" && request.Method != "api_key" {
		return Credential{}, fmt.Errorf("auth: %s does not support login method %q", d.descriptor.ProviderID, request.Method)
	}
	if interaction == nil {
		interaction = NopInteraction{}
	}
	response, err := interaction.Prompt(ctx, Prompt{ID: "api_key", Kind: PromptSecret, Title: "API key for " + d.descriptor.DisplayName, Optional: !d.descriptor.Required})
	if err != nil {
		return Credential{}, err
	}
	key := strings.TrimSpace(response.Value)
	if key == "" {
		return Credential{}, errors.New("auth: empty API key")
	}
	return Credential{Provider: d.descriptor.ProviderID, Type: CredentialAPIKey, Key: key}, nil
}

func (d *APIKeyDriver) Validate(credential Credential) error {
	if credential.Key == "" && !d.descriptor.Required {
		return nil
	}
	_, err := d.Inspect(credential)
	return err
}
func (*APIKeyDriver) NeedsRefresh(Credential, time.Time) bool { return false }
func (d *APIKeyDriver) Refresh(context.Context, Credential, RefreshReason) (Credential, error) {
	return Credential{}, fmt.Errorf("%w for %s", ErrNotRefreshable, d.descriptor.ProviderID)
}

// NoAuthDriver is used by deterministic/local providers that require no credential.
type NoAuthDriver struct{ descriptor Descriptor }

func NewNoAuthDriver(providerID, displayName string) *NoAuthDriver {
	return &NoAuthDriver{descriptor: Descriptor{ProviderID: providerID, DisplayName: displayName}}
}
func (d *NoAuthDriver) Descriptor() Descriptor { return d.descriptor }
func (d *NoAuthDriver) Inspect(Credential) (Status, error) {
	return Status{ProviderID: d.descriptor.ProviderID, State: StateConfigured, Summary: "authentication not required"}, nil
}
func (d *NoAuthDriver) Login(context.Context, LoginRequest, Interaction) (Credential, error) {
	return Credential{}, fmt.Errorf("auth: %s does not support login", d.descriptor.ProviderID)
}
func (*NoAuthDriver) Validate(Credential) error               { return nil }
func (*NoAuthDriver) NeedsRefresh(Credential, time.Time) bool { return false }
func (d *NoAuthDriver) Refresh(context.Context, Credential, RefreshReason) (Credential, error) {
	return Credential{}, fmt.Errorf("%w for %s", ErrNotRefreshable, d.descriptor.ProviderID)
}
