package protocol

import (
	"maps"
	"slices"
	"testing"
)

func schemaReasoningState() RPCSessionReasoningState {
	return RPCSessionReasoningState{SessionID: "session", BranchID: "main", Provider: "fake", Model: "fake-1", Mode: ModeDefault, PermissionMode: "deny", Thinking: ThinkingOff, ReasoningSummary: ReasoningSummaryAuto, TextVerbosity: TextVerbosityLow}
}

func TestManagedRuntimeSchemaRequests(t *testing.T) {
	request := resolveRPCSchema(t, "request.schema.json")
	binding := RPCHistoryControlBinding{SessionID: "session", SourceBranchID: "main", TargetBranchID: "main"}
	cases := map[string]any{
		"history_branch_fork":   RPCManagedBranchForkParams{RPCHistoryControlBinding: binding, Name: "fork"},
		"history_session_fork":  RPCManagedSessionForkParams{RPCHistoryControlBinding: binding, Name: "detached"},
		"history_branch_rename": RPCManagedBranchRenameParams{RPCHistoryControlBinding: binding, OldName: "", Name: "renamed"},
		"compaction_start":      RPCCompactionStartParams{SessionID: "session", BranchID: "main"},
		"managed_steer":         RPCManagedSteerParams{SessionID: "session", TurnID: "turn", RootEpoch: 1, RequestID: "steer", Text: "Keep the public API unchanged"},
		"session_reasoning_get": RPCSessionReasoningGetParams{SessionID: "session"},
		"session_reasoning_set": RPCSessionReasoningSetParams{Expected: schemaReasoningState(), Field: "thinking", Value: "off"},
	}
	for command, params := range cases {
		t.Run(command, func(t *testing.T) {
			value := jsonValue(t, map[string]any{"id": "request", "type": command, "params": params}).(map[string]any)
			if err := request.Validate(value); err != nil {
				t.Fatal(err)
			}
			// Every supplied member is required, including explicitly empty tip
			// assertions. Omitting a CAS member must never become a wildcard.
			fields := value["params"].(map[string]any)
			for field := range fields {
				incomplete := maps.Clone(fields)
				delete(incomplete, field)
				bad := maps.Clone(value)
				bad["params"] = incomplete
				if err := request.Validate(bad); err == nil {
					t.Errorf("accepted missing params.%s", field)
				}
			}
			for _, field := range []string{"message", "content", "provider", "secret", "legacy_extra"} {
				bad := maps.Clone(value)
				bad[field] = ""
				if err := request.Validate(bad); err == nil {
					t.Errorf("accepted extra envelope field %s", field)
				}
			}
			bad := maps.Clone(value)
			delete(bad, "id")
			if err := request.Validate(bad); err == nil {
				t.Error("accepted missing correlation ID")
			}
		})
	}
}

func TestSessionReasoningSchemaRequiresCompleteExpectedState(t *testing.T) {
	request := resolveRPCSchema(t, "request.schema.json")
	expected := jsonValue(t, schemaReasoningState()).(map[string]any)
	for field := range expected {
		for _, omit := range []bool{true, false} {
			bad := maps.Clone(expected)
			if omit {
				delete(bad, field)
			} else {
				bad[field] = nil
			}
			value := map[string]any{"id": "reasoning", "type": "session_reasoning_set", "params": map[string]any{"expected": bad, "field": "thinking", "value": "off"}}
			if err := request.Validate(value); err == nil {
				t.Errorf("accepted incomplete/null expected.%s", field)
			}
		}
	}
}

