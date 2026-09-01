package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
)

// ConfigureOpenAICompatibleAuth persists a secret-free named endpoint, updates
// its runtime adapter, and optionally commits an API key through auth.Service.
// The endpoint must not contain URL credentials, query parameters, or fragments.
func (a *App) ConfigureOpenAICompatibleAuth(ctx context.Context, profileID, baseURL, apiKey string) (auth.Status, error) {
	if a == nil || a.AuthService == nil {
		return auth.Status{}, errors.New("app: auth service is unavailable")
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		profileID = openaicompat.ProviderID
	}
	if err := config.ValidateProviderProfileID(profileID); err != nil {
		return auth.Status{}, err
	}
	baseURL = strings.TrimSpace(baseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return auth.Status{}, fmt.Errorf("app: parse OpenAI-compatible endpoint: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return auth.Status{}, errors.New("app: OpenAI-compatible endpoint requires an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return auth.Status{}, errors.New("app: OpenAI-compatible endpoint must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return auth.Status{}, errors.New("app: OpenAI-compatible endpoint must not contain credentials, query parameters, or a fragment")
	}

	oldPersisted := a.PersistedCfg
	var previous config.ProviderConfig
	var hadPrevious bool
	var written config.ProviderConfig
	persist := func(latest *config.Config) error {
		if latest.Providers == nil {
			latest.Providers = make(map[string]config.ProviderConfig)
		}
		previous, hadPrevious = latest.Providers[profileID]
		written = previous
		written.BaseURL = baseURL
		if profileID != openaicompat.ProviderID {
			written.Type = config.ProviderTypeOpenAICompatible
		}
		latest.Providers[profileID] = written
		return nil
	}
	var candidate config.Config
	if a.ConfigPath != "" {
		candidate, err = config.Update(a.ConfigPath, persist)
	} else {
		candidate = oldPersisted
		err = persist(&candidate)
	}
	if err != nil {
		return auth.Status{}, fmt.Errorf("app: persist OpenAI-compatible endpoint: %w", err)
	}

	a.PersistedCfg = candidate
	oldRuntime, hadOldRuntime := a.Cfg.Providers[profileID]
	if a.Cfg.Providers == nil {
		a.Cfg.Providers = make(map[string]config.ProviderConfig)
	}
	a.Cfg.Providers[profileID] = candidate.Providers[profileID]
	if err := a.ConfigureOpenAICompatibleProfile(profileID, baseURL); err != nil {
		rollbackErr := a.rollbackOpenAICompatibleProfile(profileID, written, previous, hadPrevious, oldPersisted)
		if hadOldRuntime {
			a.Cfg.Providers[profileID] = oldRuntime
		} else {
			delete(a.Cfg.Providers, profileID)
		}
		return auth.Status{}, errors.Join(err, rollbackErr)
	}

	if strings.TrimSpace(apiKey) != "" {
		status, loginErr := a.Login(ctx, profileID, auth.LoginRequest{Method: "api_key"}, appSecretInteraction{value: apiKey})
		if loginErr != nil {
			return auth.Status{}, fmt.Errorf("app: endpoint saved but credential persistence failed: %w", loginErr)
		}
		return status, nil
	}
	_ = a.RefreshProviderModels(ctx, profileID)
	status, statusErr := a.AuthStatus(ctx, profileID)
	if statusErr != nil && status.State != auth.StateInvalid {
		return auth.Status{}, statusErr
	}
	return status, nil
}

func (a *App) rollbackOpenAICompatibleProfile(profileID string, written, previous config.ProviderConfig, hadPrevious bool, fallback config.Config) error {
	rollback := func(latest *config.Config) error {
		current, exists := latest.Providers[profileID]
		if !exists || current != written {
			return nil // A concurrent writer now owns this profile.
		}
		if hadPrevious {
			latest.Providers[profileID] = previous
		} else {
			delete(latest.Providers, profileID)
		}
		return nil
	}
	if a.ConfigPath == "" {
		candidate := a.PersistedCfg
		if err := rollback(&candidate); err != nil {
			return err
		}
		a.PersistedCfg = candidate
		return nil
	}
	updated, err := config.Update(a.ConfigPath, rollback)
	if err != nil {
		a.PersistedCfg = fallback
		return err
	}
	a.PersistedCfg = updated
	return nil
}

type appSecretInteraction struct{ value string }

func (i appSecretInteraction) Prompt(context.Context, auth.Prompt) (auth.Response, error) {
	return auth.Response{Value: i.value}, nil
}
func (appSecretInteraction) OpenURL(context.Context, string) error {
	return auth.ErrInteractionUnavailable
}
func (appSecretInteraction) Progress(auth.Progress) {}
