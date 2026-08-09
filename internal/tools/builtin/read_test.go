package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/tools"
)

type stubHost struct {
	cwd   string
	roots []string
}

func (s stubHost) CWD() string     { return s.cwd }
func (s stubHost) Roots() []string { return s.roots }
func (s stubHost) Permission() permission.Service {
	return permission.NewService(permission.ModeAllow, nil)
}
func (s stubHost) EmitProgress(tools.ToolProgressEvent) {}
func (s stubHost) Environ() []string                    { return nil }

// Ensure stubHost satisfies tools.ToolHost.
var _ tools.ToolHost = stubHost{}

func argsFor(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// argsForT marshals args without needing a *testing.T (for helpers).
func argsForT(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestRead_Basic(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRead(NewPathGuard([]string{dir}, dir))
	res, _ := r.Run(context.Background(), argsFor(t, map[string]any{"path": file}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "line2") {
		t.Errorf("content missing line2: %q", res.Content[0].Text)
	}
}

func TestRead_OffsetLimit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("l1\nl2\nl3\nl4\nl5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRead(NewPathGuard([]string{dir}, dir))

	offset, limit := 2, 2
	res, _ := r.Run(context.Background(), argsFor(t, map[string]any{"path": file, "offset": offset, "limit": limit}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	got := res.Content[0].Text
	if strings.Contains(got, "l1") || strings.Contains(got, "l4") {
		t.Errorf("offset/limit wrong, got %q", got)
	}
	if !strings.Contains(got, "l2") || !strings.Contains(got, "l3") {
		t.Errorf("offset/limit wrong, got %q", got)
	}
}

func TestRead_OffsetLimitPreservesEmptyLines(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "empty-lines.txt")
	if err := os.WriteFile(file, []byte("first\n\nthird\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRead(NewPathGuard([]string{dir}, dir))
	offset, limit := 2, 2
	res, _ := r.Run(context.Background(), argsFor(t, map[string]any{"path": file, "offset": offset, "limit": limit}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].Text)
	}
	if got := res.Content[0].Text; got != "\nthird" {
		t.Fatalf("window = %q, want empty line plus third", got)
	}
}

func TestRead_MissingFile(t *testing.T) {
	dir := t.TempDir()
	r := NewRead(NewPathGuard([]string{dir}, dir))
	res, _ := r.Run(context.Background(), argsFor(t, map[string]any{"path": filepath.Join(dir, "nope.txt")}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError {
		t.Fatal("expected error result for missing file")
	}
}

func TestRead_BinaryDetection(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bin.dat")
	if err := os.WriteFile(file, append([]byte("AB\x00CD"), make([]byte, 100)...), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRead(NewPathGuard([]string{dir}, dir))
	res, _ := r.Run(context.Background(), argsFor(t, map[string]any{"path": file}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError {
		t.Fatal("expected binary rejection")
	}
	if !strings.Contains(res.Content[0].Text, "binary") {
		t.Errorf("error should mention binary, got %q", res.Content[0].Text)
	}
}

func TestRead_Truncation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(file, []byte(strings.Repeat("x", 10000)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRead(NewPathGuard([]string{dir}, dir))
	r.MaxOutputBytes = 128
	res, _ := r.Run(context.Background(), argsFor(t, map[string]any{"path": file}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("truncation should not be an error: %s", res.Content[0].Text)
	}
	if len(res.Content[0].Text) > 128+len(truncationMarker)+8 {
		t.Errorf("output not capped: %d bytes", len(res.Content[0].Text))
	}
	if !strings.Contains(res.Content[0].Text, truncationMarker) {
		t.Error("truncation marker missing")
	}
}

func TestRead_PathEscapeDenied(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(file, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRead(NewPathGuard([]string{dir}, dir))
	res, _ := r.Run(context.Background(), argsFor(t, map[string]any{"path": file}), stubHost{cwd: dir, roots: []string{dir}})
	if !res.IsError {
		t.Fatal("expected escape rejection")
	}
}

// TestReadRejectsFIFO: reading a FIFO must return an error result instead of
// blocking the agent turn on a non-regular file.
func TestReadRejectsFIFO(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO test skipped on windows")
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	r := NewRead(NewPathGuard([]string{dir}, dir))

	done := make(chan tools.ToolResult, 1)
	go func() {
		res, _ := r.Run(context.Background(), argsForT(map[string]any{"path": fifo}), stubHost{cwd: dir, roots: []string{dir}})
		done <- res
	}()
	select {
	case res := <-done:
		if !res.IsError {
			t.Fatalf("expected error result for FIFO, got %q", res.Content[0].Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read on FIFO hung (non-regular file not rejected)")
	}
}

// TestReadUtf8TruncationBoundary: truncating to the byte cap must never split
// a multi-byte UTF-8 rune; the result stays valid UTF-8 and carries the
// truncation marker.
func TestReadUtf8TruncationBoundary(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "utf8.txt")
	// "é" is 2 bytes; 100 repetitions is 200 bytes. Cap=11 lands mid-rune
	// when slicing raw bytes (10 valid + 1 partial), exercising the boundary.
	content := strings.Repeat("é", 100)
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRead(NewPathGuard([]string{dir}, dir))
	r.MaxOutputBytes = 11
	res, _ := r.Run(context.Background(), argsForT(map[string]any{"path": file}), stubHost{cwd: dir, roots: []string{dir}})
	if res.IsError {
		t.Fatalf("truncation should not be an error: %s", res.Content[0].Text)
	}
	out := res.Content[0].Text
	if !utf8.ValidString(out) {
		t.Fatalf("truncated content is not valid UTF-8: %q", out)
	}
	if !strings.HasSuffix(out, truncationMarker) {
		t.Errorf("truncation marker missing: %q", out)
	}
	// The truncated body (before the marker) must be ≤ cap bytes.
	body := strings.TrimSuffix(out, truncationMarker)
	if len(body) > 11 {
		t.Errorf("truncated body is %d bytes, cap is 11", len(body))
	}
}
