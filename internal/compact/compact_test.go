package compact

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
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
	plan := PlannerWithOptions(msgs, PlannerOptions{RetainTokens: 1000, MinRetainedTurns: 2})
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
	plan := PlannerWithOptions(msgs, PlannerOptions{RetainTokens: 1 << 30, MinRetainedTurns: 2})
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

func TestPlannerCompactsCompletedCyclesInsideActiveToolTurn(t *testing.T) {
	messages := []protocol.Message{mkMsg("user", "", "long-running objective")}
	for i := 0; i < 5; i++ {
		callID := fmtID(i) + "-call"
		assistant := protocol.NewAssistantMessage(fmtID(i)+"-assistant", "", "test", "model", []protocol.ContentBlock{
			{Type: protocol.BlockProviderData, Name: fmtID(i) + "-state", Data: []byte(`{"opaque":true}`)},
			{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read", Arguments: json.RawMessage(`{"path":"file"}`)},
		}, protocol.StopToolUse, nil)
		messages = append(messages, assistant)
		messages = append(messages, protocol.NewToolResultMessage(fmtID(i)+"-result", "", callID, "read", []protocol.ContentBlock{protocol.NewTextBlock("complete")}, false))
	}
	withoutCycles := PlannerWithOptions(messages, PlannerOptions{RetainTokens: 1, MinRetainedTurns: 2})
	if len(withoutCycles.CompactionCandidates) != 0 {
		t.Fatalf("ordinary turn planning split an active turn: %+v", withoutCycles)
	}
	plan := PlannerWithOptions(messages, PlannerOptions{RetainTokens: 1, MinRetainedTurns: 2, AllowActiveToolCycles: true})
	if len(plan.CompactionCandidates) == 0 || plan.BoundaryID != "cid-result" {
		t.Fatalf("active-cycle plan=%+v", plan)
	}
	if messages[plan.KeepFrom].Role != protocol.RoleAssistant || !toolPairingBalancedAt(messages, plan.KeepFrom) {
		t.Fatalf("unsafe active-cycle cut at %d", plan.KeepFrom)
	}
	for _, message := range plan.CompactionCandidates {
		if message.Role == protocol.RoleAssistant {
			foundCall := false
			for _, block := range message.Content {
				foundCall = foundCall || block.Type == protocol.BlockToolCall
			}
			if !foundCall {
				t.Fatalf("provider state was detached from its owning assistant: %+v", message)
			}
		}
	}
}

func TestPlannerActiveCyclesDoNotConsumeRecentTurnFloor(t *testing.T) {
	assistantText := func(id string) protocol.Message {
		return protocol.NewAssistantMessage(id, "", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil)
	}
	messages := []protocol.Message{
		mkMsg("old-user-1", "", "old one"), assistantText("old-assistant-1"),
		mkMsg("old-user-2", "", "old two"), assistantText("old-assistant-2"),
		mkMsg("active-user", "", "active objective"),
	}
	for i := 0; i < 5; i++ {
		callID := fmtID(i) + "-active-call"
		messages = append(messages,
			protocol.NewAssistantMessage(fmtID(i)+"-active-assistant", "", "test", "model", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read"}}, protocol.StopToolUse, nil),
			protocol.NewToolResultMessage(fmtID(i)+"-active-result", "", callID, "read", []protocol.ContentBlock{protocol.NewTextBlock("complete")}, false),
		)
	}
	plan := PlannerWithOptions(messages, PlannerOptions{RetainTokens: 1, MinRetainedTurns: 3, AllowActiveToolCycles: true})
	if len(plan.CompactionCandidates) != 0 {
		t.Fatalf("active cycles consumed the exact recent-turn floor: %+v", plan)
	}
}

func TestPlannerCheckpointProjectionKeepsRetainedTerminalGoalTurn(t *testing.T) {
	messages := []protocol.Message{
		{ID: "checkpoint", Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock("working state")}},
		protocol.NewAssistantMessage("retained-goal", "checkpoint", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("prior exact goal result")}, protocol.StopStop, nil),
	}
	for i := 0; i < 5; i++ {
		callID := fmtID(i) + "-goal-call"
		messages = append(messages,
			protocol.NewAssistantMessage(fmtID(i)+"-goal-assistant", "", "test", "model", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read"}}, protocol.StopToolUse, nil),
			protocol.NewToolResultMessage(fmtID(i)+"-goal-result", "", callID, "read", []protocol.ContentBlock{protocol.NewTextBlock("complete")}, false),
		)
	}
	plan := PlannerWithOptions(messages, PlannerOptions{RetainTokens: 1, MinRetainedTurns: 3, AllowActiveToolCycles: true})
	if len(plan.CompactionCandidates) != 0 {
		t.Fatalf("checkpoint projection compacted retained terminal goal turn: %+v", plan)
	}
}

func TestPlannerCheckpointProjectionCompactsFreshParentedActiveTurn(t *testing.T) {
	checkpoint := protocol.Message{ID: "compaction-marker", Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock("working state")}}
	messages := []protocol.Message{
		checkpoint,
		protocol.NewUserMessage("active-user", "marker", "fresh long objective"),
	}
	for i := 0; i < 5; i++ {
		callID := fmtID(i) + "-fresh-call"
		messages = append(messages,
			protocol.NewAssistantMessage(fmtID(i)+"-fresh-assistant", "", "test", "model", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read"}}, protocol.StopToolUse, nil),
			protocol.NewToolResultMessage(fmtID(i)+"-fresh-result", "", callID, "read", []protocol.ContentBlock{protocol.NewTextBlock("complete")}, false),
		)
	}
	plan := PlannerWithOptions(messages, PlannerOptions{RetainTokens: 1, MinRetainedTurns: 3, AllowActiveToolCycles: true})
	if len(plan.CompactionCandidates) == 0 || plan.KeepFrom <= 1 || !toolPairingBalancedAt(messages, plan.KeepFrom) {
		t.Fatalf("fresh parented active turn was not safely compactable: %+v", plan)
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
	plan := PlannerWithOptions(msgs, PlannerOptions{RetainTokens: 1000, MinRetainedTurns: 2})
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
	plan := PlannerWithOptions(msgs, PlannerOptions{RetainTokens: 20, MinRetainedTurns: 2})
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

func TestToolPairingAllowsProviderScopedIDReuseAcrossCompleteTurns(t *testing.T) {
	call := func(id, parent string) protocol.Message {
		return protocol.NewAssistantMessage(id, parent, "test", "model", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "reused", Name: "read"}}, protocol.StopToolUse, nil)
	}
	msgs := []protocol.Message{
		mkMsg("u1", "", "first"), call("a1", "u1"), protocol.NewToolResultMessage("r1", "a1", "reused", "read", []protocol.ContentBlock{protocol.NewTextBlock("one")}, false),
		protocol.NewAssistantMessage("d1", "r1", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
		mkMsg("u2", "d1", "second"), call("a2", "u2"), protocol.NewToolResultMessage("r2", "a2", "reused", "read", []protocol.ContentBlock{protocol.NewTextBlock("two")}, false),
		protocol.NewAssistantMessage("d2", "r2", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
		mkMsg("u3", "d2", "third"), protocol.NewAssistantMessage("d3", "u3", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
		mkMsg("u4", "d3", "fourth"),
	}
	if !toolPairingBalancedAt(msgs, 8) {
		t.Fatal("complete turns with provider-scoped reused call IDs were rejected")
	}
	plan := PlannerWithOptions(msgs, PlannerOptions{RetainTokens: 1, MinRetainedTurns: 2})
	if plan.KeepFrom == 0 || len(plan.CompactionCandidates) == 0 {
		t.Fatalf("reused complete call IDs disabled compaction: %+v", plan)
	}
}

func TestPlannerRejectsToolPairingCut(t *testing.T) {
	call := protocol.NewAssistantMessage("call", "u1", "test", "model", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "tc", Name: "read"}}, protocol.StopToolUse, nil)
	msgs := []protocol.Message{
		mkMsg("u1", "", "start"),
		call,
		mkMsg("u2", "call", "malformed boundary"),
		protocol.NewToolResultMessage("result", "u2", "tc", "read", []protocol.ContentBlock{protocol.NewTextBlock("late")}, false),
		mkMsg("u3", "result", "third"),
	}
	if toolPairingBalancedAt(msgs, 2) {
		t.Fatal("pairing validator accepted a call/result split")
	}
	plan := PlannerWithOptions(msgs, PlannerOptions{RetainTokens: 1, MinRetainedTurns: 2})
	if plan.KeepFrom != 0 || len(plan.CompactionCandidates) != 0 {
		t.Fatalf("malformed tool history produced a compaction plan: %+v", plan)
	}
}

func TestFormatWorkingStateCheckpointFillsRequiredSections(t *testing.T) {
	checkpoint := FormatWorkingStateCheckpoint("current fact")
	if !strings.HasPrefix(checkpoint, WorkingStateTitle) || !strings.Contains(checkpoint, "## Current working state\ncurrent fact") {
		t.Fatalf("checkpoint wrapper=%q", checkpoint)
	}
	for _, section := range WorkingStateSections {
		if !strings.Contains(checkpoint, "## "+section) {
			t.Errorf("checkpoint missing section %q:\n%s", section, checkpoint)
		}
	}
}

func TestNormalizeWorkingStateCheckpointRepairsCriticalSections(t *testing.T) {
	messages := []protocol.Message{
		protocol.NewUserMessage("u", "", "Preserve ticket LANTERN-47 and prioritize correctness."),
		{ID: "tool", Role: protocol.RoleTool, ToolName: "bash", ToolCallID: "call", IsError: true, Content: []protocol.ContentBlock{protocol.NewTextBlock("go test ./... failed")}},
		{ID: "prior", Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock("Working-state checkpoint for compacted history: prior decision cobalt")}},
	}
	summary := WorkingStateTitle + "\n\n## Current working state\nImplementation exists."
	got, repaired, err := NormalizeWorkingStateCheckpoint(context.Background(), summary, messages)
	if err != nil {
		t.Fatal(err)
	}
	if repaired {
		t.Fatal("section augmentation should not report full fallback")
	}
	for _, want := range []string{"LANTERN-47", "go test ./... failed", "prior decision cobalt"} {
		if !strings.Contains(got, want) {
			t.Fatalf("normalized checkpoint missing %q:\n%s", want, got)
		}
	}
}

func TestNormalizeWorkingStateCheckpointCanonicalizesDuplicateVerificationSections(t *testing.T) {
	messages := []protocol.Message{{
		ID: "failed", Role: protocol.RoleTool, ToolName: "bash", ToolCallID: "verify", IsError: true,
		Content: []protocol.ContentBlock{protocol.NewTextBlock("go test ./... failed: compile error")},
	}}
	summary := WorkingStateTitle + `

## Commands and verification
- provider claimed clean

## Commands and verification
- ALL CHECKS PASSED

## Errors and failed approaches appendix
- appendix evidence

## Errors and failed approaches
- None recorded.

## Errors and failed approaches
- no failures

## Decisions and rationale
- first unique decision

## Decisions and rationale
- second unique decision`
	got, fallback, err := NormalizeWorkingStateCheckpoint(context.Background(), summary, messages)
	if err != nil {
		t.Fatal(err)
	}
	if fallback {
		t.Fatal("duplicate canonicalization unexpectedly used full fallback")
	}
	for _, heading := range []string{"Commands and verification", "Errors and failed approaches", "Decisions and rationale"} {
		exact := "\n## " + heading + "\n"
		if strings.Count(got, exact) != 1 {
			t.Fatalf("heading %q count=%d:\n%s", heading, strings.Count(got, exact), got)
		}
	}
	for _, want := range []string{
		"ALL CHECKS PASSED",
		"go test ./... failed: compile error",
		"Deterministic verification status: failures were recorded",
		"## Errors and failed approaches appendix",
		"first unique decision",
		"second unique decision",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("canonical checkpoint lost %q:\n%s", want, got)
		}
	}
}

