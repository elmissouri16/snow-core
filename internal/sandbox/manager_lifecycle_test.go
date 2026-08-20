package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestManagerPersistentDiskPrerequisiteGuardsBootPaths(t *testing.T) {
	prerequisiteErr := errors.New("mkfs.ext4 unavailable")
	project := t.TempDir()
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	blockedLauncher := &prerequisiteLauncher{
		fakeLauncher: &fakeLauncher{path: "/opt/smolvm"},
		err:          prerequisiteErr,
	}
	blocked := newTestManager(t, blockedLauncher, project, state)
	if _, err := blocked.Init(context.Background(), InitOptions{Source: "ubuntu"}); !errors.Is(err, prerequisiteErr) {
		t.Fatalf("init prerequisite error = %v", err)
	}
	if _, ok, err := blocked.Record(); err != nil || ok {
		t.Fatalf("blocked init record: ok=%v err=%v", ok, err)
	}
	if strings.Contains(flattenCalls(blockedLauncher.calls), "machine create") {
		t.Fatalf("blocked init reached machine creation: %s", flattenCalls(blockedLauncher.calls))
	}

	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager := newTestManager(t, launcher, project, state)
	if _, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocked = newTestManager(t, blockedLauncher, project, state)
	if _, err := blocked.Start(context.Background()); !errors.Is(err, prerequisiteErr) {
		t.Fatalf("start prerequisite error = %v", err)
	}
	if record, ok, err := blocked.Record(); err != nil || !ok || !record.Stopped {
		t.Fatalf("blocked start record = %+v, ok=%v err=%v", record, ok, err)
	}

	if _, err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocked = newTestManager(t, blockedLauncher, project, state)
	cmd, _, active, err := blocked.Command(context.Background(), "true", nil, time.Second)
	if !errors.Is(err, prerequisiteErr) || !active || cmd != nil {
		t.Fatalf("implicit start prerequisite = cmd:%v active:%v err:%v", cmd, active, err)
	}
	if _, err := blocked.Stop(context.Background()); err != nil {
		t.Fatalf("stop should remain available without boot prerequisite: %v", err)
	}
}

func TestManagerStaleProfileFailsClosedButAllowsDeletion(t *testing.T) {
	project := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager := newTestManager(t, launcher, project, statePath)
	if _, err := manager.Init(context.Background(), InitOptions{Profile: "go"}); err != nil {
		t.Fatal(err)
	}

	state, err := loadStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalProject := manager.Project()
	record := state.Projects[canonicalProject]
	record.Source += "-obsolete"
	state.Projects[canonicalProject] = record
	if err := saveStore(statePath, state); err != nil {
		t.Fatal(err)
	}

	options := Options{
		ProjectRoot: project, StatePath: statePath, Executable: "smolvm",
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace", Launcher: launcher,
	}
	if _, err := New(options); err == nil || !strings.Contains(err.Error(), "sandbox delete --force") || !strings.Contains(err.Error(), "sandbox init --profile go") {
		t.Fatalf("stale profile error = %v", err)
	}

	// A stale record for one project must not poison every project in the
	// operator-owned store.
	otherOptions := options
	otherOptions.ProjectRoot = t.TempDir()
	if _, err := New(otherOptions); err != nil {
		t.Fatalf("unrelated project rejected by stale record: %v", err)
	}

	options.AllowStaleProfilePolicy = true
	recovery, err := New(options)
	if err != nil {
		t.Fatalf("recovery manager: %v", err)
	}
	if err := recovery.Delete(context.Background()); err != nil {
		t.Fatalf("delete stale profile: %v", err)
	}
	if _, ok, err := NewStore(statePath).Get(canonicalProject); err != nil || ok {
		t.Fatalf("stale record after deletion: ok=%v err=%v", ok, err)
	}
}

func TestManagerStartAndStopPreserveAssociation(t *testing.T) {
	project := t.TempDir()
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager := newTestManager(t, launcher, project, filepath.Join(t.TempDir(), "sandboxes.json"))
	if _, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.Active() {
		t.Fatal("stopped sandbox retained VM routing")
	}
	if record, ok, err := manager.Record(); err != nil || !ok || !record.Stopped {
		t.Fatalf("stopped record = %+v, ok=%v err=%v", record, ok, err)
	}
	if cmd, _, active, err := manager.Command(context.Background(), "pwd", nil, time.Second); err != nil || active || cmd != nil {
		t.Fatalf("stopped command routing = cmd:%v active:%v err:%v", cmd, active, err)
	}
	resumed := newTestManager(t, launcher, project, manager.store.Path())
	if resumed.Active() {
		t.Fatal("resumed stopped association retained VM routing")
	}
	if _, err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !manager.Active() {
		t.Fatal("start/stop removed persistent association")
	}
	calls := flattenCalls(launcher.calls)
	for _, want := range []string{"machine stop --name", "machine start --name"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("lifecycle calls missing %q: %s", want, calls)
		}
	}
}

func TestManagerDeleteRemovesActivationOnlyAfterBackendSuccess(t *testing.T) {
	project := t.TempDir()
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager := newTestManager(t, launcher, project, filepath.Join(t.TempDir(), "sandboxes.json"))
	if _, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}
	launcher.failMatch = "machine delete"
	if err := manager.Delete(context.Background()); err == nil {
		t.Fatal("expected delete failure")
	}
	if !manager.Active() {
		t.Fatal("backend delete failure disabled fail-closed routing")
	}
	launcher.failMatch = ""
	if err := manager.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.Active() {
		t.Fatal("successful delete retained activation")
	}
}

