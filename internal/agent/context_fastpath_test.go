package agent

import (
	"context"
	"testing"

	"github.com/elmissouri16/snow-core/internal/compact"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type fixedContextStore struct {
	session.Store
	messages []protocol.Message
}

func (s fixedContextStore) ContextMessages() ([]protocol.Message, error) { return s.messages, nil }

func TestContextMessagesNoFailureReusesOwnedProjection(t *testing.T) {
	messages := []protocol.Message{{ID: "one", Role: protocol.RoleUser}, {ID: "two", Role: protocol.RoleAssistant, StopReason: protocol.StopStop}}
	store := fixedContextStore{Store: session.NewMemoryStore(session.Options{}), messages: messages}
	got, err := contextMessagesFromStore(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(messages) || &got[0] != &messages[0] {
		t.Fatal("failure-free context projection was copied")
	}
}

func TestProviderMessagesReusesCleanProjection(t *testing.T) {
	messages := []protocol.Message{{ID: "one", Role: protocol.RoleUser}, {ID: "two", Role: protocol.RoleAssistant}}
	got := providerMessages(messages)
	if len(got) != len(messages) || &got[0] != &messages[0] {
		t.Fatal("clean provider projection was copied")
	}
}

func TestProviderMessagesCopiesOnlyWhenSurfaceMetadataPresent(t *testing.T) {
	display := &protocol.ToolDisplay{Output: "private local display"}
	messages := []protocol.Message{
		{ID: "one", Role: protocol.RoleUser},
		{ID: "two", Role: protocol.RoleTool, ToolDisplay: display},
	}
	got := providerMessages(messages)
	if len(got) != len(messages) || &got[0] == &messages[0] {
		t.Fatal("surface metadata projection did not copy message headers")
	}
	if got[1].ToolDisplay != nil {
		t.Fatal("surface metadata crossed provider boundary")
	}
	if messages[1].ToolDisplay != display {
		t.Fatal("provider projection mutated its input")
	}
}

func BenchmarkContextMessagesNoFailure1500(b *testing.B) {
	messages := make([]protocol.Message, 1500)
	for i := range messages {
		messages[i] = protocol.Message{ID: "message", Role: protocol.RoleAssistant, StopReason: protocol.StopStop}
	}
	store := fixedContextStore{Store: session.NewMemoryStore(session.Options{}), messages: messages}
	b.ReportAllocs()
	for b.Loop() {
		got, err := contextMessagesFromStore(store)
		if err != nil || len(got) != len(messages) {
			b.Fatalf("messages=%d err=%v", len(got), err)
		}
	}
}

func BenchmarkPruneHistoricalToolResultsNoOp1500(b *testing.B) {
	messages := make([]protocol.Message, 1500)
	for i := range messages {
		messages[i] = protocol.Message{ID: "message", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{protocol.NewTextBlock("answer")}}
	}
	a := &Agent{opts: Options{Compaction: CompactionOptions{HistoricalToolResultThreshold: compact.HistoricalToolResultThreshold}}}
	b.ReportAllocs()
	for b.Loop() {
		if got := a.pruneHistoricalToolResults(context.Background(), messages); len(got) != len(messages) {
			b.Fatal(len(got))
		}
	}
}

func BenchmarkProviderMessagesClean1500(b *testing.B) {
	messages := make([]protocol.Message, 1500)
	for i := range messages {
		messages[i] = protocol.Message{ID: "message", Role: protocol.RoleAssistant}
	}
	b.ReportAllocs()
	for b.Loop() {
		if got := providerMessages(messages); len(got) != len(messages) {
			b.Fatal(len(got))
		}
	}
}
