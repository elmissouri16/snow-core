package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"

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
	slices.SortStableFunc(descriptors, func(a, b auth.Descriptor) int {
		if byOrder := cmp.Compare(providerOrder(a.ProviderID), providerOrder(b.ProviderID)); byOrder != 0 {
			return byOrder
		}
		return cmp.Compare(a.ProviderID, b.ProviderID)
	})
	return descriptors
}

func (a *App) AuthStatus(ctx context.Context, providerID string) (auth.Status, error) {
	if a == nil || a.AuthService == nil {
		return auth.Status{}, fmt.Errorf("app: auth service is unavailable")
	}
	if providerID == "chatgpt" {
		return a.AuthService.StatusAccount(ctx, providerID, a.SelectedChatGPTAccount())
	}
	return a.AuthService.Status(ctx, providerID)
}

// Login delegates all credential acquisition and persistence to auth.Service.
// A catalog refresh is best-effort after the credential has committed.
func (a *App) Login(ctx context.Context, providerID string, request auth.LoginRequest, interaction auth.Interaction) (auth.Status, error) {
	if a == nil || a.AuthService == nil {
		return auth.Status{}, fmt.Errorf("app: auth service is unavailable")
	}
	if providerID == "chatgpt" {
		return a.LoginChatGPTAccount(ctx, a.SelectedChatGPTAccount(), request, interaction)
	}
	status, err := a.AuthService.LoginAccount(ctx, providerID, "default", request, interaction)
	if err != nil {
		return auth.Status{}, err
	}
	_ = a.RefreshProviderModels(ctx, providerID)
	return status, nil
}

// LoginChatGPTAccount saves a separately authorized alias without selecting it
// or changing the preferred account. Only a login for the selected alias may
// refresh this runtime's catalog.
func (a *App) LoginChatGPTAccount(ctx context.Context, account string, request auth.LoginRequest, interaction auth.Interaction) (auth.Status, error) {
	if a == nil || a.AuthService == nil {
		return auth.Status{}, fmt.Errorf("app: auth service is unavailable")
	}
	status, err := a.AuthService.LoginAccount(ctx, "chatgpt", account, request, interaction)
	if err != nil {
		return auth.Status{}, err
	}
	if account == "" {
		account = "default"
	}
	if account == a.SelectedChatGPTAccount() {
		a.clearProviderCatalog("chatgpt")
		_ = a.RefreshProviderModels(ctx, "chatgpt")
	}
	return status, nil
}

func (a *App) Logout(ctx context.Context, providerID string) error {
	if a == nil || a.AuthService == nil {
		return fmt.Errorf("app: auth service is unavailable")
	}
	descriptor, err := a.AuthService.Descriptor(providerID)
	if err != nil {
		return err
	}
	refreshCatalog := false
	if runtime, ok := a.Providers[providerID]; ok {
		if authoritative, ok := runtime.(interface{ ModelCatalogAuthoritative() bool }); ok {
			refreshCatalog = authoritative.ModelCatalogAuthoritative()
		}
	}
	account := "default"
	if providerID == "chatgpt" {
		account = a.SelectedChatGPTAccount()
	}
	if err := a.AuthService.LogoutAccount(ctx, providerID, account); err != nil {
		return err
	}
	if descriptor.Required {
		if _, resolveErr := a.AuthService.ResolveAccount(ctx, providerID, account); resolveErr != nil {
			a.clearProviderCatalog(providerID)
			return nil
		}
		refreshCatalog = true
	}
	if refreshCatalog {
		_ = a.RefreshProviderModels(ctx, providerID)
	}
	return nil
}

// clearProviderCatalog immediately removes stale models after the last usable
// credential for a required-auth provider is removed. The next explicit login
// or catalog load can repopulate it.
func (a *App) clearProviderCatalog(providerID string) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	a.clearProviderCatalogLocked(providerID)
}

func (a *App) clearProviderCatalogLocked(providerID string) {
	delete(a.modelCatalog, providerID)
	if a.runtimeSelection != nil {
		a.runtimeSelection.mu.Lock()
		if a.runtimeSelection.catalogGeneration == nil {
			a.runtimeSelection.catalogGeneration = make(map[string]uint64)
		}
		a.runtimeSelection.catalogGeneration[providerID]++
		delete(a.runtimeSelection.catalogs, providerID)
		delete(a.runtimeSelection.catalogErrors, providerID)
		a.runtimeSelection.mu.Unlock()
	}
	a.rebuildAllModelsLocked()
	if a.ProviderID == providerID {
		a.Models = nil
	}
}
