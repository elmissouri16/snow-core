package session

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	catalogMaxFiles         = 4096
	catalogMaxDatabaseBytes = 64 << 20
	catalogQueryTimeout     = 5 * time.Second
	catalogMaxFieldBytes    = 4096
)

// Catalog is an inactive-session, read-only view of one project's session root.
// It never creates directories, leases, schemas, journals, or active sessions.
// Active or unleased databases and databases needing WAL recovery are omitted.
type Catalog struct{ root, cwd string }

func NewCatalog(root, cwd string) *Catalog {
	if root == "" {
		root = DefaultSessionsRoot()
	}
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	return &Catalog{root: root, cwd: normalizeCWD(cwd)}
}

func catalogPage(offset, limit int) (int, error) {
	if limit == 0 {
		limit = protocol.RPCCatalogDefaultLimit
	}
	if offset < 0 || offset > maxSessionQueryDepth || limit < 1 || limit > protocol.RPCCatalogMaxLimit {
		return 0, errors.New("catalog: invalid offset or limit")
	}
	return limit, nil
}

type catalogEntry struct {
	protocol.RPCSessionSummary
	path, tip string
}

// Sessions returns a bounded page, sorted by modification time then session ID.
func (c *Catalog) Sessions(ctx context.Context, offset, limit int) (protocol.RPCCatalogSessionsPage, error) {
	page := protocol.RPCCatalogSessionsPage{Sessions: []protocol.RPCSessionSummary{}, Offset: offset, NextOffset: offset}
	limit, err := catalogPage(offset, limit)
	if err != nil {
		return page, err
	}
	ctx, cancel := context.WithTimeout(ctx, catalogQueryTimeout)
	defer cancel()
	entries, err := c.entries(ctx)
	if err != nil {
		return page, err
	}
	start := min(offset, len(entries))
	end := min(start+limit, len(entries))
	for _, entry := range entries[start:end] {
		page.Sessions = append(page.Sessions, entry.RPCSessionSummary)
	}
	page.NextOffset = offset + len(page.Sessions)
	page.HasMore = end < len(entries)
	return page, nil
}

func (c *Catalog) entries(ctx context.Context) ([]catalogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(c.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("catalog: cannot open sessions root")
	}
	defer root.Close()
	var entries []catalogEntry
	seen := map[string]bool{}
	remaining := catalogMaxFiles
	for _, dir := range []string{EncodeCWD(c.cwd), legacyEncodeCWD(c.cwd)} {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		err := catalogWalk(ctx, root, dir, &remaining, func(path string, entry fs.DirEntry) error {
			if !strings.HasSuffix(path, ".db") || !entry.Type().IsRegular() {
				return nil
			}
			db, closeDB, info, err := c.open(ctx, root, path)
			if err != nil {
				return ctx.Err()
			}
			defer closeDB()
			item, err := c.inspect(ctx, db, path, info.ModTime().UnixMilli())
			if err == nil {
				entries = append(entries, item)
			}
			return ctx.Err()
		})
		if err != nil {
			return nil, err
		}
	}
	slices.SortFunc(entries, func(a, b catalogEntry) int {
		return cmp.Or(cmp.Compare(b.UpdatedAt, a.UpdatedAt), strings.Compare(a.SessionID, b.SessionID), strings.Compare(a.path, b.path))
	})
	return entries, nil
}

// open uses the existing read-only DSN with immutable=1 so SQLite cannot create
// or modify WAL/SHM sidecars. An existing lifetime lease is exclusively acquired
// without O_CREATE; a live runtime's shared lease therefore excludes the file.
func (c *Catalog) open(ctx context.Context, root *os.Root, path string) (*sql.DB, func(), os.FileInfo, error) {
	before, err := root.Lstat(path)
	if err != nil {
		return nil, nil, nil, err
	}
	if !before.Mode().IsRegular() || !singleLink(before) || before.Size() > catalogMaxDatabaseBytes {
		return nil, nil, nil, errors.New("catalog: unsupported database file")
	}
	// Reject symlinked ancestors as well as symlinked database files.
	for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
		info, err := root.Lstat(parent)
		if err != nil || !info.IsDir() {
			return nil, nil, nil, errors.New("catalog: unsafe database directory")
		}
	}
	var lease *os.File
	if info, err := root.Lstat(path + ".lock"); err == nil {
		if !info.Mode().IsRegular() || !singleLink(info) {
			return nil, nil, nil, errors.New("catalog: unsafe lease")
		}
		lease, err = root.Open(path + ".lock")
		if err != nil {
			return nil, nil, nil, err
		}
		opened, statErr := lease.Stat()
		if statErr != nil || !os.SameFile(info, opened) {
			_ = lease.Close()
			return nil, nil, nil, errors.New("catalog: lease changed")
		}
		if err := tryLockSessionExclusive(lease); err != nil {
			_ = lease.Close()
			return nil, nil, nil, err
		}
	} else {
		// No lease means inactivity cannot be established: a runtime could
		// create its lease immediately after our check. Never create one here.
		return nil, nil, nil, err
	}
	cleanup := func() {
		if lease != nil {
			unlockSessionFile(lease)
			_ = lease.Close()
		}
	}
	for _, suffix := range []string{"-wal", "-journal"} {
		if info, err := root.Lstat(path + suffix); err == nil {
			if !info.Mode().IsRegular() || info.Size() != 0 {
				cleanup()
				return nil, nil, nil, errors.New("catalog: database requires recovery")
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			cleanup()
			return nil, nil, nil, err
		}
	}
	if err := c.validateCatalogPath(root, path, before); err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	u, err := url.Parse(sqliteReadOnlyDSN(filepath.Join(c.root, path)))
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	query := u.Query()
	query.Set("immutable", "1")
	query.Add("_pragma", "temp_store(2)")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	db.SetMaxOpenConns(1)
	closeDB := func() { _ = db.Close(); cleanup() }
	if err := db.PingContext(ctx); err != nil {
		closeDB()
		return nil, nil, nil, err
	}
	if err := c.validateCatalogPath(root, path, before); err != nil {
		closeDB()
		return nil, nil, nil, err
	}
	after, err := root.Lstat(path)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		closeDB()
		return nil, nil, nil, errors.New("catalog: database changed")
	}
	return db, closeDB, before, nil
}

