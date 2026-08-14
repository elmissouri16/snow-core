package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

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
			assertJSONRoundTrip(t, RPCSessionInfo{SessionID: "s", Name: "n", Path: "p", CWD: "c", Provider: "fake", Model: "fake-1", Thinking: ThinkingOff, ThinkingLevels: []ThinkingLevel{ThinkingOff}, CollaborationMode: ModeDefault, Goal: &RPCGoalSummary{GoalID: "g", Status: GoalActive, TokenBudget: &budget, EstimatedCosts: []Cost{}}, Subagents: RPCSubagentLimits{}, PendingInputs: RPCPendingInputCounts{}})
		},
	}
	for i, test := range values {
		t.Run(fmt.Sprintf("dto_%d", i), test)
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
