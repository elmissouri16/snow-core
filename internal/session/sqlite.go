package session

import (
	"database/sql"
	"os"
	"sync"

	_ "modernc.org/sqlite"
)

// SQLiteStore is the durable session store. It keeps the active branch cursor
// in memory while branch references and entries remain in SQLite. Entries are
// queried when Messages or ContextMessages is requested. modernc.org/sqlite is
// a pure-Go, CGo-free database/sql driver.
type contextCacheKey struct {
	branchID string
	tip      string
}

type SQLiteStore struct {
	mu            sync.RWMutex
	path          string
	header        Header
	tip           string
	branchID      string
	db            *sql.DB
	lease         *os.File
	closed        bool
	deleteIfEmpty bool

	// Entries are append-only, so the decoded chain under one branch/tip key is
	// immutable. The cache remains private; ContextMessages still projects fresh
	// defensive message clones for callers.
	contextCacheKey     contextCacheKey
	contextCacheEntries []Entry
}

type goalCostQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

type sqliteQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

const subagentColumns = `thread_id,parent_thread_id,parent_branch_id,agent_path,parent_path,role,role_fingerprint,nickname,depth,status,child_session_path,model_provider,model_id,thinking,created_at,started_at,finished_at,result,error,usage_json,generation`
