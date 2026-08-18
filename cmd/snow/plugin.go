package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/snow-core/snow/internal/config"
	internalplugin "github.com/snow-core/snow/internal/plugin"
	"github.com/snow-core/snow/internal/trust"
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

type pluginConfigView struct {
	ID               string   `json:"id"`
	Enabled          bool     `json:"enabled"`
	Scope            string   `json:"scope"`
	Target           string   `json:"target"`
	Command          []string `json:"command"`
	CWD              string   `json:"cwd,omitempty"`
	Env              []string `json:"env,omitempty"`
	TimeoutMS        int      `json:"timeout_ms,omitempty"`
	MaxFrameBytes    int      `json:"max_frame_bytes,omitempty"`
	MaxOutputBytes   int      `json:"max_output_bytes,omitempty"`
	MaxProgressBytes int      `json:"max_progress_bytes,omitempty"`
	MaxInputBytes    int      `json:"max_input_bytes,omitempty"`
	MaxConcurrent    int      `json:"max_concurrent,omitempty"`
	Capabilities     []string `json:"capabilities,omitempty"`
	ConfigPresent    bool     `json:"config_present"`
	Shadowed         bool     `json:"shadowed,omitempty"`
	DisabledBy       string   `json:"disabled_by,omitempty"`
}

type pluginConfigSet struct {
	Views          []pluginConfigView
	ProjectBlocked bool
}

func pluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage Snow external plugins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPluginList(cmd, false)
		},
	}
	cmd.AddCommand(pluginListCmd(), pluginGetCmd(), pluginAddCmd(), pluginToggleCmd(true), pluginToggleCmd(false), pluginRemoveCmd(), pluginCheckCmd(), pluginSDKCmd())
	return cmd
}

func pluginListCmd() *cobra.Command {
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List configured plugins without starting them",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPluginList(cmd, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include shadowed declarations")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func runPluginList(cmd *cobra.Command, all bool) error {
	set, err := loadPluginConfig(cmd, all)
	if err != nil {
		return err
	}
	if jsonRequested(cmd) {
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(set.Views); err != nil {
			return err
		}
	} else if len(set.Views) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no plugins configured")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "ID\tSTATE\tSCOPE\tTARGET")
		for _, view := range set.Views {
			state := "enabled"
			if !view.Enabled {
				state = "disabled"
			}
			if view.Shadowed {
				state = "shadowed"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", terminalSafe(view.ID), state, terminalSafe(view.Scope), terminalSafe(view.Target))
		}
	}
	if set.ProjectBlocked {
		fmt.Fprintln(cmd.ErrOrStderr(), "plugin: project .snow/config.json exists but is not loaded until project trust is allowed")
	}
	return nil
}

func pluginGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show one effective plugin declaration without starting it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := loadPluginConfig(cmd, false)
			if err != nil {
				return err
			}
			for _, view := range set.Views {
				if view.ID != args[0] {
					continue
				}
				if jsonRequested(cmd) {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
				}
				state := "enabled"
				if !view.Enabled {
					state = "disabled"
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "ID: %s\nState: %s\nScope: %s\nCommand: %s\n", terminalSafe(view.ID), state, terminalSafe(view.Scope), terminalSafe(view.Target))
				if view.CWD != "" {
					fmt.Fprintf(out, "CWD: %s\n", terminalSafe(view.CWD))
				}
				if len(view.Capabilities) > 0 {
					fmt.Fprintf(out, "Capabilities: %s\n", terminalSafe(strings.Join(view.Capabilities, ", ")))
				}
				fmt.Fprintf(out, "Config: %s\n", map[bool]string{true: "set", false: "none"}[view.ConfigPresent])
				return nil
			}
			if set.ProjectBlocked {
				return fmt.Errorf("plugin: plugin %q is not configured in visible scopes; project configuration is trust-blocked", args[0])
			}
			return fmt.Errorf("plugin: plugin %q is not configured", args[0])
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func pluginAddCmd() *cobra.Command {
	var project, replace, enable, asJSON bool
	cmd := &cobra.Command{
		Use:   "add <manifest-or-executable>",
		Short: "Register a plugin declaration without starting it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := parsePluginSpec(args[0])
			if err != nil {
				return err
			}
			// Registration is deliberately staged disabled unless the operator
			// explicitly combines add with --enable.
			spec.Enabled = enable
			if err := publicplugin.ValidateSpec(spec); err != nil {
				return err
			}
			path, global, err := pluginMutationPath(cmd, project)
			if err != nil {
				return err
			}
			if err := config.AddPlugin(path, global, spec, replace); err != nil {
				return err
			}
			action := "added"
			if replace {
				action = "replaced"
			}
			return printPluginReceipt(cmd, commandReceipt{Resource: "plugin", Name: spec.ID, Action: action, Scope: scopeName(project), Path: path}, spec.Enabled)
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write the canonical project's .snow/config.json")
	cmd.Flags().BoolVar(&replace, "replace", false, "replace an existing declaration in the target scope")
	cmd.Flags().BoolVar(&enable, "enable", false, "enable the declaration for the next Snow launch")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func pluginToggleCmd(enable bool) *cobra.Command {
	action := "disable"
	if enable {
		action = "enable"
	}
	var project, asJSON bool
	cmd := &cobra.Command{
		Use:   action + " <id>",
		Short: strings.ToUpper(action[:1]) + action[1:] + " a plugin declaration for the next launch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			path, global, err := pluginMutationPath(cmd, project)
			if err != nil {
				return err
			}
			if err := config.SetPluginEnabled(path, global, id, enable); err != nil {
				return err
			}
			return printPluginReceipt(cmd, commandReceipt{Resource: "plugin", Name: id, Action: action + "d", Scope: scopeName(project), Path: path}, enable)
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write the canonical project's .snow/config.json")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func pluginRemoveCmd() *cobra.Command {
	var project, asJSON bool
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a plugin declaration from one configuration scope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, global, err := pluginMutationPath(cmd, project)
			if err != nil {
				return err
			}
			if err := config.RemovePlugin(path, global, args[0]); err != nil {
				return err
			}
			return printPluginReceipt(cmd, commandReceipt{Resource: "plugin", Name: args[0], Action: "removed", Scope: scopeName(project), Path: path}, false)
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write the canonical project's .snow/config.json")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func loadPluginConfig(cmd *cobra.Command, includeShadowed bool) (pluginConfigSet, error) {
	globalPath, _, _ := config.DefaultPaths()
	if override, _ := cmd.Flags().GetString("config"); override != "" {
		globalPath = override
	}
	cfg, err := config.Load(globalPath)
	if err != nil {
		return pluginConfigSet{}, err
	}
	globalSpecs, err := config.LoadPluginDeclarations(globalPath)
	if err != nil {
		return pluginConfigSet{}, err
	}
	cwd := mustCWD()
	_, _, trustPath := config.DefaultPaths()
	store, err := trust.New(trustPath)
	if err != nil {
		return pluginConfigSet{}, err
	}
	resolution, err := trust.Resolve(cwd, cfg.DefaultProjectTrust, store)
	if err != nil {
		return pluginConfigSet{}, err
	}
	projectPath := filepath.Join(resolution.Path, ".snow", "config.json")
	projectAllowed := !resolution.Prompt && resolution.Level == trust.LevelAllow
	projectBlocked := false
	var projectSpecs []publicplugin.PluginSpec
	if projectAllowed {
		projectSpecs, err = config.LoadPluginDeclarations(projectPath)
		if err != nil {
			return pluginConfigSet{}, err
		}
	} else if _, statErr := os.Stat(projectPath); statErr == nil {
		projectBlocked = true
	}
	var explicitSpecs []publicplugin.PluginSpec
	if values, _ := cmd.Flags().GetStringArray("plugin"); len(values) > 0 {
		for _, value := range values {
			spec, parseErr := parsePluginSpec(value)
			if parseErr != nil {
				return pluginConfigSet{}, parseErr
			}
			if err := publicplugin.ValidateSpec(spec); err != nil {
				return pluginConfigSet{}, err
			}
			explicitSpecs = append(explicitSpecs, spec)
		}
	}

	effectiveViews := make(map[string]pluginConfigView)
	var shadowed []pluginConfigView
	merge := func(scope string, specs []publicplugin.PluginSpec) {
		for _, spec := range specs {
			if prior, exists := effectiveViews[spec.ID]; exists && includeShadowed {
				prior.Shadowed = true
				shadowed = append(shadowed, prior)
			}
			effectiveViews[spec.ID] = newPluginConfigView(scope, spec)
		}
	}
	merge("global", globalSpecs)
	merge("project", projectSpecs)
	merge("explicit", explicitSpecs)
	views := append([]pluginConfigView(nil), shadowed...)
	for _, view := range effectiveViews {
		views = append(views, view)
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].ID != views[j].ID {
			return views[i].ID < views[j].ID
		}
		return pluginScopeRank(views[i].Scope) < pluginScopeRank(views[j].Scope)
	})
	return pluginConfigSet{Views: views, ProjectBlocked: projectBlocked}, nil
}

func newPluginConfigView(scope string, spec publicplugin.PluginSpec) pluginConfigView {
	command := redactArgs(spec.Command)
	view := pluginConfigView{
		ID: spec.ID, Enabled: spec.Enabled, Scope: scope, Command: command,
		CWD: spec.CWD, Env: redactPluginEnv(spec.Env), TimeoutMS: spec.TimeoutMS,
		MaxFrameBytes: spec.MaxFrameBytes, MaxOutputBytes: spec.MaxOutputBytes,
		MaxProgressBytes: spec.MaxProgressBytes, MaxInputBytes: spec.MaxInputBytes,
		MaxConcurrent: spec.MaxConcurrent, Capabilities: append([]string(nil), spec.Capabilities...),
		ConfigPresent: len(spec.Config) > 0,
	}
	view.Target = strings.Join(command, " ")
	if !spec.Enabled {
		view.DisabledBy = "configuration"
	}
	return view
}

func redactPluginEnv(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		key, _, ok := strings.Cut(value, "=")
		if !ok {
			key = value
		}
		out[i] = key + "=[redacted]"
	}
	return out
}

