// Package session implements durable conversation storage in SQLite with
// indexed tree branching (id/parentId), plus an in-memory variant for tests and
// ephemeral SDK use. The legacy JSONL implementation remains only for isolated
// old unit fixtures; the app and FileIndex use SQLite exclusively.
package session

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/snow-core/snow/pkg/protocol"
)

// SessionVersion is the current on-disk schema version.
const SessionVersion = 8

// Header is the first line of every session file.
type Header struct {
	Version   int    `json:"v"`
	ID        string `json:"id"`
	CreatedAt int64  `json:"created_at"`
	CWD       string `json:"cwd"`
	Name      string `json:"name,omitempty"`
}

// EntryType enumerates session file entry kinds.
type EntryType string

const (
	EntryMessage    EntryType = "message"
	EntryCompaction EntryType = "compaction"
	EntryMeta       EntryType = "meta"
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

func cloneEntry(entry Entry) Entry {
	if entry.Message != nil {
		message := entry.Message.Clone()
		entry.Message = &message
	}
	return entry
}

func cloneEntries(entries []Entry) []Entry {
	out := make([]Entry, len(entries))
	for i, entry := range entries {
		out[i] = cloneEntry(entry)
	}
	return out
}

func normalizeEntryMessage(entry *Entry) {
	if entry == nil || entry.Message == nil {
		return
	}
	entry.Message.ID = entry.ID
	entry.Message.ParentID = entry.ParentID
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
	TransitionGoal(expectedGoalID string, expectedStatus, nextStatus protocol.ThreadGoalStatus, clearDeferral bool) (*protocol.ThreadGoal, error)
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
	Path      string `json:"path"`
	ID        string `json:"id"`
	CWD       string `json:"cwd"`
	Name      string `json:"name,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	Messages  int    `json:"messages"`
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
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

const maxSessionTitleRunes = 72

// SuggestedTitle derives a short, provider-free title from the first prompt.
// It collapses whitespace, removes control characters and Markdown heading/list
// prefixes, and truncates at a word boundary when practical.
func SuggestedTitle(prompt string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.TrimSpace(prompt) {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			if b.Len() > 0 {
				space = true
			}
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	title := strings.TrimSpace(b.String())
	title = strings.TrimSpace(strings.TrimLeft(title, "#>*-+ "))
	runes := []rune(title)
	if len(runes) <= maxSessionTitleRunes {
		return title
	}
	cut := maxSessionTitleRunes - 1
	for i := cut; i >= maxSessionTitleRunes/2; i-- {
		if unicode.IsSpace(runes[i]) {
			cut = i
			break
		}
	}
	return strings.TrimSpace(string(runes[:cut])) + "…"
}

func normalizeSessionTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > maxSessionTitleRunes {
		return "", fmt.Errorf("session: title must be 1..%d runes", maxSessionTitleRunes)
	}
	for _, r := range title {
		if unicode.IsControl(r) {
			return "", errors.New("session: title contains control characters")
		}
	}
	return title, nil
}

func validateBranchName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return errors.New("session: branch name must be 1..64 runes")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("session: branch name contains control characters")
		}
	}
	return nil
}

func branchNameExists(branches map[string]protocol.SessionBranch, name, except string) bool {
	for id, branch := range branches {
		if id != except && strings.EqualFold(branch.Name, name) {
			return true
		}
	}
	return false
}

func nextBranchName(branches map[string]protocol.SessionBranch) string {
	for n := 2; ; n++ {
		name := fmt.Sprintf("branch-%d", n)
		if !branchNameExists(branches, name, "") {
			return name
		}
	}
}

var (
	ErrNotFound = errors.New("session: not found")
	// ErrConflict reports an optimistic branch-tip race between store handles.
	// The failed transaction is rolled back; callers may reload/select and retry.
	ErrConflict = errors.New("session: branch tip conflict")
)

// ---------------------------------------------------------------------------
// ID generation
// ---------------------------------------------------------------------------

func newID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixMilli(), randomSuffix())
}

func randomSuffix() string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	seed := time.Now().UnixNano()
	for i := range b {
		seed = seed*6364136223846793005 + 1442695040888963407
		b[i] = letters[(seed>>33)&0x1f]
	}
	return string(b)
}

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

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore(opts Options) *MemoryStore {
	id := opts.ID
	if id == "" {
		id = newID()
	}
	now := time.Now().UnixMilli()
	h := Header{Version: SessionVersion, ID: id, CreatedAt: now, CWD: opts.CWD, Name: opts.Name}
	s := &MemoryStore{
		id:           id,
		header:       h,
		byID:         make(map[string]int),
		branches:     make(map[string]protocol.SessionBranch),
		threadModes:  map[string]protocol.CollaborationMode{"main": protocol.ModeDefault},
		threadGoals:  make(map[string]*protocol.ThreadGoal),
		goalDeferred: make(map[string]bool),
		subagents:    make(map[string]SubagentRecord),
		activeBranch: "main",
	}
	root := Entry{Type: EntryMeta, ID: "root", Key: "root", Value: id}
	s.entries = append(s.entries, root)
	s.byID["root"] = 0
	s.tip = "root"
	s.branches["main"] = protocol.SessionBranch{ID: "main", Name: "main", TipID: "root", CreatedAt: now, UpdatedAt: now, Active: true}
	return s
}

// ID implements Store.
func (s *MemoryStore) ID() string { return s.id }

// Path implements Store.
func (s *MemoryStore) Path() string { return "" }

// Header implements Store.
func (s *MemoryStore) Header() Header {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.header
}

// SessionTitle returns the current session-wide display title.
func (s *MemoryStore) SessionTitle() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", errors.New("session: store closed")
	}
	return s.header.Name, nil
}

// RenameSession changes the display title without moving the branch tip.
func (s *MemoryStore) RenameSession(title string) error {
	title, err := normalizeSessionTitle(title)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	s.header.Name = title
	return nil
}

// AppendWithInitialTitle atomically appends the first user message and assigns
// its generated title. Existing/manual titles and any prior message win.
func (s *MemoryStore) AppendWithInitialTitle(entry Entry, title string) error {
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
	if entry.ID == "" {
		entry.ID = newID()
	}
	if entry.ParentID == "" {
		entry.ParentID = s.tip
	}
	if _, ok := s.byID[entry.ID]; ok {
		return fmt.Errorf("session: duplicate entry id %q", entry.ID)
	}
	if _, ok := s.byID[entry.ParentID]; !ok {
		return fmt.Errorf("session: unknown parent %q", entry.ParentID)
	}
	if title != "" && s.header.Name == "" {
		hasMessage := false
		for _, existing := range s.entries {
			if existing.Type == EntryMessage {
				hasMessage = true
				break
			}
		}
		if !hasMessage {
			s.header.Name = title
		}
	}
	normalizeEntryMessage(&entry)
	s.entries = append(s.entries, entry)
	s.byID[entry.ID] = len(s.entries) - 1
	s.tip = entry.ID
	branch := s.branches[s.activeBranch]
	branch.TipID = entry.ID
	branch.UpdatedAt = time.Now().UnixMilli()
	s.branches[s.activeBranch] = branch
	return nil
}

// CollaborationMode returns the active branch mode.
func (s *MemoryStore) CollaborationMode() (protocol.CollaborationMode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return protocol.ModeDefault, errors.New("session: store closed")
	}
	mode := s.threadModes[s.activeBranch]
	if mode == "" {
		mode = protocol.ModeDefault
	}
	return mode, nil
}

// SetCollaborationMode persists the active branch mode without moving its tip.
func (s *MemoryStore) SetCollaborationMode(mode protocol.CollaborationMode) error {
	parsed, err := protocol.ParseCollaborationMode(string(mode))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	s.threadModes[s.activeBranch] = parsed
	return nil
}

func (s *MemoryStore) Goal() (*protocol.ThreadGoal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return s.threadGoals[s.activeBranch].Clone(), nil
}

func (s *MemoryStore) CreateGoal(goal protocol.ThreadGoal, replace bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if old := s.threadGoals[s.activeBranch]; old != nil && !old.Status.Terminal() && !replace {
		return errors.New("session: unfinished goal already exists")
	}
	goal.SessionID, goal.BranchID = s.id, s.activeBranch
	if err := goal.Validate(); err != nil {
		return err
	}
	s.threadGoals[s.activeBranch] = goal.Clone()
	s.goalDeferred[s.activeBranch] = false
	return nil
}

func (s *MemoryStore) ReplaceGoal(expected string, goal protocol.ThreadGoal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	current := s.threadGoals[s.activeBranch]
	if expected == "" {
		if current != nil {
			return errors.New("session: stale goal id")
		}
	} else if current == nil || current.GoalID != expected {
		return errors.New("session: stale goal id")
	}
	goal.SessionID, goal.BranchID = s.id, s.activeBranch
	if err := goal.Validate(); err != nil {
		return err
	}
	s.threadGoals[s.activeBranch] = goal.Clone()
	s.goalDeferred[s.activeBranch] = false
	return nil
}

func (s *MemoryStore) ReviseGoal(expected, nextGoalID, objective string) (*protocol.ThreadGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	g := s.threadGoals[s.activeBranch]
	if g == nil {
		return nil, ErrNotFound
	}
	if g.GoalID != expected {
		return nil, errors.New("session: stale goal id")
	}
	copy := g.Clone()
	copy.GoalID = nextGoalID
	copy.Objective = strings.TrimSpace(objective)
	copy.Status = protocol.GoalActive
	copy.UpdatedAt = time.Now().UnixMilli()
	if err := copy.Validate(); err != nil {
		return nil, err
	}
	s.threadGoals[s.activeBranch] = copy
	s.goalDeferred[s.activeBranch] = false
	return copy.Clone(), nil
}

func (s *MemoryStore) TransitionGoal(expected string, expectedStatus, nextStatus protocol.ThreadGoalStatus, clearDeferral bool) (*protocol.ThreadGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	g := s.threadGoals[s.activeBranch]
	if g == nil {
		return nil, ErrNotFound
	}
	if g.GoalID != expected || g.Status != expectedStatus {
		return nil, errors.New("session: stale goal state")
	}
	if _, err := protocol.ParseThreadGoalStatus(string(nextStatus)); err != nil {
		return nil, err
	}
	copy := g.Clone()
	copy.Status = nextStatus
	copy.UpdatedAt = time.Now().UnixMilli()
	if err := copy.Validate(); err != nil {
		return nil, err
	}
	s.threadGoals[s.activeBranch] = copy
	if clearDeferral {
		s.goalDeferred[s.activeBranch] = false
	}
	return copy.Clone(), nil
}

func (s *MemoryStore) UpdateGoal(expected string, objective *string, status *protocol.ThreadGoalStatus, budget *int64) (*protocol.ThreadGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.threadGoals[s.activeBranch]
	if g == nil {
		return nil, ErrNotFound
	}
	if g.GoalID != expected {
		return nil, errors.New("session: stale goal id")
	}
	copy := g.Clone()
	if objective != nil {
		copy.Objective = strings.TrimSpace(*objective)
	}
	if status != nil {
		parsed, err := protocol.ParseThreadGoalStatus(string(*status))
		if err != nil {
			return nil, err
		}
		copy.Status = parsed
	}
	if budget != nil {
		if *budget <= 0 {
			return nil, errors.New("session: goal budget must be positive")
		}
		v := *budget
		copy.TokenBudget = &v
	}
	copy.UpdatedAt = time.Now().UnixMilli()
	if err := copy.Validate(); err != nil {
		return nil, err
	}
	s.threadGoals[s.activeBranch] = copy
	return copy.Clone(), nil
}

func (s *MemoryStore) ClearGoal(expected string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.threadGoals[s.activeBranch]
	if g == nil {
		if expected != "" {
			return errors.New("session: stale goal id")
		}
		return nil
	}
	if expected == "" || g.GoalID != expected {
		return errors.New("session: stale goal id")
	}
	delete(s.threadGoals, s.activeBranch)
	delete(s.goalDeferred, s.activeBranch)
	return nil
}

func normalizedGoalCostDelta(cost *protocol.Cost) (*protocol.Cost, error) {
	if cost == nil {
		return nil, nil
	}
	copy := *cost
	copy.Currency = strings.ToUpper(strings.TrimSpace(copy.Currency))
	if copy.Currency == "" {
		copy.Currency = "USD"
	}
	for _, value := range []float64{copy.Input, copy.Output, copy.CacheRead, copy.CacheWrite, copy.Total} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, errors.New("session: estimated goal cost delta must be finite and non-negative")
		}
	}
	return &copy, nil
}

func addGoalCost(costs []protocol.Cost, delta *protocol.Cost) ([]protocol.Cost, error) {
	if delta == nil {
		return costs, nil
	}
	for i := range costs {
		if !strings.EqualFold(costs[i].Currency, delta.Currency) {
			continue
		}
		values := []*float64{&costs[i].Input, &costs[i].Output, &costs[i].CacheRead, &costs[i].CacheWrite, &costs[i].Total}
		deltas := []float64{delta.Input, delta.Output, delta.CacheRead, delta.CacheWrite, delta.Total}
		for j := range values {
			sum := *values[j] + deltas[j]
			if math.IsNaN(sum) || math.IsInf(sum, 0) {
				return nil, errors.New("session: estimated goal cost overflow")
			}
			*values[j] = sum
		}
		return costs, nil
	}
	return append(costs, *delta), nil
}

func (s *MemoryStore) AccountGoal(expected string, tokens, seconds int64, estimatedCostDelta *protocol.Cost) (*protocol.ThreadGoal, bool, error) {
	if tokens < 0 || seconds < 0 {
		return nil, false, errors.New("session: goal usage delta cannot be negative")
	}
	cost, err := normalizedGoalCostDelta(estimatedCostDelta)
	if err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.threadGoals[s.activeBranch]
	if g == nil || g.GoalID != expected {
		return g.Clone(), false, nil
	}
	if tokens > math.MaxInt64-g.TokensUsed || seconds > math.MaxInt64-g.SecondsUsed {
		return nil, false, errors.New("session: goal usage overflow")
	}
	copy := g.Clone()
	copy.TokensUsed += tokens
	copy.SecondsUsed += seconds
	copy.EstimatedCosts, err = addGoalCost(copy.EstimatedCosts, cost)
	if err != nil {
		return nil, false, err
	}
	copy.UpdatedAt = time.Now().UnixMilli()
	crossed := false
	if copy.Status == protocol.GoalActive && copy.TokenBudget != nil && copy.TokensUsed >= *copy.TokenBudget {
		copy.Status = protocol.GoalBudgetLimited
		crossed = true
	}
	s.threadGoals[s.activeBranch] = copy
	return copy.Clone(), crossed, nil
}

func (s *MemoryStore) GoalContinuationDeferred() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.goalDeferred[s.activeBranch], nil
}
func (s *MemoryStore) SetGoalContinuationDeferred(v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	s.goalDeferred[s.activeBranch] = v
	return nil
}

// Metadata returns the latest value for key in the session.
func (s *MemoryStore) Metadata(key string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", false, errors.New("session: store closed")
	}
	for i := len(s.entries) - 1; i >= 0; i-- {
		if entry := s.entries[i]; entry.Type == EntryMeta && entry.Key == key {
			return entry.Value, true, nil
		}
	}
	return "", false, nil
}

// SetMetadata appends a metadata entry to the active branch.
func (s *MemoryStore) SetMetadata(key, value string) error {
	return s.Append(Entry{Type: EntryMeta, Key: key, Value: value})
}

// Append implements Store.
func (s *MemoryStore) Append(entry Entry) error {
	entry = cloneEntry(entry)
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
	if _, ok := s.byID[entry.ID]; ok {
		return fmt.Errorf("session: duplicate entry id %q", entry.ID)
	}
	if _, ok := s.byID[entry.ParentID]; !ok {
		return fmt.Errorf("session: unknown parent %q", entry.ParentID)
	}
	normalizeEntryMessage(&entry)
	s.entries = append(s.entries, entry)
	s.byID[entry.ID] = len(s.entries) - 1
	s.tip = entry.ID
	branch := s.branches[s.activeBranch]
	branch.TipID = entry.ID
	branch.UpdatedAt = time.Now().UnixMilli()
	s.branches[s.activeBranch] = branch
	return nil
}

// AppendBatch atomically appends an ordered chain and advances the branch once.
func (s *MemoryStore) AppendBatch(batch []Entry) error {
	if len(batch) == 0 {
		return nil
	}
	batch = cloneEntries(batch)
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
		if _, ok := s.byID[batch[i].ID]; ok || seen[batch[i].ID] {
			return fmt.Errorf("session: duplicate entry id %q", batch[i].ID)
		}
		if i == 0 {
			if _, ok := s.byID[batch[i].ParentID]; !ok {
				return fmt.Errorf("session: unknown parent %q", batch[i].ParentID)
			}
		}
		normalizeEntryMessage(&batch[i])
		seen[batch[i].ID] = true
		parent = batch[i].ID
	}
	for _, entry := range batch {
		s.entries = append(s.entries, entry)
		s.byID[entry.ID] = len(s.entries) - 1
	}
	s.tip = parent
	branch := s.branches[s.activeBranch]
	branch.TipID = parent
	branch.UpdatedAt = time.Now().UnixMilli()
	s.branches[s.activeBranch] = branch
	return nil
}

// BranchTip implements Store.
func (s *MemoryStore) BranchTip() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tip
}

// SetBranchTip implements Store.
func (s *MemoryStore) SetBranchTip(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if _, ok := s.byID[id]; !ok {
		return ErrNotFound
	}
	s.tip = id
	branch := s.branches[s.activeBranch]
	branch.TipID = id
	branch.UpdatedAt = time.Now().UnixMilli()
	s.branches[s.activeBranch] = branch
	return nil
}

// Messages implements Store.
func (s *MemoryStore) Messages() ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return linearize(s.entries, s.byID, s.tip)
}

// ContextMessages implements ContextStore.
func (s *MemoryStore) ContextMessages() ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return contextMessagesFromEntries(pathFrom(s.entries, s.byID, s.tip)), nil
}

// BranchEntries implements BranchEntryStore.
func (s *MemoryStore) BranchEntries() ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return cloneEntries(pathFrom(s.entries, s.byID, s.tip)), nil
}

// Branches implements BranchStore.
func (s *MemoryStore) Branches() ([]protocol.SessionBranch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	out := make([]protocol.SessionBranch, 0, len(s.branches))
	for _, branch := range s.branches {
		path := pathFrom(s.entries, s.byID, branch.TipID)
		branch.Messages, branch.Preview = branchStats(path)
		branch.Active = branch.ID == s.activeBranch
		out = append(out, branch)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out, nil
}

// SelectBranch implements BranchStore.
func (s *MemoryStore) SelectBranch(branchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	branch, ok := s.branches[branchID]
	if !ok {
		return ErrNotFound
	}
	for id, current := range s.branches {
		current.Active = id == branchID
		s.branches[id] = current
	}
	s.activeBranch = branchID
	s.tip = branch.TipID
	return nil
}

// ForkBranch implements BranchStore. The new branch shares the same entry tree.
func (s *MemoryStore) ForkBranch(fromEntryID string) (protocol.SessionBranch, error) {
	return s.ForkBranchWithOptions(protocol.BranchForkOptions{FromEntryID: fromEntryID})
}

func (s *MemoryStore) ForkBranchWithOptions(opts protocol.BranchForkOptions) (protocol.SessionBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fromEntryID := opts.FromEntryID
	if s.closed {
		return protocol.SessionBranch{}, errors.New("session: store closed")
	}
	if _, ok := s.byID[fromEntryID]; !ok {
		return protocol.SessionBranch{}, ErrNotFound
	}
	sourceID := opts.SourceBranchID
	if sourceID == "" {
		sourceID = s.activeBranch
	}
	source, ok := s.branches[sourceID]
	if !ok {
		return protocol.SessionBranch{}, ErrNotFound
	}
	belongs := false
	for _, entry := range pathFrom(s.entries, s.byID, source.TipID) {
		if entry.ID == fromEntryID {
			belongs = true
			break
		}
	}
	if !belongs {
		return protocol.SessionBranch{}, errors.New("session: fork entry is not on source branch")
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = nextBranchName(s.branches)
	}
	if err := validateBranchName(name); err != nil {
		return protocol.SessionBranch{}, err
	}
	if branchNameExists(s.branches, name, "") {
		return protocol.SessionBranch{}, errors.New("session: branch name already exists")
	}
	now := time.Now().UnixMilli()
	branch := protocol.SessionBranch{ID: "branch-" + randomSuffix(), Name: name, ParentID: sourceID, ForkedFromID: fromEntryID, TipID: fromEntryID, CreatedAt: now, UpdatedAt: now, Active: true}
	for id, current := range s.branches {
		current.Active = false
		s.branches[id] = current
	}
	s.branches[branch.ID] = branch
	s.threadModes[branch.ID] = s.threadModes[sourceID]
	if goal := s.threadGoals[sourceID]; goal != nil {
		copy := goal.Clone()
		copy.BranchID = branch.ID
		copy.GoalID = newID()
		copy.UpdatedAt = now
		s.threadGoals[branch.ID] = copy
	}
	s.goalDeferred[branch.ID] = s.goalDeferred[sourceID]
	if s.threadModes[branch.ID] == "" {
		s.threadModes[branch.ID] = protocol.ModeDefault
	}
	s.activeBranch = branch.ID
	s.tip = fromEntryID
	path := pathFrom(s.entries, s.byID, fromEntryID)
	branch.Messages, branch.Preview = branchStats(path)
	return branch, nil
}

func (s *MemoryStore) RenameBranch(branchID, name string) (protocol.SessionBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return protocol.SessionBranch{}, errors.New("session: store closed")
	}
	branch, ok := s.branches[branchID]
	if !ok {
		return protocol.SessionBranch{}, ErrNotFound
	}
	name = strings.TrimSpace(name)
	if err := validateBranchName(name); err != nil {
		return protocol.SessionBranch{}, err
	}
	if branchNameExists(s.branches, name, branchID) {
		return protocol.SessionBranch{}, errors.New("session: branch name already exists")
	}
	branch.Name = name
	branch.UpdatedAt = time.Now().UnixMilli()
	s.branches[branchID] = branch
	return branch, nil
}

// DeleteBranch removes an inactive non-main leaf branch and its branch-scoped state.
func (s *MemoryStore) DeleteBranch(branchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if branchID == "" || branchID == "main" || branchID == s.activeBranch {
		return errors.New("session: cannot delete active or main branch")
	}
	if _, ok := s.branches[branchID]; !ok {
		return ErrNotFound
	}
	for _, branch := range s.branches {
		if branch.ParentID == branchID {
			return errors.New("session: cannot delete branch with children")
		}
	}
	if goal := s.threadGoals[branchID]; goal != nil && !goal.Status.Terminal() {
		return errors.New("session: cannot delete branch with nonterminal goal")
	}
	for _, record := range s.subagents {
		if record.ParentBranchID == branchID {
			return errors.New("session: cannot delete branch with durable subagents")
		}
	}
	delete(s.branches, branchID)
	delete(s.threadModes, branchID)
	delete(s.threadGoals, branchID)
	delete(s.goalDeferred, branchID)
	return nil
}

func (s *MemoryStore) DeleteBranchForRollback(branchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if branchID == "" || branchID == "main" || branchID == s.activeBranch {
		return errors.New("session: cannot roll back active or main branch")
	}
	if _, ok := s.branches[branchID]; !ok {
		return ErrNotFound
	}
	delete(s.branches, branchID)
	delete(s.threadModes, branchID)
	delete(s.threadGoals, branchID)
	delete(s.goalDeferred, branchID)
	return nil
}

// Fork implements Store.
func (s *MemoryStore) Fork(fromID string) (Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	if _, ok := s.byID[fromID]; !ok {
		return nil, ErrNotFound
	}
	n := NewMemoryStore(Options{ID: s.id + "-fork", CWD: s.header.CWD, Name: s.header.Name})
	path := pathFrom(s.entries, s.byID, fromID)
	for _, e := range path {
		if e.ID == "root" {
			continue
		}
		if err := n.Append(e); err != nil {
			return nil, err
		}
	}
	return n, nil
}

func (s *MemoryStore) ActiveBranchID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeBranch
}
func (s *MemoryStore) ListSubagents() ([]SubagentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SubagentRecord, 0, len(s.subagents))
	for _, rec := range s.subagents {
		rec.State = *rec.State.Clone()
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].State.CreatedAt == out[j].State.CreatedAt {
			return out[i].State.Agent.ThreadID < out[j].State.Agent.ThreadID
		}
		return out[i].State.CreatedAt < out[j].State.CreatedAt
	})
	return out, nil
}
func (s *MemoryStore) PutSubagent(rec SubagentRecord) error {
	if err := rec.State.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subagents[rec.State.Agent.ThreadID]; ok {
		return errors.New("session: duplicate subagent thread")
	}
	for _, existing := range s.subagents {
		if existing.State.Agent.Path == rec.State.Agent.Path {
			return errors.New("session: duplicate subagent path")
		}
	}
	rec.State = *rec.State.Clone()
	s.subagents[rec.State.Agent.ThreadID] = rec
	return nil
}
func (s *MemoryStore) CompareAndSwapSubagent(id string, expected uint64, next SubagentRecord) error {
	if err := next.State.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.subagents[id]
	if !ok {
		return ErrNotFound
	}
	if current.State.Generation != expected {
		return errors.New("session: stale subagent generation")
	}
	for otherID, existing := range s.subagents {
		if otherID != id && existing.State.Agent.Path == next.State.Agent.Path {
			return errors.New("session: duplicate subagent path")
		}
	}
	next.State = *next.State.Clone()
	s.subagents[id] = next
	return nil
}
func (s *MemoryStore) DeleteSubagent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subagents[id]; !ok {
		return ErrNotFound
	}
	delete(s.subagents, id)
	return nil
}

// Close implements Store.
func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// pathFrom walks parents from id to root and returns the ordered slice.
func pathFrom(entries []Entry, byID map[string]int, id string) []Entry {
	var rev []Entry
	cur := id
	seen := make(map[string]bool)
	for cur != "" && !seen[cur] {
		idx, ok := byID[cur]
		if !ok {
			break
		}
		rev = append(rev, entries[idx])
		seen[cur] = true
		cur = entries[idx].ParentID
	}
	out := make([]Entry, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return out
}

func branchStats(path []Entry) (count int, preview string) {
	for _, entry := range path {
		if entry.Type != EntryMessage || entry.Message == nil {
			continue
		}
		count++
		var text strings.Builder
		for _, block := range entry.Message.Content {
			if block.Type == protocol.BlockText {
				text.WriteString(block.Text)
			}
		}
		if text.Len() > 0 {
			preview = strings.Join(strings.Fields(text.String()), " ")
			if len([]rune(preview)) > 120 {
				preview = string([]rune(preview)[:119]) + "…"
			}
		}
	}
	return count, preview
}

// linearize returns messages along the root→tip path, in order.
func linearize(entries []Entry, byID map[string]int, tip string) ([]protocol.Message, error) {
	path := pathFrom(entries, byID, tip)
	var msgs []protocol.Message
	for _, e := range path {
		if e.Type == EntryMessage && e.Message != nil {
			msgs = append(msgs, e.Message.Clone())
		}
	}
	return msgs, nil
}

// contextMessagesFromEntries projects a branch after its latest compaction
// marker. History remains append-only; only the provider-facing projection
// replaces the compacted prefix with one harness summary message.
func contextMessagesFromEntries(entries []Entry) []protocol.Message {
	positions := make(map[string]int, len(entries))
	for i := range entries {
		positions[entries[i].ID] = i
	}
	lastCompaction := -1
	boundaryPos := -1
	for i, entry := range entries {
		if entry.Type != EntryCompaction || strings.TrimSpace(entry.Summary) == "" {
			continue
		}
		resolved := i - 1
		if pos, ok := positions[entry.CompactedThrough]; ok && pos < i {
			resolved = pos
		}
		// Prefer the marker that hides the greatest valid prefix. A newer marker
		// wins ties, while corrupt forward/unknown references are clamped at the
		// marker so they can never resurface an older prefix.
		if resolved > boundaryPos || (resolved == boundaryPos && i > lastCompaction) {
			lastCompaction = i
			boundaryPos = resolved
		}
	}

	start := 0
	var msgs []protocol.Message
	if lastCompaction >= 0 {
		entry := entries[lastCompaction]
		msgs = append(msgs, protocol.Message{
			ID:        "compaction-" + entry.ID,
			ParentID:  entry.ParentID,
			Role:      protocol.RoleCustom,
			Content:   []protocol.ContentBlock{protocol.NewTextBlock("Conversation summary:\n" + entry.Summary)},
			Timestamp: time.Now().UnixMilli(),
		})
		start = boundaryPos + 1
	}
	for _, entry := range entries[start:] {
		if entry.Type == EntryMessage && entry.Message != nil {
			msgs = append(msgs, entry.Message.Clone())
		}
	}
	return msgs
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

// NewJSONLStore creates a store backed by a file. If the file does not exist
// it is created with header metadata from opts.
func NewJSONLStore(path, cwd string, opts Options) (*JSONLStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("session: mkdir: %w", err)
	}
	id := opts.ID
	if id == "" {
		id = newID()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session: open: %w", err)
	}
	s := &JSONLStore{path: path, byID: make(map[string]int)}
	info, err := f.Stat()
	if err == nil && info.Size() == 0 {
		s.header = Header{Version: SessionVersion, ID: id, CreatedAt: time.Now().UnixMilli(), CWD: cwd, Name: opts.Name}
		line, err := json.Marshal(map[string]any{"header": s.header})
		if err != nil {
			f.Close()
			return nil, err
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			return nil, err
		}
		s.entries = append(s.entries, Entry{Type: EntryMeta, ID: "root", Key: "root", Value: id})
		s.byID["root"] = 0
		s.tip = "root"
	} else {
		if err := s.load(f); err != nil {
			f.Close()
			return nil, err
		}
	}
	s.f = f
	return s, nil
}

// load reads an existing file into memory.
func (s *JSONLStore) load(f *os.File) error {
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	first := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineNo++
		if first {
			var wrapper struct {
				Header *Header `json:"header"`
			}
			if err := json.Unmarshal([]byte(line), &wrapper); err != nil || wrapper.Header == nil {
				return fmt.Errorf("session: line 1 must be a header, got %q", line[:min(40, len(line))])
			}
			s.header = *wrapper.Header
			if s.header.Version != SessionVersion {
				return fmt.Errorf("session: unsupported version %d", s.header.Version)
			}
			s.entries = append(s.entries, Entry{Type: EntryMeta, ID: "root", Key: "root", Value: s.header.ID})
			s.byID["root"] = 0
			s.tip = "root"
			first = false
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return fmt.Errorf("session: corrupt line %d: %w", lineNo, err)
		}
		if e.ID == "" {
			return fmt.Errorf("session: entry on line %d has no id", lineNo)
		}
		if _, ok := s.byID[e.ID]; ok {
			return fmt.Errorf("session: duplicate id %q on line %d", e.ID, lineNo)
		}
		if _, ok := s.byID[e.ParentID]; !ok && e.ParentID != "" {
			return fmt.Errorf("session: orphan parent %q for %q", e.ParentID, e.ID)
		}
		normalizeEntryMessage(&e)
		s.entries = append(s.entries, e)
		s.byID[e.ID] = len(s.entries) - 1
		if e.ParentID == s.tip || (e.ParentID == "" && e.ID != "root") {
			s.tip = e.ID
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if first {
		return errors.New("session: missing header")
	}
	return nil
}

// ID implements Store.
func (s *JSONLStore) ID() string { return s.header.ID }

// Path implements Store.
func (s *JSONLStore) Path() string { return s.path }

// Header implements Store.
func (s *JSONLStore) Header() Header { return s.header }

// Metadata returns the latest value for key in the session.
func (s *JSONLStore) Metadata(key string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", false, errors.New("session: store closed")
	}
	for i := len(s.entries) - 1; i >= 0; i-- {
		if entry := s.entries[i]; entry.Type == EntryMeta && entry.Key == key {
			return entry.Value, true, nil
		}
	}
	return "", false, nil
}

// SetMetadata appends a metadata entry to the active branch.
func (s *JSONLStore) SetMetadata(key, value string) error {
	return s.Append(Entry{Type: EntryMeta, Key: key, Value: value})
}

// Append implements Store.
func (s *JSONLStore) Append(entry Entry) error {
	entry = cloneEntry(entry)
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
	if _, ok := s.byID[entry.ID]; ok {
		return fmt.Errorf("session: duplicate entry id %q", entry.ID)
	}
	if _, ok := s.byID[entry.ParentID]; !ok {
		return fmt.Errorf("session: unknown parent %q", entry.ParentID)
	}
	normalizeEntryMessage(&entry)
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := s.f.Write(append(line, '\n')); err != nil {
		return err
	}
	if err := s.f.Sync(); err != nil {
		return err
	}
	s.entries = append(s.entries, entry)
	s.byID[entry.ID] = len(s.entries) - 1
	s.tip = entry.ID
	return nil
}

// BranchTip implements Store.
func (s *JSONLStore) BranchTip() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tip
}

// SetBranchTip implements Store.
func (s *JSONLStore) SetBranchTip(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return ErrNotFound
	}
	s.tip = id
	return nil
}

// Messages implements Store.
func (s *JSONLStore) Messages() ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return linearize(s.entries, s.byID, s.tip)
}

// ContextMessages implements ContextStore for legacy fixtures.
func (s *JSONLStore) ContextMessages() ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return contextMessagesFromEntries(pathFrom(s.entries, s.byID, s.tip)), nil
}

// BranchEntries implements BranchEntryStore for legacy fixtures.
func (s *JSONLStore) BranchEntries() ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEntries(pathFrom(s.entries, s.byID, s.tip)), nil
}

// Fork implements Store. Returns an in-memory branch for now (JSONL fork
// writes are deferred to phase 4 tree navigation).
func (s *JSONLStore) Fork(fromID string) (Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.byID[fromID]; !ok {
		return nil, ErrNotFound
	}
	n := NewMemoryStore(Options{ID: s.header.ID + "-fork", CWD: s.header.CWD, Name: s.header.Name})
	path := pathFrom(s.entries, s.byID, fromID)
	for _, e := range path {
		if e.ID == "root" {
			continue
		}
		if err := n.Append(e); err != nil {
			return nil, err
		}
	}
	return n, nil
}

// Close implements Store.
func (s *JSONLStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.f.Close()
}

// ---------------------------------------------------------------------------
// Index
// ---------------------------------------------------------------------------

// FileIndex is the default disk-backed session index.
type FileIndex struct {
	Root string
}

// DefaultSessionsRoot returns ~/.snow/sessions (override via SNOW_SESSIONS_DIR).
func DefaultSessionsRoot() string {
	if d := os.Getenv("SNOW_SESSIONS_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".snow/sessions"
	}
	return filepath.Join(home, ".snow", "sessions")
}

// NewFileIndex creates an index rooted at the given directory.
func NewFileIndex(root string) *FileIndex {
	return &FileIndex{Root: root}
}

// SessionsRoot implements Index.
func (f *FileIndex) SessionsRoot() string { return f.Root }

// Create implements Index.
func (f *FileIndex) Create(cwd string) (Store, error) {
	dir := filepath.Join(f.Root, EncodeCWD(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("session: mkdir: %w", err)
	}
	name := fmt.Sprintf("%d_%s.db", time.Now().UnixMilli(), randomSuffix())
	return NewSQLiteStore(filepath.Join(dir, name), cwd, Options{})
}

// Open implements Index. It never creates a missing session path.
func (f *FileIndex) Open(path string) (Store, error) {
	return OpenSQLiteStore(path, "", Options{})
}

// Rename changes a listed project session's display title without changing its
// path or ID. The CWD/list membership check prevents arbitrary database paths
// from being mutated through picker input.
func (f *FileIndex) Rename(cwd, path, title string) error {
	infos, err := f.List(cwd)
	if err != nil {
		return err
	}
	allowed := false
	cleanPath, cleanErr := filepath.Abs(path)
	for _, info := range infos {
		listedPath, listedErr := filepath.Abs(info.Path)
		if cleanErr == nil && listedErr == nil && filepath.Clean(cleanPath) == filepath.Clean(listedPath) {
			allowed = true
			break
		}
	}
	if !allowed {
		return ErrNotFound
	}
	st, err := f.Open(path)
	if err != nil {
		return err
	}
	titles, ok := st.(TitleStore)
	if !ok {
		_ = st.Close()
		return errors.New("session: store does not support titles")
	}
	renameErr := titles.RenameSession(title)
	return errors.Join(renameErr, st.Close())
}

// List implements Index. Returns sessions sorted by most recently updated.
// It searches the collision-resistant directory and the legacy flattened
// directory, filtering the latter by its stored CWD so colliding projects can
// never see each other's sessions.
func (f *FileIndex) List(cwd string) ([]SessionInfo, error) {
	dirNames := []string{EncodeCWD(cwd), legacyEncodeCWD(cwd)}
	seenDirs := make(map[string]bool, len(dirNames))
	seenPaths := make(map[string]bool)
	var out []SessionInfo
	for _, dirName := range dirNames {
		dir := filepath.Join(f.Root, dirName)
		if seenDirs[dir] {
			continue
		}
		seenDirs[dir] = true
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if info.IsDir() {
				if strings.HasSuffix(path, ".db.agents") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".db") || seenPaths[path] {
				return nil
			}
			// Listing is read-only discovery: validate and inspect through a
			// query-only connection without journal changes or schema migration.
			sessionInfo, include, inspectErr := inspectSQLiteSession(path, cwd, info.ModTime().UnixMilli())
			if inspectErr != nil || !include {
				return nil // skip corrupt, partial, foreign, and root-only files
			}
			out = append(out, sessionInfo)
			seenPaths[path] = true
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// EncodeCWD returns a fixed-size, collision-resistant directory name for an
// absolute project path. The v2 prefix distinguishes it from legacy slugs.
func EncodeCWD(cwd string) string {
	cleaned := normalizeCWD(cwd)
	sum := sha256.Sum256([]byte(cleaned))
	return fmt.Sprintf("cwd-v2-%x", sum[:])
}

func legacyEncodeCWD(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	cleaned := filepath.Clean(abs)
	if cleaned == "." || cleaned == "" {
		cleaned, _ = os.Getwd()
	}
	return legacyEncodeCleanedCWD(cleaned)
}

// legacyEncodeCleanedCWD intentionally reproduces the original encoder byte
// for byte. In particular it removes only one leading hyphen and preserves
// trailing hyphens.
func legacyEncodeCleanedCWD(cleaned string) string {
	if cleaned == "/" {
		return "root"
	}
	enc := strings.ReplaceAll(cleaned, "/", "-")
	enc = strings.ReplaceAll(enc, ":", "-")
	enc = strings.TrimPrefix(enc, "-")
	if enc == "" {
		enc = "root"
	}
	return enc
}

func normalizeCWD(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	cleaned := filepath.Clean(abs)
	if cleaned == "." || cleaned == "" {
		if current, getwdErr := os.Getwd(); getwdErr == nil {
			cleaned = filepath.Clean(current)
		}
	}
	return cleaned
}

func sameCWD(left, right string) bool {
	return normalizeCWD(left) == normalizeCWD(right)
}
