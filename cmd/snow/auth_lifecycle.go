package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/internal/provider/opencodego"
	"github.com/elmissouri16/snow-core/internal/provider/opencodezen"
)

func newCLIAuthService(store auth.Store, providerConfigs ...map[string]config.ProviderConfig) (*auth.Service, *chatgpt.Provider, error) {
	service := auth.NewService(store)
	chatgptProvider := chatgpt.New(chatgpt.Config{Store: store})
	for _, driver := range []auth.Driver{
		auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: opencodego.ProviderID, DisplayName: "OpenCode Go", Required: true, Environment: []string{opencodego.EnvAPIKey}}),
		auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: opencodezen.ProviderID, DisplayName: "OpenCode Zen", Required: false, Environment: []string{opencodezen.EnvAPIKey}}),
		auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: openaicompat.ProviderID, DisplayName: "OpenAI-compatible", Required: false, Environment: []string{openaicompat.EnvAPIKey}}),
		chatgpt.NewAuthDriver(chatgptProvider),
	} {
		if err := service.Register(driver); err != nil {
			return nil, nil, err
		}
	}
	if len(providerConfigs) > 0 {
		for providerID, providerConfig := range providerConfigs[0] {
			if providerID == openaicompat.ProviderID || !config.IsOpenAICompatibleProfile(providerID, providerConfig) {
				continue
			}
			if err := service.Register(auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: providerID, DisplayName: providerID, Required: false})); err != nil {
				return nil, nil, err
			}
		}
	}
	return service, chatgptProvider, nil
}

func loadCLIAuthConfig(cmd *cobra.Command) (config.Config, string, error) {
	path, _ := cmd.Flags().GetString("config")
	if path == "" {
		path, _, _ = config.DefaultPaths()
	}
	cfg, err := config.Load(path)
	return cfg, path, err
}

type cliAuthInteraction struct {
	openBrowser bool
}

func (i cliAuthInteraction) Prompt(_ context.Context, prompt auth.Prompt) (auth.Response, error) {
	var value string
	var err error
	if prompt.Kind == auth.PromptSecret {
		value, err = promptSecret("API key: ")
	} else {
		value, err = promptLine(prompt.Title + ": ")
	}
	return auth.Response{Value: value}, err
}

func (i cliAuthInteraction) OpenURL(ctx context.Context, target string) error {
	if !i.openBrowser {
		return nil
	}
	return openBrowser(ctx, target)
}

func (cliAuthInteraction) Progress(progress auth.Progress) {
	if progress.URL != "" {
		fmt.Println(progress.Message + ": " + progress.URL)
	} else if progress.Message != "" {
		fmt.Println(progress.Message)
	}
	if progress.UserCode != "" {
		fmt.Println("Device code: " + progress.UserCode)
	}
}
