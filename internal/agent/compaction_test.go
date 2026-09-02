package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/compact"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestManualCompactUsesSummaryAndPreservesHistory(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "model summary"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	for i := range 6 {
		msg := protocol.NewUserMessage(fmt.Sprintf("msg-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	beforeStats, err := a.RunStats()
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, compact.WorkingStateTitle) || !strings.Contains(result.Summary, "model summary") || result.UsedFallback || result.SummarizedMessages == 0 {
		t.Fatalf("compact result = %+v", result)
	}
	if len(prov.requests) != 1 || len(prov.requests[0].Tools) != 0 || len(prov.requests[0].Messages) != result.SummarizedMessages {
		t.Fatalf("summary request = %+v", prov.requests)
	}
	full, err := st.Messages()
	if err != nil || len(full) != 6 {
		t.Fatalf("full history = %d, err=%v", len(full), err)
	}
	projected, err := st.ContextMessages()
	if err != nil || len(projected) != result.RetainedMessages+1 || projected[0].Role != protocol.RoleCustom {
		t.Fatalf("projected context = %+v, result=%+v, err=%v", projected, result, err)
	}
	afterStats, statsErr := a.RunStats()
	if statsErr != nil || afterStats != beforeStats {
		t.Fatalf("manual compaction changed run stats from %+v to %+v (err=%v)", beforeStats, afterStats, statsErr)
	}
}

func TestManualCompactUsesCompletedGoalCyclesAfterPriorTurn(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "model summary"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, store := setup(t, prov, nil, permission.ModeDeny)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "local"}
	priorUser := protocol.NewUserMessage("prior-user", "", "start the goal")
	priorAssistant := protocol.NewAssistantMessage("prior-assistant", priorUser.ID, "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("goal started")}, protocol.StopStop, nil)
	for _, message := range []protocol.Message{priorUser, priorAssistant} {
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 3 {
		callID := fmt.Sprintf("goal-call-%d", i)
		assistant := protocol.NewAssistantMessage(fmt.Sprintf("goal-assistant-%d", i), "", "scripted", "m1", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read"}}, protocol.StopToolUse, nil)
		for _, message := range []protocol.Message{
			assistant,
			protocol.NewToolResultMessage(fmt.Sprintf("goal-result-%d", i), assistant.ID, callID, "read", []protocol.ContentBlock{protocol.NewTextBlock("complete")}, false),
		} {
			if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
				t.Fatal(err)
			}
		}
	}
	terminal := protocol.NewAssistantMessage("goal-terminal", "", "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("turn complete")}, protocol.StopStop, nil)
	if err := store.Append(session.Entry{Type: session.EntryMessage, ID: terminal.ID, Message: &terminal}); err != nil {
		t.Fatal(err)
	}

	result, err := a.Compact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.SummarizedMessages != 4 || len(prov.requests) != 1 || len(prov.requests[0].Messages) != 4 {
		t.Fatalf("manual goal-cycle compaction result=%+v requests=%+v", result, prov.requests)
	}
	projected, err := store.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 6 || projected[0].Role != protocol.RoleCustom || projected[1].ID != "goal-assistant-1" {
		t.Fatalf("manual goal-cycle projection=%+v", projected)
	}
}

func TestManualCompactPublishesSessionUpdateBeforeTerminalDone(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "model summary"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	for i := range 6 {
		msg := protocol.NewUserMessage(fmt.Sprintf("ordered-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	var events []protocol.AgentEvent
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvSessionUpdated || event.Type == protocol.EvCompactionDone {
			events = append(events, event.Clone())
		}
	})
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.bus.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != protocol.EvSessionUpdated || events[1].Type != protocol.EvCompactionDone {
		t.Fatalf("compaction lifecycle order=%+v", events)
	}
	if events[0].TurnID == "" || events[0].TurnID != events[1].TurnID || events[1].TurnOrigin != "compact" {
		t.Fatalf("compaction lifecycle identity=%+v", events)
	}
}

func TestManualCompactPersistsMailboxArrivingDuringSummary(t *testing.T) {
	provider := &blockingSummaryProvider{started: make(chan struct{}), release: make(chan struct{})}
	a, store := setup(t, provider, nil, permission.ModeDeny)
	for i := range 6 {
		message := protocol.NewUserMessage(fmt.Sprintf("mailbox-%d", i), "", fmt.Sprintf("message %d", i))
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	completed := make(chan error, 1)
	go func() {
		_, err := a.Compact(context.Background())
		completed <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("compaction summary did not start")
	}
	envelope := protocol.AgentMessage{ID: "mail-during-compact", Author: protocol.AgentPath("/root/child"), Recipient: protocol.RootAgentPath, Kind: protocol.AgentMessageNormal, Content: "important mail", CreatedAt: time.Now().UnixMilli()}
	if err := a.EnqueueMailbox(envelope); err != nil {
		t.Fatal(err)
	}
	close(provider.release)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != protocol.RoleAgent || !strings.Contains(messages[len(messages)-1].Content[0].Text, "important mail") {
		t.Fatalf("mail was not persisted after compaction: %+v", messages)
	}
}

func TestManualCompactConfiguredGuidanceAndBudgetReachProvider(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamTextDelta, Text: "summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 333, Fallback: "error", Guidance: "Preserve ticket IDs."}
	for i := range 6 {
		msg := protocol.NewUserMessage(fmt.Sprintf("configured-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) != 1 || prov.requests[0].MaxTokens != 333 || !strings.Contains(prov.requests[0].System, "Preserve ticket IDs") {
		t.Fatalf("request=%+v", prov.requests)
	}
}

func TestRepeatedCompactionSummarizesProjectedContext(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamTextDelta, Text: "first summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}, {{Type: protocol.EvStreamTextDelta, Text: "second summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 500, Fallback: "error"}
	for i := range 6 {
		msg := protocol.NewUserMessage(fmt.Sprintf("repeat-a-%d", i), "", fmt.Sprintf("first %d", i))
		_ = st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg})
	}
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		msg := protocol.NewUserMessage(fmt.Sprintf("repeat-b-%d", i), "", fmt.Sprintf("second %d", i))
		_ = st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg})
	}
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) != 2 || len(prov.requests[1].Messages) >= 10 || prov.requests[1].Messages[0].Role != protocol.RoleCustom || !strings.Contains(prov.requests[1].Messages[0].Content[0].Text, "first summary") {
		t.Fatalf("second request=%+v", prov.requests)
	}
}

