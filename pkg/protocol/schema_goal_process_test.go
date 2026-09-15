package protocol

import (
	"maps"
	"slices"
	"testing"
)

const schemaManagedProcessID = "proc_0123456789abcdef0123456789abcdef"

func TestGoalProcessControlSchemaRequests(t *testing.T) {
	request := resolveRPCSchema(t, "request.schema.json")
	create := RPCGoalRunParams{Action: "create", SessionID: "session", BranchID: "main", Objective: "Ship safely"}
	budgeted := create
	budgeted.TokenBudget = new(int64(100))
	cases := []struct {
		name, command string
		params        any
	}{
		{"inspect current", "goal_inspect", RPCGoalInspectParams{SessionID: "session"}},
		{"inspect branch", "goal_inspect", RPCGoalInspectParams{SessionID: "session", BranchID: "main"}},
		{"create unlimited", "goal_run", create},
		{"create budgeted", "goal_run", budgeted},
		{"resume", "goal_run", RPCGoalRunParams{Action: "resume", SessionID: "session", BranchID: "main", ExpectedTipID: "tip", ExpectedGoalID: "goal"}},
		{"list", "process_control_list", RPCProcessControlListParams{SessionID: "session"}},
		{"logs defaults", "process_control_logs", RPCProcessControlLogsParams{SessionID: "session", ProcessID: schemaManagedProcessID}},
		{"logs limits", "process_control_logs", RPCProcessControlLogsParams{SessionID: "session", ProcessID: schemaManagedProcessID, Cursor: new(int64(0)), MaxBytes: ProcessControlMaxBytes}},
		{"stop defaults", "process_control_stop", RPCProcessControlStopParams{SessionID: "session", ProcessID: schemaManagedProcessID}},
		{"stop grace", "process_control_stop", RPCProcessControlStopParams{SessionID: "session", ProcessID: schemaManagedProcessID, GraceMS: ProcessControlMaxGraceMS}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := map[string]any{"id": "request", "type": tc.command, "params": tc.params}
			if err := request.Validate(jsonValue(t, value)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGoalRunSchemaRejectsInvalidAssertionsAndBudgets(t *testing.T) {
	request := resolveRPCSchema(t, "request.schema.json")
	create := map[string]any{"action": "create", "session_id": "session", "branch_id": "main", "expected_tip_id": "", "expected_goal_id": "", "objective": "Ship safely"}
	cases := []struct {
		name, field string
		value       any
		remove      bool
	}{
		{"missing tip assertion", "expected_tip_id", nil, true},
		{"missing goal assertion", "expected_goal_id", nil, true},
		{"null tip assertion", "expected_tip_id", nil, false},
		{"null goal assertion", "expected_goal_id", nil, false},
		{"missing objective", "objective", nil, true},
		{"empty objective", "objective", "", false},
		{"missing branch", "branch_id", nil, true},
		{"empty branch", "branch_id", "", false},
		{"zero budget", "token_budget", 0, false},
		{"negative budget", "token_budget", -1, false},
		{"fractional budget", "token_budget", 1.5, false},
		{"null budget", "token_budget", nil, false},
		{"unknown action", "action", "continue", false},
		{"unknown field", "replace", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := maps.Clone(create)
			if tc.remove {
				delete(params, tc.field)
			} else {
				params[tc.field] = tc.value
			}
			if err := request.Validate(jsonValue(t, map[string]any{"id": "run", "type": "goal_run", "params": params})); err == nil {
				t.Fatal("invalid goal run accepted")
			}
		})
	}
	resume := maps.Clone(create)
	resume["action"], resume["expected_goal_id"] = "resume", "goal"
	delete(resume, "objective")
	for _, field := range []string{"objective", "token_budget", "expected_goal_id"} {
		t.Run("resume rejects "+field, func(t *testing.T) {
			params := maps.Clone(resume)
			params[field] = ""
			if field == "token_budget" {
				params[field] = 100
			}
			if err := request.Validate(jsonValue(t, map[string]any{"id": "run", "type": "goal_run", "params": params})); err == nil {
				t.Fatal("invalid resume accepted")
			}
		})
	}
	for _, command := range []string{"goal_run", "goal_inspect"} {
		params := any(create)
		if command == "goal_inspect" {
			params = RPCGoalInspectParams{SessionID: "session"}
		}
		for _, id := range []any{nil, ""} {
			value := map[string]any{"type": command, "params": params}
			if id != nil {
				value["id"] = id
			}
			if err := request.Validate(jsonValue(t, value)); err == nil {
				t.Errorf("%s accepted absent/empty correlation ID", command)
			}
		}
	}
}

func TestProcessControlSchemaRejectsInvalidRequests(t *testing.T) {
	request := resolveRPCSchema(t, "request.schema.json")
	cases := []struct {
		name, command, field string
		value                any
	}{
		{"session empty", "list", "session_id", ""},
		{"session path", "list", "session_id", "../session"},
		{"session null", "list", "session_id", nil},
		{"unknown command", "list", "command", "sh"},
		{"raw PID", "stop", "process_id", "1234"},
		{"negative grace", "stop", "grace_ms", -1},
		{"excess grace", "stop", "grace_ms", ProcessControlMaxGraceMS + 1},
		{"negative cursor", "logs", "cursor", -1},
		{"null cursor", "logs", "cursor", nil},
		{"tiny page", "logs", "max_bytes", 3},
		{"oversized page", "logs", "max_bytes", ProcessControlMaxBytes + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]any{"session_id": "session"}
			if tc.command != "list" {
				params["process_id"] = schemaManagedProcessID
			}
			params[tc.field] = tc.value
			value := map[string]any{"id": "control", "type": "process_control_" + tc.command, "params": params}
			if err := request.Validate(jsonValue(t, value)); err == nil {
				t.Fatal("invalid process control accepted")
			}
		})
	}
}

func TestGoalProcessControlSchemaOutputDTOs(t *testing.T) {
	output := resolveRPCSchema(t, "output.schema.json")
	process := RPCManagedProcess{ProcessID: schemaManagedProcessID, Name: "worker", Status: "running", StartedAt: 1}
	results := map[string]any{
		"goal_inspect":         RPCGoalInspection{SessionID: "session", BranchID: "main", Goal: nil},
		"goal_run":             RPCGoalRunAccepted{GoalRunID: "run", GoalID: "goal", SessionID: "session", BranchID: "main"},
		"process_control_list": RPCProcessControlList{SessionID: "session", Processes: []RPCManagedProcess{process}},
		"process_control_logs": RPCProcessControlLogs{SessionID: "session", ProcessID: schemaManagedProcessID, Status: "running", Output: "page", NextCursor: 4},
		"process_control_stop": RPCProcessControlStop{SessionID: "session", Process: process},
	}
	for command, data := range results {
		t.Run(command, func(t *testing.T) {
			value := RPCResponse{ID: "request", Type: "response", Command: command, Success: true, Data: data}
			if err := output.Validate(jsonValue(t, value)); err != nil {
				t.Fatal(err)
			}
			value.Success, value.Data, value.Error = false, nil, "rejected"
			if err := output.Validate(jsonValue(t, value)); err != nil {
				t.Fatal(err)
			}
		})
	}
	inspection := RPCGoalInspection{SessionID: "session", BranchID: "main", TipID: "tip", Deferred: true, GoalRunID: "run", BudgetRemaining: new(int64(0)), Goal: &ThreadGoal{SessionID: "session", BranchID: "main", GoalID: "goal", Objective: "ship", Status: GoalBudgetLimited, TokenBudget: new(int64(100)), TokensUsed: 100}}
	if err := output.Validate(jsonValue(t, RPCResponse{ID: "inspect", Type: "response", Command: "goal_inspect", Success: true, Data: inspection})); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"finished", "failed", "canceled"} {
		completed := RPCGoalRunCompleted{Type: RPCTypeGoalRunCompleted, RequestID: "request", GoalRunID: "run", GoalID: "goal", Status: status, GoalStatus: GoalPaused}
		if status == "failed" {
			completed.Error = "provider failed"
		}
		if err := output.Validate(jsonValue(t, completed)); err != nil {
			t.Errorf("%s: %v", status, err)
		}
	}
	for _, value := range []any{
		AgentEvent{Type: EvTextDelta, Text: "working", GoalRunID: "run"},
		AgentEvent{Type: EvTurnDone, GoalRunID: "run"},
		NewRPCReady("test"),
	} {
		if err := output.Validate(jsonValue(t, value)); err != nil {
			t.Fatalf("%T: %v", value, err)
		}
	}
	for _, capability := range []string{"goal_run", "process_control"} {
		if !slices.Contains(NewRPCReady("test").Capabilities, capability) {
			t.Errorf("missing capability %s", capability)
		}
	}
}

