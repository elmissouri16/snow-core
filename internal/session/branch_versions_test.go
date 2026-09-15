package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestBranchVersionsBoundedReadAndExactSelection(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			store := editTestStore(t, sqlite)
			appendEditTurn(t, store, 0)
			versions := store.(BranchVersionStore)
			source := store.(ActiveBranchStore).ActiveBranchID()
			tip := store.BranchTip()
			fork, err := store.(BranchStore).ForkBranch(tip)
			if err != nil {
				t.Fatal(err)
			}
			appendEditTurn(t, store, 1)
			activeTip := store.BranchTip()
			snapshot, err := versions.BranchVersion(t.Context(), source)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Branch.TipID != tip || snapshot.Active.BranchID != fork.ID || len(snapshot.Messages()) != 2 || store.BranchTip() != activeTip {
				t.Fatalf("read mutated or misbound version %+v", snapshot)
			}
			first, err := versions.BranchVersions(t.Context(), "", 1)
			if err != nil || len(first.Branches) != 1 || first.NextCursor == "" {
				t.Fatalf("page=%+v err=%v", first, err)
			}
			second, err := versions.BranchVersions(t.Context(), first.NextCursor, 1)
			if err != nil || len(second.Branches) != 1 || first.Branches[0].ID == second.Branches[0].ID {
				t.Fatalf("second=%+v err=%v", second, err)
			}
			binding := protocol.RPCBranchRestorePrepareParams{SessionID: store.ID(), SourceBranchID: fork.ID, SourceTipID: activeTip, TargetBranchID: source, TargetTipID: tip}
			for _, bad := range []protocol.RPCBranchRestorePrepareParams{
				{SessionID: store.ID(), SourceBranchID: fork.ID, SourceTipID: tip, TargetBranchID: source, TargetTipID: tip},
				{SessionID: store.ID(), SourceBranchID: fork.ID, SourceTipID: activeTip, TargetBranchID: source, TargetTipID: activeTip},
			} {
				if !errors.Is(versions.RestoreBranchVersion(t.Context(), bad, protocol.ModeDefault), ErrBranchVersionStale) {
					t.Fatal("inexact identity selected")
				}
			}
			if store.BranchTip() != activeTip {
				t.Fatal("rejection changed tip")
			}
			if err := versions.RestoreBranchVersion(t.Context(), binding, protocol.ModeDefault); err != nil {
				t.Fatal(err)
			}
			identity, err := versions.ProbeBranchVersion(t.Context())
			if err != nil || identity.BranchID != source || identity.TipID != tip {
				t.Fatalf("probe=%+v err=%v", identity, err)
			}
			if !errors.Is(versions.RestoreBranchVersion(t.Context(), binding, protocol.ModeDefault), ErrBranchVersionStale) {
				t.Fatal("stale selection replayed")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := versions.BranchVersions(ctx, "", 10); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err := versions.BranchVersion(ctx, source); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
func TestBranchVersionRejectsHugeInactiveHistoryAndIdentity(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			store := editTestStore(t, sqlite)
			appendEditTurn(t, store, 0)
			versions := store.(BranchVersionStore)
			if db, ok := store.(*SQLiteStore); ok {
				if _, err := db.db.Exec(`UPDATE entries SET message=zeroblob(?) WHERE id='assistant-0'`, messageEditMaxHistoryBytes+1); err != nil {
					t.Fatal(err)
				}
			} else {
				memory := store.(*MemoryStore)
				memory.entries[memory.byID["assistant-0"]].Message.Content[0].Data = make([]byte, messageEditMaxHistoryBytes+1)
			}
			before := store.BranchTip()
			if _, err := versions.BranchVersion(t.Context(), "main"); !errors.Is(err, errMessageEditHistoryBounds) {
				t.Fatalf("payload materialized: %v", err)
			}
			if store.BranchTip() != before {
				t.Fatal("read changed tip")
			}
			if db, ok := store.(*SQLiteStore); ok {
				if _, err := db.db.Exec(`UPDATE session_branches SET branch_name=?`, strings.Repeat("a", 257)); err != nil {
					t.Fatal(err)
				}
			} else {
				memory := store.(*MemoryStore)
				b := memory.branches["main"]
				b.Name = strings.Repeat("a", 257)
				memory.branches["main"] = b
			}
			if _, err := versions.BranchVersions(t.Context(), "", 100); err == nil {
				t.Fatal("oversized metadata returned/truncated")
			}
		})
	}
}

func TestBranchVersionSQLitePreflightDoesNotReturnUnboundedParent(t *testing.T) {
	store := editTestStore(t, true).(*SQLiteStore)
	appendEditTurn(t, store, 0)
	if _, err := store.db.Exec(`UPDATE entries SET parent_id=? WHERE id='assistant-0'`, strings.Repeat("x", messageEditMaxHistoryBytes+1)); err != nil {
		t.Fatal(err)
	}
	var count, total int
	var parent string
	if err := store.db.QueryRowContext(t.Context(), messageEditSQLitePreflight, store.BranchTip(), messageEditMaxEntries, messageEditMaxHistoryBytes).Scan(&count, &total, &parent); err != nil {
		t.Fatal(err)
	}
	if total <= messageEditMaxHistoryBytes || parent == "" || len(parent) > 16 {
		t.Fatal("preflight transferred raw unbounded ancestor identity")
	}
	if _, err := store.BranchVersion(t.Context(), "main"); !errors.Is(err, errMessageEditHistoryBounds) {
		t.Fatal(err)
	}
}
