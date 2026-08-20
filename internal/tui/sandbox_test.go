package tui

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
	internalsandbox "github.com/elmissouri16/snow-core/internal/sandbox"
)

type tuiSandboxImageFetcher struct{}

func (tuiSandboxImageFetcher) Fetch(_ context.Context, _ string, destination string) (internalsandbox.ImageFetchResult, error) {
	data := []byte("docker archive")
	if err := os.WriteFile(destination, data, 0o600); err != nil {
		return internalsandbox.ImageFetchResult{}, err
	}
	return internalsandbox.ImageFetchResult{ArchiveSHA256: sha256.Sum256(data)}, nil
}

type tuiSandboxLauncher struct{}

func (tuiSandboxLauncher) LookPath(string) (string, error) { return "/opt/smolvm", nil }
func (tuiSandboxLauncher) CombinedOutput(_ context.Context, _ string, args ...string) ([]byte, error) {
	if strings.Join(args, " ") == "--version" {
		return []byte("smolvm 1.8.1"), nil
	}
	if strings.Join(args, " ") == "machine status --name snow-project" {
		return []byte("State: Running"), nil
	}
	if len(args) >= 2 && args[0] == "machine" && args[1] == "status" {
		return []byte("State: Running"), nil
	}
	return []byte("ok"), nil
}
func (tuiSandboxLauncher) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func testSandboxManager(t *testing.T, project string) *internalsandbox.Manager {
	t.Helper()
	manager, err := internalsandbox.New(internalsandbox.Options{
		ProjectRoot: project, StatePath: filepath.Join(t.TempDir(), "sandboxes.json"),
		Executable: "smolvm", DefaultImage: config.DefaultUbuntuImage, CPUs: 2, MemoryMiB: 2048, GuestCWD: "/workspace",
		Launcher: tuiSandboxLauncher{}, ImageFetcher: tuiSandboxImageFetcher{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestSandboxSlashCommandIsAsyncAndVisible(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.app.Sandbox = testSandboxManager(t, m.app.ProjectInputRoot)

	_, cmd := m.runCommand("/sandbox status")
	if cmd == nil || !m.sandboxLoading {
		t.Fatalf("status did not start async operation: cmd=%v loading=%v", cmd, m.sandboxLoading)
	}
	_, _ = m.Update(cmd())
	if m.sandboxLoading {
		t.Fatal("status result did not clear loading")
	}
	if got := stripANSI(strings.Join(m.lines, "\n")); !strings.Contains(got, "sandbox: not initialized") || !strings.Contains(got, "Bash runs on the host") {
		t.Fatalf("status not visible in transcript: %q", got)
	}
}

func TestSandboxSetupHorizontalArrowsAdjustSelectedValue(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.app.Sandbox = testSandboxManager(t, m.app.ProjectInputRoot)

	_, cmd := m.runCommand("/sandbox init")
	if cmd != nil || !m.sandboxSetup {
		t.Fatalf("init did not open setup form: cmd=%v setup=%v", cmd, m.sandboxSetup)
	}

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.sandboxSetupIndex != 1 || m.sandboxSetupOpts.CPUs != 3 {
		t.Fatalf("right arrow navigated instead of adjusting CPUs: index=%d CPUs=%d", m.sandboxSetupIndex, m.sandboxSetupOpts.CPUs)
	}

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.sandboxSetupIndex != 2 || m.sandboxSetupOpts.MemoryMiB != 1024 {
		t.Fatalf("left arrow navigated instead of adjusting memory: index=%d memory=%d", m.sandboxSetupIndex, m.sandboxSetupOpts.MemoryMiB)
	}
}

func TestSandboxInitAndConfirmedDeleteUpdateBoundary(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 140, 30
	m.layout()
	m.app.Sandbox = testSandboxManager(t, m.app.ProjectInputRoot)

	_, cmd := m.runCommand("/sandbox init")
	if cmd != nil || !m.sandboxSetup {
		t.Fatalf("init did not open setup form: cmd=%v setup=%v", cmd, m.sandboxSetup)
	}
	if form := stripANSI(m.renderSandboxSetup()); !strings.Contains(form, "Sandbox setup") || !strings.Contains(form, "2048 MiB") || !strings.Contains(form, "Guest network") {
		t.Fatalf("setup form = %q", form)
	}
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyRight})
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyRight})
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyRight})
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyRight})
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyRight})
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeySpace})
	_, cmd = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.sandboxSetup {
		t.Fatalf("setup submit did not start init: cmd=%v setup=%v", cmd, m.sandboxSetup)
	}
	_, _ = m.Update(cmd())
	if !m.app.Sandbox.Active() {
		t.Fatal("successful init did not activate Bash sandbox")
	}
	if record, ok, err := m.app.Sandbox.Record(); err != nil || !ok || record.CPUs != 4 || record.MemoryMiB != 3072 || record.StorageGiB != 5 || record.OverlayGiB != 5 || !record.ReadOnly || record.Network || record.Profile != "" {
		t.Fatalf("configured sandbox record = %+v, ok=%v err=%v", record, ok, err)
	}
	vmHeader := m.renderHeader("idle")
	if header := stripANSI(vmHeader); !strings.Contains(header, "shell:vm") {
		t.Fatalf("header did not expose VM boundary: %q", header)
	}
	if !strings.Contains(vmHeader, styleDiffAdd.Render("shell:vm")) {
		t.Fatalf("VM boundary was not success-colored: %q", vmHeader)
	}
	_, cmd = m.runCommand("/sandbox stop")
	_, _ = m.Update(cmd())
	if m.app.Sandbox.Active() {
		t.Fatal("sandbox stop retained VM routing")
	}
	hostHeader := m.renderHeader("idle")
	if header := stripANSI(hostHeader); !strings.Contains(header, "shell:host") {
		t.Fatalf("stopped header did not expose host boundary: %q", header)
	}
	if !strings.Contains(hostHeader, styleTool.Render("shell:host")) {
		t.Fatalf("host boundary was not warning-colored: %q", hostHeader)
	}
	_, cmd = m.runCommand("/sandbox start")
	_, _ = m.Update(cmd())
	if !m.app.Sandbox.Active() {
		t.Fatal("sandbox start did not restore VM routing")
	}

	before := len(m.lines)
	_, cmd = m.runCommand("/sandbox delete")
	if cmd != nil || len(m.lines) == before {
		t.Fatal("unconfirmed delete was not rejected synchronously")
	}
	if !m.app.Sandbox.Active() {
		t.Fatal("unconfirmed delete disabled sandbox")
	}

	_, cmd = m.runCommand("/sandbox delete confirm")
	if cmd == nil {
		t.Fatal("confirmed delete returned no command")
	}
	_, _ = m.Update(cmd())
	if m.app.Sandbox.Active() {
		t.Fatal("confirmed delete retained sandbox")
	}
	if got := stripANSI(strings.Join(m.lines, "\n")); !strings.Contains(got, "future Bash commands run on the host") {
		t.Fatalf("delete warning missing: %q", got)
	}
}