func pluginScopeRank(scope string) int {
	switch scope {
	case "global":
		return 0
	case "project":
		return 1
	case "explicit":
		return 2
	default:
		return 3
	}
}

func pluginMutationPath(cmd *cobra.Command, project bool) (path string, global bool, err error) {
	configured, _ := cmd.Flags().GetString("config")
	if project {
		if configured != "" {
			return "", false, errors.New("--project cannot be combined with --config")
		}
		canonical, err := trust.CanonicalPath(mustCWD())
		if err != nil {
			return "", false, err
		}
		return filepath.Join(canonical, ".snow", "config.json"), false, nil
	}
	if configured != "" {
		return configured, true, nil
	}
	path, _, _ = config.DefaultPaths()
	return path, true, nil
}

func printPluginReceipt(cmd *cobra.Command, receipt commandReceipt, enabled bool) error {
	if err := printReceipt(cmd, receipt); err != nil {
		return err
	}
	if !jsonRequested(cmd) {
		if enabled {
			fmt.Fprintln(cmd.ErrOrStderr(), "plugin is enabled and will execute with user OS privileges on the next Snow launch; restart required")
		} else if receipt.Action != "removed" {
			fmt.Fprintln(cmd.ErrOrStderr(), "plugin remains disabled; review it, run `snow plugin check`, then enable explicitly; restart required")
		} else {
			fmt.Fprintln(cmd.ErrOrStderr(), "restart required for the running Snow process to observe this change")
		}
	}
	return nil
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
			return view, fmt.Errorf("plugin check %s: %w; diagnostics: %s", spec.ID, err, terminalSafe(diagnostics))
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
			return view, fmt.Errorf("plugin check %s shutdown: %w; diagnostics: %s", spec.ID, err, terminalSafe(diagnostics))
		}
		return view, fmt.Errorf("plugin check %s shutdown: %w", spec.ID, err)
	}
	view.Diagnostics = strings.TrimSpace(host.Diagnostics())
	return view, nil
}

func printPluginCheck(cmd *cobra.Command, view pluginCheckView) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Plugin: %s (%s)\n", terminalSafe(view.ID), terminalSafe(view.Name))
	fmt.Fprintln(out, "Status: valid")
	fmt.Fprintf(out, "Version: %s\n", terminalSafe(view.Version))
	fmt.Fprintf(out, "Protocol: %d\n", view.ProtocolVersion)
	fmt.Fprintf(out, "Startup: %dms\n", view.StartupMS)
	fmt.Fprintf(out, "CWD: %s\n", terminalSafe(view.CWD))
	fmt.Fprintf(out, "Capabilities (%d):\n", len(view.Capabilities))
	for _, capability := range view.Capabilities {
		fmt.Fprintf(out, "  - %s\n", terminalSafe(capability))
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
		fmt.Fprintf(out, "  - %s [%s]\n", terminalSafe(tool.Name), terminalSafe(metadata))
	}
	fmt.Fprintf(out, "Subscribed events (%d):\n", len(view.SupportedEvents))
	for _, eventType := range view.SupportedEvents {
		fmt.Fprintf(out, "  - %s\n", terminalSafe(string(eventType)))
	}
	fmt.Fprintf(out, "Negotiated limits (%d):\n", len(view.Limits))
	limitNames := make([]string, 0, len(view.Limits))
	for name := range view.Limits {
		limitNames = append(limitNames, name)
	}
	sort.Strings(limitNames)
	for _, name := range limitNames {
		fmt.Fprintf(out, "  - %s=%d\n", terminalSafe(name), view.Limits[name])
	}
	if view.Diagnostics == "" {
		fmt.Fprintln(out, "Diagnostics: none")
	} else {
		fmt.Fprintln(out, "Diagnostics:")
		for _, line := range strings.Split(view.Diagnostics, "\n") {
			fmt.Fprintf(out, "  %s\n", terminalSafe(line))
		}
	}
}

func terminalSafe(value string) string {
	var b strings.Builder
	for _, r := range value {
		if !unicode.IsControl(r) {
			b.WriteRune(r)
			continue
		}
		if r <= 0xff {
			fmt.Fprintf(&b, `\x%02x`, r)
		} else {
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.String()
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
