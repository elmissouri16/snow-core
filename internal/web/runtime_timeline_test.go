package web

import (
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func assertRuntimeTimeline(t *testing.T, snapshot RuntimeSnapshot, roles, texts []string) {
	t.Helper()
	gotRoles, gotTexts := make([]string, 0, len(snapshot.Messages)), make([]string, 0, len(snapshot.Messages))
	ids := make(map[string]bool)
	for _, message := range snapshot.Messages {
		gotRoles, gotTexts = append(gotRoles, message.Role), append(gotTexts, message.Text)
		if message.ID == "" || ids[message.ID] {
			t.Fatalf("missing/duplicate timeline identity: %+v", message)
		}
		ids[message.ID] = true
		if message.Role == "tool_activity" && (message.Text != "" || message.HTML != "" || message.SourceID != "" || len(message.Tools) != 0) {
			t.Fatalf("live marker claims text or persisted ownership: %+v", message)
		}
	}
	if !slices.Equal(gotRoles, roles) || !slices.Equal(gotTexts, texts) {
		t.Fatalf("timeline roles=%q text=%q; want roles=%q text=%q", gotRoles, gotTexts, roles, texts)
	}
}

func TestRuntimeTimelineTwoPromptsAndReusedCallIDs(t *testing.T) {
	m, r, peer := runtimeActivityStream(t)
	roles, texts := []string{}, []string{}
	var previous RuntimeSnapshot
	for prompt := range 2 {
		// Admit the next prompt in the decoder fixture, exactly as Prompt does,
		// without involving a provider or altering the worker fixture protocol.
		r.mu.Lock()
		r.activityPrompt++
		r.activityCanceled = false
		r.busy, r.promptID, r.turnID, r.assistant = true, "prompt", "", -1
		r.snapshot.Status = "running"
		r.addMessage(RuntimeMessage{Role: "user", Text: fmt.Sprintf("prompt %d", prompt)})
		r.mu.Unlock()
		tool, calls := "glob", 2
		if prompt == 1 {
			tool, calls = "write", 1
		}
		for call := range calls {
			event := protocol.AgentEvent{Type: protocol.EvToolStart, TurnID: "reused-turn", ToolCallID: fmt.Sprint(call), ToolName: tool}
			runtimeActivityEmit(t, peer, event)
			event.Type = protocol.EvToolEnd
			event.ToolResult = &protocol.ToolResultPreview{Text: "public result"}
			runtimeActivityEmit(t, peer, event)
		}
		runtimeActivityEmit(t, peer, protocol.AgentEvent{Type: protocol.EvTextDelta, Text: fmt.Sprintf("answer %d", prompt)})
		runtimeActivityEmit(t, peer, protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: "prompt", Status: protocol.RPCPromptCompletedStatus})
		snapshot := runtimeWait(t, m, "project", func(s RuntimeSnapshot) bool { return s.Status == "idle" })
		roles = append(roles, "user", "tool_activity", "assistant")
		texts = append(texts, fmt.Sprintf("prompt %d", prompt), "", fmt.Sprintf("answer %d", prompt))
		assertRuntimeTimeline(t, snapshot, roles, texts)
		for i, activity := range snapshot.Activities {
			marker := 1
			if i == 2 {
				marker = 4
			}
			if activity.Status != "completed" || activity.MessageID != snapshot.Messages[marker].ID {
				t.Fatalf("call misplaced: %+v", activity)
			}
		}
		if prompt == 1 && (!reflect.DeepEqual(previous.Messages, snapshot.Messages[:3]) || !reflect.DeepEqual(previous.Activities, snapshot.Activities[:2]) || snapshot.Activities[0].ID == snapshot.Activities[2].ID) {
			t.Fatal("second prompt moved or reused first prompt identities")
		}
		previous = snapshot
	}
}

