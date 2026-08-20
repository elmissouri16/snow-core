package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
)

// logoutCmd clears a provider credential from auth.json.
func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout <provider>",
		Short: "Remove a stored credential for a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			authPath, _ := cmd.Flags().GetString("auth")
			if authPath == "" {
				_, a, _ := config.DefaultPaths()
				authPath = a
			}
			store, err := auth.NewFileStore(authPath)
			if err != nil {
				return err
			}
			cfg, _, err := loadCLIAuthConfig(cmd)
			if err != nil {
				return err
			}
			service, _, err := newCLIAuthService(store, cfg.Providers)
			if err != nil {
				return err
			}
			if err := service.Logout(cmd.Context(), provider); err != nil {
				return err
			}
			fmt.Printf("removed %s credential from %s\n", provider, authPath)
			return nil
		},
	}
}
