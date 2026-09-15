package session

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func editTestStore(t *testing.T, sqlite bool) Store {
	t.Helper()
	if !sqlite {
		store := NewMemoryStore(Options{ID: "edit-session"})
		t.Cleanup(func() { _ = store.Close() })
		return store
	}
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "edit.db"), t.TempDir(), Options{ID: "edit-session"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func appendEditTurn(t *testing.T, store Store, n int) {
	t.Helper()
	turn, user, assistant := fmt.Sprintf("turn-%d", n), fmt.Sprintf("user-%d", n), fmt.Sprintf("assistant-%d", n)
	for _, entry := range []Entry{
		{Type: EntryMeta, ID: turn, Key: MetaAgentTurn, Value: "user"},
		{Type: EntryMessage, ID: user, Message: new(protocol.NewUserMessage(user, "", fmt.Sprintf("text %d", n)))},
		{Type: EntryMeta, ID: fmt.Sprintf("step-%d", n), Key: MetaAgentStep, Value: "provider"},
		{Type: EntryMessage, ID: assistant, Message: new(protocol.NewAssistantMessage(assistant, "", "fake", "fake-1", []protocol.ContentBlock{protocol.NewTextBlock("answer")}, protocol.StopStop, nil))},
	} {
		if err := store.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMessageEditExactPrefixAndOldBranchRetention(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		for selected := range 3 {
			t.Run(fmt.Sprintf("sqlite=%t/selected=%d", sqlite, selected), func(t *testing.T) {
				store := editTestStore(t, sqlite)
				for n := range 3 {
					appendEditTurn(t, store, n)
				}
				oldTip := store.BranchTip()
				original, _ := store.Messages()
				expectedBoundary := "root"
				if selected > 0 {
					expectedBoundary = fmt.Sprintf("assistant-%d", selected-1)
				}
				source, err := ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), TurnID: fmt.Sprintf("turn-%d", selected)})
				if err != nil {
					t.Fatal(err)
				}
				if source.EntryID != fmt.Sprintf("user-%d", selected) || source.BoundaryID != expectedBoundary {
					t.Fatalf("source=%+v", source)
				}
				byEntry, err := ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), EntryID: source.EntryID})
				if err != nil || byEntry != source {
					t.Fatalf("entry source=%+v err=%v", byEntry, err)
				}
				if store.BranchTip() != oldTip {
					t.Fatal("prepare changed tip")
				}
				branch, err := store.(BranchManagementStore).ForkBranchWithOptions(protocol.BranchForkOptions{SourceBranchID: source.BranchID, FromEntryID: source.BoundaryID})
				if err != nil {
					t.Fatal(err)
				}
				messages, _ := store.Messages()
				if len(messages) != selected*2 {
					t.Fatalf("retained %d messages", len(messages))
				}
				entries, _ := store.(BranchEntryStore).BranchEntries()
				turns := 0
				for _, entry := range entries {
					if IsAgentTurnMarker(entry) {
						turns++
					}
				}
				if turns != selected {
					t.Fatalf("phantom selected marker: %d turns", turns)
				}
				if err := store.(BranchStore).SelectBranch(source.BranchID); err != nil {
					t.Fatal(err)
				}
				old, _ := store.Messages()
				if len(old) != len(original) || store.BranchTip() != oldTip || store.ID() != source.SessionID {
					t.Fatal("old branch/session changed")
				}
				// Legacy empty fork remains current-tip, not an edit root sentinel.
				tipFork, err := store.(BranchManagementStore).ForkBranchWithOptions(protocol.BranchForkOptions{})
				if err != nil || tipFork.ForkedFromID != oldTip || branch.ID == source.BranchID {
					t.Fatalf("legacy fork=%+v err=%v", tipFork, err)
				}
			})
		}
	}
}

func TestMessageEditRejectsAmbiguousAndPrivateInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*MemoryStore)
	}{
		{"attachment", func(s *MemoryStore) {
			s.entries[2].Message.Content = append(s.entries[2].Message.Content, protocol.ContentBlock{Type: protocol.BlockImage})
		}},
		{"plugin", func(s *MemoryStore) { s.entries[2].Message.PluginDetails = []byte(`{"private":true}`) }},
		{"hidden-text-data", func(s *MemoryStore) { s.entries[2].Message.Content[0].Data = []byte("private") }},
		{"internal-origin", func(s *MemoryStore) { s.entries[1].Value = "goal" }},
		{"duplicate-user", func(s *MemoryStore) {
			_ = s.Append(Entry{Type: EntryMessage, ID: "duplicate", Message: new(protocol.NewUserMessage("duplicate", "", "same turn"))})
		}},
		{"fake-root", func(s *MemoryStore) { s.entries[0].Value = "other-session" }},
		{"too-large", func(s *MemoryStore) {
			s.entries[2].Message.Content[0].Text = strings.Repeat("x", protocol.RPCMessageEditMaxTextBytes+1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryStore(Options{ID: "edit-session"})
			defer store.Close()
			appendEditTurn(t, store, 0)
			tc.mutate(store)
			if _, err := ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), TurnID: "turn-0"}); err == nil {
				t.Fatal("unsafe edit accepted")
			}
		})
	}
	store := editTestStore(t, false)
	appendEditTurn(t, store, 0)
	for _, params := range []protocol.RPCMessageEditPrepareParams{
		{SessionID: store.ID()}, {SessionID: store.ID(), EntryID: "user-0", TurnID: "turn-0"},
		{SessionID: "other", EntryID: "user-0"}, {SessionID: store.ID(), EntryID: "assistant-0"},
		{SessionID: store.ID(), TurnID: "user-0"}, {SessionID: store.ID(), TurnID: "missing"},
	} {
		if _, err := ResolveMessageEdit(t.Context(), store, params); err == nil {
			t.Fatalf("accepted %+v", params)
		}
	}
}

func TestMessageEditCompactionAndToolBoundary(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			store := editTestStore(t, sqlite)
			appendEditTurn(t, store, 0)
			if err := store.Append(Entry{Type: EntryCompaction, ID: "checkpoint", Summary: "working state", CompactedThrough: "assistant-0"}); err != nil {
				t.Fatal(err)
			}
			appendEditTurn(t, store, 1)
			source, err := ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), TurnID: "turn-1"})
			if err != nil || source.BoundaryID != "checkpoint" {
				t.Fatalf("compaction source=%+v err=%v", source, err)
			}
			call := protocol.NewAssistantMessage("call", "", "fake", "fake-1", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "tool", Name: "read"}}, protocol.StopToolUse, nil)
			if err := store.Append(Entry{Type: EntryMessage, ID: call.ID, Message: &call}); err != nil {
				t.Fatal(err)
			}
			appendEditTurn(t, store, 2)
			if _, err := ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), TurnID: "turn-2"}); err == nil {
				t.Fatal("unresolved tool prefix accepted")
			}
		})
	}
}

func TestMessageEditTextValidation(t *testing.T) {
	for _, text := range []string{"", " ", "before\x00after", string([]byte{0xff}), strings.Repeat("x", protocol.RPCMessageEditMaxTextBytes+1), strings.Repeat("é", protocol.RPCMessageEditMaxTextBytes/2+1)} {
		if err := ValidateMessageEditText(text); err == nil {
			t.Fatalf("accepted invalid input of %d bytes", len(text))
		}
	}
	for _, text := range []string{"new text", "multiline\nworks", strings.Repeat("é", protocol.RPCMessageEditMaxTextBytes/2)} {
		if err := ValidateMessageEditText(text); err != nil {
			t.Fatalf("valid input: %v", err)
		}
	}
}
