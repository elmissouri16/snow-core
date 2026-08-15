package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/config"
)

type fakeImageFetcher struct {
	err   error
	calls []string
}

func (f *fakeImageFetcher) Fetch(_ context.Context, source, destination string) (ImageFetchResult, error) {
	f.calls = append(f.calls, source)
	if f.err != nil {
		return ImageFetchResult{}, f.err
	}
	if err := os.WriteFile(destination, []byte("docker archive"), 0o600); err != nil {
		return ImageFetchResult{}, err
	}
	digest, err := digestArchive(destination)
	return ImageFetchResult{ArchiveSHA256: digest}, err
}

type blockingImageFetcher struct{}

func (blockingImageFetcher) Fetch(ctx context.Context, _, _ string) (ImageFetchResult, error) {
	<-ctx.Done()
	return ImageFetchResult{}, ctx.Err()
}

type fileInstaller struct {
	mu    sync.Mutex
	home  string
	calls int
}

func (f *fileInstaller) Install(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	prefix := filepath.Join(f.home, ".smolvm")
	path := filepath.Join(f.home, ".local", "bin", "smolvm")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(prefix, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(prefix, "smolvm")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'smolvm 1.8.1'; elif [ \"$1 $2\" = \"machine status\" ]; then echo 'State: Running'; else echo ok; fi\n"
	if err := os.WriteFile(target, []byte(script), 0o700); err != nil {
		return "", err
	}
	if err := os.Symlink(target, path); err != nil {
		return "", err
	}
	platform, err := smolVMReleasePlatform()
	if err != nil {
		return "", err
	}
	if err := writeInstallReceipt(f.home, platform); err != nil {
		return "", err
	}
	return path, nil
}

func (f *fileInstaller) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeInstaller struct {
	launcher *fakeLauncher
	path     string
	err      error
	calls    []string
}

func (f *fakeInstaller) Install(_ context.Context, version string) (string, error) {
	f.calls = append(f.calls, version)
	if f.err != nil {
		return "", f.err
	}
	f.launcher.path = f.path
	return f.path, nil
}

type blockingLauncher struct{ path string }

func (b blockingLauncher) LookPath(string) (string, error) { return b.path, nil }
func (blockingLauncher) CombinedOutput(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (blockingLauncher) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

type fakeLauncher struct {
	mu        sync.Mutex
	path      string
	version   string
	calls     [][]string
	failMatch string
}

func (f *fakeLauncher) LookPath(string) (string, error) {
	if f.path == "" {
		return "", exec.ErrNotFound
	}
	return f.path, nil
}

func (f *fakeLauncher) CombinedOutput(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	joined := strings.Join(args, " ")
	if f.failMatch != "" && strings.Contains(joined, f.failMatch) {
		return []byte("intentional failure"), errors.New("failed")
	}
	if joined == "--version" {
		version := f.version
		if version == "" {
			version = "smolvm 1.8.1"
		}
		return []byte(version + "\n"), nil
	}
	if strings.Contains(joined, "machine status") {
		return []byte("State: Running\n"), nil
	}
	return []byte("ok\n"), nil
}

func (f *fakeLauncher) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func newTestManager(t *testing.T, launcher Launcher, project, state string) *Manager {
	t.Helper()
	manager, err := New(Options{
		ProjectRoot: project, StatePath: state, Executable: "smolvm",
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace",
		EnvAllowlist: []string{"LANG", "TERM"}, Launcher: launcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestManagerInitBootstrapsMissingSmolVMAndDefaultsUbuntu(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{}
	installer := &fakeInstaller{launcher: launcher, path: filepath.Join(home, ".local", "bin", "smolvm")}
	fetcher := &fakeImageFetcher{}
	manager, err := New(Options{
		ProjectRoot: project, StatePath: state, Executable: "smolvm", DefaultImage: config.DefaultUbuntuImage,
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace", Launcher: launcher,
		AutoInstall: true, Installer: installer, ImageFetcher: fetcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := manager.Init(context.Background(), InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(installer.calls) != 1 || installer.calls[0] != MinimumSmolVMVersion {
		t.Fatalf("installer calls = %#v", installer.calls)
	}
	if status.Record.Source != config.DefaultUbuntuImage || status.Record.Executable != installer.path || !status.Initialized {
		t.Fatalf("bootstrap status = %+v", status)
	}
	if len(fetcher.calls) != 1 || fetcher.calls[0] != config.DefaultUbuntuImage {
		t.Fatalf("default image fetches = %#v", fetcher.calls)
	}
	calls := flattenCalls(launcher.calls)
	if !strings.Contains(calls, "--image ") || strings.Contains(calls, "--image "+config.DefaultUbuntuImage) || strings.Contains(calls, " --net") {
		t.Fatalf("Ubuntu bootstrap local-image authority = %s", calls)
	}
}

func TestManagerAutoInstallFailureDoesNotPublishAssociation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{}
	installer := &fakeInstaller{launcher: launcher, err: errors.New("download failed")}
	manager, err := New(Options{
		ProjectRoot: project, StatePath: state, Executable: "smolvm", DefaultImage: config.DefaultUbuntuImage,
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace", Launcher: launcher,
		AutoInstall: true, Installer: installer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Init(context.Background(), InitOptions{}); err == nil || !strings.Contains(err.Error(), "install smolvm") {
		t.Fatalf("install failure = %v", err)
	}
	if manager.Active() {
		t.Fatal("failed installer activated sandbox")
	}
	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed installer published state: %v", err)
	}
}

func TestManagerInitBuildsConfinedPersistentMachine(t *testing.T) {
	project := t.TempDir()
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{path: "/opt/smolvm/bin/smolvm"}
	manager := newTestManager(t, launcher, project, state)

	status, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu@sha256:abc", CPUs: 4, MemoryMiB: 4096, StorageGiB: 40, OverlayGiB: 20, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Initialized || !manager.Active() || status.Record.Network || !status.Record.ReadOnly || status.Record.StorageGiB != 40 || status.Record.OverlayGiB != 20 {
		t.Fatalf("unexpected status: %+v active=%v", status, manager.Active())
	}
	if got := status.Record.Project; got != manager.Project() {
		t.Fatalf("project = %q, want %q", got, manager.Project())
	}
	var create []string
	for _, call := range launcher.calls {
		if strings.Contains(strings.Join(call, " "), "machine create") {
			create = call
			break
		}
	}
	if len(create) == 0 {
		t.Fatal("smolvm create was not called")
	}
	joined := strings.Join(create, " ")
	for _, want := range []string{"machine create", "--label owner=snow", "--cpus 4", "--mem 4096", "--storage 40", "--overlay 20", "--image ubuntu@sha256:abc", manager.Project() + ":/workspace:ro", "--workdir /workspace"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("create argv missing %q: %q", want, joined)
		}
	}
	for _, forbidden := range []string{"--net", "--ssh-agent", "--docker-socket", "--mount-socket", "--secret"} {
		if containsArg(create, forbidden) {
			t.Fatalf("create argv unexpectedly grants %s: %q", forbidden, joined)
		}
	}
	info, err := os.Stat(state)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o, want 600", info.Mode().Perm())
	}
}

func TestManagerProfilePinsImageAndEnablesNetwork(t *testing.T) {
	project := t.TempDir()
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager := newTestManager(t, launcher, project, filepath.Join(t.TempDir(), "sandboxes.json"))
	status, err := manager.Init(context.Background(), InitOptions{Profile: "python"})
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := FindProfile("python")
	if status.Record.Profile != "python" || status.Record.Source != profile.Source || !status.Record.Network {
		t.Fatalf("python profile record = %+v", status.Record)
	}
	calls := flattenCalls(launcher.calls)
	if !strings.Contains(calls, "--image "+profile.Source) || !strings.Contains(calls, " --net") {
		t.Fatalf("python profile create calls = %s", calls)
	}
}

func TestManagerGoProfileUsesRecommendedResourcesUnlessOverridden(t *testing.T) {
	manager := newTestManager(t, &fakeLauncher{path: "/opt/smolvm"}, t.TempDir(), filepath.Join(t.TempDir(), "sandboxes.json"))
	status, err := manager.Init(context.Background(), InitOptions{Profile: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Record.CPUs != 4 || status.Record.MemoryMiB != 6144 {
		t.Fatalf("Go profile resources = %+v", status.Record)
	}

	manager = newTestManager(t, &fakeLauncher{path: "/opt/smolvm"}, t.TempDir(), filepath.Join(t.TempDir(), "sandboxes.json"))
	status, err = manager.Init(context.Background(), InitOptions{Profile: "go", CPUs: 8, MemoryMiB: 12288})
	if err != nil {
		t.Fatal(err)
	}
	if status.Record.CPUs != 8 || status.Record.MemoryMiB != 12288 {
		t.Fatalf("overridden Go profile resources = %+v", status.Record)
	}
}

func TestManagerRejectsUnknownProfile(t *testing.T) {
	manager := newTestManager(t, &fakeLauncher{path: "/opt/smolvm"}, t.TempDir(), filepath.Join(t.TempDir(), "sandboxes.json"))
	if _, err := manager.Init(context.Background(), InitOptions{Profile: "unknown"}); err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Fatalf("unknown profile error = %v", err)
	}
}

func TestManagerExplicitZeroClearsConfiguredDiskDefaults(t *testing.T) {
	project := t.TempDir()
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager, err := New(Options{
		ProjectRoot: project, StatePath: filepath.Join(t.TempDir(), "sandboxes.json"), Executable: "smolvm",
		CPUs: 2, MemoryMiB: 2048, StorageGiB: 40, OverlayGiB: 20, GuestCWD: "/workspace", Launcher: launcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu", StorageSet: true, OverlaySet: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.Record.StorageGiB != 0 || status.Record.OverlayGiB != 0 {
		t.Fatalf("explicit zero disks = %+v", status.Record)
	}
	calls := flattenCalls(launcher.calls)
	if strings.Contains(calls, "--storage") || strings.Contains(calls, "--overlay") {
		t.Fatalf("explicit smolvm defaults emitted disk flags: %s", calls)
	}
}

func TestCommittedInitIsNotReportedFailedWhenStatusProbeFails(t *testing.T) {
	project := t.TempDir()
	launcher := &fakeLauncher{path: "/opt/smolvm", failMatch: "machine status"}
	manager := newTestManager(t, launcher, project, filepath.Join(t.TempDir(), "sandboxes.json"))
	status, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu"})
	if err != nil {
		t.Fatalf("committed init returned error: %v", err)
	}
	if !status.Initialized || status.Diagnostic == "" || !manager.Active() {
		t.Fatalf("committed init status = %+v active=%v", status, manager.Active())
	}
	if _, err := manager.Status(context.Background()); err == nil {
		t.Fatal("explicit status hid backend probe failure")
	}
}

func TestDefaultImageFetchFailureDoesNotCreateOrPublish(t *testing.T) {
	project := t.TempDir()
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager := newTestManager(t, launcher, project, filepath.Join(t.TempDir(), "sandboxes.json"))
	manager.defaultImage = config.DefaultUbuntuImage
	manager.imageFetcher = &fakeImageFetcher{err: errors.New("download failed")}
	if _, err := manager.Init(context.Background(), InitOptions{}); err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("image download error = %v", err)
	}
	if _, ok, err := manager.Record(); err != nil || ok {
		t.Fatalf("record after failure: ok=%v err=%v", ok, err)
	}
	calls := flattenCalls(launcher.calls)
	if strings.Contains(calls, "machine create") || strings.Contains(calls, "machine start") {
		t.Fatalf("failed image fetch reached machine lifecycle: %s", calls)
	}
}

func TestDefaultImageFetchHonorsBoundedTimeout(t *testing.T) {
	project := t.TempDir()
	manager, err := New(Options{
		ProjectRoot: project, StatePath: filepath.Join(t.TempDir(), "sandboxes.json"),
		Executable: "/opt/smolvm", DefaultImage: config.DefaultUbuntuImage,
		CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace",
		Launcher: &fakeLauncher{path: "/opt/smolvm"}, ImageFetcher: blockingImageFetcher{},
		ImageFetchTimeout: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = manager.Init(context.Background(), InitOptions{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded image fetch error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded image fetch took %v", elapsed)
	}
}

func TestManagerInitFailureDoesNotActivate(t *testing.T) {
	project := t.TempDir()
	launcher := &fakeLauncher{path: "/opt/smolvm", failMatch: "machine start"}
	manager := newTestManager(t, launcher, project, filepath.Join(t.TempDir(), "sandboxes.json"))
	if _, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu"}); err == nil {
		t.Fatal("expected start failure")
	}
	if manager.Active() {
		t.Fatal("failed initialization activated sandbox")
	}
	if _, ok, err := manager.Record(); err != nil || ok {
		t.Fatalf("record after failure: ok=%v err=%v", ok, err)
	}
	calls := flattenCalls(launcher.calls)
	if !strings.Contains(calls, "machine delete") {
		t.Fatalf("failed initialization did not roll back: %s", calls)
	}
}

func TestManagerCommandIsFailClosedAndForwardsAllowlistedEnvironment(t *testing.T) {
	project := t.TempDir()
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	manager := newTestManager(t, launcher, project, filepath.Join(t.TempDir(), "sandboxes.json"))
	if cmd, _, active, err := manager.Command(context.Background(), "echo host", nil, time.Second); err != nil || active || cmd != nil {
		t.Fatalf("uninitialized command = cmd:%v active:%v err:%v", cmd, active, err)
	}
	if _, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu", Network: true}); err != nil {
		t.Fatal(err)
	}
	createCalls := flattenCalls(launcher.calls)
	if !strings.Contains(createCalls, "machine create") || !strings.Contains(createCalls, " --net") {
		t.Fatalf("explicit network opt-in was not forwarded: %s", createCalls)
	}
	cmd, graceful, active, err := manager.Command(context.Background(), "printf ok", []string{"LANG=en_US.UTF-8", "SECRET=do-not-forward", "TERM=xterm"}, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !active || !graceful {
		t.Fatalf("active=%v graceful=%v", active, graceful)
	}
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{"machine exec", "--name " + machineName(manager.Project()), "--workdir /workspace", "--stream", "--env LANG=en_US.UTF-8", "--env TERM=xterm", "-- sh -c printf ok"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("exec argv missing %q: %q", want, joined)
		}
	}
	if strings.Contains(joined, "SECRET") {
		t.Fatalf("secret environment crossed boundary: %q", joined)
	}

	launcher.path = ""
	if _, _, active, err := manager.Command(context.Background(), "true", nil, time.Second); err == nil || !active {
		t.Fatalf("missing executable did not fail closed: active=%v err=%v", active, err)
	}
}

func TestConcurrentInitializersSerializeProjectLifecycle(t *testing.T) {
	project := t.TempDir()
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	first := newTestManager(t, launcher, project, state)
	second := newTestManager(t, launcher, project, state)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, manager := range []*Manager{first, second} {
		go func(manager *Manager) {
			<-start
			_, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu"})
			errs <- err
		}(manager)
	}
	close(start)
	var success, already int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrAlreadyInitialized):
			already++
		default:
			t.Fatalf("unexpected concurrent init error: %v", err)
		}
	}
	if success != 1 || already != 1 {
		t.Fatalf("concurrent results: success=%d already=%d", success, already)
	}
	calls := launcher.calls
	creates := 0
	for _, call := range calls {
		if strings.Contains(strings.Join(call, " "), "machine create") {
			creates++
		}
	}
	if creates != 1 {
		t.Fatalf("create calls = %d, want 1; calls=%s", creates, flattenCalls(calls))
	}
}

func TestConcurrentProjectsShareOneUserLocalInstall(t *testing.T) {
	if _, err := smolVMReleasePlatform(); err != nil {
		t.Skip(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	installer := &fileInstaller{home: home}
	managers := make([]*Manager, 0, 2)
	for range 2 {
		manager, err := New(Options{
			ProjectRoot: t.TempDir(), StatePath: state, Executable: "smolvm", DefaultImage: config.DefaultUbuntuImage,
			CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace",
			AutoInstall: true, Installer: installer, ImageFetcher: &fakeImageFetcher{},
		})
		if err != nil {
			t.Fatal(err)
		}
		managers = append(managers, manager)
	}
	errs := make(chan error, len(managers))
	for _, manager := range managers {
		go func(manager *Manager) {
			_, err := manager.Init(context.Background(), InitOptions{})
			errs <- err
		}(manager)
	}
	for range managers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := installer.count(); got != 1 {
		t.Fatalf("installer calls = %d, want 1", got)
	}
}

func TestConcurrentProjectsMergeState(t *testing.T) {
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	launcher := &fakeLauncher{path: "/opt/smolvm"}
	managers := []*Manager{
		newTestManager(t, launcher, t.TempDir(), state),
		newTestManager(t, launcher, t.TempDir(), state),
	}
	errs := make(chan error, len(managers))
	for _, manager := range managers {
		go func(manager *Manager) {
			_, err := manager.Init(context.Background(), InitOptions{Source: "ubuntu"})
			errs <- err
		}(manager)
	}
	for range managers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	store := NewStore(state)
	for _, manager := range managers {
		if _, ok, err := store.Get(manager.Project()); err != nil || !ok {
			t.Fatalf("merged record for %s: ok=%v err=%v", manager.Project(), ok, err)
		}
	}
}

func TestLifecycleLockHonorsContextCancellation(t *testing.T) {
	project := t.TempDir()
	state := filepath.Join(t.TempDir(), "sandboxes.json")
	manager := newTestManager(t, &fakeLauncher{path: "/opt/smolvm"}, project, state)
	if err := os.MkdirAll(filepath.Dir(state), 0o700); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockStoreFileContext(context.Background(), state+".project-"+projectHash(manager.Project())+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := manager.Init(ctx, InitOptions{Source: "ubuntu"}); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("canceled lock wait took %s", time.Since(started))
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
