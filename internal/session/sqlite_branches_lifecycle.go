package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

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
	updateArgs := append(slices.Clone(args[1:]), id, expected)
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
	s.invalidateContextCacheLocked()
	var errs []error
	if countErr != nil || closeErr != nil {
		errs = append(errs, countErr, closeErr)
	} else if meaningfulCount == 0 && s.deleteIfEmpty && s.lease != nil {
		if err := tryLockSessionExclusive(s.lease); err == nil {
			for _, suffix := range []string{"", "-wal", "-shm", "-journal", ".lock"} {
				if err := os.Remove(s.path + suffix); err != nil && !os.IsNotExist(err) {
					errs = append(errs, fmt.Errorf("session: remove empty database %q: %w", s.path+suffix, err))
				}
			}
		} else if !errors.Is(err, errSessionInUse) {
			errs = append(errs, fmt.Errorf("session: lock empty database cleanup: %w", err))
		}
	}
	if s.lease != nil {
		unlockSessionFile(s.lease)
		if err := s.lease.Close(); err != nil {
			errs = append(errs, err)
		}
		s.lease = nil
	}
	return errors.Join(errs...)
}

const agentRunStatsSQL = `WITH RECURSIVE branch(seq, id, parent_id, entry_type, meta_key, meta_value) AS (
	SELECT seq, id, parent_id, entry_type, meta_key, meta_value FROM entries WHERE id=?
	UNION ALL
	SELECT e.seq, e.id, e.parent_id, e.entry_type, e.meta_key, e.meta_value
	FROM entries e JOIN branch b ON e.id=b.parent_id
), cutoffs AS (
	SELECT
		min(CASE WHEN entry_type='meta' AND meta_key='agent_turn_v1'
			AND meta_value IN ('user', 'goal', 'subagent') THEN seq END) AS first_turn_marker,
		min(CASE WHEN entry_type='meta' AND meta_key='agent_step_v1'
			AND meta_value='provider' THEN seq END) AS first_step_marker
	FROM branch
) SELECT
	coalesce(sum(CASE WHEN b.entry_type='meta' AND b.meta_key='agent_turn_v1'
		AND b.meta_value IN ('user', 'goal', 'subagent') THEN 1 ELSE 0 END), 0),
	coalesce(sum(CASE WHEN b.entry_type='message' AND (first_turn_marker IS NULL OR b.seq < first_turn_marker)
		AND h.role='user' THEN 1 ELSE 0 END), 0),
	coalesce(sum(CASE WHEN b.entry_type='meta' AND b.meta_key='agent_step_v1'
		AND b.meta_value='provider' THEN 1 ELSE 0 END), 0),
	coalesce(sum(CASE WHEN b.entry_type='message' AND (first_step_marker IS NULL OR b.seq < first_step_marker)
		AND h.role='assistant' THEN 1 ELSE 0 END), 0)
FROM branch b CROSS JOIN cutoffs
LEFT JOIN entry_hydration_projection h ON h.entry_id=b.id`

type agentRunStatsScanner interface {
	Scan(dest ...any) error
}

func scanAgentRunStats(row agentRunStatsScanner) (AgentRunStats, error) {
	var explicitTurns, legacyUsers, explicitSteps, legacySteps uint64
	if err := row.Scan(&explicitTurns, &legacyUsers, &explicitSteps, &legacySteps); err != nil {
		return AgentRunStats{}, err
	}
	return AgentRunStats{Turns: explicitTurns + legacyUsers, Steps: explicitSteps + legacySteps}, nil
}

// AgentRunStats returns whole-branch turn/step counts without decoding message
// payloads in Go. Legacy prefixes are inferred from their durable JSON roles.
func (s *SQLiteStore) AgentRunStats() (AgentRunStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return AgentRunStats{}, errors.New("session: store closed")
	}
	return scanAgentRunStats(s.db.QueryRow(agentRunStatsSQL, s.tip))
}

// CountAgentTurns retains the legacy count-only interface.
func (s *SQLiteStore) CountAgentTurns() (uint64, error) {
	stats, err := s.AgentRunStats()
	return stats.Turns, err
}

// AggregateUsage sums persisted usage without decoding complete messages.
func (s *SQLiteStore) AggregateUsage() (protocol.Usage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return protocol.Usage{}, errors.New("session: store closed")
	}
	rows, err := s.db.Query(`WITH RECURSIVE branch(id, parent_id, entry_type, message, meta_key, meta_value) AS (
 SELECT id, parent_id, entry_type, message, meta_key, meta_value FROM entries WHERE id=?
 UNION ALL
 SELECT e.id, e.parent_id, e.entry_type, e.message, e.meta_key, e.meta_value FROM entries e JOIN branch b ON e.id=b.parent_id
 ) SELECT CASE WHEN entry_type=? THEN meta_value ELSE json_extract(CAST(message AS TEXT), '$.usage') END FROM branch
 WHERE (entry_type=? AND json_type(CAST(message AS TEXT), '$.usage')='object') OR (entry_type=? AND meta_key=?)`, s.tip, EntryMeta, EntryMessage, EntryMeta, MetaProviderUsage)
	if err != nil {
		return protocol.Usage{}, err
	}
	defer rows.Close()
	var total protocol.Usage
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return protocol.Usage{}, err
		}
		var usage protocol.Usage
		if err := json.Unmarshal([]byte(raw), &usage); err != nil {
			return protocol.Usage{}, err
		}
		total = total.Add(usage)
	}
	return total, rows.Err()
}

// CountSessionReferences counts durable successful session_reference tool
// results without materializing the complete conversation.
func (s *SQLiteStore) CountSessionReferences() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, errors.New("session: store closed")
	}
	var count int
	err := s.db.QueryRow(`WITH RECURSIVE branch(id, parent_id, entry_type, message) AS (
		SELECT id, parent_id, entry_type, message FROM entries WHERE id=?
		UNION ALL
		SELECT e.id, e.parent_id, e.entry_type, e.message FROM entries e JOIN branch b ON e.id=b.parent_id
	) SELECT count(*) FROM branch WHERE entry_type=?
		AND json_extract(CAST(message AS TEXT), '$.role')=?
		AND json_extract(CAST(message AS TEXT), '$.tool_name')='session_reference'
		AND coalesce(json_extract(CAST(message AS TEXT), '$.is_error'), 0)=0
		AND instr(CAST(message AS TEXT), 'source_session_id') > 0`, s.tip, EntryMessage, protocol.RoleTool).Scan(&count)
	return count, err
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
