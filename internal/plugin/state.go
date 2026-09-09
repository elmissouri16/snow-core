package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// StateStore is separate from session entries: background preference writes
// must never change a conversation's parent-linked tip.
type StateStore struct {
	mu     sync.Mutex
	path   string
	db     *sql.DB
	closed bool
}

func NewStateStore(path string) *StateStore { return &StateStore{path: path} }
func (s *StateStore) open(ctx context.Context) error {
	if s.closed {
		return errors.New("plugin state store closed")
	}
	if s.db != nil {
		return nil
	}
	if s.path != "" {
		if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
			return err
		}
		if info, err := os.Lstat(s.path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			return errors.New("plugin state must be a regular file")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		file, err := os.OpenFile(s.path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
	}
	dsn := s.path
	if dsn == "" {
		dsn = ":memory:"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS plugin_state (owner TEXT NOT NULL,scope TEXT NOT NULL,key TEXT NOT NULL,value TEXT NOT NULL,PRIMARY KEY(owner,scope,key))`); err != nil {
		_ = db.Close()
		return err
	}
	s.db = db
	return nil
}
func (s *StateStore) Call(ctx context.Context, owner, scope, operation string, raw json.RawMessage) (json.RawMessage, error) {
	var arg struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if err := jsonv2.Unmarshal(raw, &arg); err != nil {
		return nil, err
	}
	if arg.Key == "" || len(arg.Key) > 128 || strings.ContainsRune(arg.Key, '\x00') {
		return nil, errors.New("state key must contain 1..128 bytes")
	}
	if len(arg.Value) > 64<<10 {
		return nil, errors.New("state value exceeds 64 KiB")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.open(ctx); err != nil {
		return nil, err
	}
	switch operation {
	case "storage.get":
		var value string
		err := s.db.QueryRowContext(ctx, `SELECT value FROM plugin_state WHERE owner=? AND scope=? AND key=?`, owner, scope, arg.Key).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			return []byte("null"), nil
		}
		return []byte(value), err
	case "storage.delete":
		_, err := s.db.ExecContext(ctx, `DELETE FROM plugin_state WHERE owner=? AND scope=? AND key=?`, owner, scope, arg.Key)
		return []byte("null"), err
	case "storage.set":
		if len(arg.Value) == 0 {
			return nil, errors.New("state value is required")
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		// Acquire the write lock before checking the quota across processes.
		if _, err = tx.ExecContext(ctx, `INSERT INTO plugin_state(owner,scope,key,value) VALUES(?,?,?,?) ON CONFLICT(owner,scope,key) DO UPDATE SET value=excluded.value`, owner, scope, arg.Key, string(arg.Value)); err != nil {
			return nil, err
		}
		var size, count int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(length(CAST(value AS BLOB))),0),COUNT(*) FROM plugin_state WHERE owner=? AND scope=?`, owner, scope).Scan(&size, &count); err != nil {
			return nil, err
		}
		if size > 1<<20 || count > 1024 {
			return nil, fmt.Errorf("plugin state quota exceeded (1 MiB / 1024 keys per scope)")
		}
		return []byte("null"), tx.Commit()
	}
	return nil, errors.New("unknown storage operation")
}
func (s *StateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
