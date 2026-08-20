package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/trust"
)

func (osLauncher) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (osLauncher) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return boundedCombinedOutputEnv(ctx, smolVMProcessEnvironment(), name, args...)
}

func (osLauncher) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = smolVMProcessEnvironment()
	return cmd
}

// New constructs a manager without starting a VM. Corrupt execution state is
// rejected immediately so an intended sandbox can never silently become host execution.
func New(opts Options) (*Manager, error) {
	project, err := trust.CanonicalPath(opts.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("sandbox: project: %w", err)
	}
	if opts.StatePath == "" {
		return nil, errors.New("sandbox: state path is required")
	}
	if opts.Executable == "" {
		opts.Executable = "smolvm"
	}
	if opts.CPUs == 0 {
		opts.CPUs = 2
	}
	if opts.MemoryMiB == 0 {
		opts.MemoryMiB = 2048
	}
	if opts.StorageGiB < 0 || opts.OverlayGiB < 0 {
		return nil, errors.New("sandbox: storage and overlay sizes cannot be negative")
	}
	if opts.GuestCWD == "" {
		opts.GuestCWD = "/workspace"
	}
	if opts.EnvAllowlist == nil {
		opts.EnvAllowlist = []string{"LANG", "LC_ALL", "TERM"}
	}
	if opts.Launcher == nil {
		opts.Launcher = osLauncher{}
	}
	if opts.AutoInstall && opts.Installer == nil {
		opts.Installer = newOfficialInstaller()
	}
	if opts.ImageFetcher == nil {
		opts.ImageFetcher = registryImageFetcher{}
	}
	if opts.ImageFetchTimeout == 0 {
		opts.ImageFetchTimeout = defaultImageFetchTimeout
	}
	if opts.ImageFetchTimeout < 0 {
		return nil, errors.New("sandbox: image fetch timeout must be positive")
	}
	m := &Manager{
		project: project, store: NewStore(opts.StatePath), executable: opts.Executable,
		defaultImage: opts.DefaultImage, cpus: opts.CPUs, memoryMiB: opts.MemoryMiB,
		storageGiB: opts.StorageGiB, overlayGiB: opts.OverlayGiB, guestCWD: opts.GuestCWD, envAllowlist: append([]string(nil), opts.EnvAllowlist...), launcher: opts.Launcher,
		autoInstall: opts.AutoInstall, installer: opts.Installer, imageFetcher: opts.ImageFetcher,
		imageFetchTimeout: opts.ImageFetchTimeout,
	}
	if err := validateDefaults(m); err != nil {
		return nil, err
	}
	// Force validation now. A malformed record controls shell authority and must
	// fail app assembly rather than being treated as an absent sandbox.
	if record, ok, err := m.store.Get(m.project); err != nil {
		return nil, err
	} else if ok {
		if !opts.SkipExecutableValidation && !record.Stopped {
			validationParent := opts.Context
			if validationParent == nil {
				validationParent = context.Background()
			}
			validationCtx, cancel := context.WithTimeout(validationParent, assemblyExecutableCheckTimeout)
			err := m.validateExecutable(validationCtx, record)
			cancel()
			if err != nil {
				return nil, err
			}
		}
		m.setRecord(record, true)
	}
	return m, nil
}

func validateDefaults(m *Manager) error {
	if strings.TrimSpace(m.executable) == "" {
		return errors.New("sandbox: executable is blank")
	}
	if m.cpus < 1 || m.cpus > 64 {
		return errors.New("sandbox: CPUs must be 1..64")
	}
	if m.memoryMiB < 128 || m.memoryMiB > 262144 {
		return errors.New("sandbox: memory must be 128..262144 MiB")
	}
	if m.storageGiB < 0 || m.storageGiB > 1048576 || m.overlayGiB < 0 || m.overlayGiB > 1048576 {
		return errors.New("sandbox: storage and overlay must be 0..1048576 GiB")
	}
	if !filepath.IsAbs(m.guestCWD) || filepath.Clean(m.guestCWD) == string(filepath.Separator) {
		return errors.New("sandbox: guest working directory must be an absolute non-root path")
	}
	return validateEnvAllowlist(m.envAllowlist)
}

