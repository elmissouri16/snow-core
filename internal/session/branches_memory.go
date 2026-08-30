package session

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *MemoryStore) ForkBranchWithOptions(opts protocol.BranchForkOptions) (protocol.SessionBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fromEntryID := opts.FromEntryID
	if s.closed {
		return protocol.SessionBranch{}, errors.New("session: store closed")
	}
	sourceID := opts.SourceBranchID
	if sourceID == "" {
		sourceID = s.activeBranch
	}
	source, ok := s.branches[sourceID]
	if !ok {
		return protocol.SessionBranch{}, ErrNotFound
	}
	if fromEntryID == "" {
		fromEntryID = source.TipID
	}
	if _, ok := s.byID[fromEntryID]; !ok {
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
	slices.SortFunc(out, func(a, b SubagentRecord) int {
		if byCreated := cmp.Compare(a.State.CreatedAt, b.State.CreatedAt); byCreated != 0 {
			return byCreated
		}
		return cmp.Compare(a.State.Agent.ThreadID, b.State.Agent.ThreadID)
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

// Delete permanently removes a listed project session and returns every root
// and child session ID whose managed private state can also be removed.
func (f *FileIndex) Delete(cwd, path, expectedID string) error {
	_, err := f.DeleteWithIDs(cwd, path, expectedID)
	return err
}
