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
	mu       sync.RWMutex
	path     string
	header   Header
	tip      string
	branchID string
	db       *sql.DB
	closed   bool
}

// NewSQLiteStore opens or creates a SQLite-backed session database at path.
// Existing JSONL files are intentionally not supported; session storage now
// uses the SQLite schema exclusively.
func NewSQLiteStore(path, cwd string, opts Options) (*SQLiteStore, error) {
	if path == "" {
		return nil, errors.New("session: sqlite path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("session: mkdir: %w", err)
	}
	existing := false
	if info, err := os.Stat(path); err == nil {
		existing = true
		if info.Mode().IsRegular() && info.Size() == 0 {
			return nil, errors.New("session: existing sqlite database is empty")
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("session: stat: %w", err)
	}
	id := opts.ID
	if id == "" {
		id = newID()
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
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
	// Session databases contain prompts and tool results.
	if err := os.Chmod(path, 0o600); err != nil {
		return closeDB(fmt.Errorf("session: sqlite chmod: %w", err))
	}
	if err := createSQLiteSchema(db); err != nil {
		return closeDB(err)
	}

	s := &SQLiteStore{path: path, db: db, branchID: "main"}
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
			INSERT INTO session_branches(branch_id, tip_id, created_at, updated_at, active)
			VALUES('main', ?, ?, ?, 1)`, s.tip, s.header.CreatedAt, s.header.CreatedAt); err != nil {
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
			INSERT INTO session_branches(branch_id, tip_id, created_at, updated_at, active)
			VALUES('main', ?, ?, ?, 1)`, tip, createdAt, now); err != nil {
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

// ID implements Store.
func (s *SQLiteStore) ID() string { return s.header.ID }

// Path implements Store.
func (s *SQLiteStore) Path() string { return s.path }

// Header implements Store.
func (s *SQLiteStore) Header() Header { return s.header }

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

func (s *SQLiteStore) Goal() (*protocol.ThreadGoal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	g, err := scanGoal(s.db.QueryRow(`SELECT goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), s.header.ID, s.branchID)
	if err != nil {
		return nil, fmt.Errorf("session: sqlite goal: %w", err)
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
	g, err := scanGoal(tx.QueryRow(`UPDATE thread_goals SET goal_id=?, objective=?, status=?, updated_at=?
		WHERE branch_id=? AND goal_id=?
		RETURNING goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at`, nextGoalID, objective, protocol.GoalActive, now, s.branchID, expected), s.header.ID, s.branchID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		current, readErr := scanGoal(tx.QueryRow(`SELECT goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), s.header.ID, s.branchID)
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
	g, err := scanGoal(tx.QueryRow(`UPDATE thread_goals SET status=?, updated_at=?
		WHERE branch_id=? AND goal_id=? AND status=?
		RETURNING goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at`, nextStatus, now, s.branchID, expected, expectedStatus), s.header.ID, s.branchID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		current, readErr := scanGoal(tx.QueryRow(`SELECT goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), s.header.ID, s.branchID)
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
	now := time.Now().UnixMilli()
	g, err := scanGoal(s.db.QueryRow(`UPDATE thread_goals SET
		objective = CASE WHEN ? = 1 THEN ? ELSE objective END,
		status = CASE WHEN ? = 1 THEN ? ELSE status END,
		token_budget = CASE WHEN ? = 1 THEN ? ELSE token_budget END,
		updated_at = ?
		WHERE branch_id = ? AND goal_id = ?
		RETURNING goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at`,
		objectiveSet, objectiveValue, statusSet, statusValue, budgetSet, budgetValue, now, s.branchID, expected), s.header.ID, s.branchID)
	if err != nil {
		return nil, err
	}
	if g != nil {
		return g.Clone(), nil
	}
	current, readErr := scanGoal(s.db.QueryRow(`SELECT goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), s.header.ID, s.branchID)
	if readErr != nil {
		return nil, readErr
	}
	if current == nil {
		return nil, ErrNotFound
	}
	return nil, errors.New("session: stale goal id")
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

func (s *SQLiteStore) AccountGoal(expected string, tokens, seconds int64) (*protocol.ThreadGoal, bool, error) {
	if tokens < 0 || seconds < 0 {
		return nil, false, errors.New("session: goal usage delta cannot be negative")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	g, err := scanGoal(s.db.QueryRow(`UPDATE thread_goals
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
		current, readErr := scanGoal(s.db.QueryRow(`SELECT goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), s.header.ID, s.branchID)
		if readErr != nil {
			return nil, false, readErr
		}
		if current == nil || current.GoalID != expected {
			return current, false, nil
		}
		return nil, false, errors.New("session: goal usage overflow")
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if entry.ID == "" {
		entry.ID = newID()
	}
	if entry.ParentID == "" {
		entry.ParentID = s.tip
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
	if result, err := tx.Exec(`UPDATE session_branches SET tip_id = ?, updated_at = ? WHERE branch_id = ?`, entry.ID, now, s.branchID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite branch tip: %w", err)
	} else if n, _ := result.RowsAffected(); n != 1 {
		_ = tx.Rollback()
		return fmt.Errorf("session: sqlite active branch %q not found", s.branchID)
	}
	if _, err := tx.Exec(`UPDATE session_meta SET branch_tip = ? WHERE singleton = 1`, entry.ID); err != nil {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	parent := s.tip
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
		if batch[i].Message != nil {
			batch[i].Message.ParentID = batch[i].ParentID
		}
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
	if res, err := tx.Exec(`UPDATE session_branches SET tip_id=?,updated_at=? WHERE branch_id=?`, parent, now, s.branchID); err != nil {
		return err
	} else if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("session: sqlite active branch %q not found", s.branchID)
	}
	if _, err := tx.Exec(`UPDATE session_meta SET branch_tip=? WHERE singleton=1`, parent); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.tip = parent
	return nil
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
	var exists int
	if err := s.db.QueryRow(`SELECT count(*) FROM entries WHERE id = ?`, id).Scan(&exists); err != nil {
		return fmt.Errorf("session: sqlite lookup tip: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	now := time.Now().UnixMilli()
	if result, err := s.db.Exec(`UPDATE session_branches SET tip_id = ?, updated_at = ? WHERE branch_id = ?`, id, now, s.branchID); err != nil {
		return fmt.Errorf("session: sqlite set branch tip: %w", err)
	} else if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("session: sqlite active branch %q not found", s.branchID)
	}
	if _, err := s.db.Exec(`UPDATE session_meta SET branch_tip = ? WHERE singleton = 1`, id); err != nil {
		return fmt.Errorf("session: sqlite set tip: %w", err)
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
		SELECT entry_type, message FROM branch ORDER BY seq`, s.tip)
	if err != nil {
		return nil, fmt.Errorf("session: sqlite messages: %w", err)
	}
	defer rows.Close()
	var messages []protocol.Message
	for rows.Next() {
		var typ EntryType
		var raw []byte
		if err := rows.Scan(&typ, &raw); err != nil {
			return nil, fmt.Errorf("session: sqlite scan message: %w", err)
		}
		if typ != EntryMessage || len(raw) == 0 {
			continue
		}
		var message protocol.Message
		if err := json.Unmarshal(raw, &message); err != nil {
			return nil, fmt.Errorf("session: sqlite decode message: %w", err)
		}
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

// Branches implements BranchStore.
func (s *SQLiteStore) Branches() ([]protocol.SessionBranch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	rows, err := s.db.Query(`
		SELECT branch_id, tip_id, created_at, updated_at, active
		FROM session_branches ORDER BY created_at, branch_id`)
	if err != nil {
		return nil, fmt.Errorf("session: sqlite branches: %w", err)
	}
	var out []protocol.SessionBranch
	for rows.Next() {
		var branch protocol.SessionBranch
		var active int
		if err := rows.Scan(&branch.ID, &branch.TipID, &branch.CreatedAt, &branch.UpdatedAt, &active); err != nil {
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
	var tip string
	if err := s.db.QueryRow(`SELECT tip_id FROM session_branches WHERE branch_id = ?`, branchID).Scan(&tip); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("session: sqlite select branch: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite select branch begin: %w", err)
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	branchID := "branch-" + randomSuffix()
	now := time.Now().UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork begin: %w", err)
	}
	if _, err := tx.Exec(`UPDATE session_branches SET active = 0`); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork deactivate: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO session_branches(branch_id, tip_id, created_at, updated_at, active) VALUES(?, ?, ?, ?, 1)`, branchID, fromEntryID, now, now); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork insert: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO thread_state(branch_id, collaboration_mode)
		SELECT ?, collaboration_mode FROM thread_state WHERE branch_id = ?`, branchID, s.branchID); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork thread state: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO thread_goals(branch_id, goal_id, objective, status, token_budget, tokens_used, seconds_used, created_at, updated_at)
		SELECT ?, ?, objective, status, token_budget, tokens_used, seconds_used, created_at, ? FROM thread_goals WHERE branch_id = ?`, branchID, newID(), now, s.branchID); err != nil {
		_ = tx.Rollback()
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork goal: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO thread_goal_deferrals(branch_id, deferred) SELECT ?, deferred FROM thread_goal_deferrals WHERE branch_id = ?`, branchID, s.branchID); err != nil {
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
	return protocol.SessionBranch{ID: branchID, TipID: fromEntryID, Messages: messages, Preview: preview, CreatedAt: now, UpdatedAt: now, Active: true}, nil
}

// DeleteBranch removes an inactive non-main branch and its branch-scoped state.
func (s *SQLiteStore) DeleteBranch(branchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if branchID == "" || branchID == "main" || branchID == s.branchID {
		return errors.New("session: cannot delete active or main branch")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("session: sqlite delete branch begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRow(`SELECT count(*) FROM session_branches WHERE branch_id = ?`, branchID).Scan(&exists); err != nil {
		return fmt.Errorf("session: sqlite delete branch lookup: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
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

	var messageCount, topologyCount int
	countErr := s.db.QueryRow(`SELECT count(*) FROM entries WHERE entry_type = ?`, EntryMessage).Scan(&messageCount)
	if countErr == nil {
		countErr = s.db.QueryRow(`SELECT count(*) FROM subagent_threads`).Scan(&topologyCount)
	}
	closeErr := s.db.Close()
	s.closed = true
	if countErr != nil || closeErr != nil {
		return errors.Join(countErr, closeErr)
	}
	if messageCount > 0 || topologyCount > 0 {
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

// hasMessages reports whether the database contains any message, including
// messages on inactive historical branches.
func (s *SQLiteStore) hasMessages() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return false, errors.New("session: store closed")
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM entries WHERE entry_type = ?`, EntryMessage).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