func validateEnvAllowlist(values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if !envNameRE.MatchString(value) {
			return fmt.Errorf("sandbox: invalid environment allowlist name %q", value)
		}
		if seen[value] {
			return fmt.Errorf("sandbox: duplicate environment allowlist name %q", value)
		}
		seen[value] = true
	}
	return nil
}

func validateRecord(record Record) error {
	if !filepath.IsAbs(record.Project) || filepath.Clean(record.Project) != record.Project {
		return errors.New("project must be a canonical absolute path")
	}
	if record.Machine == "" || len(record.Machine) > 128 {
		return errors.New("machine name is missing or too long")
	}
	if record.Machine != machineName(record.Project) {
		return errors.New("machine name does not match the deterministic project identity")
	}
	if !filepath.IsAbs(record.Executable) {
		return errors.New("executable must be an absolute path")
	}
	if strings.TrimSpace(record.Source) == "" || len(record.Source) > 4096 {
		return errors.New("source is missing or too long")
	}
	if record.SourceKind != SourceImage && record.SourceKind != SourcePack {
		return fmt.Errorf("unknown source kind %q", record.SourceKind)
	}
	if record.Profile != "" {
		profile, ok := FindProfile(record.Profile)
		if !ok || record.SourceKind != SourceImage || record.Source != profile.Source || record.Network != profile.Network {
			return fmt.Errorf("profile %q does not match its built-in policy", record.Profile)
		}
	}
	if !filepath.IsAbs(record.GuestCWD) || filepath.Clean(record.GuestCWD) == string(filepath.Separator) {
		return errors.New("guest working directory must be absolute and non-root")
	}
	if record.CPUs < 1 || record.CPUs > 64 || record.MemoryMiB < 128 || record.MemoryMiB > 262144 ||
		record.StorageGiB < 0 || record.StorageGiB > 1048576 || record.OverlayGiB < 0 || record.OverlayGiB > 1048576 {
		return errors.New("resource values are outside supported bounds")
	}
	return validateEnvAllowlist(record.EnvAllowlist)
}

// Project returns the exact canonical identity used by the state store.
func (m *Manager) Project() string { return m.project }

// StatePath returns the operator-owned state file path.
func (m *Manager) StatePath() string { return m.store.Path() }

