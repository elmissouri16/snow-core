package session

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ChildIncurredUsage excludes only ForkContext's consecutive initial message
// chain after the built-in root. Later fork-looking IDs are ordinary history.
// Recompute rather than subtract: cache completeness and currency conflicts
// cannot be reversed by subtracting token/cost totals. History is not changed.
func ChildIncurredUsage(entries []Entry) (protocol.Usage, error) {
	var projection childUsageProjection
	for _, entry := range entries {
		if projection.inherited(entry) {
			continue
		}
		if entry.Type == EntryMessage && entry.Message != nil && entry.Message.Usage != nil {
			projection.total = projection.total.Add(*entry.Message.Usage)
		} else if entry.Type == EntryMeta && entry.Key == MetaProviderUsage {
			if err := projection.addJSON(entry.Value); err != nil {
				return protocol.Usage{}, err
			}
		}
	}
	return projection.total, nil
}

type childUsageProjection struct {
	total    protocol.Usage
	started  bool
	copying  bool
	nextFork int
	parent   string
}

func (p *childUsageProjection) inherited(entry Entry) bool {
	if !p.started {
		p.started = true
		p.copying = entry.Type == EntryMeta && entry.ID == "root" && entry.Key == "root" && entry.ParentID == ""
		p.nextFork, p.parent = 1, "root"
		return p.copying
	}
	if p.copying && entry.Type == EntryMessage && entry.Message != nil && entry.ParentID == p.parent && strings.HasPrefix(entry.ID, fmt.Sprintf("fork-%d-", p.nextFork)) {
		p.parent = entry.ID
		p.nextFork++
		return true
	}
	p.copying = false
	return false
}

func (p *childUsageProjection) addJSON(raw string) error {
	var usage protocol.Usage
	if err := json.Unmarshal([]byte(raw), &usage); err != nil {
		return err
	}
	p.total = p.total.Add(usage)
	return nil
}

// AggregateChildUsage is the active-branch usage projection for a child. Unlike
// AggregateUsage it does not attribute legacy inherited parent usage to it.
func (s *MemoryStore) AggregateChildUsage() (protocol.Usage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return protocol.Usage{}, errors.New("session: store closed")
	}
	return ChildIncurredUsage(pathFrom(s.entries, s.byID, s.tip))
}

func (s *SQLiteStore) AggregateChildUsage() (protocol.Usage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return protocol.Usage{}, errors.New("session: store closed")
	}
	return aggregateChildUsageSQL(context.Background(), s.db, s.tip, 0)
}

// InspectChildUsage reads an existing child's usage without opening a runtime,
// migrating its schema, changing permissions, or writing any session entries.
// The caller must first validate its pinned child locator. The expected session
// identity is checked independently so another valid database is not adopted.
func InspectChildUsage(ctx context.Context, path, expectedID string) (protocol.Usage, error) {
	if err := ctx.Err(); err != nil {
		return protocol.Usage{}, err
	}
	if path == "" || expectedID == "" {
		return protocol.Usage{}, errors.New("session: child path and identity required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return protocol.Usage{}, err
	}
	if !info.Mode().IsRegular() || !singleLink(info) {
		return protocol.Usage{}, errors.New("session: child database must be a regular, non-aliased file")
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return protocol.Usage{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := validateSQLiteSessionDBContext(ctx, db); err != nil {
		return protocol.Usage{}, err
	}
	var id, tip string
	if err := db.QueryRowContext(ctx, `SELECT session_id, branch_tip FROM session_meta WHERE singleton=1`).Scan(&id, &tip); err != nil {
		return protocol.Usage{}, err
	}
	if id != expectedID {
		return protocol.Usage{}, errors.New("session: child session identity mismatch")
	}
	if err := db.QueryRowContext(ctx, `SELECT tip_id FROM session_branches WHERE active=1 ORDER BY created_at LIMIT 1`).Scan(&tip); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table") {
		return protocol.Usage{}, err
	}
	return aggregateChildUsageSQL(ctx, db, tip, maxSessionQueryDepth)
}

type childUsageQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Decode only usage, not message bodies. Root-to-tip headers preserve prefix
// recognition; Go retains one row plus the aggregate. Cold inspection rejects
// an overlong/incomplete chain instead of replacing cached usage with a partial
// sum. Runtime aggregation retains the same uncapped history as AggregateUsage.
func aggregateChildUsageSQL(ctx context.Context, db childUsageQueryer, tip string, maxDepth int) (protocol.Usage, error) {
	rows, err := db.QueryContext(ctx, `WITH RECURSIVE branch(id,parent_id,entry_type,meta_key,has_message,usage,depth) AS (
 SELECT id,parent_id,entry_type,meta_key,message IS NOT NULL,
 CASE WHEN entry_type=? THEN meta_value ELSE json_extract(CAST(message AS TEXT),'$.usage') END,0 FROM entries WHERE id=?
 UNION ALL
 SELECT e.id,e.parent_id,e.entry_type,e.meta_key,e.message IS NOT NULL,
 CASE WHEN e.entry_type=? THEN e.meta_value ELSE json_extract(CAST(e.message AS TEXT),'$.usage') END,b.depth+1 FROM entries e JOIN branch b ON e.id=b.parent_id
 WHERE ?=0 OR b.depth<=?
 ) SELECT id,parent_id,entry_type,meta_key,has_message,usage,depth
 FROM branch ORDER BY depth DESC`, EntryMeta, tip, EntryMeta, maxDepth, maxDepth)
	if err != nil {
		return protocol.Usage{}, err
	}
	defer rows.Close()
	var projection childUsageProjection
	for rows.Next() {
		var entry Entry
		var raw sql.NullString
		var hasMessage bool
		var depth int
		if err := rows.Scan(&entry.ID, &entry.ParentID, &entry.Type, &entry.Key, &hasMessage, &raw, &depth); err != nil {
			return protocol.Usage{}, err
		}
		if maxDepth > 0 && depth > maxDepth {
			return protocol.Usage{}, errors.New("session: child usage inspection depth exceeded")
		}
		if !projection.started && (entry.ID != "root" || entry.ParentID != "") {
			return protocol.Usage{}, errors.New("session: incomplete child usage branch")
		}
		if hasMessage {
			entry.Message = new(protocol.Message{})
		}
		if projection.inherited(entry) {
			continue
		}
		if raw.Valid && ((entry.Type == EntryMessage && hasMessage) || (entry.Type == EntryMeta && entry.Key == MetaProviderUsage)) {
			if err := projection.addJSON(raw.String); err != nil {
				return protocol.Usage{}, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return protocol.Usage{}, err
	}
	if !projection.started {
		return protocol.Usage{}, errors.New("session: child usage branch not found")
	}
	return projection.total, nil
}