func TestManagedRuntimeSchemaResponses(t *testing.T) {
	output := resolveRPCSchema(t, "output.schema.json")
	branch := RPCBranchVersion{ID: "main", Name: "main"}
	cases := map[string]any{
		"history_branch_fork":   RPCManagedBranchForkResult{SessionID: "session", BranchID: "main", Branch: branch, Mode: ModeDefault, ReasoningEffort: ThinkingOff, RootEpoch: 1},
		"history_session_fork":  RPCManagedSessionForkResult{SessionID: "child", Name: "detached", Branch: branch, SourceSessionID: "session", SourceBranchID: "main", Mode: ModeDefault, RootEpoch: 1},
		"history_branch_rename": RPCManagedBranchRenameResult{SessionID: "session", BranchID: "main", Branch: branch, RootEpoch: 1},
		"compaction_start":      RPCCompactionAccepted{CompactionID: "compact", SessionID: "session", BranchID: "main", TurnID: "turn", TurnOrigin: "compact", RootEpoch: 1, TurnSequence: 1},
		"managed_steer":         RPCManagedSteerResult{SessionID: "session", TurnID: "turn", RootEpoch: 1, RequestID: "steer", ItemID: "native-item", Status: "accepted"},
		"session_reasoning_get": RPCSessionReasoning{RPCSessionReasoningState: schemaReasoningState(), ThinkingLevels: []ThinkingLevel{ThinkingOff}, ReasoningSummaries: []ReasoningSummary{ReasoningSummaryAuto}, TextVerbosities: []TextVerbosity{TextVerbosityLow}},
	}
	cases["session_reasoning_set"] = cases["session_reasoning_get"]
	for command, data := range cases {
		t.Run(command, func(t *testing.T) {
			frame := RPCResponse{ID: "request", Type: "response", Command: command, Success: true, Data: data}
			if err := output.Validate(jsonValue(t, frame)); err != nil {
				t.Fatal(err)
			}
			frame.Data = map[string]any{"unexpected": true}
			if err := output.Validate(jsonValue(t, frame)); err == nil {
				t.Fatal("typed response escaped through generic success schema")
			}
		})
	}
	for _, code := range []string{RPCHistoryControlRejectedErrorCode, RPCHistoryControlUnknownErrorCode, RPCCompactionRejectedErrorCode, RPCCompactionUnknownErrorCode, RPCManagedSteerRejectedErrorCode, RPCManagedSteerStaleErrorCode, RPCSessionReasoningRejectedErrorCode, RPCSessionReasoningUnknownErrorCode} {
		frame := RPCResponse{ID: "request", Type: "response", Success: false, Error: "request rejected", ErrorCode: code}
		if err := output.Validate(jsonValue(t, frame)); err != nil {
			t.Errorf("%s: %v", code, err)
		}
	}
}

func TestCompactionCompletedSchema(t *testing.T) {
	output := resolveRPCSchema(t, "output.schema.json")
	frame := RPCCompactionCompleted{Type: RPCTypeCompactionCompleted, RequestID: "request", CompactionID: "compact", SessionID: "session", BranchID: "main", TurnID: "turn", TurnOrigin: "compact", RootEpoch: 1, TurnSequence: 1}
	for _, status := range []string{"completed", "noop", "fallback", "canceled", "failed"} {
		frame.Status, frame.UsedFallback = status, status == "fallback"
		if err := output.Validate(jsonValue(t, frame)); err != nil {
			t.Errorf("%s: %v", status, err)
		}
	}
	valid := jsonValue(t, frame).(map[string]any)
	for field := range valid {
		bad := maps.Clone(valid)
		delete(bad, field)
		if err := output.Validate(bad); err == nil {
			t.Errorf("accepted missing completion.%s", field)
		}
	}
	for _, fields := range []map[string]any{{"status": "finished"}, {"turn_origin": "user"}, {"summarized_messages": -1}, {"error": "private provider detail"}, {"summary": "private summary"}} {
		bad := maps.Clone(valid)
		maps.Copy(bad, fields)
		if err := output.Validate(bad); err == nil {
			t.Errorf("accepted invalid completion: %v", fields)
		}
	}
}

func TestManagedSteerQueueChangeSchema(t *testing.T) {
	output := resolveRPCSchema(t, "output.schema.json")
	for _, kind := range []string{"steer_accepted", "delivered", "discarded"} {
		queue := QueueControl{SessionID: "session", TurnID: "turn", Items: []QueueControlItem{}, ReviewItems: []QueueControlItem{}, Change: QueueControlChange{Kind: kind, ItemID: "native-item"}}
		frame := RPCResponse{ID: "request", Type: "response", Command: "queue_list", Success: true, Data: queue}
		if err := output.Validate(jsonValue(t, frame)); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
		queue.Change.ItemID = ""
		frame.Data = queue
		if err := output.Validate(jsonValue(t, frame)); err == nil {
			t.Errorf("%s accepted without native item identity", kind)
		}
	}
}

func TestManagedRuntimeReadyCapabilities(t *testing.T) {
	ready := NewRPCReady("test")
	for _, capability := range []string{"history_control", "compaction_run", "managed_steer", RPCSessionReasoningCapability} {
		if !slices.Contains(ready.Capabilities, capability) {
			t.Errorf("ready missing %s", capability)
		}
	}
	for _, capability := range []string{RPCControlCapability, RPCDefaultsControlCapability, RPCProviderStatusCapability, HostOperationsCapability, RPCAPIKeyControlCapability, RPCProjectCreateCapability, RPCProjectCloneCapability} {
		if slices.Contains(ready.Capabilities, capability) {
			t.Errorf("eager runtime advertised cold capability %s", capability)
		}
	}
}
