package main

import (
	"context"
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/elmissouri16/snow-core/internal/rpc"
)

// runControl must be dispatched before buildOptions: even reads may not create
// a session, instantiate providers, refresh credentials or load extensions.
func runControl(ctx context.Context, cmd *cobra.Command) error {
	if err := validateCatalogFlags(cmd); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return errors.New("control unavailable")
	}
	return rpc.ControlMain(ctx, cwd, version)
}
