package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/config"
	internalmcp "github.com/snow-core/snow/internal/mcp"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/trust"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
)

func mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP server configuration and status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCPList(cmd, false)
		},
	}
	cmd.AddCommand(mcpListCmd(), mcpGetCmd(), mcpAddCmd(), mcpToggleCmd(true), mcpToggleCmd(false), mcpRemoveCmd(), mcpCheckCmd())
	return cmd
}

func mcpListCmd() *cobra.Command {
	var all bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List configured MCP servers without starting them",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCPList(cmd, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include shadowed declarations")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func runMCPList(cmd *cobra.Command, all bool) error {
	set, err := loadMCPConfig(cmd, all)
	if err != nil {
		return err
	}
	if jsonRequested(cmd) {
		return json.NewEncoder(os.Stdout).Encode(set.Views)
	}
	if len(set.Views) == 0 {
		fmt.Println("no MCP servers configured")
	} else {
		fmt.Println("NAME\tSTATE\tSCOPE\tTRANSPORT\tTARGET")
		for _, view := range set.Views {
			state := "enabled"
			if !view.Enabled {
				state = "disabled"
			}
			if view.Shadowed {
				state = "shadowed"
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", view.Name, state, view.Scope, view.Transport, view.Target)
		}
	}
	if set.ProjectBlocked {
		fmt.Fprintln(os.Stderr, "mcp: project .snow/config.json exists but is not loaded until project trust is allowed")
	}
	return nil
}

func mcpGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show one effective MCP server configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := loadMCPConfig(cmd, false)
			if err != nil {
				return err
			}
			for _, view := range set.Views {
				if view.Name != args[0] {
					continue
				}
				if jsonRequested(cmd) {
					return json.NewEncoder(os.Stdout).Encode(view)
				}
				state := "enabled"
				if !view.Enabled {
					state = "disabled"
				}
				fmt.Printf("Name: %s\nState: %s\nScope: %s\nTransport: %s\nTarget: %s\n", view.Name, state, view.Scope, view.Transport, view.Target)
				if view.CWD != "" {
					fmt.Printf("CWD: %s\n", view.CWD)
				}
				if view.TimeoutMS > 0 {
					fmt.Printf("Timeout: %dms\n", view.TimeoutMS)
				}
				fmt.Printf("Tool discovery: %s\n", defaultString(view.ToolDiscovery, "deferred"))
				if len(view.Env) > 0 {
					fmt.Printf("Environment: %s\n", formatStringMap(view.Env))
				}
				if len(view.Headers) > 0 {
					fmt.Printf("Headers: %s\n", formatStringMap(view.Headers))
				}
				return nil
			}
			return fmt.Errorf("mcp: server %q is not configured", args[0])
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func mcpAddCmd() *cobra.Command {
	var project, disabled, replace bool
	var endpoint, cwd, timeoutValue, discovery, bearerEnv string
	var envValues, headerValues []string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "add <name> [-- <command> [args...]]",
		Short: "Add an stdio or Streamable HTTP MCP server",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			spec := publicmcp.ServerSpec{ID: name, URL: endpoint, CWD: cwd, Disabled: disabled, ToolDiscovery: discovery}
			if endpoint == "" {
				if len(args) < 2 {
					return errors.New("mcp add: provide --url or a command after --")
				}
				spec.Transport, spec.Command, spec.Args = publicmcp.TransportStdio, args[1], append([]string(nil), args[2:]...)
			} else {
				spec.Transport = publicmcp.TransportStreamableHTTP
				if len(args) > 1 {
					return errors.New("mcp add: --url cannot be combined with a stdio command")
				}
			}
			var err error
			spec.Env, err = parseAssignments(envValues, true)
			if err != nil {
				return fmt.Errorf("mcp add: env: %w", err)
			}
			spec.Headers, err = parseAssignments(headerValues, false)
			if err != nil {
				return fmt.Errorf("mcp add: header: %w", err)
			}
			if bearerEnv != "" {
				if endpoint == "" {
					return errors.New("mcp add: --bearer-token-env requires --url")
				}
				if spec.Headers == nil {
					spec.Headers = map[string]string{}
				}
				spec.Headers["Authorization"] = "Bearer ${" + bearerEnv + "}"
			}
			if endpoint != "" && (len(spec.Env) > 0 || cwd != "") {
				return errors.New("mcp add: --env and --cwd are only valid for stdio servers")
			}
			if endpoint == "" && len(spec.Headers) > 0 {
				return errors.New("mcp add: --header is only valid with --url")
			}
			if timeoutValue != "" {
				timeout, err := time.ParseDuration(timeoutValue)
				maxInt := int64(^uint(0) >> 1)
				if err != nil || timeout <= 0 || timeout.Milliseconds() > maxInt {
					return fmt.Errorf("mcp add: invalid timeout %q", timeoutValue)
				}
				spec.TimeoutMS = int(timeout / time.Millisecond)
			}
			if err := spec.Validate(); err != nil {
				return err
			}
			path, global, err := mutationConfigPath(cmd, project)
			if err != nil {
				return err
			}
			if err := config.UpdateMCPServers(path, global, func(servers map[string]publicmcp.ServerSpec) error {
				if _, exists := servers[name]; exists && !replace {
					return fmt.Errorf("mcp: server %q already exists (use --replace)", name)
				}
				servers[name] = spec
				return nil
			}); err != nil {
				return err
			}
			return printReceipt(cmd, commandReceipt{Resource: "mcp", Name: name, Action: "added", Scope: scopeName(project), Path: path})
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write the current project's .snow/config.json")
	cmd.Flags().StringVar(&endpoint, "url", "", "Streamable HTTP server URL")
	cmd.Flags().StringArrayVar(&envValues, "env", nil, "stdio environment NAME or NAME=VALUE (repeatable)")
	cmd.Flags().StringArrayVar(&headerValues, "header", nil, "HTTP header NAME=VALUE (repeatable)")
	cmd.Flags().StringVar(&bearerEnv, "bearer-token-env", "", "environment variable containing an HTTP bearer token")
	cmd.Flags().StringVar(&cwd, "cwd", "", "stdio working directory")
	cmd.Flags().StringVar(&timeoutValue, "timeout", "", "connection timeout such as 15s")
	cmd.Flags().StringVar(&discovery, "discovery", "deferred", "tool discovery: deferred|always")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "add the server disabled")
	cmd.Flags().BoolVar(&replace, "replace", false, "replace an existing declaration")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func mcpToggleCmd(enable bool) *cobra.Command {
	var project, asJSON bool
	action := "disable"
	if enable {
		action = "enable"
	}
	cmd := &cobra.Command{
		Use:   action + " <name>",
		Short: strings.ToUpper(action[:1]) + action[1:] + " an MCP server without removing it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			path, global, err := mutationConfigPath(cmd, project)
			if err != nil {
				return err
			}
			var fallback publicmcp.ServerSpec
			var hasFallback bool
			if project {
				globalPath, _, _ := config.DefaultPaths()
				globalCfg, loadErr := config.Load(globalPath)
				if loadErr != nil {
					return loadErr
				}
				fallback, hasFallback = globalCfg.MCPServers[name]
			}
			if err := config.UpdateMCPServers(path, global, func(servers map[string]publicmcp.ServerSpec) error {
				spec, exists := servers[name]
				if !exists && hasFallback {
					spec, exists = fallback, true
				}
				if !exists {
					return fmt.Errorf("mcp: server %q is not declared in the target scope", name)
				}
				spec.ID = name
				spec.Disabled = !enable
				servers[name] = spec
				return nil
			}); err != nil {
				return err
			}
			return printReceipt(cmd, commandReceipt{Resource: "mcp", Name: name, Action: action + "d", Scope: scopeName(project), Path: path})
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write the current project's .snow/config.json")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func mcpRemoveCmd() *cobra.Command {
	var project, asJSON bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an MCP server declaration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			path, global, err := mutationConfigPath(cmd, project)
			if err != nil {
				return err
			}
			if err := config.UpdateMCPServers(path, global, func(servers map[string]publicmcp.ServerSpec) error {
				if _, exists := servers[name]; !exists {
					return fmt.Errorf("mcp: server %q is not declared in the target scope", name)
				}
				delete(servers, name)
				return nil
			}); err != nil {
				return err
			}
			return printReceipt(cmd, commandReceipt{Resource: "mcp", Name: name, Action: "removed", Scope: scopeName(project), Path: path})
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write the current project's .snow/config.json")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func mcpCheckCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "check [name]",
		Short: "Connect MCP servers and report negotiated live status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := loadMCPConfig(cmd, false)
			if err != nil {
				return err
			}
			var specs []publicmcp.ServerSpec
			if len(args) == 1 {
				spec, ok := set.Effective[args[0]]
				if !ok {
					return fmt.Errorf("mcp: server %q is not configured", args[0])
				}
				specs = append(specs, spec)
			} else {
				names := sortedMCPNames(set.Effective)
				for _, name := range names {
					specs = append(specs, set.Effective[name])
				}
			}
			noMCP, _ := cmd.Flags().GetBool("no-mcp")
			var statuses []publicmcp.Status
			if noMCP {
				for _, spec := range specs {
					statuses = append(statuses, publicmcp.Status{ID: spec.ID, Transport: spec.EffectiveTransport(), Message: "disabled by --no-mcp"})
				}
			} else {
				registry := tools.NewRegistry()
				manager := internalmcp.NewManager(registry, internalmcp.Options{CWD: mustCWD(), Roots: []string{mustCWD()}, HostName: "snow", HostVersion: version, MaxOutputBytes: set.Config.ToolOutputLimit()})
				manager.ConnectAll(cmd.Context(), specs)
				statuses = manager.Statuses()
				defer manager.Close()
			}
			if jsonRequested(cmd) {
				if err := json.NewEncoder(os.Stdout).Encode(statuses); err != nil {
					return err
				}
			} else if len(statuses) == 0 {
				fmt.Println("no MCP servers configured")
			} else {
				fmt.Println("NAME\tSTATUS\tPROTOCOL\tSERVER\tTOOLS\tCAPABILITIES")
				for _, status := range statuses {
					state := "failed"
					if status.Connected {
						state = "connected"
					} else if status.Message == "disabled" || strings.HasPrefix(status.Message, "disabled by") {
						state = "disabled"
					}
					server := strings.TrimSpace(status.ServerName + " " + status.ServerVersion)
					fmt.Printf("%s\t%s\t%s\t%s\t%d\t%s\n", status.ID, state, status.ProtocolVersion, server, status.ToolCount, strings.Join(status.Capabilities, ","))
					if status.Message != "" && state == "failed" {
						fmt.Fprintf(os.Stderr, "mcp %s: %s\n", status.ID, status.Message)
					}
				}
			}
			for _, status := range statuses {
				if !status.Connected && status.Message != "disabled" && !strings.HasPrefix(status.Message, "disabled by") {
					return errors.New("one or more enabled MCP servers failed")
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func skillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Inspect and manage Agent Skills",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runSkillsList(cmd) },
	}
	cmd.AddCommand(skillsListCmd(), skillsGetCmd(), skillsToggleCmd(true), skillsToggleCmd(false))
	return cmd
}

func skillsListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List discovered Agent Skills", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return runSkillsList(cmd) }}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func runSkillsList(cmd *cobra.Command) error {
	a, err := newInspectionApp(cmd, true)
	if err != nil {
		return err
	}
	defer a.Close()
	var inventory any = []any{}
	if a.Skills != nil {
		inventory = a.Skills.Inventory()
	}
	if jsonRequested(cmd) {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"skills": inventory, "diagnostics": a.SkillDiagnostics})
	}
	if a.Skills == nil || len(a.Skills.Inventory()) == 0 {
		fmt.Println("no Agent Skills discovered")
	} else {
		fmt.Println("NAME\tSTATE\tSCOPE\tSOURCE\tLOCATION")
		for _, skill := range a.Skills.Inventory() {
			state := "enabled"
			if !skill.Enabled {
				state = "disabled"
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", skill.Name, state, skill.Scope, skill.Source, skill.Location)
		}
	}
	for _, diagnostic := range a.SkillDiagnostics {
		fmt.Fprintf(os.Stderr, "skills: %s: %s\n", diagnostic.Path, diagnostic.Message)
	}
	return nil
}

func skillsGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use: "get <name>", Short: "Show one discovered Agent Skill", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newInspectionApp(cmd, true)
			if err != nil {
				return err
			}
			defer a.Close()
			if a.Skills == nil {
				return fmt.Errorf("skills: skill %q is not discovered", args[0])
			}
			skill, ok := a.Skills.Lookup(args[0])
			if !ok {
				return fmt.Errorf("skills: skill %q is not discovered", args[0])
			}
			count, truncated, resourceErr := a.Skills.ResourceSummary(skill.Name)
			view := struct {
				Skill     any    `json:"skill"`
				Resources int    `json:"resource_count"`
				Truncated bool   `json:"resources_truncated,omitempty"`
				Error     string `json:"resource_error,omitempty"`
			}{Skill: skill, Resources: count, Truncated: truncated}
			if resourceErr != nil {
				view.Error = resourceErr.Error()
			}
			if jsonRequested(cmd) {
				return json.NewEncoder(os.Stdout).Encode(view)
			}
			state := "enabled"
			if !skill.Enabled {
				state = "disabled"
			}
			fmt.Printf("Name: %s\nState: %s\nScope: %s\nSource: %s\nLocation: %s\nDescription: %s\nResources: %d\n", skill.Name, state, skill.Scope, skill.Source, skill.Location, skill.Description, count)
			if skill.DisabledBy != "" {
				fmt.Printf("Disabled by: %s\n", skill.DisabledBy)
			}
			if resourceErr != nil {
				fmt.Printf("Resource error: %s\n", resourceErr)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func skillsToggleCmd(enable bool) *cobra.Command {
	var project, all, asJSON bool
	action := "disable"
	if enable {
		action = "enable"
	}
	cmd := &cobra.Command{
		Use: action + " [name]", Short: strings.ToUpper(action[:1]) + action[1:] + " an Agent Skill without changing its files", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all == (len(args) == 1) {
				return fmt.Errorf("skills %s: provide exactly one name or --all", action)
			}
			path, _, err := mutationConfigPath(cmd, project)
			if err != nil {
				return err
			}
			name := "all"
			if len(args) == 1 {
				name = args[0]
			}
			if project {
				err = config.UpdateProjectSkills(path, func(skills *config.ProjectSkillsConfig) error {
					if all {
						value := !enable
						skills.Disabled = &value
					} else {
						skills.Overrides[name] = enable
					}
					return nil
				})
			} else {
				err = config.UpdateSkills(path, func(skills *config.SkillsConfig) error {
					if all {
						skills.Disabled = !enable
					} else {
						skills.Overrides[name] = enable
					}
					return nil
				})
			}
			if err != nil {
				return err
			}
			return printReceipt(cmd, commandReceipt{Resource: "skill", Name: name, Action: action + "d", Scope: scopeName(project), Path: path})
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write the current project's .snow/config.json")
	cmd.Flags().BoolVar(&all, "all", false, "apply to every skill in the target scope")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = asJSON
	return cmd
}

func newInspectionApp(cmd *cobra.Command, skillsOnly bool) (*app.App, error) {
	opts, err := buildOptions(cmd)
	if err != nil {
		return nil, err
	}
	opts.NoSession, opts.Provider, opts.Thinking, opts.NoPlugins = true, "fake", "off", true
	if skillsOnly {
		opts.NoMCP = true
	}
	return app.New(cmd.Context(), opts)
}

func loadMCPConfig(cmd *cobra.Command, includeShadowed bool) (mcpConfigSet, error) {
	configPath, _, _ := config.DefaultPaths()
	if override, _ := cmd.Flags().GetString("config"); override != "" {
		configPath = override
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return mcpConfigSet{}, err
	}
	cwd := mustCWD()
	projectPath := filepath.Join(cwd, ".snow", "config.json")
	_, _, trustPath := config.DefaultPaths()
	store, err := trust.New(trustPath)
	if err != nil {
		return mcpConfigSet{}, err
	}
	resolution, err := trust.Resolve(cwd, cfg.DefaultProjectTrust, store)
	if err != nil {
		return mcpConfigSet{}, err
	}
	projectAllowed := !resolution.Prompt && resolution.Level == trust.LevelAllow
	projectBlocked := false
	projectServers := map[string]publicmcp.ServerSpec{}
	if projectAllowed {
		extensions, err := config.LoadProjectExtensions(projectPath)
		if err != nil {
			return mcpConfigSet{}, err
		}
		projectServers = extensions.MCPServers
	} else if _, err := os.Stat(projectPath); err == nil {
		projectBlocked = true
	}

	effective := make(map[string]publicmcp.ServerSpec, len(cfg.MCPServers)+len(projectServers))
	scopes := make(map[string]string, len(effective))
	for name, spec := range cfg.MCPServers {
		spec.ID = defaultString(spec.ID, name)
		effective[name], scopes[name] = spec, "global"
	}
	var shadowed []mcpConfigView
	for name, spec := range projectServers {
		if prior, exists := effective[name]; exists && includeShadowed {
			shadowed = append(shadowed, newMCPView(name, "global", prior, true))
		}
		spec.ID = defaultString(spec.ID, name)
		effective[name], scopes[name] = spec, "project"
	}
	if values, _ := cmd.Flags().GetStringArray("mcp"); len(values) > 0 {
		for _, value := range values {
			specs, err := parseMCPSpecs(value)
			if err != nil {
				return mcpConfigSet{}, err
			}
			for _, spec := range specs {
				effective[spec.ID], scopes[spec.ID] = spec, "explicit"
			}
		}
	}
	views := append([]mcpConfigView(nil), shadowed...)
	for _, name := range sortedMCPNames(effective) {
		views = append(views, newMCPView(name, scopes[name], effective[name], false))
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Name == views[j].Name {
			return views[i].Shadowed
		}
		return views[i].Name < views[j].Name
	})
	return mcpConfigSet{Views: views, Effective: effective, ProjectBlocked: projectBlocked, Config: cfg}, nil
}

func newMCPView(name, scope string, spec publicmcp.ServerSpec, shadowed bool) mcpConfigView {
	view := mcpConfigView{Name: name, Enabled: !spec.Disabled, Scope: scope, Transport: spec.EffectiveTransport(), Command: spec.Command, Args: redactArgs(spec.Args), URL: redactURL(spec.URL), CWD: spec.CWD, Env: redactConfigMap(spec.Env, false), Headers: redactConfigMap(spec.Headers, true), TimeoutMS: spec.TimeoutMS, ToolDiscovery: spec.ToolDiscovery, Shadowed: shadowed, spec: spec}
	if spec.EffectiveTransport() == publicmcp.TransportStdio {
		view.Target = strings.TrimSpace(strings.Join(append([]string{spec.Command}, view.Args...), " "))
	} else {
		view.Target = view.URL
	}
	if spec.Disabled {
		view.DisabledBy = "configuration"
	}
	return view
}

func redactArgs(values []string) []string {
	out := append([]string(nil), values...)
	redactNext := false
	headerNext := false
	for i, value := range out {
		if redactNext {
			out[i] = "[redacted]"
			redactNext = false
			continue
		}
		if headerNext {
			out[i] = redactHeaderArgument(value)
			headerNext = false
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "-h" || lower == "--header" {
			headerNext = true
			continue
		}
		if key, val, ok := strings.Cut(value, "="); ok {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if lowerKey == "-h" || lowerKey == "--header" {
				if redactHeaderArgument(val) != val {
					out[i] = key + "=[redacted]"
				}
				continue
			}
			if sensitiveArgumentName(lowerKey) {
				out[i] = key + "=[redacted]"
				continue
			}
		}
		if redactHeaderArgument(value) != value || strings.HasPrefix(lower, "bearer ") {
			out[i] = "[redacted]"
			continue
		}
		if strings.HasPrefix(value, "-") && sensitiveArgumentName(lower) {
			redactNext = true
		}
	}
	return out
}

func sensitiveArgumentName(value string) bool {
	value = strings.TrimLeft(strings.ToLower(value), "-/")
	for _, marker := range []string{"token", "secret", "password", "passwd", "api-key", "apikey", "auth", "credential", "cookie", "private-key", "access-key", "client-key", "key-file", "key-path"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return value == "key"
}

func redactHeaderArgument(value string) string {
	name, rest, ok := strings.Cut(value, ":")
	if !ok {
		return value
	}
	lowerName := strings.ToLower(strings.TrimSpace(name))
	lowerValue := strings.ToLower(strings.TrimSpace(rest))
	if sensitiveArgumentName(lowerName) || strings.Contains(lowerName, "authorization") || strings.Contains(lowerName, "cookie") || strings.HasPrefix(lowerValue, "bearer ") {
		return name + ": [redacted]"
	}
	return value
}

func redactConfigMap(values map[string]string, headers bool) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		lower := strings.ToLower(key)
		sensitive := !headers || strings.Contains(lower, "authorization") || strings.Contains(lower, "cookie") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "key")
		if sensitive && !(strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}")) && !strings.HasPrefix(value, "Bearer ${") {
			out[key] = "[redacted]"
		} else {
			out[key] = value
		}
	}
	return out
}

func redactURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return value
	}
	if parsed.User != nil {
		parsed.User = url.User("[redacted]")
	}
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "key") || strings.Contains(lower, "auth") {
			query.Set(key, "[redacted]")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func mutationConfigPath(cmd *cobra.Command, project bool) (path string, global bool, err error) {
	configured, _ := cmd.Flags().GetString("config")
	if project {
		if configured != "" {
			return "", false, errors.New("--project cannot be combined with --config")
		}
		return filepath.Join(mustCWD(), ".snow", "config.json"), false, nil
	}
	if configured != "" {
		return configured, true, nil
	}
	path, _, _ = config.DefaultPaths()
	return path, true, nil
}
