// Package session implements durable conversation storage as append-only
// JSONL files with tree branching (id/parentId), plus an in-memory variant
// for tests and ephemeral SDK use.
package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
)

// SessionVersion is the current on-disk schema version.
const SessionVersion = 1

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
	Type     EntryType        `json:"type"`
	ID       string           `json:"id"`
	ParentID string           `json:"parent_id,omitempty"`
	Message  *protocol.Message `json:"message,omitempty"`
	// Compaction
	Summary string `json:"summary,omitempty"`
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
	// Messages returns messages linearized from the root to the branch tip.
	Messages() ([]protocol.Message, error)
	// Fork creates a new branch at fromID.
	Fork(fromID string) (Store, error)
	Close() error
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
	// Path is the JSONL file path. Empty means in-memory.
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

var (
	ErrNotFound  = errors.New("session: not found")
	ErrNoParents = errors.New("session: cannot resolve branch tip")
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
	mu       sync.RWMutex
	id       string
	header   Header
	entries  []Entry
	byID     map[string]int
	tip      string
	closed   bool
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
		id:     id,
		header: h,
		byID:   make(map[string]int),
	}
	root := Entry{Type: EntryMeta, ID: "root", Key: "root", Value: id}
	s.entries = append(s.entries, root)
	s.byID["root"] = 0
	s.tip = "root"
	return s
}

// ID implements Store.
func (s *MemoryStore) ID() string { return s.id }

// Path implements Store.
func (s *MemoryStore) Path() string { return "" }

// Header implements Store.
func (s *MemoryStore) Header() Header { return s.header }

// Append implements Store.
func (s *MemoryStore) Append(entry Entry) error {
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
	s.entries = append(s.entries, entry)
	s.byID[entry.ID] = len(s.entries) - 1
	s.tip = entry.ID
	s.header = maybeTouch(s.header)
	return nil
}

func maybeTouch(h Header) Header {
	return h
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
	if _, ok := s.byID[id]; !ok {
		return ErrNotFound
	}
	s.tip = id
	return nil
}

// Messages implements Store.
func (s *MemoryStore) Messages() ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return linearize(s.entries, s.byID, s.tip)
}

// Fork implements Store.
func (s *MemoryStore) Fork(fromID string) (Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
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

// linearize returns messages along the root→tip path, in order.
func linearize(entries []Entry, byID map[string]int, tip string) ([]protocol.Message, error) {
	path := pathFrom(entries, byID, tip)
	var msgs []protocol.Message
	for _, e := range path {
		if e.Type == EntryMessage && e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	return msgs, nil
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
		s.entries = append(s.entries, e)
		s.byID[e.ID] = len(s.entries) - 1
		if e.ParentID == s.tip || (e.ParentID == "" && e.ID != "root") {
			s.tip = e.ID
		}
	}
	return scanner.Err()
}

// ID implements Store.
func (s *JSONLStore) ID() string { return s.header.ID }

// Path implements Store.
func (s *JSONLStore) Path() string { return s.path }

// Header implements Store.
func (s *JSONLStore) Header() Header { return s.header }

// Append implements Store.
func (s *JSONLStore) Append(entry Entry) error {
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
	name := fmt.Sprintf("%d_%s.jsonl", time.Now().UnixMilli(), randomSuffix())
	return NewJSONLStore(filepath.Join(dir, name), cwd, Options{})
}

// Open implements Index.
func (f *FileIndex) Open(path string) (Store, error) {
	return NewJSONLStore(path, "", Options{})
}

// List implements Index. Returns sessions sorted by most recently updated.
func (f *FileIndex) List(cwd string) ([]SessionInfo, error) {
	dir := filepath.Join(f.Root, EncodeCWD(cwd))
	var out []SessionInfo
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		st, err := NewJSONLStore(path, cwd, Options{})
		if err != nil {
			return nil // skip corrupt/partial files
		}
		msgs, _ := st.Messages()
		last := info.ModTime().UnixMilli()
		if len(msgs) > 0 {
			if t := msgs[len(msgs)-1].Timestamp; t > 0 {
				last = t
			}
		}
		out = append(out, SessionInfo{
			Path:      path,
			ID:        st.Header().ID,
			CWD:       st.Header().CWD,
			Name:      st.Header().Name,
			CreatedAt: st.Header().CreatedAt,
			UpdatedAt: last,
			Messages:  len(msgs),
		})
		st.Close()
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// EncodeCWD encodes an absolute path into a directory name (pi-like).
func EncodeCWD(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	cleaned := filepath.Clean(abs)
	if cleaned == "." || cleaned == "" {
		cleaned, _ = os.Getwd()
	}
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
