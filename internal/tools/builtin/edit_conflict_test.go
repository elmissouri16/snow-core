package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/tools"
)

type editSaveHost struct {
	stubHost
	save func()
}

func (h editSaveHost) EmitProgress(ev tools.ToolProgressEvent) {
	if ev.Message == "editing file" {
		h.save()
	}
}

func TestEditRejectsConcurrentSave(t *testing.T) {
	for _, kind := range []string{"atomic", "in-place", "deleted", "identical-new-inode"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "shared.txt")
			original := "target before\nbaseline\n"
			newer := "target before\nnew user work\n"
			if kind == "identical-new-inode" {
				newer = original
			}
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			guard := NewPathGuard([]string{dir}, dir)
			defer guard.Close()
			host := editSaveHost{cwd: dir, roots: []string{dir}, save: func() {
				switch kind {
				case "deleted":
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				case "in-place":
					if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
						t.Fatal(err)
					}
				default:
					stage := filepath.Join(dir, "editor-save")
					if err := os.WriteFile(stage, []byte(newer), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(stage, path); err != nil {
						t.Fatal(err)
					}
				}
			}}
			res, err := NewEdit(guard).Run(t.Context(), argsFor(t, map[string]any{"path": path, "old_str": "target before", "new_str": "target after"}), host)
			if err != nil || !res.IsError || !strings.Contains(res.Content[0].Text, "file changed during edit") {
				t.Fatalf("expected conflict, got %+v, %v", res, err)
			}
			data, err := os.ReadFile(path)
			if kind == "deleted" {
				if !os.IsNotExist(err) {
					t.Fatalf("deleted file was recreated: %q, %v", data, err)
				}
			} else if err != nil || string(data) != newer {
				t.Fatalf("concurrent save changed: %q, %v", data, err)
			}
			stages, err := filepath.Glob(filepath.Join(dir, ".snow-write-*"))
			if err != nil || len(stages) != 0 {
				t.Fatalf("staged files left after conflict: %v, %v", stages, err)
			}
		})
	}
}

func TestEditChecksContentsEvenWithSameMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.txt")
	if err := os.WriteFile(path, []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := NewPathGuard([]string{dir}, dir)
	defer guard.Close()
	target, err := guard.rooted(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &rootedEditSnapshot{info: info, content: "old content"}
	if err := os.WriteFile(path, []byte("new content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	current, err := os.Stat(path)
	if err != nil || !snapshot.matchesInfo(current) {
		t.Fatalf("fixture metadata changed: %v", err)
	}
	err = atomicReplaceRooted(t.Context(), target, []byte("stale edit"), 0o600, true, snapshot)
	if err == nil || !strings.Contains(err.Error(), "file changed during edit") {
		t.Fatalf("same-metadata change not rejected: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new content" {
		t.Fatalf("concurrent content overwritten: %q, %v", data, err)
	}
}

func TestOverlappingFileMutationWaitIsCancelable(t *testing.T) {
	for _, operation := range []string{"edit", "write"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "shared.txt")
			if err := os.WriteFile(path, []byte("first second"), 0o600); err != nil {
				t.Fatal(err)
			}
			guard := NewPathGuard([]string{dir}, dir)
			defer guard.Close()
			// Use a separate guard to exercise coordination across tool instances.
			other := NewPathGuard([]string{dir}, dir)
			defer other.Close()
			host := editSaveHost{cwd: dir, roots: []string{dir}, save: func() {
				ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				defer cancel()
				var res tools.ToolResult
				var err error
				if operation == "edit" {
					res, err = NewEdit(other).Run(ctx, argsFor(t, map[string]any{"path": path, "old_str": "second", "new_str": "SECOND"}), stubHost{})
				} else {
					res, err = NewWrite(other).Run(ctx, argsFor(t, map[string]any{"path": path, "content": "replacement"}), stubHost{})
				}
				if err != nil || !res.IsError || !strings.Contains(res.Content[0].Text, context.DeadlineExceeded.Error()) {
					t.Fatalf("overlapping mutation did not wait for cancellation: %+v, %v", res, err)
				}
			}}
			res, err := NewEdit(guard).Run(t.Context(), argsFor(t, map[string]any{"path": path, "old_str": "first", "new_str": "FIRST"}), host)
			if err != nil || res.IsError {
				t.Fatalf("first edit failed: %+v, %v", res, err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "FIRST second" {
				t.Fatalf("unexpected result: %q, %v", data, err)
			}
		})
	}
}

func TestConcurrentEditsPreserveEachChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.txt")
	const count = 12
	var before, after strings.Builder
	for i := range count {
		fmt.Fprintf(&before, "before-%02d\n", i)
		fmt.Fprintf(&after, "after-%02d\n", i)
	}
	if err := os.WriteFile(path, []byte(before.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := NewPathGuard([]string{dir}, dir)
	defer guard.Close()
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range count {
		args := argsFor(t, map[string]any{"path": path, "old_str": fmt.Sprintf("before-%02d", i), "new_str": fmt.Sprintf("after-%02d", i)})
		wg.Go(func() {
			<-start
			res, err := NewEdit(guard).Run(t.Context(), args, stubHost{})
			if err != nil || res.IsError {
				t.Errorf("edit failed: %+v, %v", res, err)
			}
		})
	}
	close(start)
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != after.String() {
		t.Fatalf("edits lost: %q, %v", data, err)
	}
}
