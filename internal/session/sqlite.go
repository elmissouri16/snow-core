package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
	_ "modernc.org/sqlite"
)

// SQLiteStore is the durable session store. It keeps the active branch cursor
// in memory while branch references and entries remain in SQLite. Entries are
// queried when Messages or ContextMessages is requested. modernc.org/sqlite is
// a pure-Go, CGo-free database/sql driver.
type SQLiteStore struct {
	mu            sync.RWMutex
	path          string
	header        Header
	tip           string
	branchID      string
	db            *sql.DB
	closed        bool
	deleteIfEmpty bool
}

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
	if info, err := os.Stat(path); err == nil {
		existing = true
		if !info.Mode().IsRegular() {
			return nil, errors.New("session: sqlite path is not a regular file")
		}
		if info.Size() == 0 {
			return nil, errors.New("session: existing sqlite database is empty")
		}
	} else if existingOnly || !os.IsNotExist(err) {
		return nil, fmt.Errorf("session: stat: %w", err)
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
		return nil, fmt.Errorf("session: sqlite open: %w", err)
	}
	// One connection avoids SQLite connection-local state surprises while WAL
	// still permits readers to proceed while a writer commits.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	closeDB := func(e error) (*SQLiteStore, error) {
		_ = db.Close()
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

	s := &SQLiteStore{path: path, db: db, branchID: "main", deleteIfEmpty: !existingOnly}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM session_meta`).Scan(&count); err != nil {
		return closeDB(fmt.Errorf("session: sqlite metadata count: %w", err))
	}
	if count == 0 {
		if existing {
			return closeDB(errors.New("session: existing sqlite database has no session metadata"))
		}
		s.header = Header{
			Version:   SessionVersion,
			ID:        id,
			CreatedAt: time.Now().UnixMilli(),
			CWD:       cwd,
			Name:      opts.Name,
		}
		s.tip = "root"
		tx, err := db.Begin()
		if err != nil {
			return closeDB(fmt.Errorf("session: sqlite begin: %w", err))
		}
		if _, err := tx.Exec(`
			INSERT INTO session_meta(singleton, version, session_id, created_at, cwd, name, branch_tip)
			VALUES(1, ?, ?, ?, ?, ?, ?)`, s.header.Version, s.header.ID, s.header.CreatedAt,
			s.header.CWD, s.header.Name, s.tip); err != nil {
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
			branch_tip TEXT NOT NULL
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

// RenameSession changes the display title without moving the branch tip.
func (s *SQLiteStore) RenameSession(title string) error {
	title, err := normalizeSessionTitle(title)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if _, err := s.db.Exec(`UPDATE session_meta SET name = ? WHERE singleton = 1`, title); err != nil {
		return fmt.Errorf("session: rename: %w", err)
	}
	s.header.Name = title
	return nil
}

// AppendWithInitialTitle atomically appends the first user message and assigns
// its generated title. Existing/manual titles and any prior message win.
func (s *SQLiteStore) AppendWithInitialTitle(entry Entry, title string) error {
	entry = cloneEntry(entry)
	if title != "" {
		var err error
		title, err = normalizeSessionTitle(title)
		if err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	expectedTip := s.tip
	if entry.ID == "" {
		entry.ID = newID()
	}
	if entry.ParentID == "" {
		entry.ParentID = expectedTip
	}
	var exists int
	if err := s.db.QueryRow(`SELECT count(*) FROM entries WHERE id = ?`, entry.ID).Scan(&exists); err != nil {
		return fmt.Errorf("session: sqlite lookup entry: %w", err)
	}
	if exists != 0 {
		return fmt.Errorf("session: duplicate entry id %q", entry.ID)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM entries WHERE id = ?`, entry.ParentID).Scan(&exists); err != nil {
		return fmt.Errorf("session: sqlite lookup parent: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("session: unknown parent %q", entry.ParentID)
	}
	normalizeEntryMessage(&entry)
	var raw []byte
	var err error
	if entry.Message != nil {
		raw, err = json.Marshal(entry.Message)
		if err != nil {
			return fmt.Errorf("session: marshal message: %w", err)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	titleCreated := false
	if title != "" {
		result, err := tx.Exec(`UPDATE session_meta SET name = ? WHERE singleton = 1 AND name = '' AND NOT EXISTS (SELECT 1 FROM entries WHERE entry_type = ?)`, title, EntryMessage)
		if err != nil {
			return fmt.Errorf("session: title: %w", err)
		}
		changed, _ := result.RowsAffected()
		titleCreated = changed == 1
	}
	if _, err := tx.Exec(`
		INSERT INTO entries(id, parent_id, entry_type, message, summary, compacted_through, meta_key, meta_value)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, entry.ID, entry.ParentID, entry.Type, raw,
		entry.Summary, entry.CompactedThrough, entry.Key, entry.Value); err != nil {
		return fmt.Errorf("session: sqlite append: %w", err)
	}
	now := time.Now().UnixMilli()
	if result, err := tx.Exec(`UPDATE session_branches SET tip_id = ?, updated_at = ? WHERE branch_id = ? AND tip_id = ?`, entry.ID, now, s.branchID, expectedTip); err != nil {
		return fmt.Errorf("session: sqlite branch tip: %w", err)
	} else if n, _ := result.RowsAffected(); n != 1 {
		_ = tx.Rollback()
		s.refreshBranchTipLocked()
		return fmt.Errorf("%w on branch %q (expected %q)", ErrConflict, s.branchID, expectedTip)
	}
	if _, err := tx.Exec(`UPDATE session_meta SET branch_tip = ? WHERE singleton = 1
		AND EXISTS (SELECT 1 FROM session_branches WHERE branch_id = ? AND active = 1)`, entry.ID, s.branchID); err != nil {
		return fmt.Errorf("session: sqlite tip: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: sqlite commit: %w", err)
	}
	s.tip = entry.ID
	if titleCreated {
		s.header.Name = title
	} else if title != "" && s.header.Name == "" {
		// Another handle may have won a concurrent manual/automatic rename.
		if err := s.db.QueryRow(`SELECT name FROM session_meta WHERE singleton = 1`).Scan(&s.header.Name); err != nil {
			return fmt.Errorf("session: refresh title: %w", err)
		}
	}
	return nil
}

// CollaborationMode returns the active branch mode.
func (s *SQLiteStore) CollaborationMode() (protocol.CollaborationMode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return protocol.ModeDefault, errors.New("session: store closed")
	}
	var raw string
	err := s.db.QueryRow(`SELECT collaboration_mode FROM thread_state WHERE branch_id = ?`, s.branchID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.ModeDefault, nil
	}
	if err != nil {
		return protocol.ModeDefault, fmt.Errorf("session: sqlite collaboration mode: %w", err)
	}
	return protocol.ParseCollaborationMode(raw)
}

// SetCollaborationMode persists the active branch mode without moving its tip.
func (s *SQLiteStore) SetCollaborationMode(mode protocol.CollaborationMode) error {
	parsed, err := protocol.ParseCollaborationMode(string(mode))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	_, err = s.db.Exec(`INSERT INTO thread_state(branch_id, collaboration_mode) VALUES(?, ?)
		ON CONFLICT(branch_id) DO UPDATE SET collaboration_mode = excluded.collaboration_mode`, s.branchID, parsed)
	if err != nil {
		return fmt.Errorf("session: sqlite set collaboration mode: %w", err)
	}
	return nil
}

func scanGoal(row interface{ Scan(...any) error }, sessionID, branchID string) (*protocol.ThreadGoal, error) {
	var g protocol.ThreadGoal
	var budget sql.NullInt64
	err := row.Scan(&g.GoalID, &g.Objective, &g.Status, &budget, &g.TokensUsed, &g.SecondsUsed, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.SessionID, g.BranchID = sessionID, branchID
	if budget.Valid {
		v := budget.Int64
		g.TokenBudget = &v
	}
	return &g, g.Validate()
}

type goalCostQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func loadGoalCosts(queryer goalCostQuerier, branchID string, goal *protocol.ThreadGoal) error {
	if goal == nil {
		return nil
	}
	rows, err := queryer.Query(`SELECT currency, input_cost, output_cost, cache_read_cost, cache_write_cost, total_cost
		FROM thread_goal_costs WHERE branch_id = ? ORDER BY currency`, branchID)
	if err != nil {
		return err
	}
	defer rows.Close()
	goal.EstimatedCosts = nil
	for rows.Next() {
		var cost protocol.Cost
		if err := rows.Scan(&cost.Currency, &cost.Input, &cost.Output, &cost.CacheRead, &cost.CacheWrite, &cost.Total); err != nil {
			return err
		}
		goal.EstimatedCosts = append(goal.EstimatedCosts, cost)
	}
	return rows.Err()
}

func scanGoalWithCosts(row interface{ Scan(...any) error }, queryer goalCostQuerier, sessionID, branchID string) (*protocol.ThreadGoal, error) {
	goal, err := scanGoal(row, sessionID, branchID)
	if err != nil || goal == nil {
		return goal, err
	}
	if err := loadGoalCosts(queryer, branchID, goal); err != nil {
		return nil, err
	}
	return goal, goal.Validate()
}

func replaceGoalCosts(tx *sql.Tx, branchID string, costs []protocol.Cost) error {
	if _, err := tx.Exec(`DELETE FROM thread_goal_costs WHERE branch_id = ?`, branchID); err != nil {
		return err
	}
	for i := range costs {
		cost, err := normalizedGoalCostDelta(&costs[i])
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO thread_goal_costs(branch_id, currency, input_cost, output_cost, cache_read_cost, cache_write_cost, total_cost)
			VALUES(?,?,?,?,?,?,?)`, branchID, cost.Currency, cost.Input, cost.Output, cost.CacheRead, cost.CacheWrite, cost.Total); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) Goal() (*protocol.ThreadGoal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("session: sqlite goal begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	g, err := scanGoalWithCosts(tx.QueryRow(`SELECT goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), tx, s.header.ID, s.branchID)
	if err != nil {
		return nil, fmt.Errorf("session: sqlite goal: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("session: sqlite goal commit: %w", err)
	}
	return g, nil
}

func (s *SQLiteStore) CreateGoal(goal protocol.ThreadGoal, replace bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	goal.SessionID, goal.BranchID = s.header.ID, s.branchID
	if err := goal.Validate(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite create goal begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	query := `INSERT INTO thread_goals(branch_id, goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(branch_id) DO UPDATE SET goal_id=excluded.goal_id, objective=excluded.objective, status=excluded.status,
			token_budget=excluded.token_budget, tokens_used=excluded.tokens_used, seconds_used=excluded.seconds_used,
			created_at=excluded.created_at, updated_at=excluded.updated_at`
	args := []any{s.branchID, goal.GoalID, strings.TrimSpace(goal.Objective), goal.Status, goal.TokenBudget, goal.TokensUsed, goal.SecondsUsed, goal.CreatedAt, goal.UpdatedAt}
	if !replace {
		query += ` WHERE thread_goals.status IN (?, ?)`
		args = append(args, protocol.GoalComplete, protocol.GoalBudgetLimited)
	}
	res, err := tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("session: sqlite create goal: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("session: unfinished goal already exists")
	}
	if err := replaceGoalCosts(tx, s.branchID, goal.EstimatedCosts); err != nil {
		return fmt.Errorf("session: sqlite replace goal costs: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM thread_goal_deferrals WHERE branch_id = ?`, s.branchID); err != nil {
		return fmt.Errorf("session: sqlite clear goal deferral: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: sqlite create goal commit: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ReplaceGoal(expected string, goal protocol.ThreadGoal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	goal.SessionID, goal.BranchID = s.header.ID, s.branchID
	if err := goal.Validate(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite replace goal begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var res sql.Result
	if expected == "" {
		res, err = tx.Exec(`INSERT INTO thread_goals(branch_id, goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(branch_id) DO NOTHING`, s.branchID, goal.GoalID, strings.TrimSpace(goal.Objective), goal.Status, goal.TokenBudget, goal.TokensUsed, goal.SecondsUsed, goal.CreatedAt, goal.UpdatedAt)
	} else {
		res, err = tx.Exec(`UPDATE thread_goals SET goal_id=?, objective=?, status=?, token_budget=?, tokens_used=?, seconds_used=?, created_at=?, updated_at=?
			WHERE branch_id=? AND goal_id=?`, goal.GoalID, strings.TrimSpace(goal.Objective), goal.Status, goal.TokenBudget, goal.TokensUsed, goal.SecondsUsed, goal.CreatedAt, goal.UpdatedAt, s.branchID, expected)
	}
	if err != nil {
		return fmt.Errorf("session: sqlite replace goal: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("session: stale goal id")
	}
	if err := replaceGoalCosts(tx, s.branchID, goal.EstimatedCosts); err != nil {
		return fmt.Errorf("session: sqlite replace goal costs: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM thread_goal_deferrals WHERE branch_id = ?`, s.branchID); err != nil {
		return fmt.Errorf("session: sqlite clear goal deferral: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: sqlite replace goal commit: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ReviseGoal(expected, nextGoalID, objective string) (*protocol.ThreadGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	objective = strings.TrimSpace(objective)
	if nextGoalID == "" || objective == "" || len([]rune(objective)) > protocol.MaxThreadGoalObjectiveChars {
		return nil, errors.New("session: invalid goal revision")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("session: sqlite revise goal begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()
	g, err := scanGoalWithCosts(tx.QueryRow(`UPDATE thread_goals SET goal_id=?, objective=?, status=?, updated_at=?
		WHERE branch_id=? AND goal_id=?
		RETURNING goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at`, nextGoalID, objective, protocol.GoalActive, now, s.branchID, expected), tx, s.header.ID, s.branchID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		current, readErr := scanGoalWithCosts(tx.QueryRow(`SELECT goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), tx, s.header.ID, s.branchID)
		if readErr != nil {
			return nil, readErr
		}
		if current == nil {
			return nil, ErrNotFound
		}
		return nil, errors.New("session: stale goal id")
	}
	if _, err := tx.Exec(`DELETE FROM thread_goal_deferrals WHERE branch_id = ?`, s.branchID); err != nil {
		return nil, fmt.Errorf("session: sqlite clear goal deferral: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("session: sqlite revise goal commit: %w", err)
	}
	return g.Clone(), nil
}

func (s *SQLiteStore) TransitionGoal(expected string, expectedStatus, nextStatus protocol.ThreadGoalStatus, clearDeferral bool) (*protocol.ThreadGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := protocol.ParseThreadGoalStatus(string(nextStatus)); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("session: sqlite transition goal begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()
	g, err := scanGoalWithCosts(tx.QueryRow(`UPDATE thread_goals SET status=?, updated_at=?
		WHERE branch_id=? AND goal_id=? AND status=?
		RETURNING goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at`, nextStatus, now, s.branchID, expected, expectedStatus), tx, s.header.ID, s.branchID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		current, readErr := scanGoalWithCosts(tx.QueryRow(`SELECT goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), tx, s.header.ID, s.branchID)
		if readErr != nil {
			return nil, readErr
		}
		if current == nil {
			return nil, ErrNotFound
		}
		return nil, errors.New("session: stale goal state")
	}
	if clearDeferral {
		if _, err := tx.Exec(`DELETE FROM thread_goal_deferrals WHERE branch_id = ?`, s.branchID); err != nil {
			return nil, fmt.Errorf("session: sqlite clear goal deferral: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("session: sqlite transition goal commit: %w", err)
	}
	return g.Clone(), nil
}

func (s *SQLiteStore) UpdateGoal(expected string, objective *string, status *protocol.ThreadGoalStatus, budget *int64) (*protocol.ThreadGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	objectiveSet, objectiveValue := 0, ""
	if objective != nil {
		objectiveSet = 1
		objectiveValue = strings.TrimSpace(*objective)
		if objectiveValue == "" || len([]rune(objectiveValue)) > protocol.MaxThreadGoalObjectiveChars {
			return nil, errors.New("session: invalid goal objective")
		}
	}
	statusSet, statusValue := 0, protocol.ThreadGoalStatus("")
	if status != nil {
		if _, err := protocol.ParseThreadGoalStatus(string(*status)); err != nil {
			return nil, err
		}
		statusSet, statusValue = 1, *status
	}
	budgetSet, budgetValue := 0, int64(0)
	if budget != nil {
		if *budget <= 0 {
			return nil, errors.New("session: goal token budget must be positive")
		}
		budgetSet, budgetValue = 1, *budget
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("session: sqlite update goal begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()
	g, err := scanGoalWithCosts(tx.QueryRow(`UPDATE thread_goals SET
		objective = CASE WHEN ? = 1 THEN ? ELSE objective END,
		status = CASE WHEN ? = 1 THEN ? ELSE status END,
		token_budget = CASE WHEN ? = 1 THEN ? ELSE token_budget END,
		updated_at = ?
		WHERE branch_id = ? AND goal_id = ?
		RETURNING goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at`,
		objectiveSet, objectiveValue, statusSet, statusValue, budgetSet, budgetValue, now, s.branchID, expected), tx, s.header.ID, s.branchID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		current, readErr := scanGoalWithCosts(tx.QueryRow(`SELECT goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), tx, s.header.ID, s.branchID)
		if readErr != nil {
			return nil, readErr
		}
		if current == nil {
			return nil, ErrNotFound
		}
		return nil, errors.New("session: stale goal id")
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("session: sqlite update goal commit: %w", err)
	}
	return g.Clone(), nil
}

func (s *SQLiteStore) ClearGoal(expected string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected == "" {
		var exists int
		if err := s.db.QueryRow(`SELECT count(*) FROM thread_goals WHERE branch_id = ?`, s.branchID).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			return errors.New("session: stale goal id")
		}
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite clear goal begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`DELETE FROM thread_goals WHERE branch_id = ? AND goal_id = ?`, s.branchID, expected)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("session: stale goal id")
	}
	if _, err := tx.Exec(`DELETE FROM thread_goal_deferrals WHERE branch_id = ?`, s.branchID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) AccountGoal(expected string, tokens, seconds int64, estimatedCostDelta *protocol.Cost) (*protocol.ThreadGoal, bool, error) {
	if tokens < 0 || seconds < 0 {
		return nil, false, errors.New("session: goal usage delta cannot be negative")
	}
	cost, err := normalizedGoalCostDelta(estimatedCostDelta)
	if err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, fmt.Errorf("session: sqlite account goal begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()
	g, err := scanGoal(tx.QueryRow(`UPDATE thread_goals
		SET tokens_used = tokens_used + ?, seconds_used = seconds_used + ?,
			status = CASE WHEN status = ? AND token_budget IS NOT NULL AND tokens_used + ? >= token_budget THEN ? ELSE status END,
			updated_at = ?
		WHERE branch_id = ? AND goal_id = ? AND tokens_used <= ? AND seconds_used <= ?
		RETURNING goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at`,
		tokens, seconds, protocol.GoalActive, tokens, protocol.GoalBudgetLimited, now,
		s.branchID, expected, math.MaxInt64-tokens, math.MaxInt64-seconds), s.header.ID, s.branchID)
	if err != nil {
		return nil, false, err
	}
	if g == nil {
		current, readErr := scanGoalWithCosts(tx.QueryRow(`SELECT goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), tx, s.header.ID, s.branchID)
		if readErr != nil {
			return nil, false, readErr
		}
		if current == nil || current.GoalID != expected {
			return current, false, nil
		}
		return nil, false, errors.New("session: goal usage overflow")
	}
	if cost != nil {
		var existing protocol.Cost
		err := tx.QueryRow(`SELECT currency, input_cost, output_cost, cache_read_cost, cache_write_cost, total_cost
			FROM thread_goal_costs WHERE branch_id=? AND currency=?`, s.branchID, cost.Currency).
			Scan(&existing.Currency, &existing.Input, &existing.Output, &existing.CacheRead, &existing.CacheWrite, &existing.Total)
		costs := []protocol.Cost(nil)
		if err == nil {
			costs = append(costs, existing)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, false, fmt.Errorf("session: sqlite read goal cost: %w", err)
		}
		costs, err = addGoalCost(costs, cost)
		if err != nil {
			return nil, false, err
		}
		updatedCost := costs[0]
		if _, err := tx.Exec(`INSERT INTO thread_goal_costs(branch_id, currency, input_cost, output_cost, cache_read_cost, cache_write_cost, total_cost)
			VALUES(?,?,?,?,?,?,?) ON CONFLICT(branch_id, currency) DO UPDATE SET
				input_cost=excluded.input_cost,
				output_cost=excluded.output_cost,
				cache_read_cost=excluded.cache_read_cost,
				cache_write_cost=excluded.cache_write_cost,
				total_cost=excluded.total_cost`, s.branchID, updatedCost.Currency, updatedCost.Input, updatedCost.Output, updatedCost.CacheRead, updatedCost.CacheWrite, updatedCost.Total); err != nil {
			return nil, false, fmt.Errorf("session: sqlite account goal cost: %w", err)
		}
	}
	if err := loadGoalCosts(tx, s.branchID, g); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("session: sqlite account goal commit: %w", err)
	}
	crossed := g.Status == protocol.GoalBudgetLimited && g.TokenBudget != nil && g.TokensUsed-tokens < *g.TokenBudget
	return g.Clone(), crossed, nil
}

func (s *SQLiteStore) GoalContinuationDeferred() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var v int
	err := s.db.QueryRow(`SELECT deferred FROM thread_goal_deferrals WHERE branch_id=?`, s.branchID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v != 0, err
}
func (s *SQLiteStore) SetGoalContinuationDeferred(v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO thread_goal_deferrals(branch_id,deferred) VALUES(?,?) ON CONFLICT(branch_id) DO UPDATE SET deferred=excluded.deferred`, s.branchID, v)
	return err
}

// Metadata returns the latest value for key in the session.
func (s *SQLiteStore) Metadata(key string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", false, errors.New("session: store closed")
	}
	var value string
	err := s.db.QueryRow(`
		SELECT meta_value FROM entries
		WHERE entry_type = ? AND meta_key = ?
		ORDER BY seq DESC LIMIT 1`, EntryMeta, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("session: sqlite metadata: %w", err)
	}
	return value, true, nil
}

// SetMetadata appends a metadata entry to the active branch.
func (s *SQLiteStore) SetMetadata(key, value string) error {
	return s.Append(Entry{Type: EntryMeta, Key: key, Value: value})
}

// Append implements Store. The entry and branch-tip update commit together.
func (s *SQLiteStore) Append(entry Entry) error {
	entry = cloneEntry(entry)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	expectedTip := s.tip
	if entry.ID == "" {
		entry.ID = newID()
	}
	if entry.ParentID == "" {
		entry.ParentID = expectedTip
	}
	var exists int
	if err := s.db.QueryRow(`SELECT count(*) FROM entries WHERE id = ?`, entry.ID).Scan(&exists); err != nil {
		return fmt.Errorf("session: sqlite lookup entry: %w", err)
	}
	if exists != 0 {
		return fmt.Errorf("session: duplicate entry id %q", entry.ID)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM entries WHERE id = ?`, entry.ParentID).Scan(&exists); err != nil {
		return fmt.Errorf("session: sqlite lookup parent: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("session: unknown parent %q", entry.ParentID)
	}
	normalizeEntryMessage(&entry)

	var raw []byte
	var err error
	if entry.Message != nil {
		raw, err = json.Marshal(entry.Message)
		if err != nil {
			return fmt.Errorf("session: marshal message: %w", err)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite begin: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO entries(id, parent_id, entry_type, message, summary, compacted_through, meta_key, meta_value)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, entry.ID, entry.ParentID, entry.Type, raw,
		entry.Summary, entry.CompactedThrough, entry.Key, entry.Value); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite append: %w", err)
	}
	now := time.Now().UnixMilli()
	if result, err := tx.Exec(`UPDATE session_branches SET tip_id = ?, updated_at = ? WHERE branch_id = ? AND tip_id = ?`, entry.ID, now, s.branchID, expectedTip); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite branch tip: %w", err)
	} else if n, _ := result.RowsAffected(); n != 1 {
		_ = tx.Rollback()
		s.refreshBranchTipLocked()
		return fmt.Errorf("%w on branch %q (expected %q)", ErrConflict, s.branchID, expectedTip)
	}
	if _, err := tx.Exec(`UPDATE session_meta SET branch_tip = ? WHERE singleton = 1
		AND EXISTS (SELECT 1 FROM session_branches WHERE branch_id = ? AND active = 1)`, entry.ID, s.branchID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite tip: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: sqlite commit: %w", err)
	}
	s.tip = entry.ID
	return nil
}

// AppendBatch writes one ordered chain and advances branch/session tips in a
// single SQLite transaction.
func (s *SQLiteStore) AppendBatch(batch []Entry) error {
	if len(batch) == 0 {
		return nil
	}
	batch = cloneEntries(batch)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	expectedTip := s.tip
	parent := expectedTip
	seen := map[string]bool{}
	for i := range batch {
		if batch[i].ID == "" {
			batch[i].ID = newID()
		}
		if batch[i].ParentID == "" {
			batch[i].ParentID = parent
		}
		if batch[i].ParentID != parent {
			return errors.New("session: batch is not a linear chain")
		}
		if seen[batch[i].ID] {
			return fmt.Errorf("session: duplicate entry id %q", batch[i].ID)
		}
		normalizeEntryMessage(&batch[i])
		seen[batch[i].ID] = true
		parent = batch[i].ID
	}
	var exists int
	if err := s.db.QueryRow(`SELECT count(*) FROM entries WHERE id=?`, batch[0].ParentID).Scan(&exists); err != nil || exists == 0 {
		if err == nil {
			err = fmt.Errorf("unknown parent %q", batch[0].ParentID)
		}
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, entry := range batch {
		var raw []byte
		if entry.Message != nil {
			raw, err = json.Marshal(entry.Message)
			if err != nil {
				return err
			}
		}
		if _, err = tx.Exec(`INSERT INTO entries(id,parent_id,entry_type,message,summary,compacted_through,meta_key,meta_value) VALUES(?,?,?,?,?,?,?,?)`, entry.ID, entry.ParentID, entry.Type, raw, entry.Summary, entry.CompactedThrough, entry.Key, entry.Value); err != nil {
			return fmt.Errorf("session: sqlite append batch: %w", err)
		}
	}
	now := time.Now().UnixMilli()
	if res, err := tx.Exec(`UPDATE session_branches SET tip_id=?,updated_at=? WHERE branch_id=? AND tip_id=?`, parent, now, s.branchID, expectedTip); err != nil {
		return err
	} else if n, _ := res.RowsAffected(); n != 1 {
		_ = tx.Rollback()
		s.refreshBranchTipLocked()
		return fmt.Errorf("%w on branch %q (expected %q)", ErrConflict, s.branchID, expectedTip)
	}
	if _, err := tx.Exec(`UPDATE session_meta SET branch_tip=? WHERE singleton=1
		AND EXISTS (SELECT 1 FROM session_branches WHERE branch_id=? AND active=1)`, parent, s.branchID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.tip = parent
	return nil
}

func (s *SQLiteStore) refreshBranchTipLocked() {
	var tip string
	if err := s.db.QueryRow(`SELECT tip_id FROM session_branches WHERE branch_id=?`, s.branchID).Scan(&tip); err == nil {
		s.tip = tip
	}
}

// BranchTip implements Store.
func (s *SQLiteStore) BranchTip() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tip
}

// SetBranchTip implements Store.
func (s *SQLiteStore) SetBranchTip(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	expectedTip := s.tip
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite begin set tip: %w", err)
	}
	var exists int
	if err := tx.QueryRow(`SELECT count(*) FROM entries WHERE id = ?`, id).Scan(&exists); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite lookup tip: %w", err)
	}
	if exists == 0 {
		_ = tx.Rollback()
		return ErrNotFound
	}
	now := time.Now().UnixMilli()
	result, err := tx.Exec(`UPDATE session_branches SET tip_id = ?, updated_at = ? WHERE branch_id = ? AND tip_id = ?`, id, now, s.branchID, expectedTip)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite set branch tip: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		_ = tx.Rollback()
		s.refreshBranchTipLocked()
		return fmt.Errorf("%w on branch %q (expected %q)", ErrConflict, s.branchID, expectedTip)
	}
	if _, err := tx.Exec(`UPDATE session_meta SET branch_tip = ? WHERE singleton = 1
		AND EXISTS (SELECT 1 FROM session_branches WHERE branch_id = ? AND active = 1)`, id, s.branchID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite set tip: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: sqlite commit set tip: %w", err)
	}
	s.tip = id
	return nil
}

// Messages implements Store. The recursive query returns only complete
// messages on the active root-to-tip branch rather than loading all historical
// branches.
func (s *SQLiteStore) Messages() ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	rows, err := s.db.Query(`
		WITH RECURSIVE branch(seq, id, parent_id, entry_type, message) AS (
			SELECT seq, id, parent_id, entry_type, message
			FROM entries WHERE id = ?
			UNION ALL
			SELECT e.seq, e.id, e.parent_id, e.entry_type, e.message
			FROM entries e JOIN branch b ON e.id = b.parent_id
		)
		SELECT id, parent_id, entry_type, message FROM branch ORDER BY seq`, s.tip)
	if err != nil {
		return nil, fmt.Errorf("session: sqlite messages: %w", err)
	}
	defer rows.Close()
	var messages []protocol.Message
	for rows.Next() {
		var id, parentID string
		var typ EntryType
		var raw []byte
		if err := rows.Scan(&id, &parentID, &typ, &raw); err != nil {
			return nil, fmt.Errorf("session: sqlite scan message: %w", err)
		}
		if typ != EntryMessage || len(raw) == 0 {
			continue
		}
		var message protocol.Message
		if err := json.Unmarshal(raw, &message); err != nil {
			return nil, fmt.Errorf("session: sqlite decode message: %w", err)
		}
		message.ID, message.ParentID = id, parentID
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: sqlite messages rows: %w", err)
	}
	return messages, nil
}

// ContextMessages implements ContextStore. It preserves complete history in
// Messages while hiding entries before the latest compaction marker.
func (s *SQLiteStore) ContextMessages() ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	entries, err := s.branchEntries(s.tip)
	if err != nil {
		return nil, err
	}
	return contextMessagesFromEntries(entries), nil
}

// BranchEntries implements BranchEntryStore.
func (s *SQLiteStore) BranchEntries() ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	entries, err := s.branchEntries(s.tip)
	return cloneEntries(entries), err
}

// Branches implements BranchStore.
func (s *SQLiteStore) Branches() ([]protocol.SessionBranch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	rows, err := s.db.Query(`
		SELECT branch_id, branch_name, parent_branch_id, forked_from_id, tip_id, created_at, updated_at, active
		FROM session_branches ORDER BY created_at, branch_id`)
	if err != nil {
		return nil, fmt.Errorf("session: sqlite branches: %w", err)
	}
	var out []protocol.SessionBranch
	for rows.Next() {
		var branch protocol.SessionBranch
		var active int
		if err := rows.Scan(&branch.ID, &branch.Name, &branch.ParentID, &branch.ForkedFromID, &branch.TipID, &branch.CreatedAt, &branch.UpdatedAt, &active); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("session: sqlite branch scan: %w", err)
		}
		branch.Active = active != 0
		out = append(out, branch)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("session: sqlite branch rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("session: sqlite branch close: %w", err)
	}
	for i := range out {
		entries, err := s.branchEntries(out[i].TipID)
		if err != nil {
			return nil, err
		}
		out[i].Messages, out[i].Preview = branchStats(entries)
	}
	return out, nil
}

// SelectBranch implements BranchStore.
func (s *SQLiteStore) SelectBranch(branchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite select branch begin: %w", err)
	}
	locked, err := tx.Exec(`UPDATE session_branches SET updated_at=updated_at WHERE branch_id=?`, branchID)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite lock selected branch: %w", err)
	}
	if n, _ := locked.RowsAffected(); n != 1 {
		_ = tx.Rollback()
		return ErrNotFound
	}
	var tip string
	if err := tx.QueryRow(`SELECT tip_id FROM session_branches WHERE branch_id = ?`, branchID).Scan(&tip); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite select branch: %w", err)
	}
	if _, err := tx.Exec(`UPDATE session_branches SET active = 0`); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite deactivate branches: %w", err)
	}
	if _, err := tx.Exec(`UPDATE session_branches SET active = 1 WHERE branch_id = ?`, branchID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite activate branch: %w", err)
	}
	if _, err := tx.Exec(`UPDATE session_meta SET branch_tip = ? WHERE singleton = 1`, tip); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite active tip: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: sqlite select branch commit: %w", err)
	}
	s.branchID = branchID
	s.tip = tip
	return nil
}

// ForkBranch implements BranchStore without copying entries.
func (s *SQLiteStore) ForkBranch(fromEntryID string) (protocol.SessionBranch, error) {
	return s.ForkBranchWithOptions(protocol.BranchForkOptions{FromEntryID: fromEntryID})
}

func (s *SQLiteStore) ForkBranchWithOptions(opts protocol.BranchForkOptions) (protocol.SessionBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fromEntryID := opts.FromEntryID
	if s.closed {
		return protocol.SessionBranch{}, errors.New("session: store closed")
	}
	var exists int
	if err := s.db.QueryRow(`SELECT count(*) FROM entries WHERE id = ?`, fromEntryID).Scan(&exists); err != nil {
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork lookup: %w", err)
	}
	if exists == 0 {
		return protocol.SessionBranch{}, ErrNotFound
	}
	entries, err := s.branchEntries(fromEntryID)
	if err != nil {
		return protocol.SessionBranch{}, err
	}
	messages, preview := branchStats(entries)
	sourceID := opts.SourceBranchID
	// The no-op write acquires SQLite's writer reservation before source,
	// ancestry, and uniqueness checks. Other handles therefore cannot delete or
	// retarget the source between validation and insertion.
	tx, err := s.db.Begin()
	if err != nil {
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var locked sql.Result
	if sourceID == "" {
		locked, err = tx.Exec(`UPDATE session_branches SET updated_at=updated_at WHERE active=1`)
		if err == nil {
			err = tx.QueryRow(`SELECT branch_id FROM session_branches WHERE active=1`).Scan(&sourceID)
		}
	} else {
		locked, err = tx.Exec(`UPDATE session_branches SET updated_at=updated_at WHERE branch_id=?`, sourceID)
	}
	if err != nil {
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite lock fork source: %w", err)
	}
	if n, _ := locked.RowsAffected(); n != 1 {
		return protocol.SessionBranch{}, ErrNotFound
	}
	var belongs int
	if err := tx.QueryRow(`WITH RECURSIVE ancestry(id,parent_id) AS (
		SELECT id,parent_id FROM entries WHERE id=(SELECT tip_id FROM session_branches WHERE branch_id=?)
		UNION ALL SELECT e.id,e.parent_id FROM entries e JOIN ancestry a ON e.id=a.parent_id
	) SELECT count(*) FROM ancestry WHERE id=?`, sourceID, fromEntryID).Scan(&belongs); err != nil {
		return protocol.SessionBranch{}, err
	}
	if belongs == 0 {
		return protocol.SessionBranch{}, errors.New("session: fork entry is not on source branch")
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		var count int
		_ = tx.QueryRow(`SELECT count(*) FROM session_branches`).Scan(&count)
		for n := count + 1; ; n++ {
			name = fmt.Sprintf("branch-%d", n)
			var exists int
			_ = tx.QueryRow(`SELECT count(*) FROM session_branches WHERE branch_name=? COLLATE NOCASE`, name).Scan(&exists)
			if exists == 0 {
				break
			}
		}
	}
	if err := validateBranchName(name); err != nil {
		return protocol.SessionBranch{}, err
	}
	var duplicate int
	if err := tx.QueryRow(`SELECT count(*) FROM session_branches WHERE branch_name=? COLLATE NOCASE`, name).Scan(&duplicate); err != nil {
		return protocol.SessionBranch{}, err
	}
	if duplicate > 0 {
		return protocol.SessionBranch{}, errors.New("session: branch name already exists")
	}
	branchID := "branch-" + randomSuffix()
	now := time.Now().UnixMilli()
	if _, err := tx.Exec(`UPDATE session_branches SET active = 0`); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork deactivate: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO session_branches(branch_id, branch_name, parent_branch_id, forked_from_id, tip_id, created_at, updated_at, active) VALUES(?, ?, ?, ?, ?, ?, ?, 1)`, branchID, name, sourceID, fromEntryID, fromEntryID, now, now); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork insert: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO thread_state(branch_id, collaboration_mode)
		SELECT ?, collaboration_mode FROM thread_state WHERE branch_id = ?`, branchID, sourceID); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork thread state: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO thread_goals(branch_id, goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at)
		SELECT ?, ?, objective, status, token_budget, tokens_used, seconds_used, created_at, ? FROM thread_goals WHERE branch_id = ?`, branchID, newID(), now, sourceID); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork goal: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO thread_goal_costs(branch_id, currency, input_cost, output_cost, cache_read_cost, cache_write_cost, total_cost)
		SELECT ?, currency, input_cost, output_cost, cache_read_cost, cache_write_cost, total_cost FROM thread_goal_costs WHERE branch_id = ?`, branchID, sourceID); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork goal costs: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO thread_goal_deferrals(branch_id, deferred) SELECT ?, deferred FROM thread_goal_deferrals WHERE branch_id = ?`, branchID, sourceID); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork goal deferral: %w", err)
	}
	if _, err := tx.Exec(`UPDATE session_meta SET branch_tip = ? WHERE singleton = 1`, fromEntryID); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork tip: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork commit: %w", err)
	}
	s.branchID = branchID
	s.tip = fromEntryID
	return protocol.SessionBranch{ID: branchID, Name: name, ParentID: sourceID, ForkedFromID: fromEntryID, TipID: fromEntryID, Messages: messages, Preview: preview, CreatedAt: now, UpdatedAt: now, Active: true}, nil
}

func (s *SQLiteStore) RenameBranch(branchID, name string) (protocol.SessionBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return protocol.SessionBranch{}, errors.New("session: store closed")
	}
	name = strings.TrimSpace(name)
	if err := validateBranchName(name); err != nil {
		return protocol.SessionBranch{}, err
	}
	var duplicate int
	if err := s.db.QueryRow(`SELECT count(*) FROM session_branches WHERE branch_name=? COLLATE NOCASE AND branch_id!=?`, name, branchID).Scan(&duplicate); err != nil {
		return protocol.SessionBranch{}, err
	}
	if duplicate > 0 {
		return protocol.SessionBranch{}, errors.New("session: branch name already exists")
	}
	now := time.Now().UnixMilli()
	res, err := s.db.Exec(`UPDATE session_branches SET branch_name=?, updated_at=? WHERE branch_id=?`, name, now, branchID)
	if err != nil {
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite rename branch: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return protocol.SessionBranch{}, ErrNotFound
	}
	var branch protocol.SessionBranch
	var active int
	if err := s.db.QueryRow(`SELECT branch_id,branch_name,parent_branch_id,forked_from_id,tip_id,created_at,updated_at,active FROM session_branches WHERE branch_id=?`, branchID).Scan(&branch.ID, &branch.Name, &branch.ParentID, &branch.ForkedFromID, &branch.TipID, &branch.CreatedAt, &branch.UpdatedAt, &active); err != nil {
		return protocol.SessionBranch{}, err
	}
	branch.Active = active != 0
	return branch, nil
}

// DeleteBranch removes an inactive non-main leaf branch and its branch-scoped state.
func (s *SQLiteStore) DeleteBranch(branchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if branchID == "" || branchID == "main" {
		return errors.New("session: cannot delete active or main branch")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite delete branch begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Acquire the writer lock before checking database-authoritative active and
	// topology state. This prevents stale handles from deleting a branch another
	// handle selected concurrently.
	locked, err := tx.Exec(`UPDATE session_branches SET updated_at=updated_at WHERE branch_id=?`, branchID)
	if err != nil {
		return fmt.Errorf("session: sqlite lock delete branch: %w", err)
	}
	if n, _ := locked.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	var active int
	if err := tx.QueryRow(`SELECT active FROM session_branches WHERE branch_id=?`, branchID).Scan(&active); err != nil {
		return err
	}
	if active != 0 {
		return errors.New("session: cannot delete active or main branch")
	}
	var children, agents, goals int
	if err := tx.QueryRow(`SELECT count(*) FROM session_branches WHERE parent_branch_id=?`, branchID).Scan(&children); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT count(*) FROM subagent_threads WHERE parent_branch_id=?`, branchID).Scan(&agents); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT count(*) FROM thread_goals WHERE branch_id=? AND status NOT IN ('complete','budget_limited')`, branchID).Scan(&goals); err != nil {
		return err
	}
	if children > 0 {
		return errors.New("session: cannot delete branch with children")
	}
	if agents > 0 {
		return errors.New("session: cannot delete branch with durable subagents")
	}
	if goals > 0 {
		return errors.New("session: cannot delete branch with nonterminal goal")
	}
	for _, table := range []string{"thread_goal_deferrals", "thread_goals", "thread_state", "session_branches"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE branch_id = ?`, branchID); err != nil {
			return fmt.Errorf("session: sqlite delete branch %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: sqlite delete branch commit: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteBranchForRollback(branchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if branchID == "" || branchID == "main" || branchID == s.branchID {
		return errors.New("session: cannot roll back active or main branch")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRow(`SELECT count(*) FROM session_branches WHERE branch_id=?`, branchID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	for _, table := range []string{"thread_goal_deferrals", "thread_goals", "thread_state", "session_branches"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE branch_id=?`, branchID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) branchEntries(tip string) ([]Entry, error) {
	rows, err := s.db.Query(`
		WITH RECURSIVE branch(seq, id, parent_id, entry_type, message, summary, compacted_through, meta_key, meta_value) AS (
			SELECT seq, id, parent_id, entry_type, message, summary, compacted_through, meta_key, meta_value
			FROM entries WHERE id = ?
			UNION ALL
			SELECT e.seq, e.id, e.parent_id, e.entry_type, e.message, e.summary, e.compacted_through, e.meta_key, e.meta_value
			FROM entries e JOIN branch b ON e.id = b.parent_id
		)
		SELECT id, parent_id, entry_type, message, summary, compacted_through, meta_key, meta_value
		FROM branch ORDER BY seq`, tip)
	if err != nil {
		return nil, fmt.Errorf("session: sqlite branch: %w", err)
	}
	defer rows.Close()
	var entries []Entry
	for rows.Next() {
		var e Entry
		var raw []byte
		if err := rows.Scan(&e.ID, &e.ParentID, &e.Type, &raw, &e.Summary, &e.CompactedThrough, &e.Key, &e.Value); err != nil {
			return nil, fmt.Errorf("session: sqlite scan entry: %w", err)
		}
		if len(raw) != 0 {
			e.Message = new(protocol.Message)
			if err := json.Unmarshal(raw, e.Message); err != nil {
				return nil, fmt.Errorf("session: sqlite decode entry: %w", err)
			}
			normalizeEntryMessage(&e)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Fork implements Store. Branch copying is intentionally explicit; the
// original database remains untouched and the returned branch is in memory.
func (s *SQLiteStore) Fork(fromID string) (Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	var exists int
	if err := s.db.QueryRow(`SELECT count(*) FROM entries WHERE id = ?`, fromID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("session: sqlite lookup fork: %w", err)
	}
	if exists == 0 {
		return nil, ErrNotFound
	}
	entries, err := s.branchEntries(fromID)
	if err != nil {
		return nil, err
	}
	n := NewMemoryStore(Options{ID: s.header.ID + "-fork", CWD: s.header.CWD, Name: s.header.Name})
	for _, e := range entries {
		if e.ID == "root" {
			continue
		}
		if err := n.Append(e); err != nil {
			return nil, err
		}
	}
	return n, nil
}

func (s *SQLiteStore) ActiveBranchID() string { s.mu.RLock(); defer s.mu.RUnlock(); return s.branchID }

func scanSubagent(row interface{ Scan(...any) error }) (SubagentRecord, error) {
	var rec SubagentRecord
	var usage []byte
	err := row.Scan(&rec.State.Agent.ThreadID, &rec.State.Agent.ParentThreadID, &rec.ParentBranchID,
		&rec.State.Agent.Path, &rec.State.Agent.ParentPath, &rec.State.Agent.Role, &rec.RoleFingerprint, &rec.State.Agent.Nickname,
		&rec.State.Agent.Depth, &rec.State.Status, &rec.ChildSessionPath, &rec.State.Provider, &rec.State.Model,
		&rec.State.Thinking, &rec.State.CreatedAt, &rec.State.StartedAt, &rec.State.FinishedAt,
		&rec.State.Result, &rec.State.Error, &usage, &rec.State.Generation)
	if err != nil {
		return rec, err
	}
	if len(usage) != 0 {
		var u protocol.Usage
		if err := json.Unmarshal(usage, &u); err != nil {
			return rec, err
		}
		rec.State.Usage = &u
	}
	return rec, rec.State.Validate()
}

const subagentColumns = `thread_id,parent_thread_id,parent_branch_id,agent_path,parent_path,role,role_fingerprint,nickname,depth,status,child_session_path,model_provider,model_id,thinking,created_at,started_at,finished_at,result,error,usage_json,generation`

func (s *SQLiteStore) ListSubagents() ([]SubagentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`SELECT ` + subagentColumns + ` FROM subagent_threads ORDER BY created_at, thread_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubagentRecord
	for rows.Next() {
		rec, err := scanSubagent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func subagentArgs(rec SubagentRecord) []any {
	var usage []byte
	if rec.State.Usage != nil {
		usage, _ = json.Marshal(rec.State.Usage)
	}
	return []any{rec.State.Agent.ThreadID, rec.State.Agent.ParentThreadID, rec.ParentBranchID,
		rec.State.Agent.Path, rec.State.Agent.ParentPath, rec.State.Agent.Role, rec.RoleFingerprint, rec.State.Agent.Nickname,
		rec.State.Agent.Depth, rec.State.Status, rec.ChildSessionPath, rec.State.Provider, rec.State.Model,
		rec.State.Thinking, rec.State.CreatedAt, rec.State.StartedAt, rec.State.FinishedAt,
		rec.State.Result, rec.State.Error, usage, rec.State.Generation}
}

func (s *SQLiteStore) PutSubagent(rec SubagentRecord) error {
	if err := rec.State.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q := `INSERT INTO subagent_threads (` + subagentColumns + `) VALUES (` + strings.TrimSuffix(strings.Repeat("?,", len(strings.Split(subagentColumns, ","))), ",") + `)`
	if _, err := s.db.Exec(q, subagentArgs(rec)...); err != nil {
		return fmt.Errorf("session: sqlite put subagent: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CompareAndSwapSubagent(id string, expected uint64, rec SubagentRecord) error {
	if err := rec.State.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	args := subagentArgs(rec)
	q := `UPDATE subagent_threads SET parent_thread_id=?,parent_branch_id=?,agent_path=?,parent_path=?,role=?,role_fingerprint=?,nickname=?,depth=?,status=?,child_session_path=?,model_provider=?,model_id=?,thinking=?,created_at=?,started_at=?,finished_at=?,result=?,error=?,usage_json=?,generation=? WHERE thread_id=? AND generation=?`
	updateArgs := append(append([]any(nil), args[1:]...), id, expected)
	res, err := s.db.Exec(q, updateArgs...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("session: stale subagent generation")
	}
	return nil
}
func (s *SQLiteStore) DeleteSubagent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM subagent_threads WHERE thread_id=?`, id)
	if err == nil {
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrNotFound
		}
	}
	return err
}

// Close implements Store. Empty databases are discarded instead of being
// retained as sessions.
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}

	meaningfulCount, countErr := s.durableStateCountLocked()
	closeErr := s.db.Close()
	s.closed = true
	if countErr != nil || closeErr != nil {
		return errors.Join(countErr, closeErr)
	}
	if meaningfulCount > 0 || !s.deleteIfEmpty {
		return nil
	}

	var errs []error
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(s.path + suffix); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("session: remove empty database %q: %w", s.path+suffix, err))
		}
	}
	return errors.Join(errs...)
}

func (s *SQLiteStore) messageCount() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, errors.New("session: store closed")
	}
	var count int
	err := s.db.QueryRow(`
		WITH RECURSIVE branch(id, parent_id, entry_type) AS (
			SELECT id, parent_id, entry_type FROM entries WHERE id = ?
			UNION ALL
			SELECT e.id, e.parent_id, e.entry_type
			FROM entries e JOIN branch b ON e.id = b.parent_id
		)
		SELECT count(*) FROM branch WHERE entry_type = ?`, s.tip, EntryMessage).Scan(&count)
	return count, err
}

// hasDurableState reports whether the database contains any user-visible or
// persisted branch state, including state on inactive historical branches.
func (s *SQLiteStore) hasDurableState() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return false, errors.New("session: store closed")
	}
	count, err := s.durableStateCountLocked()
	return count > 0, err
}

func (s *SQLiteStore) durableStateCountLocked() (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT
			(SELECT count(*) FROM entries WHERE id <> 'root') +
			(SELECT count(*) FROM session_meta WHERE name <> '') +
			(SELECT count(*) FROM subagent_threads) +
			(SELECT count(*) FROM thread_goals) +
			(SELECT count(*) FROM session_branches WHERE branch_id <> 'main') +
			(SELECT count(*) FROM thread_state WHERE collaboration_mode <> 'default') +
			(SELECT count(*) FROM thread_goal_deferrals WHERE deferred <> 0)
	`).Scan(&count)
	return count, err
}
