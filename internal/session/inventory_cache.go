package session

import (
	"container/list"
	"os"
	"path/filepath"
	"sync"
)

// Inventory is still discovered on every List. Only complete successful SQLite
// inspections are retained, including exclusions (foreign CWD or root-only).
// This cache is shared because callers commonly construct a fresh FileIndex.
var sessionInventoryCache = inventoryCache{entries: make(map[inventoryKey]*list.Element)}

const (
	inventoryCacheMaxEntries = 4096
	inventoryCacheMaxBytes   = 8 << 20
)

type inventoryKey struct{ path, cwd string }

type inventoryFingerprint [3]inventoryFileStamp

type inventoryFileStamp struct {
	info  os.FileInfo // nil means absent; SameFile checks identity, not just times
	ctime int64
}

func fingerprintInventory(path string) (inventoryFingerprint, bool) {
	var fingerprint inventoryFingerprint
	for i, suffix := range [...]string{"", "-wal", "-journal"} {
		info, err := os.Lstat(path + suffix)
		if err != nil {
			if i != 0 && os.IsNotExist(err) {
				continue
			}
			return inventoryFingerprint{}, false
		}
		if !info.Mode().IsRegular() || !singleLink(info) {
			return inventoryFingerprint{}, false
		}
		ctime, ok := inventoryChangeTime(info)
		if !ok {
			return inventoryFingerprint{}, false
		}
		fingerprint[i] = inventoryFileStamp{info: info, ctime: ctime}
	}
	return fingerprint, true
}

func (a inventoryFingerprint) equal(b inventoryFingerprint) bool {
	for i, before := range a {
		after := b[i]
		if before.info == nil || after.info == nil {
			if before.info != nil || after.info != nil {
				return false
			}
			continue
		}
		if !os.SameFile(before.info, after.info) || before.info.Size() != after.info.Size() ||
			!before.info.ModTime().Equal(after.info.ModTime()) || before.info.Mode() != after.info.Mode() || before.ctime != after.ctime {
			return false
		}
	}
	return true
}

type inventoryEntry struct {
	key         inventoryKey
	fingerprint inventoryFingerprint
	row         SessionInfo
	include     bool
	bytes       int
}

type inventoryCache struct {
	mu      sync.Mutex
	entries map[inventoryKey]*list.Element
	lru     list.List
	bytes   int
}

func (c *inventoryCache) get(key inventoryKey, fingerprint inventoryFingerprint) (SessionInfo, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element := c.entries[key]
	if element == nil {
		return SessionInfo{}, false, false
	}
	entry := element.Value.(inventoryEntry)
	if !entry.fingerprint.equal(fingerprint) {
		c.remove(element)
		return SessionInfo{}, false, false
	}
	c.lru.MoveToFront(element)
	return entry.row, entry.include, true // value copy; all row fields are immutable
}

func (c *inventoryCache) put(key inventoryKey, fingerprint inventoryFingerprint, row SessionInfo, include bool) {
	// Conservatively account for the fixed entry/map/list and three FileInfo
	// allocations as well as all variable-length strings. Oversize results are
	// never retained (branch fingerprints, in particular, can grow large).
	row.Path = "" // restore the caller's spelling on return, not the first caller's
	bytes := 2048 + len(key.path) + len(key.cwd) + len(row.ID) + len(row.CWD) + len(row.Name) + len(row.searchFingerprint)
	for _, stamp := range fingerprint {
		if stamp.info != nil {
			bytes += len(stamp.info.Name())
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element := c.entries[key]; element != nil {
		c.remove(element)
	}
	if bytes > inventoryCacheMaxBytes {
		return
	}
	for len(c.entries) >= inventoryCacheMaxEntries || c.bytes+bytes > inventoryCacheMaxBytes {
		c.remove(c.lru.Back())
	}
	c.entries[key] = c.lru.PushFront(inventoryEntry{key: key, fingerprint: fingerprint, row: row, include: include, bytes: bytes})
	c.bytes += bytes
}

func (c *inventoryCache) remove(element *list.Element) {
	entry := element.Value.(inventoryEntry)
	delete(c.entries, entry.key)
	c.bytes -= entry.bytes
	c.lru.Remove(element)
}

// Only listing uses this shortcut. Real opens and deletion identity validation
// must always inspect the current database through their existing paths.
func inspectInventorySession(path, cwd string, updatedAt int64) (SessionInfo, bool, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return inspectSQLiteSession(path, cwd, updatedAt)
	}
	key := inventoryKey{path: absolute, cwd: normalizeCWD(cwd)}
	before, cacheable := fingerprintInventory(absolute)
	if cacheable {
		if row, include, hit := sessionInventoryCache.get(key, before); hit {
			row.Path = path
			return row, include, nil
		}
		updatedAt = before[0].info.ModTime().UnixMilli()
	}
	row, include, err := inspectSQLiteSession(path, cwd, updatedAt)
	if err == nil && cacheable {
		if after, ok := fingerprintInventory(absolute); ok && before.equal(after) {
			sessionInventoryCache.put(key, after, row, include)
		}
	}
	return row, include, err
}
