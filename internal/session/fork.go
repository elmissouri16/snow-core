package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ErrInvalidForkBoundary reports a snapshot that would separate a tool call
// from its result or otherwise preserve an incomplete assistant turn.
var ErrInvalidForkBoundary = errors.New("session: invalid fork boundary")

// ErrDestinationExists reports a requested destination that Snow will not
// overwrite.
var ErrDestinationExists = errors.New("session: fork destination already exists")

type forkSnapshot struct {
	entries         []Entry
	sourceSessionID string
	sourceBranchID  string
	sourceEntryID   string
	name            string
	mode            protocol.CollaborationMode
	copyMode        bool
}

type forkSnapshotSource interface {
	forkSnapshot(protocol.SessionForkOptions) (forkSnapshot, error)
}

// CreateFork materializes an independent SQLite session from one immutable
// root-to-entry chain. The source is never mutated. The returned store owns the
// newly created database and must be closed by the caller.
func (f *FileIndex) CreateFork(cwd string, source Store, opts protocol.SessionForkOptions) (Store, protocol.SessionForkResult, error) {
	if source == nil {
		return nil, protocol.SessionForkResult{}, errors.New("session: fork source is nil")
	}
	snapshots, ok := source.(forkSnapshotSource)
	if !ok {
		return nil, protocol.SessionForkResult{}, errors.New("session: store does not support durable session forks")
	}
	snapshot, err := snapshots.forkSnapshot(opts)
	if err != nil {
		return nil, protocol.SessionForkResult{}, err
	}
	if err := ValidateForkBoundary(snapshot.entries); err != nil {
		return nil, protocol.SessionForkResult{}, err
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = strings.TrimSpace(snapshot.name)
	}
	if name == "" {
		short := snapshot.sourceSessionID
		if len(short) > 8 {
			short = short[:8]
		}
		name = "Fork " + short
	}
	if _, err := normalizeSessionTitle(name); err != nil {
		return nil, protocol.SessionForkResult{}, err
	}

	path, err := f.forkDestination(cwd, opts.DestinationPath)
	if err != nil {
		return nil, protocol.SessionForkResult{}, err
	}
	tmpPath := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-"+randomSuffix())
	cleanup := func(target string) {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal", ".lock"} {
			_ = os.Remove(target + suffix)
		}
	}
	cleanup(tmpPath)

	target, err := NewSQLiteStore(tmpPath, cwd, Options{
		Name:            name,
		ParentSessionID: snapshot.sourceSessionID,
		ParentBranchID:  snapshot.sourceBranchID,
		ForkEntryID:     snapshot.sourceEntryID,
	})
	if err != nil {
		cleanup(tmpPath)
		return nil, protocol.SessionForkResult{}, fmt.Errorf("session: create fork: %w", err)
	}
	entries := cloneEntries(snapshot.entries)
	if len(entries) > 0 && entries[0].ID == "root" {
		entries = entries[1:]
	}
	if len(entries) > 0 {
		if err := target.AppendBatch(entries); err != nil {
			_ = target.Close()
			cleanup(tmpPath)
			return nil, protocol.SessionForkResult{}, fmt.Errorf("session: populate fork: %w", err)
		}
	}
	if snapshot.copyMode {
		if err := target.SetCollaborationMode(snapshot.mode); err != nil {
			_ = target.Close()
			cleanup(tmpPath)
			return nil, protocol.SessionForkResult{}, fmt.Errorf("session: copy fork mode: %w", err)
		}
	}
	if err := target.Close(); err != nil {
		cleanup(tmpPath)
		return nil, protocol.SessionForkResult{}, fmt.Errorf("session: close fork snapshot: %w", err)
	}
	// A same-directory hard link publishes the complete database atomically and,
	// unlike os.Rename on Unix, fails rather than replacing a destination created
	// concurrently after the Lstat check.
	if err := os.Link(tmpPath, path); err != nil {
		cleanup(tmpPath)
		if errors.Is(err, os.ErrExist) {
			return nil, protocol.SessionForkResult{}, ErrDestinationExists
		}
		return nil, protocol.SessionForkResult{}, fmt.Errorf("session: publish fork: %w", err)
	}
	if err := os.Remove(tmpPath); err != nil {
		cleanup(path)
		cleanup(tmpPath)
		return nil, protocol.SessionForkResult{}, fmt.Errorf("session: remove fork staging file: %w", err)
	}
	if err := os.Remove(tmpPath + ".lock"); err != nil && !os.IsNotExist(err) {
		cleanup(path)
		cleanup(tmpPath)
		return nil, protocol.SessionForkResult{}, fmt.Errorf("session: remove fork staging lock: %w", err)
	}

	opened, err := OpenSQLiteStore(path, cwd, Options{})
	if err != nil {
		cleanup(path)
		return nil, protocol.SessionForkResult{}, fmt.Errorf("session: validate fork: %w", err)
	}
	branches, err := opened.Branches()
	if err != nil || len(branches) != 1 {
		_ = opened.Close()
		cleanup(path)
		if err == nil {
			err = errors.New("session: fork destination has invalid branch topology")
		}
		return nil, protocol.SessionForkResult{}, err
	}
	header := opened.Header()
	result := protocol.SessionForkResult{
		SourceSessionID: snapshot.sourceSessionID,
		SourceBranchID:  snapshot.sourceBranchID,
		SourceEntryID:   snapshot.sourceEntryID,
		SessionID:       header.ID,
		SessionPath:     path,
		CWD:             header.CWD,
		Name:            header.Name,
		Branch:          branches[0],
	}
	return opened, result, nil
}

