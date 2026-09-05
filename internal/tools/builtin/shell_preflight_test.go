package builtin

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/tools"
)

func TestProcessStartPreflightUsesExecutionDirectory(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	manager := managedprocess.NewManager(managedprocess.Options{CWD: root})
	registry := tools.NewRegistry()
	if err := RegisterProcessTools(registry, manager, Options{Roots: []string{root}, ShellProtectedPaths: []string{filepath.Join(root, "private")}}); err != nil {
		t.Fatal(err)
	}
	tool, _ := registry.Get("process_start")
	preflight, ok := tool.(tools.PreflightTool)
	if !ok {
		t.Fatal("managed process lacks shared preflight")
	}
	got, err := preflight.Preflight(t.Context(), argsForT(map[string]any{"command": "cat private/input"}), stubHost{cwd: other, roots: []string{other}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.Capabilities, permission.CapabilityProtectedResourceAccess) {
		t.Fatal("manager execution directory or operator policy was ignored")
	}
	decision, err := (permission.DefaultPolicy{}).Evaluate(t.Context(), permission.Request{Effects: got.Effects, Capabilities: got.Capabilities})
	if err != nil || !decision.Denied {
		t.Fatal("managed process preflight did not invoke hard policy")
	}
	if len(manager.List()) != 0 {
		t.Fatal("preflight started a process")
	}
}

func TestBothShellLaunchersApplyOperatorProtection(t *testing.T) {
	root := t.TempDir()
	registry := tools.NewRegistry()
	opts := Options{ShellProtectedPaths: []string{filepath.Join(root, "private")}}
	if err := RegisterBuiltins(registry, opts); err != nil {
		t.Fatal(err)
	}
	manager := managedprocess.NewManager(managedprocess.Options{CWD: root})
	if err := RegisterProcessTools(registry, manager, opts); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bash", "process_start"} {
		tool, _ := registry.Get(name)
		got, err := tool.(tools.PreflightTool).Preflight(t.Context(), argsForT(map[string]any{"command": "printf x > private/file"}), stubHost{cwd: root})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(got.Capabilities, permission.CapabilityProtectedResourceAccess) {
			t.Fatalf("%s ignored additive protection", name)
		}
	}
}

func TestShellScopeUsesEnvironmentWithoutExposingIt(t *testing.T) {
	root := t.TempDir()
	one, err := analyzeShellEnvironment(t.Context(), "cat file", root, []string{root}, []string{"HOME=" + root, "TEST_VALUE=first"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	two, err := analyzeShellEnvironment(t.Context(), "cat file", root, []string{root}, []string{"HOME=" + root, "TEST_VALUE=second"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if one.ScopeKey == two.ScopeKey {
		t.Fatal("changed environment reused approval")
	}
	if !slices.Equal(one.Capabilities, two.Capabilities) || !slices.Equal(one.Paths, two.Paths) {
		t.Fatal("environment-only change leaked into resources")
	}
}

func TestManagedNetworkReadinessCannotBeRemembered(t *testing.T) {
	manager := managedprocess.NewManager(managedprocess.Options{CWD: t.TempDir()})
	tool := &processStartTool{manager: manager}
	got, err := tool.Preflight(t.Context(), argsForT(map[string]any{"command": "true", "readiness": map[string]any{"type": "tcp", "port": 8080}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Unknown || got.Rememberable || !slices.Contains(got.Capabilities, permission.CapabilityNetworkRead) {
		t.Fatal("readiness probe was omitted from analysis")
	}
}

func TestShellPreflightDoesNotExecuteCommand(t *testing.T) {
	root := t.TempDir()
	_, err := NewBash().Preflight(t.Context(), argsForT(map[string]any{"command": "printf changed > marker"}), stubHost{cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); !os.IsNotExist(err) {
		t.Fatal("preflight executed source")
	}
}
