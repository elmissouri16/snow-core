// Package session implements durable conversation storage in SQLite with
// indexed tree branching (id/parentId), plus an in-memory variant for tests and
// ephemeral SDK use. The legacy JSONL implementation remains only for isolated
// old unit fixtures; the app and FileIndex use SQLite exclusively.
package session

import (
	"errors"
	"os"
	"sync"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// SessionVersion is the current on-disk schema version.
const SessionVersion = 10

// Header is the first line of every session file.
type Header struct {
	Version         int    `json:"v"`
	ID              string `json:"id"`
	CreatedAt       int64  `json:"created_at"`
	CWD             string `json:"cwd"`
	Name            string `json:"name,omitempty"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	ParentBranchID  string `json:"parent_branch_id,omitempty"`
	ForkEntryID     string `json:"fork_entry_id,omitempty"`
}

// EntryType enumerates session file entry kinds.
type EntryType string

const (
	EntryMessage    EntryType = "message"
	EntryCompaction EntryType = "compaction"
	EntryMeta       EntryType = "meta"

	// MetaToolTranscript stores branch-scoped, provider-excluded presentation
	// metadata for harness tool activity without a matching tool-result message.
	MetaToolTranscript = "tool_transcript_v1"
)

// Entry is one line in a session file.
type Entry struct {
	Type     EntryType         `json:"type"`
	ID       string            `json:"id"`
	ParentID string            `json:"parent_id,omitempty"`
	Message  *protocol.Message `json:"message,omitempty"`
	// Compaction
	Summary          string `json:"summary,omitempty"`
	CompactedThrough string `json:"compacted_through,omitempty"`
	// Meta
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// Store is the session abstraction used by the agent.
type Store interface {
	ID() string
	Path() string // empty for in-memory
	Header() Header
	Append(entry Entry) error
	// BranchTip returns the active leaf id.
	BranchTip() string
	// SetBranchTip moves the active cursor (tree navigation).
	SetBranchTip(id string) error
	// Messages returns complete messages linearized from the root to the branch tip.
	Messages() ([]protocol.Message, error)
	// Fork creates a standalone legacy copy. Built-in stores also implement
	// BranchStore for durable same-database branches.
	Fork(fromID string) (Store, error)
	Close() error
}

// ContextStore provides the provider-facing logical context projection. It
// keeps complete history available through Store.Messages while hiding entries
// before the latest compaction marker from the next model request.
// BatchStore appends one ordered chain atomically. Built-in memory/SQLite
// stores implement it for collaboration mailbox delivery.
type BatchStore interface{ AppendBatch([]Entry) error }

// TitleStore manages the session-wide display title without changing branch
// tips or appending conversation history. Built-in stores implement it.
type TitleStore interface {
	SessionTitle() (string, error)
	RenameSession(title string) error
	AppendWithInitialTitle(entry Entry, title string) error
}

type ContextStore interface {
	ContextMessages() ([]protocol.Message, error)
}

// BranchEntryStore exposes a defensive root-to-tip entry snapshot for durable
// host state that must remain branch-scoped but must not enter provider message
// context. Built-in stores implement it.
type BranchEntryStore interface {
	BranchEntries() ([]Entry, error)
}

// BranchStore provides durable same-database branch references. A fork shares
// the existing entry tree and starts with its tip at fromEntryID.
type BranchStore interface {
	Branches() ([]protocol.SessionBranch, error)
	SelectBranch(branchID string) error
	ForkBranch(fromEntryID string) (protocol.SessionBranch, error)
}

// ActiveBranchStore optionally exposes the stable active branch identity used
// for provider request affinity. Custom stores may omit it and use session-wide
// affinity instead.
type ActiveBranchStore interface{ ActiveBranchID() string }

// BranchRollbackStore is an internal recovery seam for deleting a just-created
// inactive fork even when cloned state would block user-facing guarded delete.
type BranchRollbackStore interface{ DeleteBranchForRollback(branchID string) error }

// BranchManagementStore adds names/topology without breaking BranchStore.
type BranchManagementStore interface {
	BranchStore
	ForkBranchWithOptions(protocol.BranchForkOptions) (protocol.SessionBranch, error)
	RenameBranch(branchID, name string) (protocol.SessionBranch, error)
	DeleteBranch(branchID string) error
}

// MetadataStore provides append-only key/value state attached to a session.
// It is optional so legacy/custom Store implementations remain usable; built-in
// memory and SQLite stores implement it.
type MetadataStore interface {
	Metadata(key string) (value string, ok bool, err error)
	SetMetadata(key, value string) error
}

// ThreadStateStore persists branch-scoped runtime state without advancing the
// append-only conversation tip. Built-in stores copy this state on branch fork.
type ThreadStateStore interface {
	CollaborationMode() (protocol.CollaborationMode, error)
	SetCollaborationMode(protocol.CollaborationMode) error
}

// ThreadGoalStore provides atomic branch-scoped goal state without advancing
// the conversation tip. expectedGoalID is an optimistic stale-write guard.
type ThreadGoalStore interface {
	Goal() (*protocol.ThreadGoal, error)
	CreateGoal(protocol.ThreadGoal, bool) error
	UpdateGoal(expectedGoalID string, objective *string, status *protocol.ThreadGoalStatus, budget *int64) (*protocol.ThreadGoal, error)
	ClearGoal(expectedGoalID string) error
	AccountGoal(expectedGoalID string, tokenDelta, secondDelta int64, estimatedCostDelta *protocol.Cost) (*protocol.ThreadGoal, bool, error)
	GoalContinuationDeferred() (bool, error)
	SetGoalContinuationDeferred(bool) error
}

// ThreadGoalAtomicStore supplies compare-and-swap mutations needed by the goal
// controller. expectedGoalID="" in ReplaceGoal means no goal may exist.
type ThreadGoalAtomicStore interface {
	ReplaceGoal(expectedGoalID string, goal protocol.ThreadGoal) error
	ReviseGoal(expectedGoalID, nextGoalID, objective string) (*protocol.ThreadGoal, error)
	TransitionGoal(expectedGoalID string, expectedStatus, nextStatus protocol.ThreadGoalStatus, blockedReason string, clearDeferral bool) (*protocol.ThreadGoal, error)
}

// SubagentRecord stores root-scoped topology separately from conversation
// branches. ChildSessionPath points at an independent child database.
type SubagentRecord struct {
	State            protocol.SubagentState `json:"state"`
	ParentBranchID   string                 `json:"parent_branch_id"`
	ChildSessionPath string                 `json:"child_session_path"`
	// RoleFingerprint prevents a trusted config edit from silently changing
	// the authority/instructions of a durable child on reload.
	RoleFingerprint string `json:"role_fingerprint,omitempty"`
}

// SubagentTaskStore persists topology/status metadata without sharing a child
// transcript cursor with the root session.
type SubagentTaskStore interface {
	ListSubagents() ([]SubagentRecord, error)
	PutSubagent(SubagentRecord) error
	CompareAndSwapSubagent(threadID string, expectedGeneration uint64, next SubagentRecord) error
	DeleteSubagent(threadID string) error
	ActiveBranchID() string
}

// SessionInfo is a listing entry for the index.
type SessionInfo struct {
	Path           string `json:"path"`
	ID             string `json:"id"`
	CWD            string `json:"cwd"`
	Name           string `json:"name,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	Messages       int    `json:"messages"`
	MessagesCapped bool   `json:"messages_capped,omitempty"`
	// searchFingerprint tracks branch names/tips for the derived FTS cache.
	searchFingerprint string
}

// Index discovers and opens sessions on disk.
type Index interface {
	List(cwd string) ([]SessionInfo, error)
	Open(path string) (Store, error)
	Create(cwd string) (Store, error)
	// SessionsRoot returns the root sessions directory.
	SessionsRoot() string
}

// Options control store construction.
type Options struct {
	// Path is the SQLite database path. Empty means in-memory.
	Path string
	// CWD is the working directory recorded in the header.
	CWD string
	// Name is an optional display name.
	Name string
	// ID overrides the auto-generated id (tests).
	ID string
	// ParentSessionID, ParentBranchID, and ForkEntryID record immutable
	// provenance for independently materialized session forks.
	ParentSessionID string
	ParentBranchID  string
	ForkEntryID     string
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

const maxSessionTitleRunes = 72

var (
	ErrNotFound     = errors.New("session: not found")
	errSessionInUse = errors.New("session: database is open in another Snow process")
	// ErrConflict reports an optimistic branch-tip race between store handles.
	// The failed transaction is rolled back; callers may reload/select and retry.
	ErrConflict = errors.New("session: branch tip conflict")
)

// ---------------------------------------------------------------------------
// ID generation
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// In-memory store
// ---------------------------------------------------------------------------

// MemoryStore keeps entries in memory. Path() is empty.
type MemoryStore struct {
	mu           sync.RWMutex
	id           string
	header       Header
	entries      []Entry
	byID         map[string]int
	tip          string
	branches     map[string]protocol.SessionBranch
	threadModes  map[string]protocol.CollaborationMode
	threadGoals  map[string]*protocol.ThreadGoal
	goalDeferred map[string]bool
	subagents    map[string]SubagentRecord
	activeBranch string
	closed       bool
}

// ---------------------------------------------------------------------------
// JSONL store
// ---------------------------------------------------------------------------

// JSONLStore persists entries as JSONL lines. Line 0 is the header.
type JSONLStore struct {
	mu      sync.RWMutex
	path    string
	header  Header
	entries []Entry
	byID    map[string]int
	tip     string
	f       *os.File
	closed  bool
}

// ---------------------------------------------------------------------------
// Index
// ---------------------------------------------------------------------------

// FileIndex is the default disk-backed session index.
type FileIndex struct {
	Root string
}