func (c *Catalog) inspect(ctx context.Context, db *sql.DB, path string, updated int64) (catalogEntry, error) {
	item := catalogEntry{path: path}
	var cwd string
	var version int
	err := db.QueryRowContext(ctx, `SELECT m.version,m.session_id,m.cwd,m.name,m.created_at,m.branch_tip
		FROM session_meta m JOIN entries r ON r.id='root' AND r.parent_id='' AND r.entry_type='meta' AND r.meta_key='root' AND r.meta_value=m.session_id
		WHERE m.singleton=1 AND length(m.session_id)<=? AND length(m.cwd)<=? AND length(m.name)<=?
		AND EXISTS(SELECT 1 FROM entries WHERE id=m.branch_tip)`, catalogMaxFieldBytes, catalogMaxFieldBytes, catalogMaxFieldBytes).
		Scan(&version, &item.SessionID, &cwd, &item.Name, &item.CreatedAt, &item.tip)
	if err != nil {
		return item, err
	}
	if version < 1 || version > SessionVersion || item.SessionID == "" || !sameCWD(cwd, c.cwd) {
		return item, ErrNotFound
	}
	item.UpdatedAt = updated
	var durable bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM entries WHERE id<>'root') OR EXISTS(SELECT 1 FROM session_meta WHERE name<>'')`).Scan(&durable); err != nil {
		return item, err
	}
	// Current schemas also preserve goal-only and branch-only durable sessions.
	var extra bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM thread_goals) OR EXISTS(SELECT 1 FROM subagent_threads) OR EXISTS(SELECT 1 FROM session_branches WHERE branch_id<>'main') OR EXISTS(SELECT 1 FROM thread_state WHERE collaboration_mode<>'default') OR EXISTS(SELECT 1 FROM thread_goal_deferrals WHERE deferred<>0)`).Scan(&extra)
	if err != nil && !strings.Contains(err.Error(), "no such table") {
		return item, err
	}
	if !durable && !extra {
		return item, ErrNotFound
	}
	var branchUpdated int64
	err = db.QueryRowContext(ctx, `SELECT tip_id,updated_at FROM session_branches WHERE active=1 ORDER BY created_at LIMIT 1`).Scan(&item.tip, &branchUpdated)
	if err != nil && !strings.Contains(err.Error(), "no such table") {
		return item, err
	}
	item.UpdatedAt = max(item.UpdatedAt, branchUpdated)
	var depth int
	err = db.QueryRowContext(ctx, catalogBranchSQL+`SELECT count(*) FILTER (WHERE entry_type='message'),COALESCE(max(depth),0) FROM branch`, item.tip, maxSessionQueryDepth).Scan(&item.Messages, &depth)
	item.MessagesCapped = depth >= maxSessionQueryDepth
	return item, err
}

const catalogBranchSQL = `WITH RECURSIVE branch(id,parent_id,entry_type,depth) AS (
	SELECT id,parent_id,entry_type,0 FROM entries WHERE id=?
	UNION ALL SELECT e.id,e.parent_id,e.entry_type,b.depth+1 FROM entries e JOIN branch b ON e.id=b.parent_id WHERE b.depth<?
) `

// SQLite takes a pathname, not an os.Root handle. Revalidate the absolute
// pathname against the pinned root and database on both sides of opening it;
// replacing the configured root must not silently switch the catalog's scope.
func (c *Catalog) validateCatalogPath(root *os.Root, path string, expected os.FileInfo) error {
	pinnedRoot, err := root.Stat(".")
	if err != nil {
		return errors.New("catalog: sessions root unavailable")
	}
	currentRoot, err := os.Stat(c.root)
	if err != nil || !os.SameFile(pinnedRoot, currentRoot) {
		return errors.New("catalog: sessions root changed")
	}
	for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
		pinned, err := root.Lstat(parent)
		if err != nil || !pinned.IsDir() {
			return errors.New("catalog: unsafe database directory")
		}
		current, err := os.Lstat(filepath.Join(c.root, parent))
		if err != nil || !current.IsDir() || !os.SameFile(pinned, current) {
			return errors.New("catalog: database directory changed")
		}
	}
	current, err := os.Lstat(filepath.Join(c.root, path))
	if err != nil || !current.Mode().IsRegular() || !singleLink(current) || !os.SameFile(expected, current) || expected.Size() != current.Size() || !expected.ModTime().Equal(current.ModTime()) {
		return errors.New("catalog: database path changed")
	}
	return nil
}
