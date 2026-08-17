package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
	_ "modernc.org/sqlite"
)

// ValidateSQLiteSession verifies that path is an existing Snow session without
// changing its contents or permissions. Resume-oriented surfaces use this
// before runtime construction so an unrelated SQLite database is never opened
// through the schema-migrating store constructor.
func ValidateSQLiteSession(path string) error {
	if path == "" {
		return errors.New("session: sqlite path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("session: stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("session: sqlite path is not a regular file")
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return fmt.Errorf("session: validate sqlite open: %w", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("session: validate sqlite ping: %w", err)
	}
	return validateSQLiteSessionDB(db)
}

func inspectSQLiteSession(path, cwd string, updatedAt int64) (SessionInfo, bool, error) {
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return SessionInfo{}, false, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return SessionInfo{}, false, err
	}
	if err := validateSQLiteSessionDB(db); err != nil {
		return SessionInfo{}, false, err
	}
	var header Header
	var tip string
	if err := db.QueryRow(`SELECT version, session_id, created_at, cwd, name, branch_tip FROM session_meta WHERE singleton=1`).
		Scan(&header.Version, &header.ID, &header.CreatedAt, &header.CWD, &header.Name, &tip); err != nil {
		return SessionInfo{}, false, err
	}
	if !sameCWD(header.CWD, cwd) {
		return SessionInfo{}, false, nil
	}
	var durable int
	err = db.QueryRow(`SELECT
		(SELECT count(*) FROM entries WHERE id <> 'root') +
		(SELECT count(*) FROM session_meta WHERE name <> '') +
		(SELECT count(*) FROM subagent_threads) +
		(SELECT count(*) FROM thread_goals) +
		(SELECT count(*) FROM session_branches WHERE branch_id <> 'main') +
		(SELECT count(*) FROM thread_state WHERE collaboration_mode <> 'default') +
		(SELECT count(*) FROM thread_goal_deferrals WHERE deferred <> 0)`).Scan(&durable)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table") {
		err = db.QueryRow(`SELECT
			(SELECT count(*) FROM entries WHERE id <> 'root') +
			(SELECT count(*) FROM session_meta WHERE name <> '')`).Scan(&durable)
	}
	if err != nil {
		return SessionInfo{}, false, err
	}
	if durable == 0 {
		return SessionInfo{}, false, nil
	}
	activeTip := tip
	if err := db.QueryRow(`SELECT tip_id FROM session_branches WHERE active=1 ORDER BY created_at LIMIT 1`).Scan(&activeTip); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table") {
		return SessionInfo{}, false, err
	}
	var fingerprint strings.Builder
	fmt.Fprintf(&fingerprint, "%s\x00%s\x00", header.ID, header.Name)
	branchRows, branchErr := db.Query(`SELECT branch_id, branch_name, tip_id, updated_at FROM session_branches ORDER BY branch_id`)
	if branchErr != nil && strings.Contains(strings.ToLower(branchErr.Error()), "no such table") {
		fmt.Fprintf(&fingerprint, "main\x00main\x00%s\x00", activeTip)
	} else if branchErr != nil {
		return SessionInfo{}, false, branchErr
	} else {
		for branchRows.Next() {
			var id, name, branchTip string
			var branchUpdated int64
			if err := branchRows.Scan(&id, &name, &branchTip, &branchUpdated); err != nil {
				_ = branchRows.Close()
				return SessionInfo{}, false, err
			}
			fmt.Fprintf(&fingerprint, "%s\x00%s\x00%s\x00%d\x00", id, name, branchTip, branchUpdated)
		}
		if err := branchRows.Err(); err != nil {
			_ = branchRows.Close()
			return SessionInfo{}, false, err
		}
		if err := branchRows.Close(); err != nil {
			return SessionInfo{}, false, err
		}
	}
	var messages int
	if err := db.QueryRow(`WITH RECURSIVE branch(id, parent_id, entry_type) AS (
		SELECT id, parent_id, entry_type FROM entries WHERE id = ?
		UNION ALL
		SELECT e.id, e.parent_id, e.entry_type FROM entries e JOIN branch b ON e.id = b.parent_id
	) SELECT count(*) FROM branch WHERE entry_type = ?`, activeTip, EntryMessage).Scan(&messages); err != nil {
		return SessionInfo{}, false, err
	}
	return SessionInfo{Path: path, ID: header.ID, CWD: header.CWD, Name: header.Name, CreatedAt: header.CreatedAt, UpdatedAt: updatedAt, Messages: messages, searchFingerprint: fingerprint.String()}, true, nil
}

