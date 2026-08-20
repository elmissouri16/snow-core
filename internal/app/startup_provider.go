package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/internal/provider/opencodego"
)

// startupProvider is the selected provider transport plus the complete module
// registry used for model ordering and runtime provider construction.
type startupProvider struct {
	id        string
	modules   *provider.Registry
	providers map[string]provider.Provider
	selected  provider.Provider
}

func initializeProvider(opts Options, cfg config.Config, authStore auth.Store, authService *auth.Service) (startupProvider, error) {
	providerID := cfg.DefaultProvider
	if providerID == "" {
		providerID = "opencode-go"
	}
	newOpenCode := func() (provider.Transport, error) {
		ocCfg := opencodego.Config{CacheRoot: filepath.Join(config.GlobalDir(), "cache", "opencode-models")}
		if pc, ok := cfg.Providers["opencode-go"]; ok {
			ocCfg.BaseURL = pc.BaseURL
			ocCfg.DefaultModel = pc.DefaultModel
			ocCfg.StreamIdleTimeout = configuredStreamIdleTimeout(pc.StreamIdleTimeoutMS)
		}
		if opts.BaseURL != "" && providerID == "opencode-go" {
			ocCfg.BaseURL = opts.BaseURL
		}
		oc, err := opencodego.New(ocCfg)
		if err != nil {
			return nil, fmt.Errorf("app: opencode-go: %w", err)
		}
		return oc, nil
	}

	newChatGPT := func() *chatgpt.Provider {
		cgCfg := chatgpt.Config{Store: authStore, CacheRoot: filepath.Join(config.GlobalDir(), "cache", "chatgpt-models")}
		if pc, ok := cfg.Providers["chatgpt"]; ok {
			cgCfg.BaseURL = pc.BaseURL
			cgCfg.StreamIdleTimeout = configuredStreamIdleTimeout(pc.StreamIdleTimeoutMS)
		}
		if providerID == "chatgpt" && opts.BaseURL != "" {
			cgCfg.BaseURL = opts.BaseURL
		}
		return chatgpt.New(cgCfg)
	}

	newOpenAICompatible := func(id string) (*openaicompat.Provider, error) {
		compatibleCfg := openaicompat.Config{ProviderID: id, DisableEnvAPIKey: true}
		if pc, ok := cfg.Providers[id]; ok {
			compatibleCfg.BaseURL = pc.BaseURL
			compatibleCfg.DefaultModel = pc.DefaultModel
			compatibleCfg.StreamIdleTimeout = configuredStreamIdleTimeout(pc.StreamIdleTimeoutMS)
		}
		if providerID == id && opts.BaseURL != "" {
			compatibleCfg.BaseURL = opts.BaseURL
		}
		return openaicompat.New(compatibleCfg)
	}

	type builtInModule struct {
		id      string
		order   int
		build   func() (provider.Transport, error)
		authFor func(provider.Transport) (auth.Driver, error)
	}
	builtIns := []builtInModule{
		{id: "opencode-go", order: 10, build: newOpenCode, authFor: func(provider.Transport) (auth.Driver, error) {
			return auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: "opencode-go", DisplayName: "OpenCode Go", Required: true, Environment: []string{opencodego.EnvAPIKey}}), nil
		}},
		{id: openaicompat.ProviderID, order: 20, build: func() (provider.Transport, error) { return newOpenAICompatible(openaicompat.ProviderID) }, authFor: func(provider.Transport) (auth.Driver, error) {
			return auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: openaicompat.ProviderID, DisplayName: "OpenAI-compatible", Required: false, Environment: []string{openaicompat.EnvAPIKey}}), nil
		}},
		{id: chatgpt.ProviderID, order: 30, build: func() (provider.Transport, error) { return newChatGPT(), nil }, authFor: func(raw provider.Transport) (auth.Driver, error) {
			chatgptTransport, ok := raw.(*chatgpt.Provider)
			if !ok {
				return nil, errors.New("app: chatgpt transport has unexpected type")
			}
			return chatgpt.NewAuthDriver(chatgptTransport), nil
		}},
		{id: "fake", order: 40, build: func() (provider.Transport, error) {
			return provider.NoAuthTransport{Provider: fake.NewWithModels(nil)}, nil
		}, authFor: func(provider.Transport) (auth.Driver, error) {
			return auth.NewNoAuthDriver("fake", "Fake"), nil
		}},
	}
	profileIDs := make([]string, 0)
	for id, providerConfig := range cfg.Providers {
		if id != openaicompat.ProviderID && config.IsOpenAICompatibleProfile(id, providerConfig) {
			profileIDs = append(profileIDs, id)
		}
	}
	sort.Strings(profileIDs)
	for index, profileID := range profileIDs {
		id := profileID
		builtIns = append(builtIns, builtInModule{
			id: id, order: 21 + index,
			build: func() (provider.Transport, error) { return newOpenAICompatible(id) },
			authFor: func(provider.Transport) (auth.Driver, error) {
				return auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: id, DisplayName: id, Required: false}), nil
			},
		})
	}
	knownProvider := false
	for _, spec := range builtIns {
		knownProvider = knownProvider || spec.id == providerID
	}
	if !knownProvider {
		return startupProvider{}, fmt.Errorf("app: unsupported provider %q", providerID)
	}

	providerModules := provider.NewRegistry()
	var err error
	for _, spec := range builtIns {
		selected := (providerID == "fake" && spec.id == "fake") || (providerID != "fake" && spec.id != "fake")
		var raw provider.Transport
		if selected {
			raw, err = spec.build()
			if err != nil {
				return startupProvider{}, fmt.Errorf("app: initialize provider %s: %w", spec.id, err)
			}
			if compatible, ok := raw.(*openaicompat.Provider); ok && spec.id == providerID && !compatible.Configured() {
				if spec.id == openaicompat.ProviderID {
					return startupProvider{}, errors.New("app: openai-compatible base URL is required; pass --base-url or configure providers.openai-compatible.base_url")
				}
				return startupProvider{}, fmt.Errorf("app: OpenAI-compatible profile %q requires a base URL; pass --base-url or configure providers.%s.base_url", spec.id, spec.id)
			}
		} else if spec.id == chatgpt.ProviderID {
			// ChatGPT construction is local and supplies the OAuth driver even when
			// fake mode deliberately suppresses provider catalogs.
			raw = newChatGPT()
		}
		if raw == nil {
			driver, driverErr := spec.authFor(nil)
			if driverErr != nil {
				return startupProvider{}, driverErr
			}
			if err := authService.Register(driver); err != nil {
				return startupProvider{}, err
			}
			continue
		}
		driver, driverErr := spec.authFor(raw)
		if driverErr != nil {
			return startupProvider{}, driverErr
		}
		if err := authService.Register(driver); err != nil {
			return startupProvider{}, err
		}
		if selected {
			if err := providerModules.Register(provider.Module{ID: spec.id, Order: spec.order, Transport: raw, Auth: driver}); err != nil {
				return startupProvider{}, err
			}
		}
	}
	if opts.APIKey != "" {
		authService.SetExplicit(providerID, auth.Credential{Type: auth.CredentialAPIKey, Key: opts.APIKey})
	}
	providers, err := providerModules.Build(authService)
	if err != nil {
		return startupProvider{}, err
	}
	prov := providers[providerID]

	return startupProvider{id: providerID, modules: providerModules, providers: providers, selected: prov}, nil
}