func TestManagerExecutionBoundaryIsFixedUntilItsOwnLifecycleCall(t *testing.T) {
	project := t.TempDir()
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	first := newTestManager(t, launcher, project, state)
	if _, err := first.Init(context.Background(), InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}
	second := newTestManager(t, launcher, project, state)
	if err := second.Forget(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !first.Active() {
		t.Fatal("another manager silently changed the running manager boundary")
	}
	if _, ok, err := first.Record(); err != nil || !ok {
		t.Fatalf("running manager snapshot: ok=%v err=%v", ok, err)
	}
	third := newTestManager(t, launcher, project, state)
	if third.Active() {
		t.Fatal("new manager did not observe forgotten association")
	}
}

func TestStoreUsesExactCanonicalProjectIdentity(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	parentManager := newTestManager(t, launcher, root, state)
	if _, err := parentManager.Init(context.Background(), InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}
	childManager := newTestManager(t, launcher, child, state)
	if childManager.Active() {
		t.Fatal("child project inherited parent sandbox record")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(child, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	aliasManager := newTestManager(t, launcher, alias, state)
	if aliasManager.Project() != childManager.Project() {
		t.Fatalf("alias project = %q, want %q", aliasManager.Project(), childManager.Project())
	}
}

func TestActiveAssociationAssemblyVersionCheckIsInternallyBounded(t *testing.T) {
	project := t.TempDir()
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager := newTestManager(t, launcher, project, state)
	if _, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}
	previous := assemblyExecutableCheckTimeout
	assemblyExecutableCheckTimeout = 50 * time.Millisecond
	defer func() { assemblyExecutableCheckTimeout = previous }()
	started := time.Now()
	_, err := New(Options{
		ProjectRoot: project, StatePath: state, Executable: "smolvm",
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace",
		Launcher: blockingLauncher{path: "/opt/smolvm"},
	})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded version check error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded version check took %s", elapsed)
	}
}

func TestActiveAssociationFailsClosedOnExecutableVersionDrift(t *testing.T) {
	project := t.TempDir()
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager := newTestManager(t, launcher, project, state)
	if _, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}
	launcher.version = "smolvm 1.9.0"
	if _, _, active, err := manager.Command(context.Background(), "true", nil, time.Second); err == nil || !active {
		t.Fatalf("version drift command: active=%v err=%v", active, err)
	}
	if _, err := New(Options{
		ProjectRoot: project, StatePath: state, Executable: "smolvm",
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace", Launcher: launcher,
	}); err == nil {
		t.Fatal("new manager accepted drifted executable version")
	}
	if _, err := New(Options{
		SkipExecutableValidation: true, ProjectRoot: project, StatePath: state, Executable: "smolvm",
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace", Launcher: launcher,
	}); err != nil {
		t.Fatalf("recovery-only manager could not load stale association: %v", err)
	}
}

func TestStoredMachineMustMatchDeterministicProjectIdentity(t *testing.T) {
	project := t.TempDir()
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager := newTestManager(t, launcher, project, state)
	if _, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}
	stored, err := loadStore(state)
	if err != nil {
		t.Fatal(err)
	}
	record := stored.Projects[manager.Project()]
	record.Machine = "unrelated-machine"
	stored.Projects[manager.Project()] = record
	if err := saveStore(state, stored); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{
		ProjectRoot: project, StatePath: state, Executable: "smolvm",
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace", Launcher: launcher,
	}); err == nil || !strings.Contains(err.Error(), "machine name") {
		t.Fatalf("malformed machine identity error = %v", err)
	}
}

func TestSmolVMVersionValidation(t *testing.T) {
	for _, value := range []string{"smolvm 1.8.1", "smolvm v1.8.9"} {
		if err := validateSmolVMVersion(value); err != nil {
			t.Fatalf("version %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"smolvm 1.8.0", "smolvm 1.9.0", "smolvm 2.0.0", "smolvm 1.8.1-rc1", "smolvm 1.8.1.9", "smolvm 1.8.1 extra", "smolvm dev", "other 1.8.1"} {
		if err := validateSmolVMVersion(value); err == nil {
			t.Fatalf("version %q accepted", value)
		}
	}
}

func TestRetargetedSymlinkDoesNotTransferSandboxAssociation(t *testing.T) {
	base := t.TempDir()
	firstTarget := filepath.Join(base, "first")
	secondTarget := filepath.Join(base, "second")
	if err := os.Mkdir(firstTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(secondTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(firstTarget, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	first := newTestManager(t, launcher, alias, state)
	if _, err := first.Init(context.Background(), InitOptions{Source: "ubuntu"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secondTarget, alias); err != nil {
		t.Fatal(err)
	}
	second := newTestManager(t, launcher, alias, state)
	if second.Active() {
		t.Fatal("retargeted alias inherited prior target sandbox")
	}
}

func TestAllowedEnvironmentDeterministic(t *testing.T) {
	got := allowedEnvironment([]string{"TERM=x", "LANG=C", "TERM=y", "NOPE=z"}, []string{"TERM", "LANG"})
	want := []string{"LANG=C", "TERM=y"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed env = %#v, want %#v", got, want)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func flattenCalls(calls [][]string) string {
	var values []string
	for _, call := range calls {
		values = append(values, strings.Join(call, " "))
	}
	return strings.Join(values, "\n")
}
