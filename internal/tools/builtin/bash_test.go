package builtin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
	res := runBash(b, dir, map[string]any{"command": "head -c 10000 /dev/zero | tr '\\0' 'x'"})
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

func TestBash_TimeoutKillsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("timeout kill test skipped on windows")
	}
	dir := t.TempDir()
	b := NewBash()
	b.Timeout = 300 * time.Millisecond

	start := time.Now()
	res := runBash(b, dir, map[string]any{"command": "sleep 10"})
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
	res := runBash(b, dir, map[string]any{"command": "sleep 10", "timeout_ms": 300})
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

func TestBash_CWDRespected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewBash()
	res := runBash(b, dir, map[string]any{"command": "ls marker.txt"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "marker.txt") {
		t.Errorf("cwd not respected, output = %q", res.Content[0].Text)
	}
}
