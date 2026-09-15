package main

import (
	"context"
	"fmt"
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
		for _, name := range []string{"web-listen", "web-tls-cert", "web-tls-key"} {
			if cmd.Flags().Changed(name) {
				return fmt.Errorf("--%s requires --mode web", name)
			}
		}
		return nil
	}
	if cmd.Name() != "snow" {
		return fmt.Errorf("web: --mode web must be used on snow itself, not a subcommand")
	}
	var invalid string
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		switch flag.Name {
		case "mode", "web-listen", "web-tls-cert", "web-tls-key":
		default:
			if invalid == "" {
				invalid = flag.Name
			}
		}
	})
	if invalid != "" {
		return fmt.Errorf("web: --%s is not supported in manager startup; configure the host or choose provider/model during explicit browser activation", invalid)
	}
	address, _ := cmd.Flags().GetString("web-listen")
	cert, _ := cmd.Flags().GetString("web-tls-cert")
	key, _ := cmd.Flags().GetString("web-tls-key")
	certSet, keySet := cmd.Flags().Changed("web-tls-cert"), cmd.Flags().Changed("web-tls-key")
	if (certSet || keySet) && (!certSet || !keySet || cert == "" || key == "") {
		return fmt.Errorf("web: --web-tls-cert and --web-tls-key require two nonempty absolute paths together")
	}
	return (web.Options{Listen: address, TLSCertFile: cert, TLSKeyFile: key}).Validate()
}

func runWeb(ctx context.Context, cmd *cobra.Command) error {
	if err := validateWebFlags(cmd, nil); err != nil {
		return err
	}
	address, _ := cmd.Flags().GetString("web-listen")
	cert, _ := cmd.Flags().GetString("web-tls-cert")
	key, _ := cmd.Flags().GetString("web-tls-key")
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
	return web.Run(ctx, web.Options{Listen: address, Version: version, TLSCertFile: cert, TLSKeyFile: key,
		ManagerDir: managerDir, Executable: executable,
		SessionsRoot: sessionsRoot}, cmd.OutOrStdout())
}

// webManagerDirectory selects manager storage without opening configuration.
func webManagerDirectory() (string, error) {
	return filepath.Abs(filepath.Join(config.GlobalDir(), "manager"))
}
