package session

import (
	"context"
	"database/sql"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// The preflight transfers only a bounded ancestor count and byte total, never
// raw history. Blob lengths count UTF-8 bytes rather than SQLite text runes.
// Stop after the first over-budget entry, without walking the remaining prefix.
const messageEditSQLitePreflight = `WITH RECURSIVE branch(id,parent_id,depth,bytes) AS (
 SELECT id,parent_id,1,
  256 + length(CAST(id AS BLOB)) + length(CAST(parent_id AS BLOB))
  + COALESCE(length(CAST(message AS BLOB)),0) + length(CAST(summary AS BLOB))
  + length(CAST(compacted_through AS BLOB)) + length(CAST(meta_key AS BLOB)) + length(CAST(meta_value AS BLOB))
 FROM entries WHERE id=?
 UNION ALL
 SELECT e.id,e.parent_id,b.depth+1,b.bytes
  + 256 + length(CAST(e.id AS BLOB)) + length(CAST(e.parent_id AS BLOB))
  + COALESCE(length(CAST(e.message AS BLOB)),0) + length(CAST(e.summary AS BLOB))
  + length(CAST(e.compacted_through AS BLOB)) + length(CAST(e.meta_key AS BLOB)) + length(CAST(e.meta_value AS BLOB))
 FROM entries e JOIN branch b ON e.id=b.parent_id
 WHERE b.depth < ? AND b.bytes <= ?
) SELECT depth,bytes,CASE WHEN parent_id='' THEN '' ELSE 'nonempty' END FROM branch ORDER BY depth DESC LIMIT 1`

func (s *SQLiteStore) messageEditEntries(ctx context.Context) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	// Keep preflight and decoding on the same SQLite snapshot, even if another
	// handle writes concurrently. Session/tree state remains entirely read-only.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return boundedVersionEntriesTx(ctx, tx, s.tip)
}

func boundedVersionEntriesTx(ctx context.Context, tx *sql.Tx, tip string) ([]Entry, error) {
	if tip == "" {
		return []Entry{}, nil
	}
	var count, total int
	var parent string
	err := tx.QueryRowContext(ctx, messageEditSQLitePreflight, tip, messageEditMaxEntries, messageEditMaxHistoryBytes).Scan(&count, &total, &parent)
	if err != nil {
		return nil, fmt.Errorf("message edit bounded history preflight: %w", err)
	}
	if total > messageEditMaxHistoryBytes || parent != "" {
		return nil, errMessageEditHistoryBounds
	}
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE branch(id,parent_id,depth) AS (
  SELECT id,parent_id,1 FROM entries WHERE id=?
  UNION ALL
  SELECT e.id,e.parent_id,b.depth+1 FROM entries e JOIN branch b ON e.id=b.parent_id WHERE b.depth < ?
 ) SELECT e.id,e.parent_id,e.entry_type,e.message,e.summary,e.compacted_through,e.meta_key,e.meta_value
 FROM branch b JOIN entries e ON e.id=b.id ORDER BY b.depth DESC`, tip, messageEditMaxEntries)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]Entry, 0, count)
	remaining := messageEditMaxHistoryBytes
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var entry Entry
		var raw []byte
		if err := rows.Scan(&entry.ID, &entry.ParentID, &entry.Type, &raw, &entry.Summary, &entry.CompactedThrough, &entry.Key, &entry.Value); err != nil {
			return nil, err
		}
		if len(entries) >= messageEditMaxEntries || len(raw) > messageEditMaxHistoryBytes {
			return nil, errMessageEditHistoryBounds
		}
		if len(raw) != 0 {
			entry.Message = new(protocol.Message)
			if err := json.Unmarshal(raw, entry.Message); err != nil {
				return nil, fmt.Errorf("message edit decode bounded history: %w", err)
			}
			normalizeEntryMessage(&entry)
		}
		// Encoded and decoded budgets both apply. Already-owned decoded entries need
		// no second defensive clone; rejected snapshots never reach the caller.
		if err := chargeMessageEditValue(ctx, reflect.ValueOf(entry), &remaining, 0); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(entries) != count {
		return nil, errors.New("message edit bounded history is incomplete")
	}
	return entries, nil
}