func (m *Manager) lockLifecycle(ctx context.Context) (func(), error) {
	path := m.store.Path()
	if path == "" {
		return nil, errors.New("sandbox: state path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("sandbox: mkdir lifecycle state: %w", err)
	}
	return lockStoreFileContext(ctx, path+".project-"+projectHash(m.project)+".lock")
}

// Record returns the startup/lifecycle snapshot without contacting smolvm or
// re-reading mutable ambient state. A running Snow process changes routing only
// through lifecycle calls on this manager.
func (m *Manager) Record() (Record, bool, error) {
	m.recordMu.RLock()
	defer m.recordMu.RUnlock()
	if m.record == nil {
		return Record{}, false, nil
	}
	return m.record.clone(), true, nil
}

func (m *Manager) setRecord(record Record, ok bool) {
	m.recordMu.Lock()
	defer m.recordMu.Unlock()
	if ok {
		copy := record.clone()
		m.record = &copy
	} else {
		m.record = nil
	}
	m.active.Store(ok && !record.Stopped)
}

func (m *Manager) refreshRecord() (Record, bool, error) {
	record, ok, err := m.store.Get(m.project)
	if err == nil {
		m.setRecord(record, ok)
	}
	return record, ok, err
}

// Active reports whether shell execution is configured for this process.
func (m *Manager) Active() bool { return m.active.Load() }

// Init creates and starts a persistent machine, publishing state only after the
// machine is usable. Failures trigger best-effort machine deletion.
func (m *Manager) Init(ctx context.Context, opts InitOptions) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, err := m.lockLifecycle(ctx)
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	if _, ok, err := m.refreshRecord(); err != nil {
		return Status{}, err
	} else if ok {
		return Status{}, ErrAlreadyInitialized
	}
	profileID := strings.ToLower(strings.TrimSpace(opts.Profile))
	if profileID != "" {
		profile, ok := FindProfile(profileID)
		if !ok {
			return Status{}, fmt.Errorf("sandbox: unknown profile %q", opts.Profile)
		}
		if source := strings.TrimSpace(opts.Source); source != "" && source != profile.Source {
			return Status{}, fmt.Errorf("sandbox: profile %q conflicts with explicit source", profileID)
		}
		if opts.SourceKind == SourcePack {
			return Status{}, fmt.Errorf("sandbox: profile %q cannot use a .smolmachine pack", profileID)
		}
		opts.Source = profile.Source
		opts.SourceKind = SourceImage
		opts.Network = profile.Network
		if opts.CPUs == 0 && profile.CPUs > 0 {
			opts.CPUs = profile.CPUs
		}
		if opts.MemoryMiB == 0 && profile.MemoryMiB > 0 {
			opts.MemoryMiB = profile.MemoryMiB
		}
	}
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = strings.TrimSpace(m.defaultImage)
	}
	if source == "" {
		return Status{}, errors.New("sandbox: image or .smolmachine source is required")
	}
	kind := opts.SourceKind
	if kind == "" {
		kind = SourceImage
		if strings.HasSuffix(strings.ToLower(source), ".smolmachine") {
			kind = SourcePack
		}
	}
	if kind != SourceImage && kind != SourcePack {
		return Status{}, fmt.Errorf("sandbox: invalid source kind %q", kind)
	}
	if kind == SourcePack {
		abs, err := filepath.Abs(source)
		if err != nil {
			return Status{}, fmt.Errorf("sandbox: pack path: %w", err)
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return Status{}, fmt.Errorf("sandbox: pack source must be an existing file: %s", abs)
		}
		source = abs
	}
	cpus := opts.CPUs
	if cpus == 0 {
		cpus = m.cpus
	}
	memory := opts.MemoryMiB
	if memory == 0 {
		memory = m.memoryMiB
	}
	storage := m.storageGiB
	if opts.StorageSet || opts.StorageGiB != 0 {
		storage = opts.StorageGiB
	}
	overlay := m.overlayGiB
	if opts.OverlaySet || opts.OverlayGiB != 0 {
		overlay = opts.OverlayGiB
	}
	guest := opts.GuestCWD
	if guest == "" {
		guest = m.guestCWD
	}
	record := Record{
		Project: m.project, Source: source, Profile: profileID, SourceKind: kind, GuestCWD: filepath.Clean(guest),
		ReadOnly: opts.ReadOnly, Network: opts.Network, CPUs: cpus, MemoryMiB: memory,
		StorageGiB: storage, OverlayGiB: overlay,
		EnvAllowlist: append([]string(nil), m.envAllowlist...), CreatedAt: time.Now().UTC(),
	}
	if err := validateRecordForInit(record); err != nil {
		return Status{}, err
	}
	executable, err := m.resolveInitExecutable(ctx)
	if err != nil {
		return Status{}, err
	}
	record.Executable = executable
	record.Machine = machineName(m.project)
	versionOutput, err := m.run(ctx, executable, "--version")
	if err != nil {
		return Status{}, fmt.Errorf("sandbox: smolvm preflight: %w", err)
	}
	if err := validateSmolVMVersion(string(versionOutput)); err != nil {
		return Status{}, err
	}
	if err := m.checkPersistentDiskPrerequisite(); err != nil {
		return Status{}, err
	}
	// Materialize the no-argument registry image over host HTTPS and hand smolvm
	// a local Docker-save archive. The guest therefore never needs bootstrap
	// network authority, and the persisted source remains the pinned registry ref.
	createRecord := record
	if record.SourceKind == SourceImage && !record.Network && record.Source == m.defaultImage {
		archivePath, cleanup, archiveErr := createStagedImageArchive(m.store.Path())
		if archiveErr != nil {
			return Status{}, archiveErr
		}
		defer cleanup()
		fetchCtx, cancelFetch := context.WithTimeout(ctx, m.imageFetchTimeout)
		result, fetchErr := m.imageFetcher.Fetch(fetchCtx, record.Source, archivePath)
		cancelFetch()
		if fetchErr != nil {
			return Status{}, fetchErr
		}
		if err := validateStagedImageArchive(archivePath, result); err != nil {
			return Status{}, err
		}
		createRecord.Source = archivePath
	}
	args := createArgs(createRecord)
	if _, err := m.run(ctx, executable, args...); err != nil {
		return Status{}, fmt.Errorf("sandbox: create machine: %w", err)
	}
	created := true
	defer func() {
		if created {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = m.run(cleanupCtx, executable, "machine", "delete", "--name", record.Machine, "--force")
		}
	}()
	if _, err := m.run(ctx, executable, "machine", "start", "--name", record.Machine); err != nil {
		return Status{}, fmt.Errorf("sandbox: start machine: %w", err)
	}
	if err := m.store.update(ctx, func(state *storeFile) error {
		if _, exists := state.Projects[m.project]; exists {
			return ErrAlreadyInitialized
		}
		state.Projects[m.project] = record.clone()
		return nil
	}); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = m.run(cleanupCtx, executable, "machine", "stop", "--name", record.Machine)
		cancel()
		return Status{}, err
	}
	created = false
	m.setRecord(record, true)
	return m.statusAfterMutation(ctx, record), nil
}

