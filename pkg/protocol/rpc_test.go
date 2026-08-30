package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestRPCOptionalTimesOmitZeroValues(t *testing.T) {
	for name, value := range map[string]any{
		"debug": RPCDebugStatus{},
		"mcp":   RPCMCPServer{},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"started_at", "cached_at", "last_used_at"} {
			if bytes.Contains(data, []byte(`"`+field+`"`)) {
				t.Fatalf("%s zero value unexpectedly includes %q: %s", name, field, data)
			}
		}
	}
}

func TestRPCReadyIsDefensiveAndStable(t *testing.T) {
	ready := NewRPCReady("test-version")
	if ready.Type != RPCTypeReady || ready.ProtocolVersion != RPCProtocolVersion || ready.SnowVersion != "test-version" || ready.MaxInputBytes != RPCMaxInputBytes {
		t.Fatalf("ready = %+v", ready)
	}
	if !sort.StringsAreSorted(ready.Capabilities) {
		t.Fatalf("capabilities are not sorted: %v", ready.Capabilities)
	}
	if fallback := NewRPCReady(""); fallback.SnowVersion != "dev" {
		t.Fatalf("empty Snow version fallback = %q", fallback.SnowVersion)
	}
	ready.Capabilities[0] = "mutated"
	if KnownRPCCapabilities()[0] == "mutated" {
		t.Fatal("ready capabilities alias protocol state")
	}
	commands := KnownRPCCommands()
	if !sort.StringsAreSorted(commands) || len(commands) == 0 {
		t.Fatalf("commands = %v", commands)
	}
	commands[0] = "mutated"
	if KnownRPCCommands()[0] == "mutated" {
		t.Fatal("command list aliases protocol state")
	}
}

func assertJSONRoundTrip[T any](t *testing.T, value T) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var got T
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, value) {
		t.Fatalf("round trip = %#v, want %#v (json %s)", got, value, data)
	}
}

func TestRPCPublicDTOJSONRoundTrips(t *testing.T) {
	enabled := true
	budget := int64(100)
	values := []func(*testing.T){
		func(t *testing.T) { assertJSONRoundTrip(t, NewRPCReady("test")) },
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCRequest{ID: "p1", Type: "prompt", Message: "hello", Params: json.RawMessage(`{}`)})
		},
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCResponse{ID: "p1", Type: "response", Command: "prompt", Success: true})
		},
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCPromptCompleted{Type: RPCTypePromptCompleted, RequestID: "p1", Status: RPCPromptCompletedStatus})
		},
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCModelList{Provider: "fake", Current: "fake-1", Enabled: &enabled, Models: []Model{{Provider: "fake", ID: "fake-1"}}})
		},
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCGoalSummary{GoalID: "g", Status: GoalActive, TokensUsed: 1, TokenBudget: &budget, EstimatedCosts: []Cost{}})
		},
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCSubagentLimits{Enabled: true, MaxConcurrentAgents: 1, MaxConcurrentThreads: 1, MaxAgentsPerSession: 2, MaxDepth: 1, Durable: true})
		},
		func(t *testing.T) { assertJSONRoundTrip(t, RPCPendingInputCounts{Steering: 1, FollowUp: 2, Total: 3}) },
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCRequest{ID: "c1", Type: "set_reasoning_summary", ReasoningSummary: "concise", TextVerbosity: "high"})
		},
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCBranchList{Branches: []SessionBranch{{ID: "main", TipID: "entry-1", Active: true}}})
		},
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCMessagesList{Messages: []Message{{ID: "m1", Role: RoleUser, Content: []ContentBlock{}, Timestamp: 1}}})
		},
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCDiagnosticsList{Diagnostics: []ConfigDiagnostic{{Path: "config.json", Message: "warning"}}})
		},
		func(t *testing.T) {
			assertJSONRoundTrip(t, RPCSessionInfo{SessionID: "s", Name: "n", Path: "p", CWD: "c", Provider: "fake", Model: "fake-1", Thinking: ThinkingOff, ThinkingLevels: []ThinkingLevel{ThinkingOff}, CollaborationMode: ModeDefault, Goal: &RPCGoalSummary{GoalID: "g", Status: GoalActive, TokenBudget: &budget, EstimatedCosts: []Cost{}}, Subagents: RPCSubagentLimits{}, PendingInputs: RPCPendingInputCounts{}})
		},
	}
	for i, test := range values {
		t.Run(fmt.Sprintf("dto_%d", i), test)
	}
}

func TestRPCGoalSummaryBlockedReasonJSON(t *testing.T) {
	active, err := json.Marshal(RPCGoalSummary{GoalID: "active", Status: GoalActive})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(active, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["blocked_reason"]; ok {
		t.Fatalf("active goal exposed blocked_reason: %s", active)
	}
	blocked, err := json.Marshal(RPCGoalSummary{GoalID: "blocked", Status: GoalBlocked, BlockedReason: "CI unavailable"})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(blocked, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["blocked_reason"] != "CI unavailable" {
		t.Fatalf("blocked goal JSON = %s", blocked)
	}
}

func TestRPCWireDTOJSON(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  map[string]any
	}{
		{
			name:  "request",
			value: RPCRequest{ID: "p1", Type: "prompt", Message: "hello"},
			want:  map[string]any{"id": "p1", "type": "prompt", "message": "hello"},
		},
		{
			name:  "response",
			value: RPCResponse{ID: "p1", Type: "response", Command: "prompt", Success: true},
			want:  map[string]any{"id": "p1", "type": "response", "command": "prompt", "success": true},
		},
		{
			name:  "completed",
			value: RPCPromptCompleted{Type: RPCTypePromptCompleted, RequestID: "p1", Status: RPCPromptCompletedStatus},
			want:  map[string]any{"type": "prompt_completed", "request_id": "p1", "status": "completed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("json = %s, want %+v", data, tt.want)
			}
		})
	}
}
