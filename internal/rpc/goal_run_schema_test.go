package rpc

import (
	"testing"
)

func TestGoalRunStrictSchemaContract(t *testing.T) {
	request := resolveWireSchema(t, "request.schema.json")
	for _, frame := range []string{
		`{"id":"run","type":"goal_run","params":{"action":"create","session_id":"s","branch_id":"b","expected_tip_id":"","expected_goal_id":"","objective":"work"}}`,
		`{"id":"run","type":"goal_run","params":{"action":"create","session_id":"s","branch_id":"b","expected_tip_id":"t","expected_goal_id":"","objective":"work","token_budget":100}}`,
		`{"id":"run","type":"goal_run","params":{"action":"resume","session_id":"s","branch_id":"b","expected_tip_id":"t","expected_goal_id":"g"}}`,
		`{"id":"inspect","type":"goal_inspect","params":{"session_id":"s"}}`,
	} {
		if err := request.Validate(decodedJSON(t, []byte(frame))); err != nil {
			t.Fatalf("valid request %s: %v", frame, err)
		}
	}
	for _, frame := range []string{
		`{"id":"run","type":"goal_run","params":{"action":"create","session_id":"s","branch_id":"b","expected_tip_id":"","objective":"work"}}`,
		`{"id":"run","type":"goal_run","params":{"action":"create","session_id":"s","branch_id":"b","expected_tip_id":"","expected_goal_id":"","objective":"work","token_budget":0}}`,
		`{"id":"run","type":"goal_run","params":{"action":"resume","session_id":"s","branch_id":"b","expected_tip_id":"t","expected_goal_id":"g","objective":""}}`,
		`{"id":"run","type":"goal_run","message":"","params":{"action":"resume","session_id":"s","branch_id":"b","expected_tip_id":"t","expected_goal_id":"g"}}`,
	} {
		if err := request.Validate(decodedJSON(t, []byte(frame))); err == nil {
			t.Fatalf("unsafe request accepted %s", frame)
		}
	}
	output := resolveWireSchema(t, "output.schema.json")
	for _, frame := range []string{
		`{"id":"run","type":"response","command":"goal_run","success":true,"data":{"goal_run_id":"r","goal_id":"g","session_id":"s","branch_id":"b"}}`,
		`{"type":"goal_run_completed","request_id":"run","goal_run_id":"r","goal_id":"g","status":"finished","goal_status":"paused"}`,
		`{"id":"inspect","type":"response","command":"goal_inspect","success":true,"data":{"session_id":"s","branch_id":"b","tip_id":"","goal":null,"deferred":false}}`,
	} {
		if err := output.Validate(decodedJSON(t, []byte(frame))); err != nil {
			t.Fatalf("valid output %s: %v", frame, err)
		}
	}
}
