package session

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestLiveMessageImageExactTurnAndMessageFences(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			store := editTestStore(t, sqlite)
			message := imageTestMessage(t)
			for _, entry := range []Entry{{Type: EntryMeta, ID: "turn-image", Key: MetaAgentTurn, Value: "user"}, {Type: EntryMessage, ID: message.ID, Message: &message}} {
				if err := store.Append(entry); err != nil {
					t.Fatal(err)
				}
			}
			tip := store.BranchTip()
			for _, p := range []protocol.RPCMessageImageParams{{SessionID: store.ID(), MessageID: message.ID, Index: 1}, {SessionID: store.ID(), TurnID: "turn-image", Index: 1}} {
				got, err := ReadMessageImage(t.Context(), store, p)
				if err != nil || got.MessageID != message.ID || !bytes.Equal(got.Data, message.Content[1].Data) {
					t.Fatalf("read=%+v err=%v", got, err)
				}
			}
			if store.BranchTip() != tip {
				t.Fatal("image read changed branch")
			}
			if _, err := ResolveMessageEdit(t.Context(), store, protocol.RPCMessageEditPrepareParams{SessionID: store.ID(), EntryID: message.ID}); err == nil {
				t.Fatal("image read made multimodal user editable")
			}
			for _, p := range []protocol.RPCMessageImageParams{
				{SessionID: "foreign", MessageID: message.ID, Index: 1}, {SessionID: store.ID(), MessageID: message.ID, TurnID: "turn-image", Index: 1},
				{SessionID: store.ID(), TurnID: "missing", Index: 1}, {SessionID: store.ID(), TurnID: message.ID, Index: 1},
				{SessionID: store.ID(), MessageID: message.ID, Index: 2},
			} {
				if _, err := ReadMessageImage(t.Context(), store, p); err == nil {
					t.Fatalf("accepted %+v", p)
				}
			}
			if err := store.SetBranchTip("root"); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadMessageImage(t.Context(), store, protocol.RPCMessageImageParams{SessionID: store.ID(), MessageID: message.ID, Index: 1}); err == nil {
				t.Fatal("read image off current branch")
			}
		})
	}
}

func TestLiveImageTurnRejectsAmbiguousUsers(t *testing.T) {
	store := editTestStore(t, false)
	message := imageTestMessage(t)
	for _, entry := range []Entry{{Type: EntryMeta, ID: "turn", Key: MetaAgentTurn, Value: "user"}, {Type: EntryMessage, ID: message.ID, Message: &message}, {Type: EntryMessage, ID: "second", Message: new(protocol.NewUserMessage("second", "", "second"))}} {
		if err := store.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ReadMessageImage(t.Context(), store, protocol.RPCMessageImageParams{SessionID: store.ID(), TurnID: "turn", Index: 1}); err == nil {
		t.Fatal("guessed ambiguous turn user")
	}
}