func TestSandboxSetupGoProfileUsesRecommendedResourcesAndRestoresCustom(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.app.Sandbox = testSandboxManager(t, m.app.ProjectInputRoot)
	_, _ = m.runCommand("/sandbox init")
	m.sandboxSetupOpts.CPUs = 3
	m.sandboxSetupOpts.MemoryMiB = 3072
	m.selectSandboxProfile(1) // custom -> Ubuntu
	m.selectSandboxProfile(1) // Ubuntu -> Go
	if m.sandboxSetupOpts.Profile != "go" || m.sandboxSetupOpts.CPUs != 4 || m.sandboxSetupOpts.MemoryMiB != 6144 {
		t.Fatalf("Go profile options = %+v", m.sandboxSetupOpts)
	}
	m.selectSandboxProfile(-1) // Go -> Ubuntu
	m.selectSandboxProfile(-1) // Ubuntu -> custom
	if m.sandboxSetupOpts.Profile != "" || m.sandboxSetupOpts.CPUs != 3 || m.sandboxSetupOpts.MemoryMiB != 3072 {
		t.Fatalf("restored custom options = %+v", m.sandboxSetupOpts)
	}
}

func TestSandboxSetupSelectsPythonUVProfileWithNetwork(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.app.Sandbox = testSandboxManager(t, m.app.ProjectInputRoot)
	_, _ = m.runCommand("/sandbox init")
	m.selectSandboxProfile(-1)
	if m.sandboxSetupOpts.Profile != "python" || !m.sandboxSetupOpts.Network || !strings.Contains(m.sandboxSetupOpts.Source, "astral-sh/uv") {
		t.Fatalf("python profile options = %+v", m.sandboxSetupOpts)
	}
	if form := stripANSI(m.renderSandboxSetup()); !strings.Contains(form, "Python 3.12 + uv") || !strings.Contains(form, "required by profile") {
		t.Fatalf("python profile form = %q", form)
	}
}

