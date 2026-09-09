package main

import (
	"fmt"
	"path/filepath"

	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/spf13/cobra"
)

func pluginInitCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "init <directory>", Short: "Scaffold a local JavaScript extension", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			id = filepath.Base(filepath.Clean(args[0]))
		}
		ts, _ := cmd.Flags().GetBool("typescript")
		if err := javascript.Scaffold(args[0], id, ts); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s. Register with: snow plugin add %s\n", args[0], args[0])
		return nil
	}}
	cmd.Flags().String("id", "", "plugin identifier (defaults to directory name)")
	cmd.Flags().Bool("typescript", false, "include TypeScript source and single-bundle build configuration")
	return cmd
}