func validateRecordForInit(record Record) error {
	// Executable and machine are assigned only after policy validation.
	copy := record
	copy.Executable = string(filepath.Separator) + "placeholder"
	copy.Machine = machineName(copy.Project)
	return validateRecord(copy)
}

func createArgs(record Record) []string {
	hash := projectHash(record.Project)
	args := []string{"machine", "create", "--name", record.Machine,
		"--label", "owner=snow", "--label", "project=" + hash,
		"--cpus", fmt.Sprint(record.CPUs), "--mem", fmt.Sprint(record.MemoryMiB)}
	if record.StorageGiB > 0 {
		args = append(args, "--storage", fmt.Sprint(record.StorageGiB))
	}
	if record.OverlayGiB > 0 {
		args = append(args, "--overlay", fmt.Sprint(record.OverlayGiB))
	}
	if record.SourceKind == SourcePack {
		args = append(args, "--from", record.Source)
	} else {
		args = append(args, "--image", record.Source)
	}
	mount := record.Project + ":" + record.GuestCWD
	if record.ReadOnly {
		mount += ":ro"
	}
	args = append(args, "--volume", mount, "--workdir", record.GuestCWD)
	if record.Network {
		args = append(args, "--net")
	}
	return args
}

// Status returns persisted policy and bounded smolvm runtime output. Missing
// state is a successful, uninitialized status.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok, err := m.Record()
	if err != nil {
		return Status{}, err
	}
	if !ok {
		return Status{Initialized: false}, nil
	}
	if record.Stopped {
		if err := m.validateExecutable(ctx, record); err != nil {
			return Status{Initialized: true, Record: record.clone(), Diagnostic: err.Error()}, nil
		}
		return m.statusAfterMutation(ctx, record), nil
	}
	if err := m.validateExecutable(ctx, record); err != nil {
		return Status{Initialized: true, Record: record.clone()}, err
	}
	return m.statusLocked(ctx, record)
}

func (m *Manager) statusLocked(ctx context.Context, record Record) (Status, error) {
	out, err := m.run(ctx, record.Executable, "machine", "status", "--name", record.Machine)
	if err != nil {
		return Status{Initialized: true, Record: record.clone()}, fmt.Errorf("sandbox: status: %w", err)
	}
	return Status{Initialized: true, Record: record.clone(), Runtime: strings.TrimSpace(string(out))}, nil
}

func (m *Manager) statusAfterMutation(ctx context.Context, record Record) Status {
	status, err := m.statusLocked(ctx, record)
	if err != nil {
		status.Initialized = true
		status.Record = record.clone()
		status.Diagnostic = err.Error()
	}
	return status
}

func (m *Manager) Start(ctx context.Context) (Status, error) {
	return m.setStopped(ctx, false)
}

func (m *Manager) Stop(ctx context.Context) (Status, error) {
	return m.setStopped(ctx, true)
}

