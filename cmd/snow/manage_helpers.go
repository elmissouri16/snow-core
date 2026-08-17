package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	publicmcp "github.com/snow-core/snow/pkg/mcp"
)

func parseAssignments(values []string, allowBare bool) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if key == "" || strings.ContainsAny(key, " \t\r\n") {
			return nil, fmt.Errorf("invalid assignment %q", value)
		}
		if !ok {
			if !allowBare {
				return nil, fmt.Errorf("assignment %q must use NAME=VALUE", value)
			}
			val = "${" + key + "}"
		}
		out[key] = val
	}
	return out, nil
}

func sortedMCPNames(values map[string]publicmcp.ServerSpec) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func jsonRequested(cmd *cobra.Command) bool {
	if flag := cmd.Flags().Lookup("json"); flag != nil {
		value, _ := strconv.ParseBool(flag.Value.String())
		if value {
			return true
		}
	}
	mode, _ := cmd.Flags().GetString("mode")
	return mode == "json"
}

func printReceipt(cmd *cobra.Command, receipt commandReceipt) error {
	if jsonRequested(cmd) {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(receipt)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s in %s config (%s)\n", receipt.Action, receipt.Resource, receipt.Name, receipt.Scope, receipt.Path)
	if receipt.Scope == "project" {
		fmt.Fprintln(cmd.ErrOrStderr(), "project configuration is loaded only after project trust is allowed")
	}
	return nil
}

func scopeName(project bool) string {
	if project {
		return "project"
	}
	return "global"
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func formatStringMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, ", ")
}
