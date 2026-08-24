package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/compact"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func appendCompleteTurns(t *testing.T, store *session.MemoryStore, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		user := protocol.NewUserMessage(fmt.Sprintf("pressure-user-%d", i), "", fmt.Sprintf("old user %d", i))
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: user.ID, Message: &user}); err != nil {
			t.Fatal(err)
		}
		assistant := protocol.NewAssistantMessage(fmt.Sprintf("pressure-assistant-%d", i), user.ID, "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("old answer")}, protocol.StopStop, nil)
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, ParentID: user.ID, Message: &assistant}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOrdinaryTurnAutoCompactsInsideToolChain(t *testing.T) {
	tool := &testTool{name: "read", schema: protocol.ToolSchema{Name: "read", Parameters: []byte(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		return tools.TextResult("tool output")
	}}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 80}}, {Type: protocol.EvStreamToolCallDone, ToolCallID: "read-1", ToolName: "read", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamTextDelta, Text: "summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "done"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, store := setup(t, p, registry, permission.ModeDeny)
	a.model.ContextWindow = 100
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "local", AutoThresholdPercent: 80}
	appendCompleteTurns(t, store, 4)
	if err := a.Prompt(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if p.call != 3 || !strings.Contains(p.requests[1].System, "working-state checkpoint") || p.requests[2].Messages[0].Role != protocol.RoleCustom {
		t.Fatalf("calls=%d requests=%+v", p.call, p.requests)
	}
}

