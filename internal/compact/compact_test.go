package compact

import (
	"context"
	"testing"

	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

func mkMsg(id, parent, text string) protocol.Message {
	m := protocol.NewUserMessage(id, parent, text)
	return m
}

func TestPlannerKeepsTail(t *testing.T) {
	msgs := []protocol.Message{}
	for i := 0; i < 10; i++ {
		msgs = append(msgs, mkMsg(string(rune('a'+i)), "", "message "+string(rune('a'+i))))
	}
	plan := Planner(msgs, 1000)
	if len(plan.CompactionCandidates) == 0 {
		t.Fatal("expected some candidates")
	}
	if plan.KeepFrom == 0 {
		t.Fatal("expected some kept tail")
	}
}

func TestPlannerKeepsMinimumTail(t *testing.T) {
	// With a huge budget, at least the last 4 messages must survive even for
	// short conversations (regression: the walk bound was inverted).
	msgs := []protocol.Message{
		mkMsg("1", "", "hello"),
		mkMsg("2", "", "world"),
		mkMsg("3", "", "foo"),
		mkMsg("4", "", "bar"),
		mkMsg("5", "", "baz"),
	}
	plan := Planner(msgs, 1<<30)
	if tail := len(msgs) - plan.KeepFrom; tail < 4 {
		t.Fatalf("tail = %d messages, want >= 4 (KeepFrom=%d)", tail, plan.KeepFrom)
	}
}

func TestPlannerSmallConversation(t *testing.T) {
	msgs := []protocol.Message{
		mkMsg("1", "", "hello"),
		mkMsg("2", "", "world"),
	}
	plan := Planner(msgs, 1000)
	if plan.KeepFrom != 0 {
		t.Fatalf("small conversation should not compact, KeepFrom=%d", plan.KeepFrom)
	}
}

func TestApplyAppendsSummary(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	for i := 0; i < 6; i++ {
		m := mkMsg(fmtID(i), "", "text "+fmtID(i))
		_ = st.Append(session.Entry{Type: session.EntryMessage, ID: m.ID, Message: &m})
	}
	msgs, _ := st.Messages()
	plan := Planner(msgs, 20)
	res, err := Apply(context.Background(), st, DefaultSummarizer, plan)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if plan.KeepFrom == 0 {
		t.Fatal("expected compaction candidates")
	}
	// Compaction entries are not messages; the tail messages must still load.
	after, _ := st.Messages()
	if len(after) == 0 {
		t.Fatal("tail messages should still load after compaction")
	}
	if res.SummarizedMessages <= 0 {
		t.Fatalf("expected removed messages > 0, got %d", res.SummarizedMessages)
	}
	if res.BeforeEntries <= 0 || res.AfterEntries <= 0 {
		t.Fatal("expected entry counts")
	}
	projected, err := st.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != res.RetainedMessages+1 || projected[0].Role != protocol.RoleCustom {
		t.Fatalf("projected context = %+v, result = %+v", projected, res)
	}
}

func TestDefaultSummarizerBounded(t *testing.T) {
	var msgs []protocol.Message
	for i := 0; i < 50; i++ {
		msgs = append(msgs, mkMsg(fmtID(i), "", "long text "+fmtID(i)))
	}
	s, err := DefaultSummarizer(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) == 0 {
		t.Fatal("expected summary")
	}
}

func fmtID(i int) string {
	return string(rune('a'+i)) + "id"
}
