package main

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/elmissouri16/snow-core/internal/config"
	internalplugin "github.com/elmissouri16/snow-core/internal/plugin"
	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/spf13/cobra"
)

func pluginCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "plugin", Short: "Manage local JavaScript plugin registrations", Args: cobra.NoArgs}
	cmd.AddCommand(pluginInspectCmd("list"), pluginInspectCmd("get"), pluginInspectCmd("check"))
	cmd.AddCommand(pluginRunCmd(), pluginInitCmd())
	for _, action := range []string{"add", "enable", "disable", "remove"} {
		cmd.AddCommand(pluginMutateCmd(action))
	}
	return cmd
}

type pluginView struct {
	ID       string               `json:"id"`
	Scope    string               `json:"scope"`
	Path     string               `json:"path"`
	Disabled bool                 `json:"disabled"`
	Manifest *javascript.Manifest `json:"manifest,omitempty"`
	Error    string               `json:"error,omitempty"`
}

func pluginInspectCmd(action string) *cobra.Command {
	args := cobra.ExactArgs(1)
	use := action + " <id>"
	if action == "list" {
		args = cobra.NoArgs
		use = action
	}
	cmd := &cobra.Command{Use: use, Short: map[string]string{"list": "List JavaScript plugins without executing scripts", "get": "Inspect a JavaScript package without executing it", "check": "Validate and initialize a plugin with host I/O disabled"}[action], Args: args,
		RunE: func(cmd *cobra.Command, args []string) error {
			declarations, err := loadPluginDeclarations(cmd)
			if err != nil {
				return err
			}
			views := []pluginView{}
			for _, d := range declarations {
				if action != "list" && d.ID != args[0] {
					continue
				}
				view := pluginView{ID: d.ID, Scope: d.Scope, Path: d.Path, Disabled: d.Disabled}
				p, err := javascript.ReadPackage(cmd.Context(), d.Path, d.Root, d.Spec.Config)
				if err == nil && p.Manifest.ID != d.ID {
					err = fmt.Errorf("manifest ID %q does not match %q", p.Manifest.ID, d.ID)
				}
				if err != nil {
					if action != "list" {
						return err
					}
					view.Error = err.Error()
				} else {
					view.Manifest = &p.Manifest
				}
				if action == "check" {
					if disabled, _ := cmd.Flags().GetBool("no-plugins"); disabled {
						return errors.New("plugin check cannot run with --no-plugins")
					}
					if err := checkJavaScript(cmd.Context(), p); err != nil {
						return err
					}
				}
				views = append(views, view)
			}
			if action != "list" && len(views) == 0 {
				return fmt.Errorf("JavaScript plugin %q not found in allowed configuration scopes", args[0])
			}
			if jsonRequested(cmd) {
				if action == "get" {
					return jsonv2.MarshalWrite(cmd.OutOrStdout(), views[0])
				}
				return jsonv2.MarshalWrite(cmd.OutOrStdout(), views)
			}
			if len(views) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no JavaScript plugins configured")
			}
			for _, v := range views {
				state := "enabled"
				if v.Disabled {
					state = "disabled"
				}
				if action == "check" {
					state = "checked (host I/O disabled)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%q\n", v.ID, state, v.Scope, v.Path)
				if v.Error != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %q\n", v.ID, v.Error)
				}
				if action == "get" && v.Manifest != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "name=%q version=%q entry=%q host_tools=%q\n", v.Manifest.Name, v.Manifest.Version, v.Manifest.Entry, v.Manifest.HostTools)
				}
			}
			return nil
		}}
	cmd.Flags().Bool("json", false, "output JSON")
	return cmd
}
func checkJavaScript(ctx context.Context, p *javascript.Package) error {
	manager := internalplugin.NewManager(tools.NewRegistry())
	runtime := javascript.New(p, javascript.Options{})
	if err := manager.LoadJavaScript(runtime, p.Fingerprint); err != nil {
		return err
	}
	initErr := manager.Initialize(ctx)
	return errors.Join(initErr, manager.Close(ctx))
}

