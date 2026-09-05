package session

import (
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestAuxiliaryUsageSurvivesBranchesAndReopen(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		name := "memory"
		if sqlite {
			name = "sqlite"
		}
		t.Run(name, func(t *testing.T) {
			var st interface {
				Store
				BranchStore
				ActiveBranchStore
				ContextStore
				AggregateUsage() (protocol.Usage, error)
			}
			path := filepath.Join(t.TempDir(), "usage.db")
			if sqlite {
				var err error
				st, err = NewSQLiteStore(path, t.TempDir(), Options{})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				st = NewMemoryStore(Options{})
			}
			defer func() { st.Close() }()
			message := protocol.NewAssistantMessage("reply", "", "fake", "m", []protocol.ContentBlock{protocol.NewTextBlock("hello")}, protocol.StopStop, &protocol.Usage{Total: 5})
			if err := st.Append(Entry{Type: EntryMessage, ID: message.ID, Message: &message}); err != nil {
				t.Fatal(err)
			}
			root := st.ActiveBranchID()
			if err := st.Append(Entry{Type: EntryMeta, ID: "usage-1", Key: MetaProviderUsage, Value: `{"input":30,"output":5,"total_tokens":35}`}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.ForkBranch(""); err != nil {
				t.Fatal(err)
			}
			if err := st.Append(Entry{Type: EntryMeta, ID: "usage-2", Key: MetaProviderUsage, Value: `{"total_tokens":7}`}); err != nil {
				t.Fatal(err)
			}
			usage, err := st.AggregateUsage()
			if err != nil || usage.Total != 47 {
				t.Fatalf("fork usage=%+v err=%v", usage, err)
			}
			if err := st.SelectBranch(root); err != nil {
				t.Fatal(err)
			}
			usage, err = st.AggregateUsage()
			if err != nil || usage.Total != 40 {
				t.Fatalf("root usage=%+v err=%v", usage, err)
			}
			messages, err := st.ContextMessages()
			if err != nil || len(messages) != 1 {
				t.Fatalf("auxiliary usage entered context: %d err=%v", len(messages), err)
			}
			if sqlite {
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
				st, err = OpenSQLiteStore(path, t.TempDir(), Options{})
				if err != nil {
					t.Fatal(err)
				}
				usage, err = st.AggregateUsage()
				if err != nil || usage.Total != 40 {
					t.Fatalf("reopened usage=%+v err=%v", usage, err)
				}
			}
		})
	}
}