func TestGoalRunCompletedSchemaRejectsInvalidFrames(t *testing.T) {
	output := resolveRPCSchema(t, "output.schema.json")
	frame := map[string]any{"type": "goal_run_completed", "request_id": "request", "goal_run_id": "run", "goal_id": "goal", "status": "finished"}
	for _, field := range []string{"type", "request_id", "goal_run_id", "goal_id", "status"} {
		value := maps.Clone(frame)
		delete(value, field)
		if err := output.Validate(jsonValue(t, value)); err == nil {
			t.Errorf("completion without %s accepted", field)
		}
	}
	for _, change := range []map[string]any{{"status": "complete"}, {"goal_status": "finished"}, {"goal_run_id": ""}, {"error": 1}, {"unknown": true}} {
		value := maps.Clone(frame)
		maps.Copy(value, change)
		if err := output.Validate(jsonValue(t, value)); err == nil {
			t.Errorf("invalid completion accepted: %v", change)
		}
	}
}

func TestGoalProcessControlSchemaErrorCodes(t *testing.T) {
	output := resolveRPCSchema(t, "output.schema.json")
	for _, tc := range []struct {
		command, code, message string
	}{
		{"goal_run", RPCGoalRunRejectedErrorCode, "goal run rejected before mutation"},
		{"goal_run", RPCGoalRunUnknownErrorCode, "goal run outcome requires authoritative refresh"},
		{"goal_inspect", RPCGoalRunRejectedErrorCode, "goal control requires a correlated id and type"},
		// Fixed app process-control errors currently map to rpcErrorCode's
		// existing invalid fallback, not a new process-specific wire code.
		{"process_control_list", "invalid", "app: managed process request rejected; review current session, handle and limits"},
		{"process_control_logs", "invalid", "app: managed process request rejected; review current session, handle and limits"},
		{"process_control_stop", "invalid", "app: stopping managed processes requires Default mode and current process_stop permission"},
	} {
		t.Run(tc.command+"/"+tc.code, func(t *testing.T) {
			frame := RPCResponse{ID: "request", Type: "response", Command: tc.command, Success: false, Error: tc.message, ErrorCode: tc.code}
			if err := output.Validate(jsonValue(t, frame)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