func (f *FileIndex) forkDestination(cwd, requested string) (string, error) {
	var path string
	if requested != "" {
		abs, err := filepath.Abs(requested)
		if err != nil {
			return "", fmt.Errorf("session: resolve fork destination: %w", err)
		}
		path = filepath.Clean(abs)
	} else {
		dir := filepath.Join(f.Root, EncodeCWD(cwd))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("session: mkdir fork destination: %w", err)
		}
		path = filepath.Join(dir, fmt.Sprintf("%d_%s.db", nowUnixMilli(), randomSuffix()))
	}
	if filepath.Ext(path) != ".db" {
		return "", errors.New("session: fork destination must end in .db")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("session: mkdir fork destination: %w", err)
	}
	if _, err := os.Lstat(path); err == nil {
		return "", ErrDestinationExists
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("session: inspect fork destination: %w", err)
	}
	return path, nil
}

func nowUnixMilli() int64 { return timeNow().UnixMilli() }

var retainedArtifactPattern = regexp.MustCompile(`Full retained tool result:\s*(artifact-[a-f0-9]{32})`)

// ForkArtifactIDs returns artifact references retained in the active copied
// branch. It recognizes Snow's exact private-spill marker rather than arbitrary
// user text containing an artifact-shaped token.
func ForkArtifactIDs(store Store) ([]string, error) {
	entries, ok := store.(BranchEntryStore)
	if !ok {
		return nil, errors.New("session: store does not expose fork entries")
	}
	branch, err := entries.BranchEntries()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var ids []string
	collect := func(value string) {
		for _, match := range retainedArtifactPattern.FindAllStringSubmatch(value, -1) {
			if len(match) != 2 {
				continue
			}
			if _, exists := seen[match[1]]; exists {
				continue
			}
			seen[match[1]] = struct{}{}
			ids = append(ids, match[1])
		}
	}
	// Scan newest-first so repeated carried references are attributed to their
	// latest trusted marker. Reverse once at the end for stable chronological
	// copy order; callers may then take a bounded newest suffix.
	for i := len(branch) - 1; i >= 0; i-- {
		entry := branch[i]
		collect(entry.Summary)
		collect(entry.Value)
		if entry.Message == nil {
			continue
		}
		collect(entry.Message.Error)
		for _, block := range entry.Message.Content {
			collect(block.Text)
		}
	}
	for left, right := 0, len(ids)-1; left < right; left, right = left+1, right-1 {
		ids[left], ids[right] = ids[right], ids[left]
	}
	return ids, nil
}

