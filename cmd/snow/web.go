package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/web"
)

func validateWebFlags(cmd *cobra.Command, _ []string) error {
	if err := validateCatalogFlags(cmd); err != nil {
		return err
	}
	mode, _ := cmd.Flags().GetString("mode")
	if mode != "web" {
		return nil
	}
	if cmd.Name() != "snow" {
		return fmt.Errorf("web: --mode web must be used on snow itself, not a subcommand")
	}
	var invalid string
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if flag.Name != "mode" && invalid == "" {
			invalid = flag.Name
		}
	})
	if invalid != "" {
		return fmt.Errorf("web: --%s is not supported in manager startup; configure the host or choose provider/model during explicit browser activation", invalid)
	}
	opts, err := automaticWebOptions(availableInterfaceAddrs)
	if err != nil {
		return err
	}
	return opts.Validate()
}

func runWeb(ctx context.Context, cmd *cobra.Command) error {
	if err := validateWebFlags(cmd, nil); err != nil {
		return err
	}
	opts, err := automaticWebOptions(availableInterfaceAddrs)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	sessionsRoot, err := filepath.Abs(session.DefaultSessionsRoot())
	if err != nil {
		return err
	}
	managerDir, err := webManagerDirectory()
	if err != nil {
		return err
	}
	opts.Version = version
	opts.ManagerDir = managerDir
	opts.Executable = executable
	opts.SessionsRoot = sessionsRoot
	return web.Run(ctx, opts, cmd.OutOrStdout())
}

func automaticWebOptions(interfaceAddrs func() ([]net.Addr, error)) (web.Options, error) {
	addresses, err := interfaceAddrs()
	if err != nil {
		return web.Options{}, fmt.Errorf("web: detect private LAN address: %w", err)
	}
	address, ok := firstAutomaticWebAddress(addresses)
	if !ok {
		return web.Options{Listen: "127.0.0.1:7331"}, nil
	}
	return web.Options{Listen: netip.AddrPortFrom(address, automaticWebPort).String()}, nil
}

// webManagerDirectory selects manager storage without opening configuration.
func webManagerDirectory() (string, error) {
	return filepath.Abs(filepath.Join(config.GlobalDir(), "manager"))
}
