package web

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRuntimeActivityLifecycleAndIdentity(t *testing.T) {
	r := &liveRuntime{activityPrompt: 1}
	start := protocol.AgentEvent{Type: protocol.EvToolStart, TurnID: "turn-one", ToolCallID: "call-reused", ToolName: "bash", Message: "SECRET-COMMAND"}
	r.projectActivity(start)
	first := r.snapshot.Activities[0]
	if first.Status != "running" || first.Summary != "bash" || first.Output != "" || first.IsError {
		t.Fatalf("start: %+v", first)
	}
	r.projectActivity(start)
	if len(r.snapshot.Activities) != 1 {
		t.Fatal("duplicate start created another row")
	}
	end := start
	end.Type, end.ToolOutput = protocol.EvToolEnd, "PRIVATE-DETAIL-PREVIEW"
	r.projectActivity(end)
	if got := r.snapshot.Activities[0]; got.ID != first.ID || got.Status != "completed" || got.Output != "" {
		t.Fatalf("completion: %+v", got)
	}
	r.projectActivity(start)
	if r.snapshot.Activities[0].Status != "completed" {
		t.Fatal("late start resurrected completed activity")
	}
	start.TurnID = "turn-two"
	r.projectActivity(start)
	if len(r.snapshot.Activities) != 2 || r.snapshot.Activities[1].ID == first.ID {
		t.Fatal("call ID reused in another turn collided")
	}
	end.TurnID, end.IsError = start.TurnID, true
	r.projectActivity(end)
	if got := r.snapshot.Activities[1]; got.Status != "failed" || !got.IsError {
		t.Fatalf("failed tool: %+v", got)
	}
	// Legacy untagged events still get distinct IDs across admitted prompts.
	r.activityPrompt++
	r.projectActivity(start)
	if len(r.snapshot.Activities) != 3 || r.snapshot.Activities[2].ID == r.snapshot.Activities[1].ID {
		t.Fatal("call ID reused in another prompt collided")
	}
	r.cancelActivities()
	if got := r.snapshot.Activities[2]; got.Status != "canceled" || got.IsError {
		t.Fatalf("cancellation: %+v", got)
	}
	r.projectActivity(end)
	if r.snapshot.Activities[2].Status != "canceled" {
		t.Fatal("late completion changed canceled activity")
	}
	start.ToolCallID = "queued-start-after-abort"
	r.projectActivity(start)
	if len(r.snapshot.Activities) != 3 {
		t.Fatal("late start survived prompt cancellation")
	}
}

func TestRuntimeActivitySyntheticResultAndStrictOmission(t *testing.T) {
	r := &liveRuntime{}
	r.projectActivity(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: "denied", ToolName: "write", IsError: true, ToolOutput: "PRIVATE-PREVIEW", Message: "SECRET-ARGS", Text: "SECRET-TEXT"})
	if got := r.snapshot.Activities[0]; got.Status != "failed" || !got.IsError || got.Output != "" || got.Summary != "write" {
		t.Fatalf("synthetic denial: %+v", got)
	}
	for _, event := range []protocol.AgentEvent{
		{Type: protocol.EvToolEnd, ToolOutput: "uncorrelated"},
		{Type: protocol.EvToolProgress, ToolCallID: "progress", Text: "SECRET-PROGRESS"},
		{Type: protocol.EvThinkingDelta, ToolCallID: "thinking", Text: "SECRET-THINKING"},
	} {
		r.projectActivity(event)
	}
	if len(r.snapshot.Activities) != 1 {
		t.Fatal("non-lifecycle event or missing ID created activity")
	}
}

func TestRuntimeActivityBoundsAndImmutableClone(t *testing.T) {
	r := &liveRuntime{}
	for i := range 100 {
		r.projectActivity(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: fmt.Sprint(i), ToolName: strings.Repeat("界", 100) + "\x00\x1b"})
	}
	if len(r.snapshot.Activities) != runtimeActivityCount || len(r.activityKeys) != runtimeActivityCount || !r.snapshot.ActivitiesTruncated {
		t.Fatal("activity entries/correlation not bounded")
	}
	for i := range r.snapshot.Activities {
		// Independently enforce bounds even for oversized public preview input.
		r.snapshot.Activities[i].Output = strings.Repeat("界", runtimeActivityOutputBytes)
	}
	r.trimActivities()
	total := 0
	for _, activity := range r.snapshot.Activities {
		total += len(activity.Output)
		if len(activity.Tool) > 128 || len(activity.Summary) > 128 || len(activity.Output) > runtimeActivityOutputBytes || !utf8.ValidString(activity.Tool) || !utf8.ValidString(activity.Output) || !activity.Truncated {
			t.Fatalf("unbounded/invalid activity: %d, %d", len(activity.Tool), len(activity.Output))
		}
	}
	if total > runtimeActivityTotalBytes {
		t.Fatalf("total output bound: %d", total)
	}
	before := r.snapshot.Activities[0]
	clone := r.snapshot.clone()
	clone.Activities[0].Status = "tampered"
	clone.Activities[0].Output = "tampered"
	r.cancelActivities()
	if clone.Activities[0].Status != "tampered" || r.snapshot.Activities[0].Output != before.Output {
		t.Fatal("snapshot aliases internal activity storage")
	}
	r.snapshot.Activities[0].Output = "hello\x00\x1b\x7f\n\tworld"
	r.trimActivities()
	if got := r.snapshot.Activities[0].Output; got != "hello\n\tworld" {
		t.Fatalf("control stripping: %q", got)
	}
}

func TestRuntimeActivityMalformedTextStaysBounded(t *testing.T) {
	r := &liveRuntime{}
	r.projectActivity(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: "call", ToolName: strings.Repeat("\xff", 128)})
	r.snapshot.Activities[0].Output = strings.Repeat("\xff", runtimeActivityOutputBytes)
	r.trimActivities()
	activity := r.snapshot.Activities[0]
	if len(activity.Tool) > 128 || len(activity.Output) > runtimeActivityOutputBytes || !utf8.ValidString(activity.Tool) || !utf8.ValidString(activity.Output) {
		t.Fatalf("malformed UTF-8 expanded beyond projection bounds: %+v", activity)
	}
}

func TestRuntimeActivityPublicResultChannel(t *testing.T) {
	r := &liveRuntime{}
	for i := range 25 {
		r.projectActivity(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: fmt.Sprint(i), ToolName: "read", ToolOutput: "PRIVATE-LEGACY-PREVIEW", ToolResult: &protocol.ToolResultPreview{Text: strings.Repeat("界", 8000), Truncated: true}})
	}
	total := 0
	for _, activity := range r.snapshot.Activities {
		total += len(activity.Output)
		if activity.Output == "" || strings.Contains(activity.Output, "PRIVATE") || len(activity.Output) > runtimeActivityOutputBytes || !utf8.ValidString(activity.Output) || !activity.Truncated {
			t.Fatal("public result projection invalid")
		}
	}
	if total > runtimeActivityTotalBytes || !r.snapshot.ActivitiesTruncated {
		t.Fatal("public output total budget not enforced")
	}
}
