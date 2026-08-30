package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/internal/provider/opencodego"
	"github.com/elmissouri16/snow-core/internal/provider/opencodezen"
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
	cacheRoot := filepath.Join(config.GlobalDir(), "cache")
	baseURLOverride := opts.BaseURL
	newOpenCode := func(pc config.ProviderConfig) (provider.Transport, error) {
		ocCfg := opencodego.Config{
			CacheRoot:         filepath.Join(cacheRoot, "opencode-models"),
			BaseURL:           pc.BaseURL,
			DefaultModel:      pc.DefaultModel,
			StreamIdleTimeout: configuredStreamIdleTimeout(pc.StreamIdleTimeoutMS),
		}
		if baseURLOverride != "" && providerID == "opencode-go" {
			ocCfg.BaseURL = baseURLOverride
		}
		oc, err := opencodego.New(ocCfg)
		if err != nil {
			return nil, fmt.Errorf("app: opencode-go: %w", err)
		}
		return oc, nil
	}

	newOpenCodeZen := func(pc config.ProviderConfig) (provider.Transport, error) {
		zenCfg := opencodezen.Config{
			CacheRoot:         filepath.Join(cacheRoot, "opencode-zen-models"),
			BaseURL:           pc.BaseURL,
			DefaultModel:      pc.DefaultModel,
			StreamIdleTimeout: configuredStreamIdleTimeout(pc.StreamIdleTimeoutMS),
		}
		if baseURLOverride != "" && providerID == opencodezen.ProviderID {
			zenCfg.BaseURL = baseURLOverride
		}
		zen, err := opencodezen.New(zenCfg)
		if err != nil {
			return nil, fmt.Errorf("app: opencode-zen: %w", err)
		}
		return zen, nil
	}

	newChatGPT := func(pc config.ProviderConfig) *chatgpt.Provider {
		cgCfg := chatgpt.Config{
			Store:             authStore,
			CacheRoot:         filepath.Join(cacheRoot, "chatgpt-models"),
			BaseURL:           pc.BaseURL,
			StreamIdleTimeout: configuredStreamIdleTimeout(pc.StreamIdleTimeoutMS),
		}
		if providerID == "chatgpt" && baseURLOverride != "" {
			cgCfg.BaseURL = baseURLOverride
		}
		return chatgpt.New(cgCfg)
	}

	newOpenAICompatible := func(id string, pc config.ProviderConfig) (*openaicompat.Provider, error) {
		compatibleCfg := openaicompat.Config{
			ProviderID:        id,
			DisableEnvAPIKey:  true,
			BaseURL:           pc.BaseURL,
			DefaultModel:      pc.DefaultModel,
			StreamIdleTimeout: configuredStreamIdleTimeout(pc.StreamIdleTimeoutMS),
		}
		if providerID == id && baseURLOverride != "" {
			compatibleCfg.BaseURL = baseURLOverride
		}
		return openaicompat.New(compatibleCfg)
	}

	type builtInModule struct {
		id      string
		order   int
		build   func() (provider.Transport, error)
		authFor func(provider.Transport) (auth.Driver, error)
	}
	openCodeConfig := cfg.Providers["opencode-go"]
	zenConfig := cfg.Providers[opencodezen.ProviderID]
	compatibleConfig := cfg.Providers[openaicompat.ProviderID]
	chatGPTConfig := cfg.Providers[chatgpt.ProviderID]
	builtIns := []builtInModule{
		{id: "opencode-go", order: 10, build: func() (provider.Transport, error) { return newOpenCode(openCodeConfig) }, authFor: func(provider.Transport) (auth.Driver, error) {
			return auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: "opencode-go", DisplayName: "OpenCode Go", Required: true, Environment: []string{opencodego.EnvAPIKey}}), nil
		}},
		{id: opencodezen.ProviderID, order: 15, build: func() (provider.Transport, error) { return newOpenCodeZen(zenConfig) }, authFor: func(provider.Transport) (auth.Driver, error) {
			return auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: opencodezen.ProviderID, DisplayName: "OpenCode Zen", Required: false, Environment: []string{opencodezen.EnvAPIKey}}), nil
		}},
		{id: openaicompat.ProviderID, order: 20, build: func() (provider.Transport, error) {
			return newOpenAICompatible(openaicompat.ProviderID, compatibleConfig)
		}, authFor: func(provider.Transport) (auth.Driver, error) {
			return auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: openaicompat.ProviderID, DisplayName: "OpenAI-compatible", Required: false, Environment: []string{openaicompat.EnvAPIKey}}), nil
		}},
		{id: chatgpt.ProviderID, order: 30, build: func() (provider.Transport, error) { return newChatGPT(chatGPTConfig), nil }, authFor: func(raw provider.Transport) (auth.Driver, error) {
			switch transport := raw.(type) {
			case *chatgpt.Provider:
				return chatgpt.NewAuthDriver(transport), nil
			case *provider.LazyTransport:
				return chatgpt.NewLazyAuthDriver(func() (*chatgpt.Provider, error) {
					materialized, err := transport.Materialize()
					if err != nil {
						return nil, err
					}
					chatgptTransport, ok := materialized.(*chatgpt.Provider)
					if !ok {
						return nil, errors.New("app: chatgpt transport has unexpected type")
					}
					return chatgptTransport, nil
				}), nil
			default:
				return nil, errors.New("app: chatgpt transport has unexpected type")
			}
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
	slices.Sort(profileIDs)
	for index, profileID := range profileIDs {
		id := profileID
		profileConfig := cfg.Providers[id]
		builtIns = append(builtIns, builtInModule{
			id: id, order: 21 + index,
			build: func() (provider.Transport, error) { return newOpenAICompatible(id, profileConfig) },
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
	for _, spec := range builtIns {
		runtimeEnabled := (providerID == "fake" && spec.id == "fake") || (providerID != "fake" && spec.id != "fake")
		active := spec.id == providerID
		var raw provider.Transport
		if active {
			var err error
			raw, err = spec.build()
			if err != nil {
				return startupProvider{}, fmt.Errorf("app: initialize provider %s: %w", spec.id, err)
			}
			if compatible, ok := raw.(*openaicompat.Provider); ok && !compatible.Configured() {
				if spec.id == openaicompat.ProviderID {
					return startupProvider{}, errors.New("app: openai-compatible base URL is required; pass --base-url or configure providers.openai-compatible.base_url")
				}
				return startupProvider{}, fmt.Errorf("app: OpenAI-compatible profile %q requires a base URL; pass --base-url or configure providers.%s.base_url", spec.id, spec.id)
			}
		} else if runtimeEnabled || spec.id == chatgpt.ProviderID {
			// Inactive runtime providers retain only their immutable constructor.
			// ChatGPT also needs a deferred constructor in fake mode so OAuth login
			// remains available without allocating its HTTP adapter at startup.
			var err error
			raw, err = provider.NewLazyTransport(spec.id, spec.build)
			if err != nil {
				return startupProvider{}, err
			}
		}
		driver, driverErr := spec.authFor(raw)
		if driverErr != nil {
			return startupProvider{}, driverErr
		}
		if err := authService.Register(driver); err != nil {
			return startupProvider{}, err
		}
		if runtimeEnabled {
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