func TestNormalizeWorkingStateCheckpointSanitizesMarkupFromPriorCheckpoint(t *testing.T) {
	prior := WorkingStateTitle + `

## Objective and constraints
- preserve FACT-BEFORE-MARKUP

## Current working state
<｜DSML｜tool_calls><｜DSML｜invoke name="bash">dangerous command

## Unresolved next steps
- preserve FACT-AFTER-MARKUP`
	messages := []protocol.Message{{ID: "prior", Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock(prior)}}}
	got, fallback, err := NormalizeWorkingStateCheckpoint(context.Background(), WorkingStateTitle+"\n\n## Current working state\n- continue safely", messages)
	if err != nil {
		t.Fatal(err)
	}
	if fallback {
		t.Fatal("provider summary itself was valid; prior-state sanitization should not report full fallback")
	}
	if containsProviderToolMarkup(got) || !strings.Contains(got, "FACT-BEFORE-MARKUP") || !strings.Contains(got, "FACT-AFTER-MARKUP") {
		t.Fatalf("prior checkpoint markup was not safely sanitized:\n%s", got)
	}
}

func TestNormalizeWorkingStateCheckpointRejectsProviderToolMarkup(t *testing.T) {
	messages := []protocol.Message{protocol.NewUserMessage("u", "", "objective lantern")}
	got, repaired, err := NormalizeWorkingStateCheckpoint(context.Background(), "<｜DSML｜tool_calls><｜DSML｜invoke name=\"bash\">", messages)
	if err != nil {
		t.Fatal(err)
	}
	if !repaired || strings.Contains(strings.ToLower(got), "dsml") || !strings.Contains(got, "objective lantern") {
		t.Fatalf("markup fallback repaired=%v:\n%s", repaired, got)
	}
}

