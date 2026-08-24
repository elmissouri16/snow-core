package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
	_ "modernc.org/sqlite"
)

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
	if err := insertHydrationProjection(tx, entry); err != nil {
		return err
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
	s.advanceContextCacheLocked(expectedTip, []Entry{entry})
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
	err := row.Scan(&g.GoalID, &g.Objective, &g.Status, &g.BlockedReason, &budget, &g.TokensUsed, &g.SecondsUsed, &g.CreatedAt, &g.UpdatedAt)
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
	g, err := scanGoalWithCosts(tx.QueryRow(`SELECT goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), tx, s.header.ID, s.branchID)
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
	query := `INSERT INTO thread_goals(branch_id, goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(branch_id) DO UPDATE SET goal_id=excluded.goal_id, objective=excluded.objective, status=excluded.status,
			blocked_reason=excluded.blocked_reason, token_budget=excluded.token_budget, tokens_used=excluded.tokens_used, seconds_used=excluded.seconds_used,
			created_at=excluded.created_at, updated_at=excluded.updated_at`
	args := []any{s.branchID, goal.GoalID, strings.TrimSpace(goal.Objective), goal.Status, goal.BlockedReason, goal.TokenBudget, goal.TokensUsed, goal.SecondsUsed, goal.CreatedAt, goal.UpdatedAt}
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
		res, err = tx.Exec(`INSERT INTO thread_goals(branch_id, goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(branch_id) DO NOTHING`, s.branchID, goal.GoalID, strings.TrimSpace(goal.Objective), goal.Status, goal.BlockedReason, goal.TokenBudget, goal.TokensUsed, goal.SecondsUsed, goal.CreatedAt, goal.UpdatedAt)
	} else {
		res, err = tx.Exec(`UPDATE thread_goals SET goal_id=?, objective=?, status=?, blocked_reason=?, token_budget=?, tokens_used=?, seconds_used=?, created_at=?, updated_at=?
			WHERE branch_id=? AND goal_id=?`, goal.GoalID, strings.TrimSpace(goal.Objective), goal.Status, goal.BlockedReason, goal.TokenBudget, goal.TokensUsed, goal.SecondsUsed, goal.CreatedAt, goal.UpdatedAt, s.branchID, expected)
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
	g, err := scanGoalWithCosts(tx.QueryRow(`UPDATE thread_goals SET goal_id=?, objective=?, status=?, blocked_reason='', updated_at=?
		WHERE branch_id=? AND goal_id=?
		RETURNING goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at`, nextGoalID, objective, protocol.GoalActive, now, s.branchID, expected), tx, s.header.ID, s.branchID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		current, readErr := scanGoalWithCosts(tx.QueryRow(`SELECT goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), tx, s.header.ID, s.branchID)
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

func (s *SQLiteStore) TransitionGoal(expected string, expectedStatus, nextStatus protocol.ThreadGoalStatus, blockedReason string, clearDeferral bool) (*protocol.ThreadGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := protocol.ParseThreadGoalStatus(string(nextStatus)); err != nil {
		return nil, err
	}
	blockedReason = strings.TrimSpace(blockedReason)
	if nextStatus == protocol.GoalBlocked && blockedReason == "" {
		return nil, errors.New("session: blocked status requires a reason")
	}
	if nextStatus != protocol.GoalBlocked {
		blockedReason = ""
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("session: sqlite transition goal begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()
	g, err := scanGoalWithCosts(tx.QueryRow(`UPDATE thread_goals SET status=?, blocked_reason=?, updated_at=?
		WHERE branch_id=? AND goal_id=? AND status=?
		RETURNING goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at`, nextStatus, blockedReason, now, s.branchID, expected, expectedStatus), tx, s.header.ID, s.branchID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		current, readErr := scanGoalWithCosts(tx.QueryRow(`SELECT goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), tx, s.header.ID, s.branchID)
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
		if *status == protocol.GoalBlocked {
			return nil, errors.New("session: blocked status requires reason-bearing transition")
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
		blocked_reason = CASE WHEN ? = 1 THEN '' ELSE blocked_reason END,
		token_budget = CASE WHEN ? = 1 THEN ? ELSE token_budget END,
		updated_at = ?
		WHERE branch_id = ? AND goal_id = ?
		RETURNING goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at`,
		objectiveSet, objectiveValue, statusSet, statusValue, statusSet, budgetSet, budgetValue, now, s.branchID, expected), tx, s.header.ID, s.branchID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		current, readErr := scanGoalWithCosts(tx.QueryRow(`SELECT goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), tx, s.header.ID, s.branchID)
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
		RETURNING goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at`,
		tokens, seconds, protocol.GoalActive, tokens, protocol.GoalBudgetLimited, now,
		s.branchID, expected, math.MaxInt64-tokens, math.MaxInt64-seconds), s.header.ID, s.branchID)
	if err != nil {
		return nil, false, err
	}
	if g == nil {
		current, readErr := scanGoalWithCosts(tx.QueryRow(`SELECT goal_id, objective, status, blocked_reason, token_budget, tokens_used, seconds_used, created_at, updated_at FROM thread_goals WHERE branch_id = ?`, s.branchID), tx, s.header.ID, s.branchID)
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

func (s *SQLiteStore) invalidateContextCacheLocked() {
	s.contextCacheKey = contextCacheKey{}
	s.contextCacheEntries = nil
}

// advanceContextCacheLocked extends a warm decoded chain after a successful
// append. Compaction markers intentionally force one fresh boundary query;
// ordinary message/meta appends stay on the allocation-only cache-hit path.
func (s *SQLiteStore) advanceContextCacheLocked(expectedTip string, entries []Entry) {
	if len(s.contextCacheEntries) == 0 || s.contextCacheKey != (contextCacheKey{branchID: s.branchID, tip: expectedTip}) {
		s.invalidateContextCacheLocked()
		return
	}
	parent := expectedTip
	for i := range entries {
		if entries[i].ParentID != parent || (entries[i].Type == EntryCompaction && strings.TrimSpace(entries[i].Summary) != "") {
			s.invalidateContextCacheLocked()
			return
		}
		s.contextCacheEntries = append(s.contextCacheEntries, cloneEntry(entries[i]))
		parent = entries[i].ID
	}
	s.contextCacheKey.tip = parent
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
	if err := insertHydrationProjection(tx, entry); err != nil {
		_ = tx.Rollback()
		return err
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
	s.advanceContextCacheLocked(expectedTip, []Entry{entry})
	return nil
}
