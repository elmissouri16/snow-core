package session

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCatalogOmitsUnleasedDatabaseWithoutCreatingLease(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	id, path := catalogFixture(t, root, cwd, "unleased")
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	before := catalogSnapshot(t, root)
	catalog := NewCatalog(root, cwd)
	page, err := catalog.Sessions(t.Context(), 0, 50)
	if err != nil || len(page.Sessions) != 0 {
		t.Fatalf("listed unleased session: %+v %v", page, err)
	}
	if _, err := catalog.Messages(t.Context(), id, 0, 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unleased history: %v", err)
	}
	if !reflect.DeepEqual(before, catalogSnapshot(t, root)) {
		t.Fatal("unleased discovery mutated files or created a lease")
	}
}

func TestCatalogReadDirectoryBoundsEnumerationBeforeVisiting(t *testing.T) {
	// Model an arbitrarily large directory. The fake is called only with a
	// positive, fixed-size bound, and returns no more entries than requested.
	remaining, read, visited := catalogMaxFiles, 0, 0
	err := catalogReadDirectory(t.Context(), func(n int) ([]fs.DirEntry, error) {
		if n < 1 || n > catalogReadDirBatch {
			t.Fatalf("unbounded ReadDir(%d)", n)
		}
		read += n
		return make([]fs.DirEntry, n), nil
	}, &remaining, func(fs.DirEntry) error { visited++; return nil })
	if !errors.Is(err, errCatalogScanLimit) || read != catalogMaxFiles+1 || visited != catalogMaxFiles {
		t.Fatalf("err=%v read=%d visited=%d", err, read, visited)
	}
}

func TestCatalogReadDirectoryChecksCancellationBetweenBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	remaining, calls, visited := catalogMaxFiles, 0, 0
	err := catalogReadDirectory(ctx, func(n int) ([]fs.DirEntry, error) {
		calls++
		return make([]fs.DirEntry, n), nil
	}, &remaining, func(fs.DirEntry) error {
		visited++
		if visited == catalogReadDirBatch {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestCatalogWalkSharesBudgetAcrossDirectoriesAndSkipsChildren(t *testing.T) {
	rootPath := t.TempDir()
	for _, dir := range []string{"first", "second", "first/owner.db.agents"} {
		if err := os.MkdirAll(filepath.Join(rootPath, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"first/a", "first/owner.db.agents/hidden", "second/b", "second/c"} {
		if err := os.WriteFile(filepath.Join(rootPath, path), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	remaining := 4
	var visited []string
	visit := func(path string, _ fs.DirEntry) error { visited = append(visited, path); return nil }
	if err := catalogWalk(t.Context(), root, "first", &remaining, visit); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 || !reflect.DeepEqual(visited, []string{"first/a"}) {
		t.Fatalf("remaining=%d visited=%v", remaining, visited)
	}
	if err := catalogWalk(t.Context(), root, "second", &remaining, visit); !errors.Is(err, errCatalogScanLimit) {
		t.Fatalf("second walk bypassed shared budget: %v", err)
	}
}
