package cli

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
)

func authAccountLabel(provider, account string) string {
	if account == "" || account == "default" {
		return provider
	}
	return provider + " account " + account
}

// loadCLIAccountService reads global configuration and stored credentials only.
// Account inspection never resolves credentials or contacts a provider.
func loadCLIAccountService(cmd *cobra.Command) (*auth.Service, config.Config, string, error) {
	cfg, configPath, err := loadCLIAuthConfig(cmd)
	if err != nil {
		return nil, cfg, configPath, err
	}
	authPath, _ := cmd.Flags().GetString("auth")
	if authPath == "" {
		_, authPath, _ = config.DefaultPaths()
	}
	store, err := auth.NewFileStore(authPath)
	if err != nil {
		return nil, cfg, configPath, err
	}
	service, _, err := newCLIAuthService(store, cfg.Providers)
	return service, cfg, configPath, err
}

func accountsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accounts [provider]",
		Short: "List stored ChatGPT account aliases and local authentication status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := chatgpt.ProviderID
			if len(args) != 0 {
				provider = args[0]
			}
			if provider != chatgpt.ProviderID {
				return fmt.Errorf("accounts: named accounts are only supported for chatgpt")
			}
			service, cfg, _, err := loadCLIAccountService(cmd)
			if err != nil {
				return err
			}
			accounts, err := service.Accounts(provider)
			if err != nil {
				return err
			}
			if len(accounts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "chatgpt: no stored accounts")
				return nil
			}
			preferred := cfg.ChatGPTAccounts.Preferred
			if preferred == "" {
				preferred = "default"
			}
			for _, account := range accounts {
				status, err := service.StatusAccount(cmd.Context(), provider, account)
				if err != nil {
					return err
				}
				marker := ""
				if account == preferred {
					marker = " (preferred)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s%s\n", account, status.State, marker)
			}
			return nil
		},
	}
}

func authUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use chatgpt <account>",
		Short: "Select a stored ChatGPT account as the global preferred account",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, account := args[0], args[1]
			if provider != chatgpt.ProviderID {
				return fmt.Errorf("auth use: named accounts are only supported for chatgpt")
			}
			if account == "" {
				return fmt.Errorf("auth use: account name cannot be empty")
			}
			if err := auth.ValidateAccountName(account); err != nil {
				return err
			}
			service, _, configPath, err := loadCLIAccountService(cmd)
			if err != nil {
				return err
			}
			accounts, err := service.Accounts(provider)
			if err != nil {
				return err
			}
			if !slices.Contains(accounts, account) {
				return fmt.Errorf("auth use: chatgpt account %q is not stored; sign in with snow login chatgpt --account %s", account, account)
			}
			if _, err := config.Update(configPath, func(cfg *config.Config) error {
				cfg.ChatGPTAccounts.Preferred = account
				return nil
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "chatgpt: preferred account is %s\n", account)
			return nil
		},
	}
}