func subagentChildSessionPaths(path string) (map[string]bool, error) {
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT child_session_path FROM subagent_threads WHERE child_session_path <> ''`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	paths := make(map[string]bool)
	for rows.Next() {
		var childPath string
		if err := rows.Scan(&childPath); err != nil {
			return nil, err
		}
		if absolute, err := filepath.Abs(childPath); err == nil {
			paths[filepath.Clean(absolute)] = true
		}
	}
	return paths, rows.Err()
}

func validateSQLiteSessionDB(db *sql.DB) error {
	var version, count int
	var id, cwd, name, tip string
	var createdAt int64
	if err := db.QueryRow(`SELECT count(*) FROM session_meta`).Scan(&count); err != nil {
		return fmt.Errorf("session: invalid sqlite session metadata: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("session: invalid sqlite session metadata rows: %d", count)
	}
	if err := db.QueryRow(`SELECT version, session_id, created_at, cwd, name, branch_tip FROM session_meta WHERE singleton = 1`).
		Scan(&version, &id, &createdAt, &cwd, &name, &tip); err != nil {
		return fmt.Errorf("session: invalid sqlite session metadata: %w", err)
	}
	if version < 1 || version > SessionVersion {
		return fmt.Errorf("session: unsupported version %d", version)
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(tip) == "" {
		return errors.New("session: invalid sqlite session identity")
	}
	var seq int64
	var parentID, entryType, summary, metaKey, metaValue string
	var message []byte
	if err := db.QueryRow(`SELECT seq, parent_id, entry_type, message, summary, meta_key, meta_value FROM entries WHERE id = 'root'`).
		Scan(&seq, &parentID, &entryType, &message, &summary, &metaKey, &metaValue); err != nil {
		return fmt.Errorf("session: invalid sqlite session root: %w", err)
	}
	if seq < 1 || parentID != "" || entryType != string(EntryMeta) || metaKey != "root" || metaValue != id {
		return errors.New("session: invalid sqlite session root")
	}
	var tipCount int
	if err := db.QueryRow(`SELECT count(*) FROM entries WHERE id = ?`, tip).Scan(&tipCount); err != nil {
		return fmt.Errorf("session: invalid sqlite session tip: %w", err)
	}
	if tipCount != 1 {
		return errors.New("session: invalid sqlite session tip")
	}
	return nil
}

// NewSQLiteStore opens or creates a SQLite-backed session database at path.
// Existing JSONL files are intentionally not supported; session storage now
// uses the SQLite schema exclusively.
func NewSQLiteStore(path, cwd string, opts Options) (*SQLiteStore, error) {
	return newSQLiteStore(path, cwd, opts, false)
}

// OpenSQLiteStore opens an existing Snow session without creating a missing
// path or mutating a non-Snow SQLite database.
func OpenSQLiteStore(path, cwd string, opts Options) (*SQLiteStore, error) {
	return newSQLiteStore(path, cwd, opts, true)
}

func newSQLiteStore(path, cwd string, opts Options, existingOnly bool) (*SQLiteStore, error) {
	if path == "" {
		return nil, errors.New("session: sqlite path is empty")
	}
	if !existingOnly {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("session: mkdir: %w", err)
		}
	}
	existing := false
	if info, err := os.Lstat(path); err == nil {
		existing = true
		if !info.Mode().IsRegular() || !singleLink(info) {
			return nil, errors.New("session: sqlite path must be a regular, non-aliased file")
		}
		if info.Size() == 0 {
			return nil, errors.New("session: existing sqlite database is empty")
		}
	} else if existingOnly || !os.IsNotExist(err) {
		return nil, fmt.Errorf("session: stat: %w", err)
	}
	lease, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session: open lifetime lease: %w", err)
	}
	closeLease := func() {
		unlockSessionFile(lease)
		_ = lease.Close()
	}
	if err := lockSessionShared(lease); err != nil {
		closeLease()
		return nil, fmt.Errorf("session: lock lifetime lease: %w", err)
	}
	// Recheck after acquiring the path-stable lease. A deleter may have moved
	// the database while this opener waited for its shared lock.
	if info, statErr := os.Lstat(path); statErr == nil {
		existing = true
		if !info.Mode().IsRegular() || !singleLink(info) {
			closeLease()
			return nil, errors.New("session: sqlite path must be a regular, non-aliased file")
		}
		if info.Size() == 0 {
			closeLease()
			return nil, errors.New("session: existing sqlite database is empty")
		}
	} else if existingOnly || !os.IsNotExist(statErr) {
		closeLease()
		return nil, fmt.Errorf("session: stat after lifetime lease: %w", statErr)
	} else {
		existing = false
	}
	id := opts.ID
	if id == "" {
		id = newID()
	}
	dsn := sqliteDSN(path)
	if existingOnly {
		dsn = sqliteExistingDSN(path)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		closeLease()
		return nil, fmt.Errorf("session: sqlite open: %w", err)
	}
	// One connection avoids SQLite connection-local state surprises while WAL
	// still permits readers to proceed while a writer commits.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	closeDB := func(e error) (*SQLiteStore, error) {
		_ = db.Close()
		closeLease()
		return nil, e
	}
	if err := db.Ping(); err != nil {
		return closeDB(fmt.Errorf("session: sqlite ping: %w", err))
	}
	if existingOnly {
		// Validate through the exact read/write connection before chmod, persistent
		// journal changes, or schema migration. This closes both missing-path
		// recreation and replacement races without touching unrelated databases.
		if err := validateSQLiteSessionDB(db); err != nil {
			return closeDB(err)
		}
		if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000`); err != nil {
			return closeDB(fmt.Errorf("session: sqlite configure existing: %w", err))
		}
	}
	// New/create-capable paths are owned by Snow and contain prompts and tool
	// results. Existing-only opens intentionally avoid name-based chmod: the
	// validated SQLite connection is pinned to an inode, while path could be
	// replaced with an unrelated file or symlink before chmod executes.
	if !existingOnly {
		if err := os.Chmod(path, 0o600); err != nil {
			return closeDB(fmt.Errorf("session: sqlite chmod: %w", err))
		}
	}
	if err := createSQLiteSchema(db); err != nil {
		return closeDB(err)
	}

	s := &SQLiteStore{path: path, db: db, lease: lease, branchID: "main", deleteIfEmpty: !existingOnly}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM session_meta`).Scan(&count); err != nil {
		return closeDB(fmt.Errorf("session: sqlite metadata count: %w", err))
	}
	if count == 0 {
		if existing {
			return closeDB(errors.New("session: existing sqlite database has no session metadata"))
		}
		s.header = Header{
			Version:         SessionVersion,
			ID:              id,
			CreatedAt:       time.Now().UnixMilli(),
			CWD:             cwd,
			Name:            opts.Name,
			ParentSessionID: opts.ParentSessionID,
			ParentBranchID:  opts.ParentBranchID,
			ForkEntryID:     opts.ForkEntryID,
		}
		s.tip = "root"
		tx, err := db.Begin()
		if err != nil {
			return closeDB(fmt.Errorf("session: sqlite begin: %w", err))
		}
		if _, err := tx.Exec(`
			INSERT INTO session_meta(singleton, version, session_id, created_at, cwd, name, branch_tip, parent_session_id, parent_branch_id, fork_entry_id)
			VALUES(1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.header.Version, s.header.ID, s.header.CreatedAt,
			s.header.CWD, s.header.Name, s.tip, s.header.ParentSessionID, s.header.ParentBranchID, s.header.ForkEntryID); err != nil {
			_ = tx.Rollback()
			return closeDB(fmt.Errorf("session: sqlite metadata: %w", err))
		}
		if _, err := tx.Exec(`
			INSERT INTO entries(id, parent_id, entry_type, message, summary, meta_key, meta_value)
			VALUES(?, ?, ?, NULL, '', ?, ?)`, "root", "", EntryMeta, "root", s.header.ID); err != nil {
			_ = tx.Rollback()
			return closeDB(fmt.Errorf("session: sqlite root: %w", err))
		}
		if _, err := tx.Exec(`
			INSERT INTO session_branches(branch_id, branch_name, parent_branch_id, forked_from_id, tip_id, created_at, updated_at, active)
			VALUES('main', 'main', '', '', ?, ?, ?, 1)`, s.tip, s.header.CreatedAt, s.header.CreatedAt); err != nil {
			_ = tx.Rollback()
			return closeDB(fmt.Errorf("session: sqlite main branch: %w", err))
		}
		if err := tx.Commit(); err != nil {
			return closeDB(fmt.Errorf("session: sqlite commit: %w", err))
		}
		return s, nil
	}

	if count != 1 {
		return closeDB(fmt.Errorf("session: sqlite invalid metadata rows: %d", count))
	}
	if err := db.QueryRow(`
		SELECT version, session_id, created_at, cwd, name, branch_tip
		FROM session_meta WHERE singleton = 1`).Scan(
		&s.header.Version, &s.header.ID, &s.header.CreatedAt, &s.header.CWD,
		&s.header.Name, &s.tip); err != nil {
		return closeDB(fmt.Errorf("session: sqlite metadata: %w", err))
	}
	if s.header.Version > SessionVersion || s.header.Version < 1 {
		return closeDB(fmt.Errorf("session: unsupported version %d", s.header.Version))
	}
	if err := ensureBranches(db, s.tip, s.header.CreatedAt, s.header.Version); err != nil {
		return closeDB(err)
	}
	if err := db.QueryRow(`SELECT parent_session_id, parent_branch_id, fork_entry_id FROM session_meta WHERE singleton = 1`).Scan(
		&s.header.ParentSessionID, &s.header.ParentBranchID, &s.header.ForkEntryID); err != nil {
		return closeDB(fmt.Errorf("session: sqlite fork provenance: %w", err))
	}
	if s.header.Version < SessionVersion {
		s.header.Version = SessionVersion
	}
	var activeTip string
	if err := db.QueryRow(`SELECT branch_id, tip_id FROM session_branches WHERE active = 1 ORDER BY created_at LIMIT 1`).Scan(&s.branchID, &activeTip); err != nil {
		return closeDB(fmt.Errorf("session: sqlite active branch: %w", err))
	}
	s.tip = activeTip
	return s, nil
}

