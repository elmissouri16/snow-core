package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
	_ "modernc.org/sqlite"
)

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
	s.advanceContextCacheLocked(expectedTip, batch)
	return nil
}

func (s *SQLiteStore) refreshBranchTipLocked() {
	var tip string
	if err := s.db.QueryRow(`SELECT tip_id FROM session_branches WHERE branch_id=?`, s.branchID).Scan(&tip); err == nil {
		s.tip = tip
		s.invalidateContextCacheLocked()
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
	s.invalidateContextCacheLocked()
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
// Messages while hiding entries before the latest compaction marker. Decoded
// entries are cached by the immutable active branch/tip chain; returned
// messages remain defensive clones owned by the caller.
func (s *SQLiteStore) ContextMessages() ([]protocol.Message, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, errors.New("session: store closed")
	}
	key := contextCacheKey{branchID: s.branchID, tip: s.tip}
	if len(s.contextCacheEntries) != 0 && s.contextCacheKey == key {
		messages := contextMessagesFromEntries(s.contextCacheEntries)
		s.mu.RUnlock()
		return messages, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	key = contextCacheKey{branchID: s.branchID, tip: s.tip}
	if len(s.contextCacheEntries) == 0 || s.contextCacheKey != key {
		entries, err := s.contextBranchEntries(s.tip)
		if err != nil {
			return nil, err
		}
		s.contextCacheKey = key
		s.contextCacheEntries = entries
	}
	return contextMessagesFromEntries(s.contextCacheEntries), nil
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
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("session: sqlite branches snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Query(`
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
	stats, err := branchStatsAllFrom(tx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Messages = stats[out[i].ID].messages
		out[i].Preview = stats[out[i].ID].preview
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("session: sqlite branches snapshot commit: %w", err)
	}
	return out, nil
}

type branchStatsValue struct {
	messages int
	preview  string
}

func branchStatsAllFrom(q sqliteQueryer) (map[string]branchStatsValue, error) {
	rows, err := q.Query(`WITH RECURSIVE branch(branch_id, seq, id, parent_id, entry_type, message) AS (
		SELECT sb.branch_id, e.seq, e.id, e.parent_id, e.entry_type, e.message
		FROM session_branches sb JOIN entries e ON e.id=sb.tip_id
		UNION ALL
		SELECT b.branch_id, e.seq, e.id, e.parent_id, e.entry_type, e.message
		FROM branch b JOIN entries e ON e.id=b.parent_id
	) SELECT branch_id, message FROM branch WHERE entry_type=? ORDER BY branch_id, seq DESC`, EntryMessage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := make(map[string]branchStatsValue)
	for rows.Next() {
		var branchID string
		var raw []byte
		if err := rows.Scan(&branchID, &raw); err != nil {
			return nil, err
		}
		stat := stats[branchID]
		stat.messages++
		if stat.preview == "" {
			var message protocol.Message
			if err := json.Unmarshal(raw, &message); err != nil {
				return nil, err
			}
			var text strings.Builder
			for _, block := range message.Content {
				if block.Type == protocol.BlockText {
					text.WriteString(block.Text)
				}
			}
			if text.Len() > 0 {
				preview := strings.Join(strings.Fields(text.String()), " ")
				runes := []rune(preview)
				if len(runes) > 120 {
					preview = string(runes[:119]) + "…"
				}
				stat.preview = preview
			}
		}
		stats[branchID] = stat
	}
	return stats, rows.Err()
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
	s.invalidateContextCacheLocked()
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
	if fromEntryID == "" {
		if err := tx.QueryRow(`SELECT tip_id FROM session_branches WHERE branch_id=?`, sourceID).Scan(&fromEntryID); err != nil {
			return protocol.SessionBranch{}, err
		}
	}
	var exists int
	if err := tx.QueryRow(`SELECT count(*) FROM entries WHERE id=?`, fromEntryID).Scan(&exists); err != nil {
		return protocol.SessionBranch{}, fmt.Errorf("session: sqlite fork lookup: %w", err)
	}
	if exists == 0 {
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
	entries, err := branchEntriesFrom(tx, fromEntryID)
	if err != nil {
		return protocol.SessionBranch{}, err
	}
	messages, preview := branchStats(entries)
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
	s.invalidateContextCacheLocked()
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
	if _, err := tx.Exec(`DELETE FROM subagent_threads WHERE parent_branch_id=?`, branchID); err != nil {
		return err
	}
	for _, table := range []string{"thread_goal_deferrals", "thread_goals", "thread_state", "session_branches"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE branch_id=?`, branchID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) contextBranchEntries(tip string) ([]Entry, error) {
	var markerID, boundaryID string
	err := s.db.QueryRow(`
		WITH RECURSIVE ancestry(id, parent_id, entry_type, summary, compacted_through, depth) AS (
			SELECT id, parent_id, entry_type, summary, compacted_through, 0
			FROM entries WHERE id = ?
			UNION ALL
			SELECT e.id, e.parent_id, e.entry_type, e.summary, e.compacted_through, a.depth + 1
			FROM entries e JOIN ancestry a ON e.id = a.parent_id
			WHERE NOT (a.entry_type = ? AND trim(a.summary) <> '')
		)
		SELECT id, compacted_through FROM ancestry
		WHERE entry_type = ? AND trim(summary) <> ''
		ORDER BY depth ASC LIMIT 1`, tip, EntryCompaction, EntryCompaction).Scan(&markerID, &boundaryID)
	if errors.Is(err, sql.ErrNoRows) {
		return s.branchEntries(tip)
	}
	if err != nil {
		return nil, fmt.Errorf("session: sqlite find context boundary: %w", err)
	}
	stopID, excludeID := boundaryID, boundaryID
	if stopID != "" {
		var onBranch int
		lineageErr := s.db.QueryRow(`WITH RECURSIVE lineage(id, parent_id) AS (
			SELECT id, parent_id FROM entries WHERE id=(SELECT parent_id FROM entries WHERE id=?)
			UNION ALL
			SELECT e.id, e.parent_id FROM entries e JOIN lineage l ON e.id=l.parent_id
		) SELECT count(*) FROM lineage WHERE id=?`, markerID, stopID).Scan(&onBranch)
		if lineageErr != nil {
			return nil, fmt.Errorf("session: sqlite validate context boundary: %w", lineageErr)
		}
		if onBranch == 0 {
			stopID, excludeID = markerID, ""
		}
	}
	if stopID == "" {
		stopID, excludeID = markerID, ""
	}
	rows, err := s.db.Query(`
		WITH RECURSIVE branch(seq, id, parent_id, entry_type, message, summary, compacted_through, meta_key, meta_value) AS (
			SELECT seq, id, parent_id, entry_type, message, summary, compacted_through, meta_key, meta_value
			FROM entries WHERE id = ?
			UNION ALL
			SELECT e.seq, e.id, e.parent_id, e.entry_type, e.message, e.summary, e.compacted_through, e.meta_key, e.meta_value
			FROM entries e JOIN branch b ON e.id = b.parent_id
			WHERE b.id <> ?
		)
		SELECT id, parent_id, entry_type,
			CASE WHEN id = ? THEN NULL ELSE message END,
			summary, compacted_through, meta_key, meta_value
		FROM branch ORDER BY seq`, tip, stopID, excludeID)
	if err != nil {
		return nil, fmt.Errorf("session: sqlite context branch: %w", err)
	}
	return scanBranchEntries(rows)
}

func (s *SQLiteStore) branchEntries(tip string) ([]Entry, error) {
	return branchEntriesFrom(s.db, tip)
}

func branchEntriesFrom(q sqliteQueryer, tip string) ([]Entry, error) {
	rows, err := q.Query(`
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
	return scanBranchEntries(rows)
}

func scanBranchEntries(rows *sql.Rows) ([]Entry, error) {
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
