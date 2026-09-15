package session

import (
	"context"
	"errors"
)

// MessageEditEntryPresenceStore probes one exact durable entry ID after an
// ambiguous Append failure. It must query durable storage, not a cached tip or
// projection. false,nil alone proves the replacement was never committed.
// Custom stores without this seam remain safe: callers retain the new branch
// and report an unknown outcome instead of guessing or retrying.
type MessageEditEntryPresenceStore interface {
	MessageEditEntryExists(context.Context, string) (bool, error)
}

func (s *MemoryStore) MessageEditEntryExists(ctx context.Context, id string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return false, errors.New("session: store closed")
	}
	_, ok := s.byID[id]
	return ok, nil
}

func (s *SQLiteStore) MessageEditEntryExists(ctx context.Context, id string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return false, errors.New("session: store closed")
	}
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM entries WHERE id=?)`, id).Scan(&found)
	return found, err
}