func TestDefaultSummarizerExtractsCleanPathFromToolArguments(t *testing.T) {
	msgs := []protocol.Message{protocol.NewAssistantMessage("call", "u", "test", "model", []protocol.ContentBlock{{
		Type: protocol.BlockToolCall, ToolCallID: "read", Name: "read", Arguments: json.RawMessage(`{"path":"internal/compact/compact.go"}`),
	}}, protocol.StopToolUse, nil)}
	summary, err := DefaultSummarizer(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "- internal/compact/compact.go") || strings.Contains(summary, `path\":\"internal/compact/compact.go`) {
		t.Fatalf("tool argument path extraction was malformed:\n%s", summary)
	}
}

func TestDefaultSummarizerPreservesPriorCheckpointAndAgentUpdate(t *testing.T) {
	msgs := []protocol.Message{
		{Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock("prior fact artifact-0123456789abcdef0123456789abcdef")}},
		{Role: protocol.RoleAgent, Content: []protocol.ContentBlock{protocol.NewTextBlock("reviewer found unresolved race")}},
		mkMsg("u", "", "preserve constraints"),
		protocol.NewAssistantMessage("a", "u", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("changed `internal/agent/agent.go` in `compactActiveContext`")}, protocol.StopStop, nil),
	}
	summary, err := DefaultSummarizer(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{WorkingStateTitle, "Prior working-state checkpoints", "prior fact artifact-0123456789abcdef0123456789abcdef", "Attributed agent updates", "reviewer found unresolved race", "Files and symbols", "internal/agent/agent.go", "compactActiveContext", "Active tool batch"} {
		if !strings.Contains(summary, want) {
			t.Errorf("fallback checkpoint missing %q:\n%s", want, summary)
		}
	}
	retrievalStart := strings.Index(summary, "## Retrieval references")
	retrievalEnd := strings.Index(summary, "## Unresolved next steps")
	if retrievalStart < 0 || retrievalEnd < retrievalStart || strings.Contains(summary[retrievalStart:retrievalEnd], "artifact-") {
		t.Fatalf("unverified bare artifact token was promoted to retrieval evidence:\n%s", summary)
	}
}

