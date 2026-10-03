//go:build darwin || linux

package session

import (
	"container/list"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func inventoryTestList(t *testing.T, root, cwd string, count int) []SessionInfo {
	t.Helper()
	rows, err := NewFileIndex(root).List(cwd) // deliberately never reuse the index
	if err != nil || len(rows) != count {
		t.Fatalf("listed %d of %d: %v", len(rows), count, err)
	}
	return rows
}

func inventoryTestEntry(t *testing.T, path, cwd string) *list.Element {
	t.Helper()
	sessionInventoryCache.mu.Lock()
	defer sessionInventoryCache.mu.Unlock()
	entry := sessionInventoryCache.entries[inventoryKey{path: normalizeSessionPath(path), cwd: normalizeCWD(cwd)}]
	if entry == nil {
		t.Fatal("successful stable inspection was not cached")
	}
	return entry
}

func inventoryTestCreate(t *testing.T, path, cwd, id string) {
	t.Helper()
	store, err := NewSQLiteStore(path, cwd, Options{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(msg("message", "", "saved")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryCacheReuseAcrossFreshIndexes(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	path := filepath.Join(root, EncodeCWD(cwd), "saved.db")
	inventoryTestCreate(t, path, cwd, "saved")
	// A first read-only open may create a persistent zero-length WAL. Admission
	// requires equal pre/post stamps, so allow the second scan to warm it.
	inventoryTestList(t, root, cwd, 1)
	inventoryTestList(t, root, cwd, 1)
	entry := inventoryTestEntry(t, path, cwd)
	rows := inventoryTestList(t, root, filepath.Join(cwd, "unused", ".."), 1)
	rows[0].Name = "caller mutation"
	rows = inventoryTestList(t, root, cwd, 1)
	if rows[0].Name != "" || inventoryTestEntry(t, path, cwd) != entry {
		t.Fatal("fresh index did not reuse the immutable inspection (a miss replaces the entry)")
	}
	// Reading the file advances atime on supporting mounts, but not the stamp.
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	inventoryTestList(t, root, cwd, 1)
	if inventoryTestEntry(t, path, cwd) != entry {
		t.Fatal("file access invalidated the cache")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 10 {
				got := inventoryTestList(t, root, cwd, 1)
				if got[0].ID != "saved" || got[0].Name != "" {
					t.Error("concurrent listing returned altered data")
				}
				got[0].Name = "local"
			}
		})
	}
	wg.Wait()
}

func TestInventoryCacheWALOnlyWriteAndRootOnlyDurability(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	store, err := NewFileIndex(root).Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	inventoryTestList(t, root, cwd, 0)
	entry := inventoryTestEntry(t, store.Path(), cwd)
	if entry.Value.(inventoryEntry).include {
		t.Fatal("root-only exclusion not cached")
	}
	inventoryTestList(t, root, cwd, 0)
	if inventoryTestEntry(t, store.Path(), cwd) != entry {
		t.Fatal("root-only exclusion not reused")
	}
	before, ok := fingerprintInventory(store.Path())
	if !ok {
		t.Fatal("could not fingerprint database")
	}
	if err := store.Append(msg("first", "", "durable")); err != nil {
		t.Fatal(err)
	}
	after, ok := fingerprintInventory(store.Path())
	if !ok || !(inventoryFingerprint{before[0]}).equal(inventoryFingerprint{after[0]}) || before.equal(after) {
		t.Fatal("fixture must change WAL without changing DB stamp")
	}
	rows := inventoryTestList(t, root, cwd, 1)
	if rows[0].Messages != 1 {
		t.Fatalf("root-only WAL write was not discovered: %+v", rows)
	}
	if err := store.Append(msg("second", "", "more")); err != nil {
		t.Fatal(err)
	}
	rows = inventoryTestList(t, root, cwd, 1)
	if rows[0].Messages != 2 {
		t.Fatalf("WAL-only count update was not discovered: %+v", rows)
	}
	sqlite := store.(*SQLiteStore)
	if err := sqlite.RenameSession("WAL title"); err != nil {
		t.Fatal(err)
	}
	rows = inventoryTestList(t, root, cwd, 1)
	if rows[0].Name != "WAL title" {
		t.Fatalf("WAL-only title update was not discovered: %+v", rows)
	}
	fork, err := sqlite.ForkBranch("first")
	if err != nil {
		t.Fatal(err)
	}
	rows = inventoryTestList(t, root, cwd, 1)
	if rows[0].Messages != 1 || !fork.Active {
		t.Fatalf("active fork was not inspected: %+v", rows)
	}
	if err := sqlite.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	rows = inventoryTestList(t, root, cwd, 1)
	if rows[0].Messages != 2 || rows[0].Name != "WAL title" {
		t.Fatalf("active branch switch returned stale inspection: %+v", rows)
	}
	after, ok = fingerprintInventory(store.Path())
	if !ok || !(inventoryFingerprint{before[0]}).equal(inventoryFingerprint{after[0]}) {
		t.Fatal("title and branch changes must remain WAL-only")
	}
}

func TestInventoryCacheReplacementAndFreshDiscovery(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	dir := filepath.Join(root, EncodeCWD(cwd))
	path := filepath.Join(dir, "saved.db")
	inventoryTestCreate(t, path, cwd, "original")
	inventoryTestList(t, root, cwd, 1)
	inventoryTestList(t, root, cwd, 1)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(t.TempDir(), "replace.db")
	inventoryTestCreate(t, replacement, cwd, "replaced")
	if err := os.Chtimes(replacement, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	rows := inventoryTestList(t, root, cwd, 1)
	if rows[0].ID != "replaced" {
		t.Fatal("same-path replacement with restored mtime returned old identity")
	}
	added := filepath.Join(dir, "added.db")
	inventoryTestCreate(t, added, cwd, "added")
	inventoryTestList(t, root, cwd, 2)
	renamed := filepath.Join(dir, "renamed.db")
	if err := os.Rename(added, renamed); err != nil {
		t.Fatal(err)
	}
	rows = inventoryTestList(t, root, cwd, 2)
	for _, row := range rows {
		if row.ID == "added" && row.Path != renamed {
			t.Fatal("rename returned stale path")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	rows = inventoryTestList(t, root, cwd, 1)
	if rows[0].ID != "added" {
		t.Fatal("deleted entry survived fresh discovery")
	}
	// A previously eligible cached file must disappear when it becomes aliased.
	alias := filepath.Join(t.TempDir(), "alias.db")
	if err := os.Link(renamed, alias); err != nil {
		t.Fatal(err)
	}
	inventoryTestList(t, root, cwd, 0)
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	inventoryTestList(t, root, cwd, 1)
	if err := os.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(alias, renamed); err != nil {
		t.Fatal(err)
	}
	inventoryTestList(t, root, cwd, 0)
}

func TestInventoryCacheCWDFilteringAndFailureNotRetained(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "a-b", "c"), filepath.Join(root, "a", "b-c")
	path := filepath.Join(root, legacyEncodeCWD(first), "saved.db")
	inventoryTestCreate(t, path, first, "first")
	for range 3 {
		inventoryTestList(t, root, first, 1)
		inventoryTestList(t, root, second, 0)
	}
	if inventoryTestEntry(t, path, second).Value.(inventoryEntry).include {
		t.Fatal("foreign CWD exclusion not cached separately")
	}
	bad := filepath.Join(filepath.Dir(path), "bad.db")
	if err := os.WriteFile(bad, []byte("not SQLite"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventoryTestList(t, root, first, 1)
	sessionInventoryCache.mu.Lock()
	entry := sessionInventoryCache.entries[inventoryKey{path: bad, cwd: normalizeCWD(first)}]
	sessionInventoryCache.mu.Unlock()
	if entry != nil {
		t.Fatal("failed inspection was cached")
	}
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	inventoryTestCreate(t, bad, first, "repaired")
	inventoryTestList(t, root, first, 2)
}

func TestInventoryCacheSameInodeRestoredMtime(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	path := filepath.Join(root, EncodeCWD(cwd), "saved.db")
	inventoryTestCreate(t, path, cwd, "saved")
	db, err := sql.Open("sqlite", sqliteExistingDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA journal_mode=DELETE`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE session_meta SET name='old' WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	inventoryTestList(t, root, cwd, 1)
	entry := inventoryTestEntry(t, path, cwd)
	before, ok := fingerprintInventory(path)
	if !ok {
		t.Fatal("could not fingerprint database")
	}
	if _, err := db.Exec(`UPDATE session_meta SET name='new' WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before[0].info.ModTime(), before[0].info.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, ok := fingerprintInventory(path)
	if !ok || !os.SameFile(before[0].info, after[0].info) || before[0].info.Size() != after[0].info.Size() || !before[0].info.ModTime().Equal(after[0].info.ModTime()) {
		t.Fatal("fixture must preserve inode, size and mtime")
	}
	rows := inventoryTestList(t, root, cwd, 1)
	if rows[0].Name != "new" || inventoryTestEntry(t, path, cwd) == entry {
		t.Fatal("same inode/size/restored mtime change did not invalidate cached inspection via ctime")
	}
}

func TestInventoryCacheBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stamp.db")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, ok := fingerprintInventory(path)
	if !ok {
		t.Fatal("could not fingerprint regular file")
	}
	cache := inventoryCache{entries: make(map[inventoryKey]*list.Element)}
	for i := range inventoryCacheMaxEntries + 1 {
		cache.put(inventoryKey{path: fmt.Sprint(i)}, after, SessionInfo{}, false)
	}
	if len(cache.entries) > inventoryCacheMaxEntries || cache.bytes > inventoryCacheMaxBytes || cache.entries[inventoryKey{path: "0"}] != nil {
		t.Fatal("cache exceeded bounds or failed to evict oldest inspection")
	}
	cache.put(inventoryKey{path: "oversize"}, after, SessionInfo{Name: strings.Repeat("x", inventoryCacheMaxBytes)}, true)
	if cache.entries[inventoryKey{path: "oversize"}] != nil || cache.bytes > inventoryCacheMaxBytes {
		t.Fatal("oversize inspection retained")
	}
}