func TestLongActiveToolTurnAutoCompactsCompletedCycles(t *testing.T) {
	tool := &testTool{name: "read", schema: protocol.ToolSchema{Name: "read", Parameters: []byte(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		return tools.TextResult(strings.Repeat("completed-cycle ", 50))
	}}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	toolCall := func(id string) []protocol.StreamEvent {
		return []protocol.StreamEvent{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: id, ToolName: "read", Arguments: []byte(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		}
	}
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		toolCall("active-1"),
		toolCall("active-2"),
		toolCall("active-3"),
		toolCall("active-4"),
		{{Type: protocol.EvStreamTextDelta, Text: "provider checkpoint"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "finished after checkpoint"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, _ := setup(t, p, registry, permission.ModeDeny)
	a.model.ContextWindow = 1000
	a.opts.Compaction = CompactionOptions{
		RetainTokens:                  1,
		MinRetainedTurns:              2,
		SummaryMaxTokens:              128,
		Fallback:                      "local",
		ToolHistoryBudgetPercent:      5,
		HistoricalToolResultThreshold: 8 << 10,
	}
	if err := a.Prompt(context.Background(), "perform one long tool-driven task"); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 6 {
		t.Fatalf("provider requests=%d, want four tool cycles, summary, continuation", len(p.requests))
	}
	if !strings.Contains(p.requests[4].System, compact.WorkingStateTitle) {
		t.Fatalf("fifth request was not compaction summary: %q", p.requests[4].System)
	}
	continued := p.requests[5].Messages
	if len(continued) == 0 || continued[0].Role != protocol.RoleCustom {
		t.Fatalf("continued long turn lacks checkpoint: %+v", continued)
	}
	for _, message := range continued {
		for _, block := range message.Content {
			if block.Type == protocol.BlockToolCall && block.ToolCallID == "active-1" {
				t.Fatalf("old completed cycle remained exact after checkpoint: %+v", continued)
			}
		}
	}
}

func appendToolTurn(t *testing.T, store *session.MemoryStore, index, resultBytes int) {
	t.Helper()
	parent := store.BranchTip()
	user := protocol.NewUserMessage(fmt.Sprintf("tool-user-%d", index), parent, fmt.Sprintf("work item %d", index))
	if err := store.Append(session.Entry{Type: session.EntryMessage, ID: user.ID, ParentID: parent, Message: &user}); err != nil {
		t.Fatal(err)
	}
	callID := fmt.Sprintf("tool-call-%d", index)
	call := protocol.NewAssistantMessage(fmt.Sprintf("tool-assistant-%d", index), user.ID, "scripted", "m1", []protocol.ContentBlock{
		{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read", Arguments: json.RawMessage(`{"path":"file"}`)},
		{Type: protocol.BlockProviderData, Name: fmt.Sprintf("provider-state-%d", index), Data: []byte(fmt.Sprintf(`{"turn":%d}`, index))},
	}, protocol.StopToolUse, nil)
	if err := store.Append(session.Entry{Type: session.EntryMessage, ID: call.ID, ParentID: user.ID, Message: &call}); err != nil {
		t.Fatal(err)
	}
	result := protocol.NewToolResultMessage(fmt.Sprintf("tool-result-%d", index), call.ID, callID, "read", []protocol.ContentBlock{protocol.NewTextBlock(strings.Repeat(fmt.Sprintf("result-%d ", index), resultBytes/9+1))}, false)
	if err := store.Append(session.Entry{Type: session.EntryMessage, ID: result.ID, ParentID: call.ID, Message: &result}); err != nil {
		t.Fatal(err)
	}
	done := protocol.NewAssistantMessage(fmt.Sprintf("tool-done-%d", index), result.ID, "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("recorded")}, protocol.StopStop, nil)
	if err := store.Append(session.Entry{Type: session.EntryMessage, ID: done.ID, ParentID: result.ID, Message: &done}); err != nil {
		t.Fatal(err)
	}
}

func TestAggregateToolHistoryCompactsOldCompleteTurnsWithRetrieval(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamTextDelta, Text: "objective and current file state"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "continued"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, store := setup(t, p, nil, permission.ModeDeny)
	a.model.ContextWindow = 1000
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "local", AutoThresholdPercent: 0, ToolHistoryBudgetPercent: 20, HistoricalToolResultThreshold: 8 << 10}
	artifacts, err := artifact.NewLocalStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	a.opts.Artifacts = artifacts
	for i := 0; i < 4; i++ {
		appendToolTurn(t, store, i, 900)
	}
	var started string
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvCompactionStarted {
			started = event.Message
		}
	})
	if err := a.Prompt(context.Background(), "continue from the retained working state"); err != nil {
		t.Fatal(err)
	}
	if p.call != 2 {
		t.Fatalf("provider calls=%d, want summary plus continued turn", p.call)
	}
	if !strings.Contains(started, "completed tool history reached 20%") {
		t.Fatalf("compaction start=%q", started)
	}
	if !strings.Contains(p.requests[0].System, "# Working State Checkpoint") || !strings.Contains(p.requests[0].System, "## Active tool batch") {
		t.Fatalf("summary contract missing structured checkpoint headings:\n%s", p.requests[0].System)
	}
	continued := p.requests[1].Messages
	if len(continued) == 0 || continued[0].Role != protocol.RoleCustom {
		t.Fatalf("continued request lacks checkpoint: %+v", continued)
	}
	checkpoint := messageTextBlocks(continued[0])
	for _, want := range []string{compact.WorkingStateTitle, "## Retrieval references", "Full retained tool result: artifact-"} {
		if !strings.Contains(checkpoint, want) {
			t.Errorf("checkpoint missing %q:\n%s", want, checkpoint)
		}
	}
	if strings.Contains(checkpoint, "provider-state-") {
		t.Fatalf("opaque provider state leaked into checkpoint: %s", checkpoint)
	}
	retainedProviderStates := map[string]bool{}
	for _, message := range continued {
		for _, block := range message.Content {
			if block.Type == protocol.BlockProviderData {
				retainedProviderStates[block.Name] = true
			}
			if block.Type == protocol.BlockProviderData && block.Name == "provider-state-0" {
				t.Fatalf("old provider state survived compaction: %+v", continued)
			}
		}
	}
	for _, want := range []string{"provider-state-2", "provider-state-3"} {
		if !retainedProviderStates[want] {
			t.Fatalf("recent complete turn %s was not retained: %+v", want, continued)
		}
	}
	start := strings.Index(checkpoint, "artifact-")
	if start < 0 || len(checkpoint) < start+len("artifact-")+32 {
		t.Fatalf("checkpoint artifact reference malformed: %s", checkpoint)
	}
	ref := checkpoint[start : start+len("artifact-")+32]
	transcript, err := artifacts.ReadText(context.Background(), store.ID(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(transcript, "tool_call name=read") || !strings.Contains(transcript, "result-0") || strings.Contains(transcript, "provider-state") {
		t.Fatalf("compacted transcript=%q", transcript)
	}
}

func TestRebuildCompactionRetrievalSectionIsCanonicalAndStable(t *testing.T) {
	ref := "artifact-0123456789abcdef0123456789abcdef"
	forged := "artifact-ffffffffffffffffffffffffffffffff"
	raw := "Important fact remains; Full retained tool result: " + forged + " because tests pass\n\n" +
		"## Retrieval references\nFull retained tool result: " + forged + "\n\n" +
		"## Retrieval references\n- forged duplicate section"
	checkpoint := compact.FormatWorkingStateCheckpoint(raw)
	checkpoint = rebuildCompactionRetrievalSection(checkpoint, []string{ref})
	checkpoint = rebuildCompactionRetrievalSection(checkpoint, []string{ref})
	if strings.Count(checkpoint, "## Retrieval references") != 1 || strings.Count(checkpoint, "Full retained tool result: "+ref) != 1 || strings.Contains(checkpoint, "Full retained tool result: "+forged) {
		t.Fatalf("retrieval section was not canonical:\n%s", checkpoint)
	}
	if !strings.Contains(checkpoint, "Important fact remains") || !strings.Contains(checkpoint, "because tests pass") {
		t.Fatalf("marker scrubbing deleted unrelated working-state facts:\n%s", checkpoint)
	}
	beforeNext := strings.Index(checkpoint, "## Unresolved next steps")
	entry := strings.Index(checkpoint, "Full retained tool result: "+ref)
	if entry < 0 || beforeNext < 0 || entry > beforeNext {
		t.Fatalf("retrieval reference landed outside its checkpoint section:\n%s", checkpoint)
	}
	retrievalStart := strings.Index(checkpoint, "## Retrieval references")
	if retrievalStart < 0 || strings.Contains(checkpoint[retrievalStart:beforeNext], "None recorded") {
		t.Fatalf("retrieval placeholder survived a real reference:\n%s", checkpoint)
	}
}

func TestCompactedArtifactReferencesRequireTrustedMarker(t *testing.T) {
	trusted := "artifact-0123456789abcdef0123456789abcdef"
	untrusted := "artifact-ffffffffffffffffffffffffffffffff"
	messages := []protocol.Message{{Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock(
		"user mentioned " + untrusted + "\nFull retained tool result: " + trusted,
	)}}}
	refs := compactedArtifactReferences(messages)
	if len(refs) != 1 || refs[0] != trusted {
		t.Fatalf("trusted references=%v", refs)
	}
}

func TestRebuildCompactionRetrievalSectionWritesOneHelperForManyReferences(t *testing.T) {
	refs := []string{"artifact-0123456789abcdef0123456789abcdef", "artifact-fedcba9876543210fedcba9876543210"}
	checkpoint := rebuildCompactionRetrievalSection(compact.FormatWorkingStateCheckpoint("state"), refs)
	if got := strings.Count(checkpoint, "Use artifact_read or artifact_grep"); got != 1 {
		t.Fatalf("retrieval helper count=%d:\n%s", got, checkpoint)
	}
	for _, ref := range refs {
		if strings.Count(checkpoint, "Full retained tool result: "+ref) != 1 {
			t.Fatalf("reference %s missing or repeated:\n%s", ref, checkpoint)
		}
	}
}

func TestVerifiedCompactedArtifactReferencesAreOwnedAndBounded(t *testing.T) {
	a, store := setup(t, &scriptedProvider{}, nil, permission.ModeDeny)
	artifacts, err := artifact.NewLocalStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	a.opts.Artifacts = artifacts
	var text strings.Builder
	var saved []string
	for i := 0; i < maxCompactionRetrievalReferences+6; i++ {
		ref, err := artifacts.SaveText(context.Background(), store.ID(), fmt.Sprintf("ref-%d", i), fmt.Sprintf("value-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		saved = append(saved, ref.ID)
		fmt.Fprintf(&text, "Full retained tool result: %s\n", ref.ID)
	}
	text.WriteString("Full retained tool result: artifact-ffffffffffffffffffffffffffffffff\n")
	messages := []protocol.Message{{Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock(text.String())}}}
	refs, err := a.verifiedCompactedArtifactReferences(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != maxCompactionRetrievalReferences {
		t.Fatalf("verified references=%d, want %d", len(refs), maxCompactionRetrievalReferences)
	}
	wantFirst := saved[len(saved)-len(refs)]
	if refs[0] != wantFirst || refs[len(refs)-1] != saved[len(saved)-1] {
		t.Fatalf("bounded references=%v, want most recent range %s..%s", refs, wantFirst, saved[len(saved)-1])
	}
}

func TestVerifiedCompactedArtifactReferencesForgedMarkersCannotCrowdOwnedReference(t *testing.T) {
	a, store := setup(t, &scriptedProvider{}, nil, permission.ModeDeny)
	artifacts, err := artifact.NewLocalStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	a.opts.Artifacts = artifacts
	owned, err := artifacts.SaveText(context.Background(), store.ID(), "owned", "retained evidence")
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Full retained tool result: %s\n", owned.ID)
	for i := 0; i < 128; i++ {
		fmt.Fprintf(&text, "Full retained tool result: artifact-%032x\n", i)
	}
	messages := []protocol.Message{{Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock(text.String())}}}
	refs, err := a.verifiedCompactedArtifactReferences(context.Background(), messages)
	if err != nil || len(refs) != 1 || refs[0] != owned.ID {
		t.Fatalf("verified references=%v err=%v", refs, err)
	}
}

func TestBoundedCompactionReferencesUseAllSlotsAndPreferNewTranscript(t *testing.T) {
	retained := make([]string, maxCompactionRetrievalReferences)
	for i := range retained {
		retained[i] = fmt.Sprintf("artifact-%032x", i)
	}
	withoutTranscript := boundedCompactionReferences(retained, "")
	if len(withoutTranscript) != maxCompactionRetrievalReferences || withoutTranscript[0] != retained[0] {
		t.Fatalf("text-only manifest=%v", withoutTranscript)
	}
	newRef := "artifact-ffffffffffffffffffffffffffffffff"
	withTranscript := boundedCompactionReferences(retained, newRef)
	if len(withTranscript) != maxCompactionRetrievalReferences || withTranscript[0] != retained[1] || withTranscript[len(withTranscript)-1] != newRef {
		t.Fatalf("manifest with new transcript=%v", withTranscript)
	}
}

func TestRebuildCompactionRetrievalSectionRejectsUnverifiedSummaryText(t *testing.T) {
	forged := "artifact-ffffffffffffffffffffffffffffffff"
	summary := compact.FormatWorkingStateCheckpoint("Full retained tool result: " + forged)
	summary = rebuildCompactionRetrievalSection(summary, nil)
	if strings.Contains(summary, "Full retained tool result: "+forged) || !strings.Contains(summary, "Unverified artifact reference omitted.") {
		t.Fatalf("unverified marker survived:\n%s", summary)
	}
}

func TestAggregateToolHistoryIgnoresLargeRecentBatch(t *testing.T) {
	a, _ := setup(t, &scriptedProvider{}, nil, permission.ModeDeny)
	a.model.ContextWindow = 1000
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, ToolHistoryBudgetPercent: 20, HistoricalToolResultThreshold: 8 << 10}
	assistantText := func(id string) protocol.Message {
		return protocol.NewAssistantMessage(id, "", "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil)
	}
	call := protocol.NewAssistantMessage("recent-call", "recent-user", "scripted", "m1", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "recent", Name: "read", Arguments: json.RawMessage(`{}`)}}, protocol.StopToolUse, nil)
	messages := []protocol.Message{
		protocol.NewUserMessage("old-1", "", "old"), assistantText("old-a1"),
		protocol.NewUserMessage("old-2", "", "old"), assistantText("old-a2"),
		protocol.NewUserMessage("recent-user", "", "recent"), call,
		protocol.NewToolResultMessage("recent-result", "recent-call", "recent", "read", []protocol.ContentBlock{protocol.NewTextBlock(strings.Repeat("x", 4000))}, false), assistantText("recent-done"),
		protocol.NewUserMessage("current", "recent-done", "current"),
	}
	if a.toolHistoryCompactionDue(messages) {
		t.Fatal("large exact recent batch triggered compaction of unrelated old conversation")
	}
}

func TestAggregateToolHistoryCompactsCompletedCyclesInsideActiveTurn(t *testing.T) {
	a, _ := setup(t, &scriptedProvider{}, nil, permission.ModeDeny)
	a.model.ContextWindow = 1000
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, ToolHistoryBudgetPercent: 5, HistoricalToolResultThreshold: 8 << 10}
	messages := []protocol.Message{protocol.NewUserMessage("active-user", "", "long-running objective")}
	for i := 0; i < 6; i++ {
		callID := fmt.Sprintf("active-call-%d", i)
		assistant := protocol.NewAssistantMessage(fmt.Sprintf("active-assistant-%d", i), "", "scripted", "m1", []protocol.ContentBlock{
			{Type: protocol.BlockProviderData, Name: fmt.Sprintf("active-state-%d", i), Data: []byte(`{"opaque":true}`)},
			{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read", Arguments: json.RawMessage(`{"path":"large-file"}`)},
		}, protocol.StopToolUse, nil)
		messages = append(messages, assistant)
		messages = append(messages, protocol.NewToolResultMessage(fmt.Sprintf("active-result-%d", i), assistant.ID, callID, "read", []protocol.ContentBlock{protocol.NewTextBlock(strings.Repeat("result ", 80))}, false))
	}
	plan := compact.PlannerWithOptions(messages, a.compactionPlannerOptions(a.Model(), messages))
	if len(plan.CompactionCandidates) == 0 || !a.toolHistoryCompactionDue(messages) {
		t.Fatalf("active tool cycles were not compactable under aggregate pressure: %+v", plan)
	}
	if messages[plan.KeepFrom].Role != protocol.RoleAssistant {
		t.Fatalf("active compacted tail begins with %s, want assistant call", messages[plan.KeepFrom].Role)
	}
	retainedState := false
	for _, block := range messages[plan.KeepFrom].Content {
		retainedState = retainedState || block.Type == protocol.BlockProviderData
	}
	if !retainedState {
		t.Fatal("retained provider state was detached from its complete owning cycle")
	}
}

func TestAggregateToolHistoryCountsMultimodalToolResults(t *testing.T) {
	a, _ := setup(t, &scriptedProvider{}, nil, permission.ModeDeny)
	a.model.ContextWindow = 5000
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, ToolHistoryBudgetPercent: 20, HistoricalToolResultThreshold: 8 << 10}
	call := protocol.NewAssistantMessage("image-call", "image-user", "scripted", "m1", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "image", Name: "inspect", Arguments: json.RawMessage(`{}`)}}, protocol.StopToolUse, nil)
	messages := []protocol.Message{
		protocol.NewUserMessage("image-user", "", "inspect"), call,
		protocol.NewToolResultMessage("image-result", "image-call", "image", "inspect", []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/webp", Data: []byte("unknown dimensions")}}, false),
		protocol.NewAssistantMessage("image-done", "image-result", "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
		protocol.NewUserMessage("recent-1", "image-done", "recent"), protocol.NewAssistantMessage("recent-a1", "recent-1", "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
		protocol.NewUserMessage("recent-2", "recent-a1", "recent"), protocol.NewAssistantMessage("recent-a2", "recent-2", "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
	}
	if !a.toolHistoryCompactionDue(messages) {
		t.Fatal("old multimodal tool result did not contribute to aggregate pressure")
	}
}

func TestAggregateToolHistoryMeasuresPrunedProjection(t *testing.T) {
	a, _ := setup(t, &scriptedProvider{}, nil, permission.ModeDeny)
	a.model.ContextWindow = 10000
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, ToolHistoryBudgetPercent: 20, HistoricalToolResultThreshold: 8 << 10}
	call := protocol.NewAssistantMessage("old-call", "old-user", "scripted", "m1", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "old", Name: "read", Arguments: json.RawMessage(`{}`)}}, protocol.StopToolUse, nil)
	messages := []protocol.Message{
		protocol.NewUserMessage("old-user", "", "old"), call,
		protocol.NewToolResultMessage("old-result", "old-call", "old", "read", []protocol.ContentBlock{protocol.NewTextBlock(strings.Repeat("x", 100000))}, false),
		protocol.NewAssistantMessage("old-done", "old-result", "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
		protocol.NewUserMessage("recent-1", "old-done", "recent"), protocol.NewAssistantMessage("recent-a1", "recent-1", "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
		protocol.NewUserMessage("recent-2", "recent-a1", "recent"), protocol.NewAssistantMessage("recent-a2", "recent-2", "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil),
	}
	if a.toolHistoryCompactionDue(messages) {
		t.Fatal("raw durable oversized result triggered aggregate compaction despite bounded provider projection")
	}
}

func TestCompactionWarnsWhenToolTranscriptCannotBePersisted(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "checkpoint"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, store := setup(t, p, nil, permission.ModeDeny)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "local"}
	artifacts, err := artifact.NewLocalStore(t.TempDir(), 128)
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	a.opts.Artifacts = artifacts
	for i := 0; i < 4; i++ {
		appendToolTurn(t, store, i, 900)
	}
	messages, err := a.contextMessagesCurrent()
	if err != nil {
		t.Fatal(err)
	}
	plan := compact.PlannerWithOptions(messages, a.compactionPlannerOptions(a.Model(), messages))
	if estimateToolHistoryTokens(plan.CompactionCandidates) == 0 {
		t.Fatalf("test setup produced no compactable tool history: %+v", plan)
	}
	if _, err := a.saveCompactedToolTranscript(context.Background(), plan.CompactionCandidates, plan.BoundaryID); err == nil {
		t.Fatal("undersized artifact store unexpectedly accepted compacted transcript")
	}
	var doneMessage string
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvCompactionDone {
			doneMessage = event.Message
		}
	})
	result, err := a.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result.SummarizedMessages == 0 || !strings.Contains(doneMessage, "exact compacted tool transcript is unavailable") {
		t.Fatalf("result=%+v done message=%q", result, doneMessage)
	}
	if strings.Contains(result.Summary, "Full retained tool result:") {
		t.Fatalf("failed transcript was advertised as retrievable:\n%s", result.Summary)
	}
}

func TestOversizedToolResultSpillsToPrivateArtifact(t *testing.T) {
	full := strings.Repeat("head", 5000) + " NEEDLE " + strings.Repeat("tail", 5000)
	tool := &testTool{name: "read", schema: protocol.ToolSchema{Name: "read", Parameters: []byte(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		return tools.TextResult(full)
	}}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "large", ToolName: "read", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamTextDelta, Text: "done"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, store := setup(t, p, registry, permission.ModeDeny)
	artifacts, err := artifact.NewLocalStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	a.opts.Artifacts = artifacts
	a.opts.Compaction.ToolResultInlineBytes = 4096
	if err := a.Prompt(context.Background(), "read it"); err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	var preview string
	for _, message := range messages {
		if message.Role == protocol.RoleTool {
			preview = message.Content[0].Text
		}
	}
	if len(preview) >= len(full) || !strings.Contains(preview, "bytes omitted") || !strings.Contains(preview, "artifact-") {
		t.Fatalf("preview bytes=%d text=%q", len(preview), preview)
	}
	start := strings.Index(preview, "artifact-")
	ref := preview[start : start+len("artifact-")+32]
	got, err := artifacts.ReadText(context.Background(), store.ID(), ref)
	if err != nil || got != full {
		t.Fatalf("artifact bytes=%d err=%v", len(got), err)
	}
}

func TestContextOverflowCompactsAndRetriesOnce(t *testing.T) {
	overflow := errors.New("maximum context length exceeded")
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamError, Err: overflow}},
		{{Type: protocol.EvStreamTextDelta, Text: "summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "recovered"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, store := setup(t, p, nil, permission.ModeDeny)
	a.model.ContextWindow = 100
	a.opts.MaxTurns = 1
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "local", AutoThresholdPercent: 80}
	appendCompleteTurns(t, store, 4)
	var errorsSeen int
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvError {
			errorsSeen++
		}
	})
	if err := a.Prompt(context.Background(), "recover"); err != nil {
		t.Fatal(err)
	}
	if p.call != 3 || p.requests[2].Messages[0].Role != protocol.RoleCustom || errorsSeen != 0 {
		t.Fatalf("calls=%d errors=%d retry=%+v", p.call, errorsSeen, p.requests)
	}
	for requestIndex, request := range p.requests[1:] {
		for _, message := range request.Messages {
			if message.Role == protocol.RoleAssistant && message.StopReason == protocol.StopError {
				t.Fatalf("failed overflow response leaked into request %d: %+v", requestIndex+1, request.Messages)
			}
		}
	}
	durable, durableErr := store.Messages()
	if durableErr != nil {
		t.Fatal(durableErr)
	}
	foundFailedAttempt := false
	for _, message := range durable {
		if message.Role == protocol.RoleAssistant && message.StopReason == protocol.StopError {
			foundFailedAttempt = true
		}
	}
	if !foundFailedAttempt {
		t.Fatalf("overflow failure was not retained durably: %+v", durable)
	}
	if !provider.IsContextWindowExceeded(overflow) {
		t.Fatal("overflow classifier rejected known diagnostic")
	}
}
