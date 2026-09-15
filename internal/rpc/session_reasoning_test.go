package rpc

import (
	json "encoding/json/v2"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSessionReasoningParamsStrictBoundedCAS(t *testing.T) {
	state := protocol.RPCSessionReasoningState{SessionID: "session", BranchID: "branch", TipID: "", Provider: "p", Model: "m", Mode: protocol.ModePlan, PermissionMode: "deny", Thinking: protocol.ThinkingMedium, ReasoningSummary: protocol.ReasoningSummaryAuto, TextVerbosity: protocol.TextVerbosityLow}
	valid, _ := json.Marshal(protocol.RPCSessionReasoningSetParams{Expected: state, Field: "thinking", Value: "off"})
	if err := validateSessionReasoningParams(Request{Type: "session_reasoning_set", Params: valid}); err != nil {
		t.Fatal(err)
	}
	if err := validateSessionReasoningParams(Request{Type: "session_reasoning_get", Params: []byte(`{"session_id":"session"}`)}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{}`, `null`, `{"session_id":"session","debug_enabled":true}`, `{"session_id":null}`, `{"session_id":"session","session_id":"other"}`,
	} {
		if err := validateSessionReasoningParams(Request{Type: "session_reasoning_get", Params: []byte(raw)}); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	var fields map[string]any
	if err := json.Unmarshal(valid, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session_id", "branch_id", "tip_id", "provider", "model", "mode", "permission_mode", "thinking", "reasoning_summary", "text_verbosity"} {
		t.Run(key, func(t *testing.T) {
			var copy map[string]any
			if err := json.Unmarshal(valid, &copy); err != nil {
				t.Fatal(err)
			}
			delete(copy["expected"].(map[string]any), key)
			raw, _ := json.Marshal(copy)
			if err := validateSessionReasoningParams(Request{Type: "session_reasoning_set", Params: raw}); err == nil {
				t.Fatal("missing CAS field accepted")
			}
		})
	}
	for _, key := range []string{"scope", "debug_enabled", "plugins", "mcp", "settings", "config_path", "model"} {
		t.Run(key, func(t *testing.T) {
			fields[key] = "unsafe"
			defer delete(fields, key)
			raw, _ := json.Marshal(fields)
			if err := validateSessionReasoningParams(Request{Type: "session_reasoning_set", Params: raw}); err == nil {
				t.Fatal("extra field accepted")
			}
		})
	}
}