func (m *Manager) setStopped(ctx context.Context, stopped bool) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, err := m.lockLifecycle(ctx)
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	record, ok, err := m.refreshRecord()
	if err != nil {
		return Status{}, err
	}
	if !ok {
		return Status{}, ErrNotInitialized
	}
	if err := m.validateExecutable(ctx, record); err != nil {
		return Status{}, err
	}
	if !stopped {
		if err := m.checkPersistentDiskPrerequisite(); err != nil {
			return Status{}, err
		}
	}
	action := "start"
	rollback := "stop"
	if stopped {
		action = "stop"
		rollback = "start"
	}
	if _, err := m.run(ctx, record.Executable, "machine", action, "--name", record.Machine); err != nil {
		return Status{}, fmt.Errorf("sandbox: %s machine: %w", action, err)
	}
	updated := record.clone()
	updated.Stopped = stopped
	if err := m.store.update(ctx, func(state *storeFile) error {
		if _, exists := state.Projects[m.project]; !exists {
			return ErrNotInitialized
		}
		state.Projects[m.project] = updated.clone()
		return nil
	}); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = m.run(rollbackCtx, record.Executable, "machine", rollback, "--name", record.Machine)
		cancel()
		return Status{}, fmt.Errorf("sandbox: persist %s routing state: %w", action, err)
	}
	m.setRecord(updated, true)
	return m.statusAfterMutation(ctx, updated), nil
}

// Delete stops/deletes the machine through smolvm and removes its association.
// State is retained if deletion fails, preserving its current routing policy.
func (m *Manager) Delete(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, err := m.lockLifecycle(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	record, ok, err := m.refreshRecord()
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotInitialized
	}
	if err := m.validateExecutable(ctx, record); err != nil {
		return err
	}
	if _, err := m.run(ctx, record.Executable, "machine", "delete", "--name", record.Machine, "--force"); err != nil {
		return fmt.Errorf("sandbox: delete machine: %w", err)
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.store.update(cleanupCtx, func(state *storeFile) error {
		delete(state.Projects, m.project)
		return nil
	}); err != nil {
		return fmt.Errorf("sandbox: machine deleted but association cleanup failed; retry with sandbox delete --force --forget: %w", err)
	}
	m.setRecord(Record{}, false)
	return nil
}

// Forget removes a stale association without contacting smolvm. Surfaces must
// require explicit destructive confirmation because future Bash calls use the host.
func (m *Manager) Forget(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, err := m.lockLifecycle(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if _, ok, err := m.refreshRecord(); err != nil {
		return err
	} else if !ok {
		return ErrNotInitialized
	}
	if err := m.store.update(ctx, func(state *storeFile) error {
		delete(state.Projects, m.project)
		return nil
	}); err != nil {
		return err
	}
	m.setRecord(Record{}, false)
	return nil
}

// Command builds a streaming guest shell command. The returned bool asks the
// Bash process supervisor to send SIGINT before forced termination so smolvm can
// cancel the guest exec without destroying the persistent VM.
func (m *Manager) Command(ctx context.Context, command string, hostEnv []string, _ time.Duration) (*exec.Cmd, bool, bool, error) {
	record, ok, err := m.Record()
	if err != nil {
		return nil, true, true, err
	}
	if !ok || record.Stopped {
		return nil, false, false, nil
	}
	if err := m.validateExecutable(ctx, record); err != nil {
		return nil, true, true, err
	}
	if err := m.checkPersistentDiskPrerequisite(); err != nil {
		return nil, true, true, err
	}
	args := []string{"machine", "exec", "--name", record.Machine, "--workdir", record.GuestCWD, "--stream"}
	for _, value := range allowedEnvironment(hostEnv, record.EnvAllowlist) {
		args = append(args, "--env", value)
	}
	args = append(args, "--", "sh", "-c", command)
	return m.launcher.CommandContext(ctx, record.Executable, args...), true, true, nil
}

func allowedEnvironment(environ, allowlist []string) []string {
	allowed := make(map[string]bool, len(allowlist))
	for _, name := range allowlist {
		allowed[name] = true
	}
	values := make(map[string]string, len(allowlist))
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if ok && allowed[name] {
			values[name] = entry
		}
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, values[name])
	}
	return out
}