func pluginMutateCmd(action string) *cobra.Command {
	var project bool
	use := action + " <id>"
	if action == "add" {
		use = action + " <directory>"
	}
	cmd := &cobra.Command{Use: use, Short: action + " a local JavaScript plugin registration", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, global, err := mutationConfigPath(cmd, project)
		if err != nil {
			return err
		}
		var pkg *javascript.Package
		name := args[0]
		if action == "add" {
			dir, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			root := ""
			if project {
				root, err = filepath.EvalSymlinks(mustCWD())
				if err != nil {
					return err
				}
			}
			pkg, err = javascript.ReadPackage(cmd.Context(), dir, root, nil)
			if err != nil {
				return err
			}
			name = pkg.Manifest.ID
		} else if err := plugin.ValidateIdentifier("plugin id", name); err != nil {
			return err
		}
		err = config.UpdateJavaScriptPlugins(path, global, func(specs map[string]plugin.JavaScriptSpec) error {
			previous, exists := specs[name]
			if action == "add" {
				if exists {
					return fmt.Errorf("plugin %s already exists in %s configuration", name, scopeName(project))
				}
				target := pkg.Path
				if project {
					root, err := filepath.EvalSymlinks(mustCWD())
					if err != nil {
						return err
					}
					target, err = filepath.Rel(root, target)
					if err != nil {
						return err
					}
				}
				specs[name] = plugin.JavaScriptSpec{Path: target}
				return nil
			}
			if !exists {
				return fmt.Errorf("plugin %s is not registered in %s configuration", name, scopeName(project))
			}
			switch action {
			case "remove":
				delete(specs, name)
			case "enable":
				previous.Disabled = false
				specs[name] = previous
			case "disable":
				previous.Disabled = true
				specs[name] = previous
			}
			return nil
		})
		if err != nil {
			return err
		}
		return printReceipt(cmd, commandReceipt{Action: action, Resource: "JavaScript plugin", Name: name, Scope: scopeName(project), Path: path})
	}}
	cmd.Flags().BoolVar(&project, "project", false, "edit project configuration; does not grant project trust")
	cmd.Flags().Bool("json", false, "output JSON")
	return cmd
}

func loadPluginDeclarations(cmd *cobra.Command) ([]javascript.Declaration, error) {
	path, _, trustPath := config.DefaultPaths()
	if override, _ := cmd.Flags().GetString("config"); override != "" {
		path = override
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	store, err := trust.New(trustPath)
	if err != nil {
		return nil, err
	}
	resolution, err := trust.Resolve(mustCWD(), cfg.DefaultProjectTrust, store)
	if err != nil {
		return nil, err
	}
	project := map[string]plugin.JavaScriptSpec{}
	if !resolution.Prompt && resolution.Level == trust.LevelAllow {
		extensions, err := config.LoadProjectExtensions(filepath.Join(resolution.Path, ".snow", "config.json"))
		if err != nil {
			return nil, err
		}
		project = extensions.JavaScriptPlugins
	}
	explicit := map[string]plugin.JavaScriptSpec{}
	paths, _ := cmd.Flags().GetStringArray("js-plugin")
	for _, entry := range paths {
		p, err := javascript.ReadPackage(cmd.Context(), entry, "", nil)
		if err != nil {
			return nil, err
		}
		if _, ok := explicit[p.Manifest.ID]; ok {
			return nil, fmt.Errorf("duplicate explicit JavaScript plugin %s", p.Manifest.ID)
		}
		explicit[p.Manifest.ID] = plugin.JavaScriptSpec{Path: p.Path}
	}
	return javascript.Resolve(cfg.JavaScriptPlugins, project, explicit, filepath.Dir(path), resolution.Path, mustCWD())
}