func TestRepeatedCompactionSendsLatestCheckpointToNextTurn(t *testing.T) {
	first := compact.WorkingStateTitle + "\n\n## Objective and constraints\n- first checkpoint sentinel"
	second := compact.WorkingStateTitle + "\n\n## Objective and constraints\n- latest checkpoint sentinel"
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamTextDelta, Text: first}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: second}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "recalled latest"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 500, Fallback: "local"}
	memoryValues := []string{"ALPHA-17", "BRAVO-29", "COBALT-31", "DELTA-43", "EMBER-59", "FROST-61", "GARNET-73", "HARBOR-89"}
	appendUsers := func(prefix string, count int) {
		t.Helper()
		for i := range count {
			text := fmt.Sprintf("%s message %d", prefix, i)
			if prefix == "first" && i == 2 {
				text = "Facts available only in this old turn: " + strings.Join(memoryValues, ", ")
			}
			msg := protocol.NewUserMessage(fmt.Sprintf("%s-%d", prefix, i), "", text)
			if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
				t.Fatal(err)
			}
		}
	}
	appendUsers("first", 6)
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	appendUsers("second", 4)
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "Recall the latest checkpoint."); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) != 3 {
		t.Fatalf("provider requests=%d, want 3", len(prov.requests))
	}
	request := prov.requests[2]
	custom := 0
	for _, message := range request.Messages {
		if message.Role != protocol.RoleCustom {
			continue
		}
		custom++
		text := messageTextBlocks(message)
		if !strings.Contains(text, "latest checkpoint sentinel") {
			t.Fatalf("active checkpoint does not contain latest summary:\n%s", text)
		}
		for _, value := range memoryValues {
			if !strings.Contains(text, value) {
				t.Fatalf("active checkpoint lost old fact %q after two compactions:\n%s", value, text)
			}
		}
	}
	if custom != 1 || request.Messages[0].Role != protocol.RoleCustom {
		t.Fatalf("latest request custom checkpoints=%d messages=%+v", custom, request.Messages)
	}
}

func TestManualCompactRejectsMalformedSummaryWhenFallbackIsError(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: `<｜DSML｜tool_calls><｜DSML｜invoke name="bash">`},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 500, Fallback: "error"}
	for i := range 6 {
		msg := protocol.NewUserMessage(fmt.Sprintf("malformed-%d", i), "", fmt.Sprintf("objective message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Compact(context.Background()); err == nil || !strings.Contains(err.Error(), "tool-protocol markup") {
		t.Fatalf("malformed summary error=%v", err)
	}
	projected, err := st.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range projected {
		if message.Role == protocol.RoleCustom {
			t.Fatalf("failed quality validation persisted a compaction marker: %+v", projected)
		}
	}
}

func TestManualCompactFallsBackWhenProviderFails(t *testing.T) {
	prov := &scriptedProvider{resolveErr: errors.New("summary unavailable")}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	for i := range 6 {
		msg := protocol.NewUserMessage(fmt.Sprintf("fallback-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.UsedFallback || result.Summary == "" {
		t.Fatalf("fallback result = %+v", result)
	}
}