func TestSandboxSetupRestoresCustomPackAfterProfileCycle(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.app.Sandbox = testSandboxManager(t, m.app.ProjectInputRoot)
	pack := filepath.Join(t.TempDir(), "dev.smolmachine")
	if err := os.WriteFile(pack, []byte("pack"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _ = m.runCommand("/sandbox init --from " + pack)
	if !m.sandboxSetup || m.sandboxSetupOpts.SourceKind != internalsandbox.SourcePack {
		t.Fatalf("pack setup options = %+v", m.sandboxSetupOpts)
	}
	m.selectSandboxProfile(-1)
	if m.sandboxSetupOpts.Profile != "python" || m.sandboxSetupOpts.SourceKind != internalsandbox.SourceImage {
		t.Fatalf("profile cycle options = %+v", m.sandboxSetupOpts)
	}
	m.selectSandboxProfile(1)
	if m.sandboxSetupOpts.Profile != "" || m.sandboxSetupOpts.Source != pack || m.sandboxSetupOpts.SourceKind != internalsandbox.SourcePack || m.sandboxSetupOpts.Network {
		t.Fatalf("restored pack options = %+v", m.sandboxSetupOpts)
	}
	_, cmd := m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("restored pack submit returned no command")
	}
	_, _ = m.Update(cmd())
	record, ok, err := m.app.Sandbox.Record()
	if err != nil || !ok || record.SourceKind != internalsandbox.SourcePack || record.Source != pack {
		t.Fatalf("restored pack record = %+v, ok=%v err=%v", record, ok, err)
	}
}

func TestSandboxSetupShowsConfiguredImageAndCanClearDiskDefaults(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	customImage := "registry.example/ubuntu@sha256:abcdef"
	m.app.Cfg.Sandbox.DefaultImage = customImage
	m.app.Cfg.Sandbox.StorageGiB = 40
	m.app.Cfg.Sandbox.OverlayGiB = 20
	manager, err := internalsandbox.New(internalsandbox.Options{
		ProjectRoot: m.app.ProjectInputRoot, StatePath: filepath.Join(t.TempDir(), "sandboxes.json"),
		Executable: "smolvm", DefaultImage: customImage, CPUs: 2, MemoryMiB: 2048,
		StorageGiB: 40, OverlayGiB: 20, GuestCWD: "/workspace",
		Launcher: tuiSandboxLauncher{}, ImageFetcher: tuiSandboxImageFetcher{},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.app.Sandbox = manager

	_, cmd := m.runCommand("/sandbox init")
	if cmd != nil || !m.sandboxSetup {
		t.Fatalf("setup did not open: cmd=%v setup=%v", cmd, m.sandboxSetup)
	}
	if form := stripANSI(m.renderSandboxSetup()); !strings.Contains(form, customImage) || !strings.Contains(form, "40 GiB") || !strings.Contains(form, "20 GiB") {
		t.Fatalf("configured setup form = %q", form)
	}
	m.sandboxSetupOpts.StorageGiB = 0
	m.sandboxSetupOpts.OverlayGiB = 0
	_, cmd = m.handleSandboxSetupKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("setup submit returned no command")
	}
	_, _ = m.Update(cmd())
	record, ok, err := manager.Record()
	if err != nil || !ok || record.Source != customImage || record.StorageGiB != 0 || record.OverlayGiB != 0 {
		t.Fatalf("configured setup record = %+v, ok=%v err=%v", record, ok, err)
	}
}

func TestSandboxSlashCommandUsage(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.app.Sandbox = testSandboxManager(t, m.app.ProjectInputRoot)
	_, cmd := m.runCommand("/sandbox explode")
	if cmd != nil {
		t.Fatal("invalid sandbox action started work")
	}
	if got := stripANSI(strings.Join(m.lines, "\n")); !strings.Contains(got, "usage: /sandbox") {
		t.Fatalf("usage not shown: %q", got)
	}
}
