package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/session"
)

func validateCatalogFlags(cmd *cobra.Command) error {
	startup, _ := cmd.Flags().GetString("rpc-startup")
	mode, _ := cmd.Flags().GetString("mode")
	if cmd.Flags().Changed("rpc-startup") && mode != "rpc" {
		return fmt.Errorf("--rpc-startup requires --mode rpc")
	}
	if startup != "" && startup != "eager" && startup != "catalog" && startup != "control" {
		return fmt.Errorf("--rpc-startup must be eager, catalog or control")
	}
	if startup != "catalog" && startup != "control" {
		return nil
	}
	if mode != "rpc" || cmd.Name() != "snow" {
		return fmt.Errorf("%s startup requires snow --mode rpc, not a subcommand", startup)
	}
	var invalid string
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if flag.Name != "mode" && flag.Name != "rpc-startup" && invalid == "" {
			invalid = flag.Name
		}
	})
	if invalid != "" {
		return fmt.Errorf("%s: --%s is not supported; no runtime configuration is loaded", startup, invalid)
	}
	return nil
}

func runCatalog(ctx context.Context, cmd *cobra.Command) error {
	if err := validateCatalogFlags(cmd); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return rpc.CatalogMain(ctx, cwd, session.DefaultSessionsRoot(), version)
}
