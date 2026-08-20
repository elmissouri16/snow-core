package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/elmissouri16/snow-core/internal/config"
	internalsandbox "github.com/elmissouri16/snow-core/internal/sandbox"
)

var sandboxImageFetcher internalsandbox.ImageFetcher

func sandboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage the optional persistent smolvm shell sandbox",
		Args:  cobra.NoArgs,
		RunE:  runSandboxStatus,
	}
	cmd.PersistentFlags().Bool("json", false, "emit machine-readable JSON")
	cmd.AddCommand(sandboxInitCmd(), sandboxStatusCmd(), sandboxStartCmd(), sandboxStopCmd(), sandboxDeleteCmd())
	return cmd
}

func sandboxInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [image-or-pack]",
		Short: "Create and start a project sandbox",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, cfg, err := newSandboxManager(cmd)
			if err != nil {
				return err
			}
			profile, _ := cmd.Flags().GetString("profile")
			if profile != "" && len(args) == 1 {
				return errors.New("sandbox init accepts either --profile or an image/pack source, not both")
			}
			source := cfg.Sandbox.DefaultImage
			if profile != "" {
				source = ""
			} else if len(args) == 1 {
				source = args[0]
			}
			fromPack, _ := cmd.Flags().GetBool("from")
			kind := ""
			if fromPack {
				kind = internalsandbox.SourcePack
			}
			cpus, _ := cmd.Flags().GetInt("cpus")
			memory, _ := cmd.Flags().GetInt("memory")
			storage, _ := cmd.Flags().GetInt("storage")
			overlay, _ := cmd.Flags().GetInt("overlay")
			guestCWD, _ := cmd.Flags().GetString("guest-cwd")
			readOnly, _ := cmd.Flags().GetBool("read-only")
			network, _ := cmd.Flags().GetBool("network")
			status, err := manager.Init(cmd.Context(), internalsandbox.InitOptions{
				Profile: profile, Source: source, SourceKind: kind, CPUs: cpus, MemoryMiB: memory,
				StorageGiB: storage, OverlayGiB: overlay,
				StorageSet: cmd.Flags().Changed("storage"), OverlaySet: cmd.Flags().Changed("overlay"),
				GuestCWD: guestCWD, ReadOnly: readOnly, Network: network,
			})
			if err != nil {
				return err
			}
			return printSandboxStatus(cmd, status)
		},
	}
	cmd.Flags().String("profile", "", "built-in environment profile: ubuntu, go, node, or python")
	cmd.Flags().Bool("from", false, "treat source as a local .smolmachine artifact")
	cmd.Flags().Int("cpus", 0, "virtual CPU count (default from global config)")
	cmd.Flags().Int("memory", 0, "memory limit in MiB (default from global config)")
	cmd.Flags().Int("storage", 0, "storage disk GiB (omit for config default; explicit 0 uses smolvm default)")
	cmd.Flags().Int("overlay", 0, "overlay disk GiB (omit for config default; explicit 0 uses smolvm default)")
	cmd.Flags().String("guest-cwd", "", "guest project mount path (default from global config)")
	cmd.Flags().Bool("read-only", false, "mount the host project read-only")
	cmd.Flags().Bool("network", false, "enable guest network access (disabled by default)")
	return cmd
}

func sandboxStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show the project sandbox status", Args: cobra.NoArgs, RunE: runSandboxStatus}
}

func runSandboxStatus(cmd *cobra.Command, _ []string) error {
	manager, _, err := newSandboxManager(cmd)
	if err != nil {
		return err
	}
	status, err := manager.Status(cmd.Context())
	if err != nil {
		return err
	}
	return printSandboxStatus(cmd, status)
}

func sandboxStartCmd() *cobra.Command {
	return &cobra.Command{
		Use: "start", Short: "Start the persistent project sandbox", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, _, err := newSandboxManager(cmd)
			if err != nil {
				return err
			}
			status, err := manager.Start(cmd.Context())
			if err != nil {
				return err
			}
			return printSandboxStatus(cmd, status)
		},
	}
}

func sandboxStopCmd() *cobra.Command {
	return &cobra.Command{
		Use: "stop", Short: "Stop the sandbox and route future Bash calls to the host", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, _, err := newSandboxManager(cmd)
			if err != nil {
				return err
			}
			status, err := manager.Stop(cmd.Context())
			if err != nil {
				return err
			}
			return printSandboxStatus(cmd, status)
		},
	}
}

func sandboxDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "delete", Aliases: []string{"remove", "rm"},
		Short: "Delete the project sandbox and return future Bash calls to the host",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			force, _ := cmd.Flags().GetBool("force")
			if !force {
				return errors.New("sandbox delete requires --force because future Bash calls will run on the host")
			}
			manager, _, err := newSandboxManager(cmd)
			if err != nil {
				return err
			}
			forget, _ := cmd.Flags().GetBool("forget")
			if forget {
				err = manager.Forget(cmd.Context())
			} else {
				err = manager.Delete(cmd.Context())
			}
			if err != nil {
				return err
			}
			if jsonRequested(cmd) {
				if forget {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"forgotten": true, "machine_deleted": false})
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"deleted": true, "machine_deleted": true})
			}
			if forget {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "sandbox association forgotten; the smolvm machine was not deleted—inspect/remove it manually; future Bash commands will run on the host")
			} else {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "sandbox deleted; future Bash commands will run on the host")
			}
			return err
		},
	}
	cmd.Flags().Bool("force", false, "confirm deletion and host-shell fallback")
	cmd.Flags().Bool("forget", false, "remove a stale association without contacting smolvm")
	return cmd
}

func newSandboxManager(cmd *cobra.Command) (*internalsandbox.Manager, config.Config, error) {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath, _, _ = config.DefaultPaths()
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, cfg, err
	}
	forget, _ := cmd.Flags().GetBool("forget")
	manager, err := internalsandbox.New(internalsandbox.Options{
		Context:                  cmd.Context(),
		SkipExecutableValidation: forget,
		AllowStaleProfilePolicy:  cmd.Name() == "delete",
		ProjectRoot:              mustCWD(),
		StatePath:                filepath.Join(config.GlobalDir(), "sandboxes.json"),
		Executable:               cfg.Sandbox.Executable,
		DefaultImage:             cfg.Sandbox.DefaultImage,
		CPUs:                     cfg.Sandbox.CPUs,
		MemoryMiB:                cfg.Sandbox.MemoryMiB,
		StorageGiB:               cfg.Sandbox.StorageGiB,
		OverlayGiB:               cfg.Sandbox.OverlayGiB,
		GuestCWD:                 cfg.Sandbox.GuestCWD,
		EnvAllowlist:             cfg.Sandbox.EnvAllowlist,
		AutoInstall:              true,
		ImageFetcher:             sandboxImageFetcher,
	})
	if err != nil {
		return nil, cfg, err
	}
	return manager, cfg, nil
}

func printSandboxStatus(cmd *cobra.Command, status internalsandbox.Status) error {
	if jsonRequested(cmd) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}
	if !status.Initialized {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "sandbox: not initialized (Bash runs on the host)\nrun: snow sandbox init  # installs smolvm if needed and creates Ubuntu")
		return err
	}
	r := status.Record
	mountMode := "read-write"
	if r.ReadOnly {
		mountMode = "read-only"
	}
	network := "disabled"
	if r.Network {
		network = "enabled"
	}
	routing := "Bash routing: VM"
	if r.Stopped {
		routing = "Bash routing: host (sandbox stopped; run `snow sandbox start` to restore VM routing)"
	}
	resources := fmt.Sprintf("resources: %d CPUs · %d MiB", r.CPUs, r.MemoryMiB)
	if r.StorageGiB > 0 {
		resources += fmt.Sprintf(" · %d GiB storage", r.StorageGiB)
	}
	if r.OverlayGiB > 0 {
		resources += fmt.Sprintf(" · %d GiB overlay", r.OverlayGiB)
	}
	lines := []string{
		"sandbox: configured",
		routing,
		"machine: " + r.Machine,
	}
	if r.Profile != "" {
		lines = append(lines, "profile: "+r.Profile)
	}
	lines = append(lines,
		"source: "+r.Source,
		resources,
		fmt.Sprintf("Bash mount: %s → %s (%s)", r.Project, r.GuestCWD, mountMode),
		"guest network: "+network,
		"boundary: Bash only; Snow, file tools, plugins, MCP, and provider traffic remain host-side",
	)
	if runtime := strings.TrimSpace(status.Runtime); runtime != "" {
		lines = append(lines, "runtime:\n"+runtime)
	}
	if diagnostic := strings.TrimSpace(status.Diagnostic); diagnostic != "" {
		lines = append(lines, "status diagnostic: "+diagnostic)
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
	return err
}
