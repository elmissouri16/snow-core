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

func TestPlannerKeepsToolCallsWithResultsAcrossAutonomousTurns(t *testing.T) {
	assistant := func(id string, stop protocol.StopReason, blocks ...protocol.ContentBlock) protocol.Message {
		return protocol.NewAssistantMessage(id, "", "test", "model", blocks, stop, nil)
	}
	msgs := []protocol.Message{
		mkMsg("user", "", "start"),
		assistant("call-1", protocol.StopToolUse, protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "tc-1", Name: "read"}),
		protocol.NewToolResultMessage("result-1", "", "tc-1", "read", []protocol.ContentBlock{protocol.NewTextBlock("ok")}, false),
		assistant("done-1", protocol.StopStop, protocol.NewTextBlock("done")),
		assistant("call-2", protocol.StopToolUse, protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "tc-2", Name: "read"}),
		protocol.NewToolResultMessage("result-2", "", "tc-2", "read", []protocol.ContentBlock{protocol.NewTextBlock("ok")}, false),
		assistant("done-2", protocol.StopStop, protocol.NewTextBlock("done")),
		assistant("done-3", protocol.StopStop, protocol.NewTextBlock("continued")),
		assistant("done-4", protocol.StopStop, protocol.NewTextBlock("continued")),
	}
	plan := PlannerWithOptions(msgs, PlannerOptions{RetainTokens: 1, MinRetainedTurns: 2})
	if plan.KeepFrom != 7 {
		t.Fatalf("KeepFrom=%d, want complete autonomous turn boundary 7", plan.KeepFrom)
	}
	if msgs[plan.KeepFrom].Role == protocol.RoleTool {
		t.Fatal("retained context begins with an orphan tool result")
	}
}

func TestPlannerTreatsMailboxAsTurnBoundary(t *testing.T) {
	msgs := []protocol.Message{
		mkMsg("user", "", "start"),
		protocol.NewAssistantMessage("a1", "", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
		{ID: "mail-1", Role: protocol.RoleAgent, Content: []protocol.ContentBlock{protocol.NewTextBlock("mail")}},
		protocol.NewAssistantMessage("a2", "", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
		{ID: "mail-2", Role: protocol.RoleAgent, Content: []protocol.ContentBlock{protocol.NewTextBlock("mail")}},
	}
	plan := PlannerWithOptions(msgs, PlannerOptions{RetainTokens: 1, MinRetainedTurns: 2})
	if plan.KeepFrom != 2 {
		t.Fatalf("KeepFrom=%d, want mailbox boundary 2", plan.KeepFrom)
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

func TestApplyResolvesVirtualCompactionBoundary(t *testing.T) {
	store := session.NewMemoryStore(session.Options{})
	for _, id := range []string{"a", "b", "c", "d"} {
		message := mkMsg(id, "", id)
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: id, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	messages, _ := store.Messages()
	first := Plan{KeepFrom: 2, TotalMessages: 4, BoundaryID: "b", CompactionCandidates: messages[:2]}
	if _, err := Apply(context.Background(), store, func(context.Context, []protocol.Message) (string, error) { return "first", nil }, first); err != nil {
		t.Fatal(err)
	}
	projected, err := store.ContextMessages()
	if err != nil || len(projected) != 3 {
		t.Fatalf("first projection=%+v err=%v", projected, err)
	}
	second := Plan{KeepFrom: 1, TotalMessages: len(projected), BoundaryID: projected[0].ID, CompactionCandidates: projected[:1]}
	if _, err := Apply(context.Background(), store, func(context.Context, []protocol.Message) (string, error) { return "second", nil }, second); err != nil {
		t.Fatal(err)
	}
	entries, err := store.BranchEntries()
	if err != nil {
		t.Fatal(err)
	}
	latest := entries[len(entries)-1]
	if latest.Type != session.EntryCompaction || latest.CompactedThrough != "b" {
		t.Fatalf("latest marker=%+v", latest)
	}
	projected, err = store.ContextMessages()
	if err != nil || len(projected) != 3 || projected[1].ID != "c" || projected[2].ID != "d" {
		t.Fatalf("second projection=%+v err=%v", projected, err)
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
