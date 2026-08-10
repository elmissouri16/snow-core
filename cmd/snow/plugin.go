package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	internalplugin "github.com/snow-core/snow/internal/plugin"
	publicplugin "github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
)

type pluginCheckToolView struct {
	Name         string                  `json:"name"`
	Description  string                  `json:"description,omitempty"`
	Parameters   json.RawMessage         `json:"parameters"`
	Discovery    *protocol.ToolDiscovery `json:"discovery,omitempty"`
	Risk         string                  `json:"risk"`
	Capabilities []string                `json:"capabilities,omitempty"`
}

type pluginCheckView struct {
	Valid           bool                     `json:"valid"`
	ID              string                   `json:"id"`
	Name            string                   `json:"name"`
	Version         string                   `json:"version"`
	ProtocolVersion int                      `json:"protocol_version"`
	CWD             string                   `json:"cwd"`
	StartupMS       int64                    `json:"startup_ms"`
	Capabilities    []string                 `json:"capabilities,omitempty"`
	Tools           []pluginCheckToolView    `json:"tools"`
	SupportedEvents []publicplugin.EventType `json:"supported_events"`
	Limits          map[string]int           `json:"limits,omitempty"`
	Diagnostics     string                   `json:"diagnostics,omitempty"`
}

func pluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Inspect Snow external plugins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(pluginCheckCmd())
	return cmd
}

func pluginCheckCmd() *cobra.Command {
	var asJSON bool
	var timeout time.Duration
	var cwd string
	cmd := &cobra.Command{
		Use:   "check <manifest-or-executable>",
		Short: "Start an external plugin and validate its protocol contract",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if timeout <= 0 {
				return errors.New("plugin check: timeout must be positive")
			}
			spec, err := parsePluginSpec(args[0])
			if err != nil {
				return err
			}
			if cwd == "" {
				cwd, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("plugin check: cwd: %w", err)
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			view, err := inspectExternalPlugin(ctx, spec, cwd)
			if err != nil {
				return err
			}
			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(view)
			}
			printPluginCheck(cmd, view)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "startup and validation timeout")
	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory supplied to the plugin (default current directory)")
	return cmd
}

func inspectExternalPlugin(ctx context.Context, spec publicplugin.PluginSpec, cwd string) (pluginCheckView, error) {
	var view pluginCheckView
	// An explicit check validates disabled declarations without changing their
	// stored enabled state.
	spec.Enabled = true
	started := time.Now()
	host, err := internalplugin.SpawnExternal(ctx, spec, cwd)
	if err != nil {
		return view, fmt.Errorf("plugin check %s: %w", spec.ID, err)
	}
	initialized, err := host.Initialize(ctx, version, host.WorkingDir(), "plugin-check", []string{"tools", "events"})
	startup := time.Since(started)
	if err != nil {
		_ = host.Close(context.Background())
		diagnostics := strings.TrimSpace(host.Diagnostics())
		if diagnostics != "" {
			return view, fmt.Errorf("plugin check %s: %w; diagnostics: %s", spec.ID, err, diagnostics)
		}
		return view, fmt.Errorf("plugin check %s: %w", spec.ID, err)
	}

	view = pluginCheckView{
		Valid: true, ID: initialized.Manifest.ID, Name: initialized.Manifest.Name,
		Version: initialized.Manifest.Version, ProtocolVersion: initialized.Manifest.ProtocolVersion,
		CWD: host.WorkingDir(), StartupMS: startup.Milliseconds(),
		Capabilities:    publicplugin.MergeCapabilities(initialized.Manifest.Capabilities, initialized.Capabilities, spec.Capabilities),
		SupportedEvents: append([]publicplugin.EventType(nil), initialized.SupportedEvents...),
		Limits:          cloneIntMap(initialized.Limits),
		Diagnostics:     strings.TrimSpace(host.Diagnostics()),
	}
	for _, tool := range initialized.Tools {
		risk := tool.Risk
		if risk == "" {
			risk = "exec"
		}
		view.Tools = append(view.Tools, pluginCheckToolView{
			Name: tool.Name, Description: tool.Description,
			Parameters: append(json.RawMessage(nil), tool.Parameters...),
			Discovery:  cloneToolDiscovery(tool.Discovery), Risk: risk,
			Capabilities: publicplugin.MergeCapabilities(view.Capabilities, tool.Capabilities),
		})
	}
	sort.Slice(view.Tools, func(i, j int) bool { return view.Tools[i].Name < view.Tools[j].Name })
	sort.Slice(view.SupportedEvents, func(i, j int) bool { return view.SupportedEvents[i] < view.SupportedEvents[j] })

	closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := host.Close(closeCtx); err != nil {
		diagnostics := strings.TrimSpace(host.Diagnostics())
		if diagnostics != "" {
			return view, fmt.Errorf("plugin check %s shutdown: %w; diagnostics: %s", spec.ID, err, diagnostics)
		}
		return view, fmt.Errorf("plugin check %s shutdown: %w", spec.ID, err)
	}
	view.Diagnostics = strings.TrimSpace(host.Diagnostics())
	return view, nil
}

func printPluginCheck(cmd *cobra.Command, view pluginCheckView) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Plugin: %s (%s)\n", view.ID, view.Name)
	fmt.Fprintln(out, "Status: valid")
	fmt.Fprintf(out, "Version: %s\n", view.Version)
	fmt.Fprintf(out, "Protocol: %d\n", view.ProtocolVersion)
	fmt.Fprintf(out, "Startup: %dms\n", view.StartupMS)
	fmt.Fprintf(out, "CWD: %s\n", view.CWD)
	fmt.Fprintf(out, "Capabilities (%d):\n", len(view.Capabilities))
	for _, capability := range view.Capabilities {
		fmt.Fprintf(out, "  - %s\n", capability)
	}
	fmt.Fprintf(out, "Tools (%d):\n", len(view.Tools))
	for _, tool := range view.Tools {
		discovery := string(protocol.ToolDiscoveryAlways)
		if tool.Discovery != nil && tool.Discovery.Mode != "" {
			discovery = string(tool.Discovery.Mode)
		}
		metadata := fmt.Sprintf("risk=%s, discovery=%s", tool.Risk, discovery)
		if len(tool.Capabilities) > 0 {
			metadata += ", capabilities=" + strings.Join(tool.Capabilities, ",")
		}
		fmt.Fprintf(out, "  - %s [%s]\n", tool.Name, metadata)
	}
	fmt.Fprintf(out, "Subscribed events (%d):\n", len(view.SupportedEvents))
	for _, eventType := range view.SupportedEvents {
		fmt.Fprintf(out, "  - %s\n", eventType)
	}
	fmt.Fprintf(out, "Negotiated limits (%d):\n", len(view.Limits))
	limitNames := make([]string, 0, len(view.Limits))
	for name := range view.Limits {
		limitNames = append(limitNames, name)
	}
	sort.Strings(limitNames)
	for _, name := range limitNames {
		fmt.Fprintf(out, "  - %s=%d\n", name, view.Limits[name])
	}
	if view.Diagnostics == "" {
		fmt.Fprintln(out, "Diagnostics: none")
	} else {
		fmt.Fprintln(out, "Diagnostics:")
		for _, line := range strings.Split(view.Diagnostics, "\n") {
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
}

func cloneIntMap(input map[string]int) map[string]int {
	if input == nil {
		return nil
	}
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneToolDiscovery(input *protocol.ToolDiscovery) *protocol.ToolDiscovery {
	if input == nil {
		return nil
	}
	output := *input
	output.Keywords = append([]string(nil), input.Keywords...)
	return &output
}