func TestRuntimeTimelineInterleavedSyntheticAndDuplicateEvents(t *testing.T) {
	m, _, peer := runtimeActivityStream(t)
	text := func(value string) {
		runtimeActivityEmit(t, peer, protocol.AgentEvent{Type: protocol.EvTextDelta, Text: value})
	}
	text("before")
	start := protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: "first", ToolName: "glob"}
	runtimeActivityEmit(t, peer, start)
	// Text starts before the end arrives. Neither that late end nor duplicate
	// lifecycle events may split or rebind this already-streaming run.
	text("between")
	end := start
	end.Type = protocol.EvToolEnd
	runtimeActivityEmit(t, peer, end)
	runtimeActivityEmit(t, peer, start)
	runtimeActivityEmit(t, peer, end)
	text(" calls")
	denial := protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: "denied", ToolName: "write", IsError: true, ToolResult: &protocol.ToolResultPreview{Text: "Denied"}, Message: "PRIVATE-ARGS", ToolOutput: "PRIVATE-OUTPUT", Text: "PRIVATE-TEXT"}
	runtimeActivityEmit(t, peer, denial)
	text("final")
	runtimeActivityEmit(t, peer, denial)
	text(" answer")
	runtimeActivityEmit(t, peer, protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: "prompt", Status: protocol.RPCPromptCompletedStatus})
	snapshot := runtimeWait(t, m, "project", func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	assertRuntimeTimeline(t, snapshot, []string{"assistant", "tool_activity", "assistant", "tool_activity", "assistant"}, []string{"before", "", "between calls", "", "final answer"})
	if len(snapshot.Activities) != 2 || snapshot.Activities[0].MessageID != snapshot.Messages[1].ID || snapshot.Activities[1].MessageID != snapshot.Messages[3].ID || snapshot.Activities[1].Status != "failed" {
		t.Fatalf("interleaved calls misplaced: %+v", snapshot.Activities)
	}
	data, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(data), "PRIVATE") || !strings.Contains(string(data), `"message_id":`) {
		t.Fatalf("invalid public timeline JSON: %s, %v", data, err)
	}
	clone := snapshot.clone()
	clone.Messages[1].ID = "tampered-marker"
	clone.Activities[0].MessageID = "tampered-owner"
	fresh, _ := m.Snapshot("project")
	if !reflect.DeepEqual(fresh.Messages, snapshot.Messages) || !reflect.DeepEqual(fresh.Activities, snapshot.Activities) {
		t.Fatal("snapshot clone aliases timeline ownership")
	}
}

func TestRuntimeTimelinePrunedMarkerKeepsOriginalBinding(t *testing.T) {
	for _, budget := range []string{"count", "bytes"} {
		t.Run(budget, func(t *testing.T) {
			r := &liveRuntime{instanceID: "test", assistant: -1, plan: -1}
			event := protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: "pending", ToolName: "read"}
			r.projectActivity(event)
			owner := r.snapshot.Activities[0].MessageID
			count, text := runtimeHistoryCount, "answer"
			if budget == "bytes" {
				count, text = 5, strings.Repeat("x", runtimeMessageBytes)
			}
			for range count {
				r.addMessage(RuntimeMessage{Role: "assistant", Text: text})
			}
			r.assistant = len(r.snapshot.Messages) - 1
			before := slices.Clone(r.snapshot.Messages)
			event.Type, event.ToolResult = protocol.EvToolEnd, &protocol.ToolResultPreview{Text: "public"}
			r.projectActivity(event)
			r.projectActivity(event)
			if !r.snapshot.HistoryTruncated || len(r.snapshot.Messages) > runtimeHistoryCount || !reflect.DeepEqual(before, r.snapshot.Messages) || r.assistant != len(before)-1 {
				t.Fatal("late end recreated a pruned marker or broke assistant streaming")
			}
			bytes := 0
			for _, message := range r.snapshot.Messages {
				bytes += len(message.Text)
				if message.ID == owner {
					t.Fatal("fixture did not evict original marker")
				}
			}
			if bytes > runtimeHistoryBytes || r.snapshot.Activities[0].MessageID != owner || r.snapshot.Activities[0].Status != "completed" {
				t.Fatal("trim exceeded budget or rebound an orphaned activity")
			}
		})
	}
}

