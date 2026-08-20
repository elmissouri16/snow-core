package app

import (
	"context"
	"fmt"
	"sort"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
)

// AuthProviders returns the deterministic, safe authentication inventory.
func (a *App) AuthProviders() []auth.Descriptor {
	if a == nil || a.AuthService == nil {
		return nil
	}
	descriptors := a.AuthService.Providers()
	providerOrder := func(id string) int {
		switch id {
		case "opencode-go":
			return 10
		case "chatgpt":
			return 30
		case "fake":
			return 40
		}
		if config.IsOpenAICompatibleProfile(id, a.Cfg.Providers[id]) {
			return 20
		}
		return 50
	}
	sort.SliceStable(descriptors, func(i, j int) bool {
		left, right := providerOrder(descriptors[i].ProviderID), providerOrder(descriptors[j].ProviderID)
		if left == right {
			return descriptors[i].ProviderID < descriptors[j].ProviderID
		}
		return left < right
	})
	return descriptors
}

func (a *App) AuthStatus(ctx context.Context, providerID string) (auth.Status, error) {
	if a == nil || a.AuthService == nil {
		return auth.Status{}, fmt.Errorf("app: auth service is unavailable")
	}
	return a.AuthService.Status(ctx, providerID)
}

// Login delegates all credential acquisition and persistence to auth.Service.
// A catalog refresh is best-effort after the credential has committed.
func (a *App) Login(ctx context.Context, providerID string, request auth.LoginRequest, interaction auth.Interaction) (auth.Status, error) {
	if a == nil || a.AuthService == nil {
		return auth.Status{}, fmt.Errorf("app: auth service is unavailable")
	}
	status, err := a.AuthService.Login(ctx, providerID, request, interaction)
	if err != nil {
		return auth.Status{}, err
	}
	_ = a.RefreshProviderModels(ctx, providerID)
	return status, nil
}

func (a *App) Logout(ctx context.Context, providerID string) error {
	if a == nil || a.AuthService == nil {
		return fmt.Errorf("app: auth service is unavailable")
	}
	refreshCatalog := false
	if runtime, ok := a.Providers[providerID]; ok {
		if authoritative, ok := runtime.(interface{ ModelCatalogAuthoritative() bool }); ok {
			refreshCatalog = authoritative.ModelCatalogAuthoritative()
		}
	}
	if err := a.AuthService.Logout(ctx, providerID); err != nil {
		return err
	}
	// Account-scoped providers replace their inventory with an offline fallback
	// after logout; ordinary API-key providers keep their current catalog.
	if refreshCatalog {
		_ = a.RefreshProviderModels(ctx, providerID)
	}
	return nil
}
