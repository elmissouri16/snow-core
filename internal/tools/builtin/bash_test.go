package builtin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/tools"
)

func runBash(b *Bash, dir string, args map[string]any) tools.ToolResult {
	res, _ := b.Run(context.Background(), argsForT(args), stubHost{cwd: dir, roots: []string{dir}})
	return res
}

func TestBash_SimpleCommand(t *testing.T) {
	dir := t.TempDir()
	b := NewBash()
	res := runBash(b, dir, map[string]any{"command": "echo hello"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "hello") {
		t.Errorf("output = %q, want hello", res.Content[0].Text)
	}
}

func TestBash_ExitCodeReportedNotError(t *testing.T) {
	dir := t.TempDir()
	b := NewBash()
	res := runBash(b, dir, map[string]any{"command": "exit 3"})
	if res.IsError {
		t.Fatalf("non-zero exit should not be a tool error: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "exit code 3") {
		t.Errorf("output should report exit code: %q", res.Content[0].Text)
	}
}

func TestBash_OutputCap(t *testing.T) {
	dir := t.TempDir()
	b := NewBash()
	b.MaxOutputBytes = 256
	res := runBash(b, dir, map[string]any{"command": testOutputCapCommand()})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	if len(res.Content[0].Text) > 256+len("\n... [output truncated]")+8 {
		t.Errorf("output not capped: %d bytes", len(res.Content[0].Text))
	}
	if !strings.Contains(res.Content[0].Text, "output truncated") {
		t.Error("truncation marker missing")
	}
}

func TestBoundedProcessWaitDelay(t *testing.T) {
	if got := boundedProcessWaitDelay(50 * time.Millisecond); got != 50*time.Millisecond {
		t.Fatalf("short wait delay = %s, want 50ms", got)
	}
	if got := boundedProcessWaitDelay(30 * time.Second); got != defaultProcessWaitDelay {
		t.Fatalf("long wait delay = %s, want %s", got, defaultProcessWaitDelay)
	}
}

func TestSanitizeBoundedUTF8(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		cap  int
	}{
		{name: "split rune", data: []byte("abcé"), cap: 4},
		{name: "invalid bytes", data: []byte{'a', 0xff, 'b'}, cap: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeBoundedUTF8(tt.data, tt.cap)
			if !utf8.ValidString(got) {
				t.Fatalf("output is not valid UTF-8: %q", got)
			}
			if len(got) > tt.cap {
				t.Fatalf("output is %d bytes, cap is %d: %q", len(got), tt.cap, got)
			}
		})
	}
}

func TestBash_TimeoutKillsProcess(t *testing.T) {
	dir := t.TempDir()
	b := NewBash()
	b.Timeout = 300 * time.Millisecond

	start := time.Now()
	res := runBash(b, dir, map[string]any{"command": testSleepCommand(10 * time.Second)})
	elapsed := time.Since(start)

	if !res.IsError {
		t.Fatalf("expected timeout error, got %q", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "timed out") {
		t.Errorf("error should mention timeout: %q", res.Content[0].Text)
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout took too long: %s", elapsed)
	}
}

func TestBash_CustomTimeoutMS(t *testing.T) {
	dir := t.TempDir()
	b := NewBash()
	b.Timeout = 30 * time.Second // overridden by args
	start := time.Now()
	res := runBash(b, dir, map[string]any{"command": testSleepCommand(10 * time.Second), "timeout_ms": 300})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("custom timeout took too long: %s", elapsed)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "timed out") {
		t.Errorf("expected timeout from timeout_ms, got %q", res.Content[0].Text)
	}
}

func TestBash_EmptyCommand(t *testing.T) {
	dir := t.TempDir()
	b := NewBash()
	res := runBash(b, dir, map[string]any{"command": ""})
	if !res.IsError {
		t.Fatal("expected error for empty command")
	}
}

type environmentHost struct {
	stubHost
	environ []string
}

func (h environmentHost) Environ() []string { return h.environ }