func TestRuntimeTimelineActivityEvictionPrunesOnlyEmptyGroups(t *testing.T) {
	r := &liveRuntime{instanceID: "test", assistant: -1, plan: -1}
	for i := range 17 {
		event := protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: fmt.Sprint(i), ToolName: "read"}
		r.projectActivity(event)
		if i < 16 {
			event.Type, event.ToolResult = protocol.EvToolEnd, &protocol.ToolResultPreview{Text: strings.Repeat("x", runtimeActivityOutputBytes)}
			r.projectActivity(event)
		}
		if i == 0 {
			r.addMessage(RuntimeMessage{Role: "assistant", Text: "between groups"})
		}
	}
	r.addMessage(RuntimeMessage{Role: "plan", Text: "plan"})
	r.plan = len(r.snapshot.Messages) - 1
	r.addMessage(RuntimeMessage{Role: "assistant", Text: "streaming"})
	r.assistant = len(r.snapshot.Messages) - 1
	before := slices.Clone(r.snapshot.Messages)
	r.projectActivity(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: "16", ToolResult: &protocol.ToolResultPreview{Text: strings.Repeat("x", runtimeActivityOutputBytes)}})
	if !r.snapshot.ActivitiesTruncated || len(r.snapshot.Activities) != 16 || !reflect.DeepEqual(r.snapshot.Messages, before[1:]) || r.plan != 2 || r.assistant != 3 {
		t.Fatalf("empty group eviction damaged timeline or streaming indices: %+v", r.snapshot)
	}
	for _, activity := range r.snapshot.Activities {
		if activity.MessageID != r.snapshot.Messages[1].ID {
			t.Fatal("partial surviving group was pruned or rebound")
		}
	}
}

func TestRuntimeTimelineCountEvictionLeavesNoEmptyMarkers(t *testing.T) {
	r := &liveRuntime{instanceID: "test", assistant: -1, plan: -1}
	for i := range runtimeActivityCount + 10 {
		r.addMessage(RuntimeMessage{Role: "user", Text: "prompt"})
		r.projectActivity(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: fmt.Sprint(i), ToolName: "read"})
	}
	if len(r.snapshot.Activities) != runtimeActivityCount || len(r.activityKeys) != runtimeActivityCount || len(r.snapshot.Messages) > runtimeHistoryCount || !r.snapshot.ActivitiesTruncated || !r.snapshot.HistoryTruncated {
		t.Fatal("timeline count bounds not enforced")
	}
	for _, message := range r.snapshot.Messages {
		if message.Role == "tool_activity" && !slices.ContainsFunc(r.snapshot.Activities, func(a RuntimeActivity) bool { return a.MessageID == message.ID }) {
			t.Fatalf("orphan marker retained: %+v", message)
		}
	}
}

func TestRuntimeTimelineSessionSwitchDropsLiveOwnership(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "workflow")
	project := projects[0]
	before, err := m.Open(t.Context(), project, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.runtime(project.ID, before.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.projectActivity(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: "old", ToolName: "read"})
	old := r.snapshot.Activities[0]
	r.mu.Unlock()
	next, err := m.Switch(t.Context(), project.ID, before.InstanceID, "saved", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Activities) != 0 || next.ActivitiesTruncated || next.InstanceID == before.InstanceID {
		t.Fatalf("session switch retained live activity: %+v", next)
	}
	for _, message := range next.Messages {
		if message.Role == "tool_activity" || message.ID == old.MessageID {
			t.Fatal("old live marker survived history replacement")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.activityKeys) != 0 {
		t.Fatal("old call correlation survived switch")
	}
	r.projectActivity(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: "old", ToolName: "read"})
	if activity := r.snapshot.Activities[0]; activity.ID == old.ID || activity.MessageID == old.MessageID {
		t.Fatal("new session reused old timeline ownership")
	}
}