func (m *Manager) resolveInitExecutable(ctx context.Context) (string, error) {
	executable, err := m.resolveExecutable(m.executable)
	if err == nil {
		return executable, nil
	}
	if !m.autoInstall || m.installer == nil || m.executable != "smolvm" || !errors.Is(err, exec.ErrNotFound) {
		return "", err
	}
	home, homeErr := installerHome()
	if homeErr != nil {
		return "", homeErr
	}
	unlock, lockErr := lockStoreFileContext(ctx, filepath.Join(home, ".snow-smolvm-install.lock"))
	if lockErr != nil {
		return "", fmt.Errorf("sandbox: lock smolvm installation: %w", lockErr)
	}
	defer unlock()
	// Another Snow process may have completed the shared user-local install
	// while this initializer waited. Accept only Snow's exact verified layout
	// and pinned-release receipt, never an arbitrary version-forgeable binary.
	if executable, retryErr := validateVerifiedOfficialInstall(home); retryErr == nil {
		m.executable = executable
		return executable, nil
	}
	installed, installErr := m.installer.Install(ctx, MinimumSmolVMVersion)
	if installErr != nil {
		return "", fmt.Errorf("sandbox: install smolvm %s: %w", MinimumSmolVMVersion, installErr)
	}
	executable, err = m.resolveExecutable(installed)
	if err != nil {
		return "", fmt.Errorf("sandbox: validate installed smolvm: %w", err)
	}
	m.executable = executable
	return executable, nil
}

func (m *Manager) validateExecutable(ctx context.Context, record Record) error {
	if ctx == nil {
		ctx = context.Background()
	}
	resolved, err := m.resolveExecutable(record.Executable)
	if err != nil {
		return err
	}
	if filepath.Clean(resolved) != filepath.Clean(record.Executable) {
		return fmt.Errorf("sandbox: pinned smolvm executable changed from %q to %q", record.Executable, resolved)
	}
	out, err := m.run(ctx, record.Executable, "--version")
	if err != nil {
		return fmt.Errorf("sandbox: smolvm version check: %w", err)
	}
	return validateSmolVMVersion(string(out))
}

func (m *Manager) resolveExecutable(name string) (string, error) {
	resolved, err := m.launcher.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("sandbox: smolvm executable %q unavailable: %w", name, err)
	}
	if !filepath.IsAbs(resolved) {
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return "", fmt.Errorf("sandbox: resolve smolvm executable: %w", err)
		}
	}
	return filepath.Clean(resolved), nil
}

func (m *Manager) checkPersistentDiskPrerequisite() error {
	checker, ok := m.launcher.(persistentDiskPrerequisiteChecker)
	if !ok {
		return nil
	}
	return checker.checkPersistentDiskPrerequisite()
}

func (m *Manager) run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	out, err := m.launcher.CombinedOutput(ctx, executable, args...)
	if len(out) > 16<<10 {
		out = out[len(out)-(16<<10):]
	}
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message != "" {
			return out, fmt.Errorf("%w: %s", err, message)
		}
		return out, err
	}
	return out, nil
}

func validateSmolVMVersion(output string) error {
	value := strings.TrimSpace(output)
	match := smolVMVersionRE.FindStringSubmatch(value)
	if match == nil {
		return fmt.Errorf("sandbox: unrecognized stable smolvm --version output %q", value)
	}
	got := [3]int{}
	for i := range got {
		parsed, err := strconv.Atoi(match[i+1])
		if err != nil {
			return fmt.Errorf("sandbox: parse smolvm version %q: %w", value, err)
		}
		got[i] = parsed
	}
	minimum := [3]int{1, 8, 1}
	// Network-off is an audited 1.8.x default (the CLI has only opt-in --net),
	// so do not assume a future minor/major preserves this security boundary.
	if got[0] != minimum[0] || got[1] != minimum[1] || got[2] < minimum[2] {
		return fmt.Errorf("sandbox: supported smolvm versions are 1.8.x at or above %s (found %s)", MinimumSmolVMVersion, strings.TrimPrefix(value, "smolvm "))
	}
	return nil
}

func projectHash(project string) string {
	sum := sha256.Sum256([]byte(project))
	return hex.EncodeToString(sum[:16])
}