// ValidateForkBoundary rejects a copied prefix with unresolved tool calls or
// an explicitly partial assistant message. Complete tool batches and ordinary
// user/assistant boundaries remain valid provider continuation points.
func ValidateForkBoundary(entries []Entry) error {
	pending := make(map[string]struct{})
	for _, entry := range entries {
		if entry.Message == nil {
			continue
		}
		message := entry.Message
		if message.Role == protocol.RoleAssistant {
			if message.StopReason == protocol.StopPending {
				return fmt.Errorf("%w: assistant response is incomplete", ErrInvalidForkBoundary)
			}
			for _, block := range message.Content {
				if block.Type != protocol.BlockToolCall {
					continue
				}
				if block.ToolCallID == "" {
					return fmt.Errorf("%w: tool call has no id", ErrInvalidForkBoundary)
				}
				pending[block.ToolCallID] = struct{}{}
			}
		}
		if message.Role == protocol.RoleTool && message.ToolCallID != "" {
			delete(pending, message.ToolCallID)
		}
	}
	if len(pending) != 0 {
		return fmt.Errorf("%w: unresolved tool calls", ErrInvalidForkBoundary)
	}
	return nil
}

func (s *MemoryStore) forkSnapshot(opts protocol.SessionForkOptions) (forkSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return forkSnapshot{}, errors.New("session: store closed")
	}
	sourceID := opts.SourceBranchID
	if sourceID == "" {
		sourceID = s.activeBranch
	}
	branch, ok := s.branches[sourceID]
	if !ok {
		return forkSnapshot{}, ErrNotFound
	}
	fromID := opts.FromEntryID
	if fromID == "" {
		fromID = branch.TipID
	}
	belongs := false
	for _, entry := range pathFrom(s.entries, s.byID, branch.TipID) {
		if entry.ID == fromID {
			belongs = true
			break
		}
	}
	if !belongs {
		return forkSnapshot{}, errors.New("session: fork entry is not on source branch")
	}
	mode := protocol.ModeDefault
	copyMode := fromID == branch.TipID
	if copyMode && s.threadModes[sourceID] != "" {
		mode = s.threadModes[sourceID]
	}
	return forkSnapshot{
		entries:         cloneEntries(pathFrom(s.entries, s.byID, fromID)),
		sourceSessionID: s.id,
		sourceBranchID:  sourceID,
		sourceEntryID:   fromID,
		name:            s.header.Name,
		mode:            mode,
		copyMode:        copyMode,
	}, nil
}

func (s *SQLiteStore) forkSnapshot(opts protocol.SessionForkOptions) (forkSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return forkSnapshot{}, errors.New("session: store closed")
	}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return forkSnapshot{}, fmt.Errorf("session: fork snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	sourceID := opts.SourceBranchID
	if sourceID == "" {
		sourceID = s.branchID
	}
	var tip string
	if err := tx.QueryRow(`SELECT tip_id FROM session_branches WHERE branch_id=?`, sourceID).Scan(&tip); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return forkSnapshot{}, ErrNotFound
		}
		return forkSnapshot{}, err
	}
	fromID := opts.FromEntryID
	if fromID == "" {
		fromID = tip
	}
	var belongs int
	if err := tx.QueryRow(`WITH RECURSIVE branch(id, parent_id) AS (
		SELECT id, parent_id FROM entries WHERE id=?
		UNION ALL
		SELECT e.id, e.parent_id FROM entries e JOIN branch b ON e.id=b.parent_id
	) SELECT count(*) FROM branch WHERE id=?`, tip, fromID).Scan(&belongs); err != nil {
		return forkSnapshot{}, err
	}
	if belongs == 0 {
		return forkSnapshot{}, errors.New("session: fork entry is not on source branch")
	}
	entries, err := branchEntriesFrom(tx, fromID)
	if err != nil {
		return forkSnapshot{}, err
	}
	mode := protocol.ModeDefault
	copyMode := fromID == tip
	if copyMode {
		var raw string
		err := tx.QueryRow(`SELECT collaboration_mode FROM thread_state WHERE branch_id=?`, sourceID).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return forkSnapshot{}, err
		}
		if raw != "" {
			if parsed, parseErr := protocol.ParseCollaborationMode(raw); parseErr == nil {
				mode = parsed
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return forkSnapshot{}, err
	}
	return forkSnapshot{
		entries:         cloneEntries(entries),
		sourceSessionID: s.header.ID,
		sourceBranchID:  sourceID,
		sourceEntryID:   fromID,
		name:            s.header.Name,
		mode:            mode,
		copyMode:        copyMode,
	}, nil
}
