package protocol

import (
	"slices"
	"strings"
	"testing"
)

func TestColdSessionDeleteSchemaRequiresExactIDOnly(t *testing.T) {
	schema := resolveRPCSchema(t, "control-request.schema.json")
	for _, params := range []any{
		nil,
		map[string]any{},
		map[string]any{"session_id": ""},
		map[string]any{"session_id": "../saved"},
		map[string]any{"session_id": "saved.db"},
		map[string]any{"session_id": " saved "},
		map[string]any{"session_id": "saved\n"},
		map[string]any{"session_id": "é"},
		map[string]any{"session_id": strings.Repeat("a", 129)},
		map[string]any{"session_id": "saved", "cwd": "/workspace"},
		map[string]any{"session_id": "saved", "path": "/workspace/session.db"},
		map[string]any{"session_id": "saved", "scope": "global"},
		map[string]any{"session_id": "saved", "instance_id": "owner"},
		map[string]any{"session_id": "saved", "confirm": "delete"},
	} {
		frame := map[string]any{"type": "session_delete", "params": params}
		if err := schema.Validate(frame); err == nil {
			t.Errorf("accepted invalid cold deletion: %#v", frame)
		}
	}
	if err := schema.Validate(map[string]any{"type": "session_delete"}); err == nil {
		t.Error("accepted omitted params")
	}
	for _, id := range []string{"a", "1789411100929-ngungt0j", "A_z-09", strings.Repeat("a", 128)} {
		if err := schema.Validate(jsonValue(t, map[string]any{"type": "session_delete", "params": RPCSessionDeleteParams{SessionID: id}})); err != nil {
			t.Errorf("valid ID %q: %v", id, err)
		}
	}
}

func TestColdSessionDeleteSchemaOnlyAcknowledgesCertainDeletion(t *testing.T) {
	schema := resolveRPCSchema(t, "control-output.schema.json")
	for _, data := range []any{
		nil,
		map[string]any{},
		map[string]any{"session_id": "saved"},
		map[string]any{"session_id": "saved", "deleted": false},
		map[string]any{"session_id": "", "deleted": true},
		map[string]any{"session_id": "../saved", "deleted": true},
		map[string]any{"session_id": "saved", "deleted": true, "path": "/private"},
		map[string]any{"session_id": "saved", "deleted": true, "cleanup_error": "private"},
	} {
		frame := RPCResponse{Type: "response", Command: "session_delete", Success: true, Data: data}
		if err := schema.Validate(jsonValue(t, frame)); err == nil {
			t.Errorf("accepted uncertain or private success: %#v", data)
		}
	}
	for _, code := range []string{"invalid", "unsupported", "unavailable"} {
		frame := RPCResponse{Type: "response", Command: "session_delete", ErrorCode: code, Error: "fixed public error"}
		if err := schema.Validate(jsonValue(t, frame)); err != nil {
			t.Errorf("%s: %v", code, err)
		}
		frame.Data = RPCSessionDeleteResult{SessionID: "saved", Deleted: true}
		if err := schema.Validate(jsonValue(t, frame)); err == nil {
			t.Error("failure accepted success data")
		}
	}
}

func TestColdSessionDeleteCapabilityRemainsOptionalAndEagerUnchanged(t *testing.T) {
	if slices.Contains(NewRPCControlReady("test").Capabilities, RPCSessionDeleteControlCapability) {
		t.Fatal("baseline control grants uncomposed deletion capability")
	}
	if slices.Contains(NewRPCReady("test").Capabilities, RPCSessionDeleteControlCapability) {
		t.Fatal("eager advertises cold-only capability")
	}
	if !slices.Contains(KnownRPCCommands(), "session_delete") {
		t.Fatal("removed eager session_delete")
	}
	// Cold parameters are deliberately narrower than the established eager API.
	request := map[string]any{"type": "session_delete", "params": map[string]any{"session_id": "saved"}}
	if err := resolveRPCSchema(t, "request.schema.json").Validate(request); err != nil {
		t.Fatal("eager request changed", err)
	}
	result := RPCResponse{Type: "response", Command: "session_delete", Success: true, Data: RPCSessionDeleteResult{SessionID: "saved", Deleted: true}}
	if err := resolveRPCSchema(t, "response.schema.json").Validate(jsonValue(t, result)); err != nil {
		t.Fatal("eager result changed", err)
	}
}