func TestDeduplicateCheckpointBulletsPreservesSemanticSections(t *testing.T) {
	checkpoint := WorkingStateTitle + `

## Current working state
- shared fact
- shared fact

## Commands and verification
- shared fact
- shared fact`
	got := deduplicateCheckpointBullets(checkpoint)
	if strings.Count(got, "- shared fact") != 2 {
		t.Fatalf("cross-section fact was lost or within-section duplicate survived:\n%s", got)
	}
}

func TestDefaultSummarizerDoesNotRepeatEvidencePayloads(t *testing.T) {
	assistant := protocol.NewAssistantMessage("assistant", "user", "test", "m", []protocol.ContentBlock{protocol.NewTextBlock("implementation is partially complete")}, protocol.StopToolUse, nil)
	assistant.Content = append(assistant.Content, protocol.ContentBlock{Type: protocol.BlockToolCall, Name: "bash", ToolCallID: "call-1", Arguments: json.RawMessage(`{"command":"go test ./..."}`)})
	tool := protocol.NewToolResultMessage("tool", "assistant", "call-1", "bash", []protocol.ContentBlock{protocol.NewTextBlock("FAIL package example")}, true)
	messages := []protocol.Message{assistant, tool}
	for i := range 10 {
		messages = append(messages, protocol.NewToolResultMessage("later-"+fmtID(i), "assistant", "later-call-"+fmtID(i), "bash", []protocol.ContentBlock{protocol.NewTextBlock("PASS later check " + fmtID(i) + " " + strings.Repeat("x", 600))}, false))
	}
	summary, err := DefaultSummarizer(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(summary, "FAIL package example"); got != 1 {
		t.Fatalf("failure payload repeated %d times:\n%s", got, summary)
	}
	if got := strings.Count(summary, "implementation is partially complete"); got != 1 {
		t.Fatalf("assistant evidence repeated %d times:\n%s", got, summary)
	}
	if !strings.Contains(summary, "See Commands and verification entry bash/call-1") {
		t.Fatalf("failure reference missing:\n%s", summary)
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