func sqliteReadOnlyDSN(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := u.Query()
	q.Set("mode", "ro")
	q.Add("_pragma", "query_only(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	return u.String()
}

func sqliteExistingDSN(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := u.Query()
	q.Set("mode", "rw")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	return u.String()
}

func sqliteDSN(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	// A file URI keeps SQLite from interpreting query characters in a path as
	// DSN options. modernc.org/sqlite accepts standard URI query pragmas.
	u := url.URL{Scheme: "file", Path: abs}
	q := u.Query()
	q.Set("_pragma", "journal_mode(WAL)")
	// NORMAL avoids an fsync for every individual append while retaining WAL
	// recovery guarantees. SQLite transactions remain atomic.
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	return u.String()
}

func createSQLiteSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS session_meta (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			version INTEGER NOT NULL,
			session_id TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			cwd TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			branch_tip TEXT NOT NULL,
			parent_session_id TEXT NOT NULL DEFAULT '',
			parent_branch_id TEXT NOT NULL DEFAULT '',
			fork_entry_id TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS entries (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			id TEXT NOT NULL UNIQUE,
			parent_id TEXT NOT NULL DEFAULT '',
			entry_type TEXT NOT NULL,
			message BLOB,
			summary TEXT NOT NULL DEFAULT '',
			compacted_through TEXT NOT NULL DEFAULT '',
			meta_key TEXT NOT NULL DEFAULT '',
			meta_value TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS entries_parent_idx ON entries(parent_id);
		CREATE INDEX IF NOT EXISTS entries_type_idx ON entries(entry_type);
		CREATE TABLE IF NOT EXISTS session_branches (
			branch_id TEXT PRIMARY KEY,
			branch_name TEXT NOT NULL DEFAULT '',
			parent_branch_id TEXT NOT NULL DEFAULT '',
			forked_from_id TEXT NOT NULL DEFAULT '',
			tip_id TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			active INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS session_branches_active_idx ON session_branches(active);
		CREATE TABLE IF NOT EXISTS thread_state (
			branch_id TEXT PRIMARY KEY,
			collaboration_mode TEXT NOT NULL DEFAULT 'default'
		);
		CREATE TABLE IF NOT EXISTS thread_goals (
			branch_id TEXT PRIMARY KEY,
			goal_id TEXT NOT NULL,
			objective TEXT NOT NULL,
			status TEXT NOT NULL,
			token_budget INTEGER,
			tokens_used INTEGER NOT NULL DEFAULT 0,
			seconds_used INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS thread_goal_costs (
			branch_id TEXT NOT NULL,
			currency TEXT NOT NULL,
			input_cost REAL NOT NULL DEFAULT 0,
			output_cost REAL NOT NULL DEFAULT 0,
			cache_read_cost REAL NOT NULL DEFAULT 0,
			cache_write_cost REAL NOT NULL DEFAULT 0,
			total_cost REAL NOT NULL DEFAULT 0,
			PRIMARY KEY(branch_id, currency),
			FOREIGN KEY(branch_id) REFERENCES thread_goals(branch_id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS thread_goal_deferrals (
			branch_id TEXT PRIMARY KEY,
			deferred INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS subagent_threads (
			thread_id TEXT PRIMARY KEY,
			parent_thread_id TEXT NOT NULL,
			parent_branch_id TEXT NOT NULL,
			agent_path TEXT NOT NULL UNIQUE,
			parent_path TEXT NOT NULL,
			role TEXT NOT NULL,
			role_fingerprint TEXT NOT NULL DEFAULT '',
			nickname TEXT NOT NULL DEFAULT '',
			depth INTEGER NOT NULL,
			status TEXT NOT NULL,
			child_session_path TEXT NOT NULL,
			model_provider TEXT NOT NULL,
			model_id TEXT NOT NULL,
			thinking TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			started_at INTEGER NOT NULL DEFAULT 0,
			finished_at INTEGER NOT NULL DEFAULT 0,
			result TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			usage_json BLOB,
			generation INTEGER NOT NULL DEFAULT 1
		);
		CREATE INDEX IF NOT EXISTS subagent_threads_parent_idx ON subagent_threads(parent_thread_id, created_at);
		CREATE INDEX IF NOT EXISTS subagent_threads_branch_idx ON subagent_threads(parent_branch_id, created_at);
	`)
	if err != nil {
		return fmt.Errorf("session: sqlite schema: %w", err)
	}
	return nil
}

func ensureBranches(db *sql.DB, tip string, createdAt int64, version int) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite branch migration begin: %w", err)
	}
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM session_branches`).Scan(&count); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite branch migration count: %w", err)
	}
	if count == 0 {
		now := time.Now().UnixMilli()
		if createdAt == 0 {
			createdAt = now
		}
		if _, err := tx.Exec(`
			INSERT INTO session_branches(branch_id, branch_name, parent_branch_id, forked_from_id, tip_id, created_at, updated_at, active)
			VALUES('main', 'main', '', '', ?, ?, ?, 1)`, tip, createdAt, now); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("session: sqlite branch migration insert: %w", err)
		}
	}
	var active int
	if err := tx.QueryRow(`SELECT count(*) FROM session_branches WHERE active = 1`).Scan(&active); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite active branch count: %w", err)
	}
	if active == 0 {
		if _, err := tx.Exec(`UPDATE session_branches SET active = CASE WHEN branch_id = 'main' THEN 1 ELSE 0 END`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("session: sqlite active branch repair: %w", err)
		}
	}
	// Version 1 entries did not have a compaction boundary column.
	if version < 2 {
		if _, err := tx.Exec(`ALTER TABLE entries ADD COLUMN compacted_through TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			_ = tx.Rollback()
			return fmt.Errorf("session: sqlite compaction migration: %w", err)
		}
	}
	if version < 6 {
		if _, err := tx.Exec(`ALTER TABLE subagent_threads ADD COLUMN role_fingerprint TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			_ = tx.Rollback()
			return fmt.Errorf("session: subagent role fingerprint migration: %w", err)
		}
	}
	if version < 7 {
		for _, statement := range []string{
			`ALTER TABLE session_branches ADD COLUMN branch_name TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE session_branches ADD COLUMN parent_branch_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE session_branches ADD COLUMN forked_from_id TEXT NOT NULL DEFAULT ''`,
		} {
			if _, err := tx.Exec(statement); err != nil && !strings.Contains(err.Error(), "duplicate column") {
				_ = tx.Rollback()
				return fmt.Errorf("session: branch topology migration: %w", err)
			}
		}
		if _, err := tx.Exec(`UPDATE session_branches SET branch_name = CASE WHEN branch_id='main' THEN 'main' ELSE branch_id END WHERE branch_name=''`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("session: branch name migration: %w", err)
		}
		if _, err := tx.Exec(`UPDATE session_branches SET parent_branch_id='main' WHERE branch_id!='main' AND parent_branch_id=''`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("session: branch parent migration: %w", err)
		}
	}
	if version < 8 {
		if err := backfillGoalCosts(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("session: goal cost migration: %w", err)
		}
	}
	if version < 9 {
		for _, statement := range []string{
			`ALTER TABLE session_meta ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE session_meta ADD COLUMN parent_branch_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE session_meta ADD COLUMN fork_entry_id TEXT NOT NULL DEFAULT ''`,
		} {
			if _, err := tx.Exec(statement); err != nil && !strings.Contains(err.Error(), "duplicate column") {
				_ = tx.Rollback()
				return fmt.Errorf("session: fork provenance migration: %w", err)
			}
		}
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS session_branches_name_idx ON session_branches(branch_name COLLATE NOCASE)`); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: branch name index: %w", err)
	}
	if version < SessionVersion {
		if _, err := tx.Exec(`UPDATE session_meta SET version = ? WHERE singleton = 1`, SessionVersion); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("session: sqlite version migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: sqlite branch migration commit: %w", err)
	}
	return nil
}

func backfillGoalCosts(tx *sql.Tx) error {
	type legacyGoal struct {
		branchID           string
		tokens, start, end int64
	}
	rows, err := tx.Query(`SELECT branch_id, tokens_used, created_at, updated_at FROM thread_goals
		WHERE tokens_used > 0 AND NOT EXISTS (SELECT 1 FROM thread_goal_costs WHERE thread_goal_costs.branch_id = thread_goals.branch_id)`)
	if err != nil {
		return err
	}
	var goals []legacyGoal
	for rows.Next() {
		var goal legacyGoal
		if err := rows.Scan(&goal.branchID, &goal.tokens, &goal.start, &goal.end); err != nil {
			_ = rows.Close()
			return err
		}
		goals = append(goals, goal)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, goal := range goals {
		messageRows, err := tx.Query(`WITH RECURSIVE chain(id, parent_id, entry_type, message) AS (
			SELECT e.id, e.parent_id, e.entry_type, e.message FROM entries e
			JOIN session_branches b ON b.tip_id = e.id WHERE b.branch_id = ?
			UNION ALL
			SELECT e.id, e.parent_id, e.entry_type, e.message FROM entries e
			JOIN chain c ON c.parent_id = e.id WHERE c.parent_id != ''
		) SELECT message FROM chain WHERE entry_type = ? AND message IS NOT NULL`, goal.branchID, EntryMessage)
		if err != nil {
			return err
		}
		var tokenTotal int64
		costs := []protocol.Cost(nil)
		priced := true
		for messageRows.Next() {
			var raw []byte
			if err := messageRows.Scan(&raw); err != nil {
				_ = messageRows.Close()
				return err
			}
			var message protocol.Message
			if err := json.Unmarshal(raw, &message); err != nil || message.Role != protocol.RoleAssistant || message.Usage == nil || message.Timestamp < goal.start || message.Timestamp > goal.end {
				continue
			}
			tokens := message.Usage.Total
			if tokens == 0 {
				tokens = message.Usage.Input + message.Usage.Output
			}
			tokenTotal += int64(tokens)
			cost, costErr := normalizedGoalCostDelta(message.Usage.Cost)
			if costErr != nil || cost == nil {
				priced = false
				continue
			}
			costs, costErr = addGoalCost(costs, cost)
			if costErr != nil {
				_ = messageRows.Close()
				return costErr
			}
		}
		if err := messageRows.Close(); err != nil {
			return err
		}
		// Historical messages did not carry an explicit goal-origin marker.
		// Backfill only when their exact token sum proves an unambiguous match.
		if !priced || tokenTotal != goal.tokens || len(costs) == 0 {
			continue
		}
		if err := replaceGoalCosts(tx, goal.branchID, costs); err != nil {
			return err
		}
	}
	return nil
}

// ID implements Store.
func (s *SQLiteStore) ID() string { return s.header.ID }

// Path implements Store.
func (s *SQLiteStore) Path() string { return s.path }

// Header implements Store.
func (s *SQLiteStore) Header() Header {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.header
}

// SessionTitle returns the current session-wide display title.
func (s *SQLiteStore) SessionTitle() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", errors.New("session: store closed")
	}
	return s.header.Name, nil
}
