package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/elmissouri16/snow-core/internal/pluginsdk"
)

func pluginSDKCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sdk",
		Short: "Manage embedded private plugin SDK assets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(pluginSDKVendorCmd())
	return cmd
}

func pluginSDKVendorCmd() *cobra.Command {
	var runtimeName string
	var replace, asJSON bool
	cmd := &cobra.Command{
		Use:   "vendor --runtime <python|javascript> <plugin-directory>",
		Short: "Copy an embedded private SDK into a plugin directory without network access",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtimeName == "" {
				return errors.New("plugin sdk vendor: --runtime is required")
			}
			runtime, err := pluginsdk.ParseRuntime(runtimeName)
			if err != nil {
				return err
			}
			receipt, err := pluginsdk.Vendor(pluginsdk.Options{
				Runtime: runtime, Destination: args[0], Replace: replace, HostVersion: version,
			})
			if err != nil {
				return err
			}
			if jsonRequested(cmd) {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(receipt)
			}
			printPluginSDKVendorReceipt(cmd, receipt)
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimeName, "runtime", "", "SDK runtime: python or javascript")
	cmd.Flags().BoolVar(&replace, "replace", false, "replace an existing vendored SDK through staged renames after review")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func printPluginSDKVendorReceipt(cmd *cobra.Command, receipt pluginsdk.Receipt) {
	action := "Vendored"
	if receipt.Replaced {
		action = "Replaced"
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s %s plugin SDK %s\n", action, terminalSafe(string(receipt.Runtime)), terminalSafe(receipt.SDKVersion))
	fmt.Fprintf(out, "Destination: %s\n", terminalSafe(receipt.Destination))
	fmt.Fprintf(out, "Files (%d):\n", len(receipt.Files))
	for _, file := range receipt.Files {
		fmt.Fprintf(out, "  %s  %s  %d bytes\n", terminalSafe(file.SHA256), terminalSafe(file.Path), file.Bytes)
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "SDK files were copied but not executed; review the destination and hashes before `snow plugin check`")
}