func TestBashPreservesBackendEnvironment(t *testing.T) {
	dir := t.TempDir()
	b := NewBash()
	b.CommandFactory = func(ctx context.Context, _ string, _ []string, _ time.Duration) (*exec.Cmd, bool, bool, error) {
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `printf %s "$BACKEND_ENV"`)
		cmd.Env = []string{"BACKEND_ENV=preserved"}
		return cmd, false, true, nil
	}
	res, _ := b.Run(context.Background(), argsFor(t, map[string]any{"command": "true"}), environmentHost{
		stubHost: stubHost{cwd: dir, roots: []string{dir}},
		environ:  []string{"BACKEND_ENV=overwritten"},
	})
	if res.IsError || res.Content[0].Text != "preserved" {
		t.Fatalf("backend environment result = %+v", res)
	}
}

func TestBash_BackendRoutesCommandAndFailsClosed(t *testing.T) {
	dir := t.TempDir()
	b := NewBash()
	b.CommandFactory = func(ctx context.Context, command string, _ []string, _ time.Duration) (*exec.Cmd, bool, bool, error) {
		return exec.CommandContext(ctx, "sh", "-c", "printf sandbox"), false, true, nil
	}
	res := runBash(b, dir, map[string]any{"command": "printf host"})
	if res.IsError || res.Content[0].Text != "sandbox" {
		t.Fatalf("sandbox backend result = %+v", res)
	}

	b.CommandFactory = func(context.Context, string, []string, time.Duration) (*exec.Cmd, bool, bool, error) {
		return nil, true, true, errors.New("sandbox unavailable")
	}
	res = runBash(b, dir, map[string]any{"command": "printf must-not-run"})
	if !res.IsError || !strings.Contains(res.Content[0].Text, "sandbox unavailable") {
		t.Fatalf("backend failure did not fail closed: %+v", res)
	}
}

func TestBash_InactiveBackendUsesHostShell(t *testing.T) {
	b := NewBash()
	b.CommandFactory = func(context.Context, string, []string, time.Duration) (*exec.Cmd, bool, bool, error) {
		return nil, false, false, nil
	}
	res := runBash(b, t.TempDir(), map[string]any{"command": "printf host"})
	if res.IsError || res.Content[0].Text != "host" {
		t.Fatalf("inactive backend result = %+v", res)
	}
}

func TestBash_CWDRespected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewBash()
	res := runBash(b, dir, map[string]any{"command": testListCommand("marker.txt")})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "marker.txt") {
		t.Errorf("cwd not respected, output = %q", res.Content[0].Text)
	}
}

// TestBashModelTimeoutBoundedByCap: a model-supplied timeout_ms must never
// override the operator-configured cap (b.Timeout). With a 50ms cap and a
// 60s model timeout, the command must still be killed at ~50ms.
func TestBashHugeTimeoutDoesNotOverflow(t *testing.T) {
	b := NewBash()
	b.Timeout = time.Second
	maxInt := int(^uint(0) >> 1)
	res := runBash(b, t.TempDir(), map[string]any{"command": testPrintCommand("ok"), "timeout_ms": maxInt})
	if res.IsError || res.Content[0].Text != "ok" {
		t.Fatalf("huge timeout overflowed: %+v", res)
	}
}

func TestBashCancellationKillsDescendants(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "escaped.txt")
	b := NewBash()
	b.Timeout = 200 * time.Millisecond
	res := runBash(b, dir, map[string]any{"command": testDescendantCommand(marker)})
	if !res.IsError {
		t.Fatalf("expected timeout: %+v", res)
	}
	time.Sleep(time.Second)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("descendant survived cancellation: %v", err)
	}
}

func TestBashModelTimeoutBoundedByCap(t *testing.T) {
	dir := t.TempDir()
	b := NewBash()
	b.Timeout = 50 * time.Millisecond // operator cap

	start := time.Now()
	res := runBash(b, dir, map[string]any{"command": testSleepCommand(time.Second), "timeout_ms": 60000})
	elapsed := time.Since(start)

	if !res.IsError {
		t.Fatalf("expected timeout error, got %q", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "timed out") {
		t.Errorf("error should mention timeout: %q", res.Content[0].Text)
	}
	// The cap (50ms) must win over the model timeout (60s); if the model
	// timeout had won, this would take ~1s+ anyway, but assert tightness
	// relative to the cap.
	if elapsed > 5*time.Second {
		t.Errorf("command not killed by operator cap: took %s", elapsed)
	}
}
