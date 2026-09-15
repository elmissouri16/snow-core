package session

import (
	"fmt"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestInputSpansFailClosedAndPreserveRootSelectors(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		for _, variant := range []string{"valid", "unknown-key", "unknown-version", "unknown-member", "duplicate", "wrong-root", "wrong-user", "dangling"} {
			t.Run(fmt.Sprintf("%t/%s", sqlite, variant), func(t *testing.T) {
				st := editTestStore(t, sqlite)
				appendEditTurn(t, st, 0)
				span, err := NewAgentInputSpanEntry("span", AgentInputSpan{Version: 1, RootTurnID: "turn-0", QueueID: "queue", UserEntryID: "queued-user", Kind: protocol.QueuedInputFollowUp})
				if err != nil {
					t.Fatal(err)
				}
				switch variant {
				case "unknown-key":
					span.Key += "_v2"
				case "unknown-version":
					span.Value = `{"version":2,"root_turn_id":"turn-0","queue_id":"queue","user_entry_id":"queued-user","kind":"follow_up"}`
				case "unknown-member":
					span.Value = span.Value[:len(span.Value)-1] + `,"extra":true}`
				case "duplicate":
					span.Value = span.Value[:len(span.Value)-1] + `,"version":1}`
				case "wrong-root":
					span.Value = `{"version":1,"root_turn_id":"different","queue_id":"queue","user_entry_id":"queued-user","kind":"follow_up"}`
				case "wrong-user":
					span.Value = `{"version":1,"root_turn_id":"turn-0","queue_id":"queue","user_entry_id":"different","kind":"follow_up"}`
				}
				batch := []Entry{span}
				if variant != "dangling" {
					batch = append(batch, Entry{Type: EntryMessage, ID: "queued-user", Message: new(protocol.NewUserMessage("queued-user", "", "next"))}, Entry{Type: EntryMessage, ID: "queued-reply", Message: new(protocol.NewAssistantMessage("queued-reply", "", "fake", "fake-1", []protocol.ContentBlock{protocol.NewTextBlock("next reply")}, protocol.StopStop, nil))})
				}
				if err := st.(BatchStore).AppendBatch(batch); err != nil {
					t.Fatal(err)
				}
				for _, entryID := range []string{"user-0", "queued-user"} {
					_, err := ResolveMessageEdit(t.Context(), st, protocol.RPCMessageEditPrepareParams{SessionID: st.ID(), EntryID: entryID})
					if (err == nil) != (variant == "valid") {
						t.Fatalf("edit %s: %v", entryID, err)
					}
				}
				for _, entryID := range []string{"assistant-0", "queued-reply"} {
					_, err := ResolveMessageRegenerate(t.Context(), st, protocol.RPCMessageRegeneratePrepareParams{SessionID: st.ID(), EntryID: entryID})
					if (err == nil) != (variant == "valid") {
						t.Fatalf("regenerate %s: %v", entryID, err)
					}
				}
			})
		}
	}
}
