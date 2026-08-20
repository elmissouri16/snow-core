package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/elmissouri16/snow-core/internal/config"
	internalsandbox "github.com/elmissouri16/snow-core/internal/sandbox"
)

type cliFakeImageFetcher struct{}

func (cliFakeImageFetcher) Fetch(_ context.Context, _ string, destination string) (internalsandbox.ImageFetchResult, error) {
	data := []byte("docker archive")
	if err := os.WriteFile(destination, data, 0o600); err != nil {
		return internalsandbox.ImageFetchResult{}, err
	}
	return internalsandbox.ImageFetchResult{ArchiveSHA256: sha256.Sum256(data)}, nil
}

func TestSandboxCLILifecycleAndJSON(t *testing.T) {
	sandboxImageFetcher = cliFakeImageFetcher{}
	t.Cleanup(func() { sandboxImageFetcher = nil })
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	fake := filepath.Join(home, "smolvm")
	script := `#!/bin/sh
case "$*" in
  "--version") echo "smolvm 1.8.1" ;;
  *"machine status"*) echo "State: Running" ;;
  *) echo "ok" ;;
esac
exit 0
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "mkfs.ext4"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", home+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := config.Default()
	cfg.Sandbox.Executable = fake
	if err := config.Save(filepath.Join(home, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}

	output, err := runSandboxCLI("sandbox", "status")
	if err != nil || !strings.Contains(output, "not initialized") {
		t.Fatalf("initial status: output=%q err=%v", output, err)
	}
	output, err = runSandboxCLI("sandbox", "init", "--cpus", "4", "--memory", "4096", "--storage", "40", "--overlay", "20")
	if err != nil || !strings.Contains(output, "sandbox: configured") || !strings.Contains(output, "network: disabled") {
		t.Fatalf("init: output=%q err=%v", output, err)
	}
	output, err = runSandboxCLI("sandbox", "--json", "status")
	if err != nil {
		t.Fatalf("JSON status: %v (%q)", err, output)
	}
	var status internalsandbox.Status
	if err := json.Unmarshal([]byte(output), &status); err != nil || !status.Initialized || status.Record.Executable != fake || status.Record.Source != config.DefaultUbuntuImage || status.Record.CPUs != 4 || status.Record.MemoryMiB != 4096 || status.Record.StorageGiB != 40 || status.Record.OverlayGiB != 20 {
		t.Fatalf("JSON status = %+v parse=%v output=%q", status, err, output)
	}
	if output, err = runSandboxCLI("sandbox", "stop"); err != nil || !strings.Contains(output, "Bash routing: host") {
		t.Fatalf("stop: output=%q err=%v", output, err)
	}
	if output, err = runSandboxCLI("sandbox", "start"); err != nil || !strings.Contains(output, "Bash routing: VM") {
		t.Fatalf("start: output=%q err=%v", output, err)
	}
	if _, err := runSandboxCLI("sandbox", "delete"); err == nil || !strings.Contains(err.Error(), "requires --force") {
		t.Fatalf("unconfirmed delete error = %v", err)
	}
	if err := os.Remove(fake); err != nil {
		t.Fatal(err)
	}
	output, err = runSandboxCLI("sandbox", "delete", "--force", "--forget", "--json")
	if err != nil || !strings.Contains(output, `"forgotten":true`) || !strings.Contains(output, `"machine_deleted":false`) || strings.Contains(output, `"deleted":true`) {
		t.Fatalf("forget: output=%q err=%v", output, err)
	}
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runSandboxCLI("sandbox", "init", "ubuntu@sha256:abc"); err != nil {
		t.Fatal(err)
	}
	output, err = runSandboxCLI("sandbox", "delete", "--force")
	if err != nil || !strings.Contains(output, "sandbox deleted") {
		t.Fatalf("delete: output=%q err=%v", output, err)
	}
	output, err = runSandboxCLI("sandbox", "init", "--profile", "python")
	if err != nil || !strings.Contains(output, "profile: python") || !strings.Contains(output, "guest network: enabled") {
		t.Fatalf("profile init: output=%q err=%v", output, err)
	}

	// Simulate a Snow upgrade changing the digest-pinned built-in profile.
	statePath := filepath.Join(home, "sandboxes.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Projects map[string]map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	for project, record := range state.Projects {
		source, ok := record["source"].(string)
		if !ok {
			t.Fatalf("sandbox source = %#v", record["source"])
		}
		record["source"] = source + "-obsolete"
		state.Projects[project] = record
	}
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runSandboxCLI("sandbox", "status"); err == nil || !strings.Contains(err.Error(), "sandbox delete --force") {
		t.Fatalf("stale profile status error = %v", err)
	}
	if _, err := runSandboxCLI("sandbox", "delete", "--force"); err != nil {
		t.Fatalf("delete stale profile: %v", err)
	}
}

func TestSandboxCLIExplicitMissingExecutableDoesNotAutoInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	cfg := config.Default()
	cfg.Sandbox.Executable = filepath.Join(home, "missing-smolvm")
	if err := config.Save(filepath.Join(home, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	_, err := runSandboxCLI("sandbox", "init")
	if err == nil || !strings.Contains(err.Error(), "missing-smolvm") || strings.Contains(err.Error(), "install smolvm") {
		t.Fatalf("explicit missing executable error = %v", err)
	}
}

func runSandboxCLI(args ...string) (string, error) {
	root := &cobra.Command{Use: "snow", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", "", "")
	root.AddCommand(sandboxCmd())
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(args)
	err := root.Execute()
	return output.String(), err
}
