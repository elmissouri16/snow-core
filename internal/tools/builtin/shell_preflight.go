package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/shellanalysis"
	"github.com/elmissouri16/snow-core/internal/tools"
)

func analyzeHostShell(ctx context.Context, command string, host tools.ToolHost, protected []string) (permission.Analysis, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return permission.Analysis{}, err
	}
	env := os.Environ()
	roots := []string{cwd}
	if host != nil {
		if host.CWD() != "" {
			cwd = host.CWD()
			roots = []string{cwd}
		}
		if len(host.Roots()) > 0 {
			roots = host.Roots()
		}
		if host.Environ() != nil {
			env = host.Environ()
		}
	}
	return analyzeShellEnvironment(ctx, command, cwd, roots, env, protected)
}

func analyzeShellEnvironment(ctx context.Context, command, cwd string, roots, env, protected []string) (permission.Analysis, error) {
	home := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "HOME="); ok {
			home = value
		}
	}
	analysis, err := shellanalysis.AnalyzeWithOptions(ctx, command, cwd, roots, home, shellanalysis.Options{ProtectedPaths: protected})
	if err != nil {
		return permission.Analysis{}, err
	}
	// Environment data never enters effects or logs. Keep approval reuse tied
	// to the launch environment using length-delimited hash input only.
	hash := sha256.New()
	fmt.Fprintf(hash, "%d:%s", len(analysis.ScopeKey), analysis.ScopeKey)
	for _, entry := range env {
		fmt.Fprintf(hash, "%d:%s", len(entry), entry)
	}
	analysis.ScopeKey = fmt.Sprintf("%x", hash.Sum(nil))
	return analysis, nil
}

var _ tools.PreflightTool = (*processStartTool)(nil)

func (t *processStartTool) Preflight(ctx context.Context, args json.RawMessage, _ tools.ToolHost) (permission.Analysis, error) {
	var input processStartArgs
	if err := jsonv2.Unmarshal(args, &input); err != nil {
		return permission.Analysis{}, fmt.Errorf("process_start: invalid arguments: %w", err)
	}
	if input.Command == "" {
		return permission.Analysis{}, fmt.Errorf("process_start: command is required")
	}
	cwd := t.manager.WorkingDirectory()
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return permission.Analysis{}, err
		}
	}
	roots := t.roots
	if len(roots) == 0 {
		roots = []string{cwd}
	}
	// The manager inherits Snow's environment, independently of ToolHost.
	analysis, err := analyzeShellEnvironment(ctx, input.Command, cwd, roots, os.Environ(), t.protectedPaths)
	if err != nil {
		return permission.Analysis{}, err
	}
	if input.Readiness != nil && input.Readiness.Type != "log" {
		analysis.Unknown = true
		analysis.Rememberable = false
		analysis.Capabilities = append(analysis.Capabilities, permission.CapabilityNetworkRead, permission.CapabilityUnknown)
		slices.Sort(analysis.Capabilities)
		analysis.Capabilities = slices.Compact(analysis.Capabilities)
		analysis.Effects = append(analysis.Effects, permission.Effect{Type: "network", Capability: permission.CapabilityNetworkRead, Operation: "readiness probe", Reason: "runtime readiness network probe", Confidence: "low", Dynamic: true})
	}
	return analysis, nil
}
