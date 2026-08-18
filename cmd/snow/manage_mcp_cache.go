package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/snow-core/snow/internal/config"
	internalmcp "github.com/snow-core/snow/internal/mcp"
	"github.com/snow-core/snow/internal/tools"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
)

func mcpCacheCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "cache [name]",
		Short: "Inspect MCP catalog cache without starting servers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runMCPCacheStatus(cmd, name)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	cmd.AddCommand(mcpCacheStatusCmd(), mcpCacheRefreshCmd(), mcpCacheClearCmd())
	return cmd
}

func mcpCacheStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status [name]",
		Short: "Show secret-free MCP cache status without starting servers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runMCPCacheStatus(cmd, name)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func runMCPCacheStatus(cmd *cobra.Command, name string) error {
	set, err := loadMCPConfig(cmd, false)
	if err != nil {
		return err
	}
	declarations, err := cacheDeclarations(set, name)
	if err != nil {
		return err
	}
	manager := internalmcp.NewManager(tools.NewRegistry(), internalmcp.Options{
		CWD: set.ProjectIdentity, Roots: []string{set.ProjectIdentity}, CacheRoot: config.GlobalDir(),
	})
	defer manager.Close()
	statuses := manager.CacheStatuses(declarations)
	if jsonRequested(cmd) {
		return json.NewEncoder(os.Stdout).Encode(statuses)
	}
	if len(statuses) == 0 {
		fmt.Println("no MCP servers configured")
		return nil
	}
	fmt.Println("NAME\tSTATE\tSCOPE\tCACHED AT\tEXPIRES\tTOOLS\tCAPABILITIES")
	for _, status := range statuses {
		cachedAt, expiresAt := "-", "-"
		if !status.CachedAt.IsZero() {
			cachedAt = status.CachedAt.Format(time.RFC3339)
		}
		if !status.ExpiresAt.IsZero() {
			expiresAt = status.ExpiresAt.Format(time.RFC3339)
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%d\t%s\n", status.ID, status.State, status.Scope, cachedAt, expiresAt, status.ToolCount, strings.Join(status.Capabilities, ","))
		if status.Message != "" {
			fmt.Fprintf(os.Stderr, "mcp cache %s: %s\n", status.ID, status.Message)
		}
	}
	return nil
}

func mcpCacheRefreshCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "refresh <name>",
		Short: "Connect to one MCP server and atomically refresh its catalog cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := loadMCPConfig(cmd, false)
			if err != nil {
				return err
			}
			declarations, err := cacheDeclarations(set, args[0])
			if err != nil {
				return err
			}
			decl := declarations[0]
			if decl.Spec.Disabled {
				return fmt.Errorf("mcp: server %q is disabled", args[0])
			}
			if err := decl.Spec.Validate(); err != nil {
				return fmt.Errorf("mcp cache refresh %s: %w", args[0], err)
			}
			decl.Spec.Lifecycle = publicmcp.LifecycleLazy
			decl.Spec.CacheBootstrap = publicmcp.CacheBootstrapAuto
			decl.Spec.IdleTimeoutMS = 0
			manager := internalmcp.NewManager(tools.NewRegistry(), internalmcp.Options{
				CWD: set.ProjectIdentity, Roots: []string{set.ProjectIdentity}, CacheRoot: config.GlobalDir(),
				HostName: "snow", HostVersion: version, MaxOutputBytes: set.Config.ToolOutputLimit(), ForceRefresh: true,
			})
			defer manager.Close()
			manager.Initialize(cmd.Context(), []internalmcp.Declaration{decl})
			live := manager.Statuses()
			if len(live) != 1 || live[0].State == "failed" || strings.Contains(strings.ToLower(live[0].Message), "cache write") {
				if len(live) == 1 && live[0].Message != "" {
					return fmt.Errorf("mcp cache refresh %s failed: %s", args[0], live[0].Message)
				}
				return fmt.Errorf("mcp cache refresh %s failed", args[0])
			}
			cacheStatus := manager.CacheStatuses([]internalmcp.Declaration{decl})
			if len(cacheStatus) != 1 || !cacheStatus[0].Valid {
				return fmt.Errorf("mcp cache refresh %s did not produce a valid catalog", args[0])
			}
			if jsonRequested(cmd) {
				return json.NewEncoder(os.Stdout).Encode(cacheStatus[0])
			}
			fmt.Printf("refreshed MCP cache %s (%d tools, expires %s)\n", args[0], cacheStatus[0].ToolCount, cacheStatus[0].ExpiresAt.Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func mcpCacheClearCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "clear <name>",
		Short: "Remove cached catalogs for one configured MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := loadMCPConfig(cmd, false)
			if err != nil {
				return err
			}
			declarations, err := cacheDeclarations(set, args[0])
			if err != nil {
				return err
			}
			manager := internalmcp.NewManager(tools.NewRegistry(), internalmcp.Options{
				CWD: set.ProjectIdentity, Roots: []string{set.ProjectIdentity}, CacheRoot: config.GlobalDir(),
			})
			defer manager.Close()
			removed, err := manager.ClearCache(declarations[0])
			if err != nil {
				return err
			}
			result := struct {
				ID      string `json:"id"`
				Removed int    `json:"removed"`
			}{ID: args[0], Removed: removed}
			if jsonRequested(cmd) {
				return json.NewEncoder(os.Stdout).Encode(result)
			}
			fmt.Printf("cleared MCP cache %s (%d entries)\n", args[0], removed)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func cacheDeclarations(set mcpConfigSet, name string) ([]internalmcp.Declaration, error) {
	if name != "" {
		spec, ok := set.Effective[name]
		if !ok {
			return nil, fmt.Errorf("mcp: server %q is not configured", name)
		}
		return []internalmcp.Declaration{{Spec: spec, Scope: set.Scopes[name], ProjectIdentity: set.ProjectIdentity}}, nil
	}
	names := sortedMCPNames(set.Effective)
	declarations := make([]internalmcp.Declaration, 0, len(names))
	for _, serverName := range names {
		declarations = append(declarations, internalmcp.Declaration{Spec: set.Effective[serverName], Scope: set.Scopes[serverName], ProjectIdentity: set.ProjectIdentity})
	}
	return declarations, nil
}
