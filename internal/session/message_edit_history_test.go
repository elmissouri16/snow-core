package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestMessageEditHistoryRejectsOversizeBeforeMaterialization(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			store := editTestStore(t, sqlite)
			appendEditTurn(t, store, 0)
			if db, ok := store.(*SQLiteStore); ok {
				// Deliberately invalid JSON must never be decoded: SQL's size preflight
				// rejects it before transferring this oversized payload to Go.
				if _, err := db.db.Exec(`UPDATE entries SET message=zeroblob(?) WHERE id='assistant-0'`, messageEditMaxHistoryBytes+1); err != nil {
					t.Fatal(err)
				}
			} else {
				memory := store.(*MemoryStore)
				memory.entries[memory.byID["assistant-0"]].Message.Content[0].Data = make([]byte, messageEditMaxHistoryBytes+1)
			}
			tip := store.BranchTip()
			branch := store.(ActiveBranchStore).ActiveBranchID()
			_, err := ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), TurnID: "turn-0"})
			if !errors.Is(err, errMessageEditHistoryBounds) {
				t.Fatalf("oversized history decoded/cloned instead of preflight rejection: %v", err)
			}
			if store.BranchTip() != tip || store.(ActiveBranchStore).ActiveBranchID() != branch {
				t.Fatal("bounded preparation mutated session")
			}
		})
	}
}

func TestMessageEditHistoryAggregateByteBound(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			store := editTestStore(t, sqlite)
			appendEditTurn(t, store, 0)
			// Each field fits by itself, but the combined retained history must not be
			// materialized. Include multibyte UTF-8 to catch SQL rune-count mistakes.
			large := strings.Repeat("é", messageEditMaxHistoryBytes/4)
			for i := range 2 {
				if err := store.Append(Entry{Type: EntryMeta, ID: fmt.Sprint("large-", i), Key: "payload", Value: large}); err != nil {
					t.Fatal(err)
				}
			}
			before := store.BranchTip()
			_, err := ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), EntryID: "user-0"})
			if !errors.Is(err, errMessageEditHistoryBounds) || store.BranchTip() != before {
				t.Fatalf("aggregate byte budget: %v", err)
			}
		})
	}
}

func TestMessageEditSQLiteTraversalStopsAtEntryLimit(t *testing.T) {
	store := editTestStore(t, true).(*SQLiteStore)
	appendEditTurn(t, store, 0)
	// Direct fixture insertion avoids 100,000 unrelated title/context/hydration
	// operations. The production reader still traverses real persisted SQLite.
	_, err := store.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<?)
 INSERT INTO entries(id,parent_id,entry_type,meta_key,meta_value)
 SELECT 'limit-'||i,CASE WHEN i=1 THEN 'assistant-0' ELSE 'limit-'||(i-1) END,'meta','test','x' FROM n`, messageEditMaxEntries+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetBranchTip(fmt.Sprintf("limit-%d", messageEditMaxEntries+1)); err != nil {
		t.Fatal(err)
	}
	before := store.BranchTip()
	var count, total int
	var parent string
	if err := store.db.QueryRowContext(t.Context(), messageEditSQLitePreflight, before, messageEditMaxEntries, messageEditMaxHistoryBytes).Scan(&count, &total, &parent); err != nil {
		t.Fatal(err)
	}
	if count != messageEditMaxEntries || total > messageEditMaxHistoryBytes || parent == "" {
		t.Fatalf("preflight did not stop at depth: count=%d bytes=%d parent=%s", count, total, parent)
	}
	_, err = ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), TurnID: "turn-0"})
	if !errors.Is(err, errMessageEditHistoryBounds) || store.BranchTip() != before {
		t.Fatalf("entry-limit rejection: %v", err)
	}
}

func TestMessageEditHistoryCanceledContext(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			store := editTestStore(t, sqlite)
			appendEditTurn(t, store, 0)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			before := store.BranchTip()
			_, err := ResolveMessageEdit(ctx, store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), TurnID: "turn-0"})
			if !errors.Is(err, context.Canceled) || store.BranchTip() != before {
				t.Fatalf("canceled bounded read: %v", err)
			}
		})
	}
}

// A legacy adapter exposing an unbounded snapshot must not be used as fallback.
type editUnboundedOnlyStore struct {
	Store
	ActiveBranchStore
	called bool
}

func (s *editUnboundedOnlyStore) BranchEntries() ([]Entry, error) {
	s.called = true
	return nil, errors.New("unbounded read must not run")
}
func TestMessageEditRequiresBoundedHistoryStore(t *testing.T) {
	original := editTestStore(t, false)
	store := &editUnboundedOnlyStore{Store: original, ActiveBranchStore: original.(ActiveBranchStore)}
	_, err := ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), TurnID: "turn-0"})
	if err == nil || store.called {
		t.Fatalf("unsupported history fallback: called=%t err=%v", store.called, err)
	}
}

func TestMessageEditMemoryBoundedHistoryReturnsDefensiveCopies(t *testing.T) {
	store := editTestStore(t, false).(*MemoryStore)
	appendEditTurn(t, store, 0)
	entries, err := store.messageEditEntries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	entries[2].Message.Content[0].Text = "mutated"
	source, err := ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), TurnID: "turn-0"})
	if err != nil || source.Text != "text 0" {
		t.Fatalf("bounded history aliased store: %+v %v", source, err)
	}
}
