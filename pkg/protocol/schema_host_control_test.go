package protocol

import (
	"maps"
	"slices"
	"testing"
)

func schemaHostDirectory() HostDirectoryIdentity {
	return HostDirectoryIdentity{Path: "/workspace", Device: "1", Inode: "2"}
}

func TestColdControlSchemaRequests(t *testing.T) {
	request := resolveRPCSchema(t, "control-request.schema.json")
	cases := map[string]any{
		"defaults_get":         HostDefaultsRequest{Scope: "global"},
		"defaults_update":      HostDefaultsUpdateRequest{Scope: "global", Revision: "revision", Global: &HostGlobalDefaultsPatch{Thinking: &HostStringOperation{Op: "set", Value: new("high")}, TextVerbosity: &HostStringOperation{Op: "reset"}}},
		"provider_status_list": struct{}{},
		"session_delete":       RPCSessionDeleteParams{SessionID: "saved-session"},
		"api_key_inspect":      HostAPIKeyInspectRequest{ProviderID: "openai-compatible"},
		"api_key_set":          HostAPIKeySetRequest{ProviderID: "openai-compatible", ExpectedRevision: "missing", Secret: "test-only-placeholder", ConfirmReplace: false},
		"project_prepare":      RPCProjectPrepareParams{OperationID: "operation", Parent: schemaHostDirectory(), Leaf: "child"},
		"project_clone_start":  RPCProjectCloneStartParams{OperationID: "operation", Child: schemaHostDirectory(), URL: "https://example.invalid/repository.git"},
		"project_cancel":       RPCProjectCancelParams{OperationID: "operation"},
	}
	for command, params := range cases {
		t.Run(command, func(t *testing.T) {
			value := jsonValue(t, map[string]any{"id": "request", "type": command, "params": params}).(map[string]any)
			if err := request.Validate(value); err != nil {
				t.Fatal(err)
			}
			bad := maps.Clone(value)
			bad["message"] = ""
			if err := request.Validate(bad); err == nil {
				t.Fatal("cold request accepted unrelated legacy field")
			}
		})
	}
	for _, params := range []any{
		HostDefaultsRequest{Scope: "project", CWD: "/workspace"},
		HostDefaultsUpdateRequest{Scope: "project", CWD: "/workspace", Revision: "revision", Project: &HostProjectDefaultsPatch{ProviderModel: &HostProviderModelOperation{Op: "set", Value: &HostProviderModel{Provider: "fake", Model: "model"}}}},
	} {
		command := "defaults_get"
		if _, ok := params.(HostDefaultsUpdateRequest); ok {
			command = "defaults_update"
		}
		if err := request.Validate(jsonValue(t, map[string]any{"type": command, "params": params})); err != nil {
			t.Fatal(err)
		}
	}
	if err := request.Validate(map[string]any{"type": "provider_status_list"}); err != nil {
		t.Fatal(err)
	}
}

func TestColdControlSchemaRejectsInvalidRequests(t *testing.T) {
	request := resolveRPCSchema(t, "control-request.schema.json")
	cases := []struct {
		command string
		params  any
	}{
		{"defaults_get", map[string]any{"scope": "project"}},
		{"defaults_get", map[string]any{"scope": "global", "cwd": "/project"}},
		{"defaults_update", map[string]any{"scope": "global", "global": map[string]any{}}},
		{"defaults_update", map[string]any{"scope": "global", "revision": "revision", "global": map[string]any{"thinking": map[string]any{"op": "reset", "value": "off"}}}},
		{"defaults_update", map[string]any{"scope": "global", "revision": "revision", "global": map[string]any{"thinking": map[string]any{"op": "set"}}}},
		{"defaults_update", map[string]any{"scope": "project", "cwd": "/workspace", "revision": "revision", "project": map[string]any{"reasoning_summary": map[string]any{"op": "reset"}}}},
		{"provider_status_list", map[string]any{"refresh": true}},
		{"api_key_set", map[string]any{"provider_id": "fake", "expected_revision": "missing", "secret": "test-only-placeholder"}},
		{"api_key_set", map[string]any{"provider_id": "fake", "expected_revision": "stale", "secret": "test-only-placeholder", "confirm_replace": false}},
		{"project_prepare", map[string]any{"operation_id": "operation", "parent": schemaHostDirectory(), "leaf": "../escape"}},
		{"project_clone_start", RPCProjectCloneStartParams{OperationID: "operation", Child: schemaHostDirectory(), URL: "ssh://example.invalid/repo"}},
		{"project_clone_start", RPCProjectCloneStartParams{OperationID: "operation", Child: schemaHostDirectory(), URL: "https://user@example.invalid/repo"}},
	}
	for _, tc := range cases {
		if err := request.Validate(jsonValue(t, map[string]any{"id": "request", "type": tc.command, "params": tc.params})); err == nil {
			t.Errorf("%s accepted invalid parameters", tc.command)
		}
	}
}

func TestColdControlSchemaOutputs(t *testing.T) {
	output := resolveRPCSchema(t, "control-output.schema.json")
	status := HostProviderStatus{ProviderID: "fake", State: "configured", Reason: "credential_present", CheckedLocally: true}
	defaults := HostDefaultsResponse{Scope: "global", Revision: "revision", AppliesTo: "future_runtime", Global: &HostGlobalDefaults{
		ProviderModel:    HostProviderModelDefault{Effective: HostProviderModel{Provider: "fake"}, Source: "builtin"},
		Thinking:         HostStringDefault{Effective: "off", Source: "builtin"},
		ReasoningSummary: HostStringDefault{Effective: "auto", Source: "builtin"},
		TextVerbosity:    HostStringDefault{Effective: "low", Source: "builtin"},
	}}
	prepared := RPCProjectPrepared{OperationID: "operation", Parent: schemaHostDirectory(), Child: HostDirectoryIdentity{Path: "/workspace/child", Device: "1", Inode: "3"}, Leaf: "child"}
	keyStatus := HostAPIKeyStatusResponse{ProviderID: "fake", APIKeySupported: true, ReplaceRequired: true, Revision: "missing", Status: status, CheckedLocally: true, AppliesTo: "future_runtime"}
	cases := map[string]any{
		"defaults_get": defaults, "defaults_update": defaults,
		"provider_status_list": HostProviderStatusResponse{Providers: []HostProviderStatus{status}, CheckedLocally: true},
		"api_key_inspect":      keyStatus, "api_key_set": keyStatus,
		"project_prepare": prepared, "project_clone_start": prepared,
		"project_cancel": RPCProjectCancelParams{OperationID: "operation"},
		"session_delete": RPCSessionDeleteResult{SessionID: "saved-session", Deleted: true},
	}
	for command, data := range cases {
		t.Run(command, func(t *testing.T) {
			frame := RPCResponse{ID: "request", Type: "response", Command: command, Success: true, Data: data}
			if err := output.Validate(jsonValue(t, frame)); err != nil {
				t.Fatal(err)
			}
			badData := jsonValue(t, data).(map[string]any)
			badData["secret"] = "must-not-appear"
			frame.Data = badData
			if err := output.Validate(jsonValue(t, frame)); err == nil {
				t.Fatal("public result accepted a secret field")
			}
		})
	}
	for _, code := range []string{"invalid", "unsupported", "unavailable", "revision_conflict", "canceled", "busy", "exists", "identity_changed"} {
		frame := RPCResponse{ID: "request", Type: "response", Command: "defaults_update", Error: "fixed public error", ErrorCode: code}
		if err := output.Validate(jsonValue(t, frame)); err != nil {
			t.Errorf("%s: %v", code, err)
		}
	}
	for _, status := range []HostOperationStatus{HostOperationSucceeded, HostOperationFailed, HostOperationCanceled, HostOperationTimedOut, HostOperationOutputLimit, HostOperationCleanupFailed} {
		frame := RPCProjectCompleted{Type: RPCTypeProjectCompleted, RequestID: "request", OperationID: "operation", Child: prepared.Child, Status: status}
		if err := output.Validate(jsonValue(t, frame)); err != nil {
			t.Errorf("%s: %v", status, err)
		}
		bad := jsonValue(t, frame).(map[string]any)
		bad["output"] = "private Git output"
		if err := output.Validate(bad); err == nil {
			t.Error("project completion accepted process output")
		}
	}
}

func TestColdControlReadyIsSeparateFromEagerInventory(t *testing.T) {
	ready := NewRPCControlReady("test")
	want := []string{RPCControlCapability, RPCDefaultsControlCapability, RPCProviderStatusCapability}
	if !slices.Equal(ready.Capabilities, want) || ready.MaxInputBytes != RPCControlMaxInputBytes {
		t.Fatalf("unexpected baseline control ready: %#v", ready)
	}
	if err := resolveRPCSchema(t, "control-output.schema.json").Validate(jsonValue(t, ready)); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"defaults_get", "defaults_update", "provider_status_list", "api_key_inspect", "api_key_set", "project_prepare", "project_clone_start", "project_cancel"} {
		if slices.Contains(KnownRPCCommands(), command) {
			t.Errorf("cold-only command %s in eager inventory", command)
		}
	}
}

func TestColdControlSchemaAllowsOptionalServiceCapabilities(t *testing.T) {
	output := resolveRPCSchema(t, "control-output.schema.json")
	for _, capabilities := range [][]string{
		{HostOperationsCapability, RPCProjectCreateCapability, RPCProjectCloneCapability},
		{RPCAPIKeyControlCapability},
		{RPCSessionDeleteControlCapability},
		{HostOperationsCapability, RPCProjectCreateCapability, RPCProjectCloneCapability, RPCAPIKeyControlCapability, RPCSessionDeleteControlCapability},
	} {
		ready := NewRPCControlReady("test")
		ready.Capabilities = append(ready.Capabilities, capabilities...)
		if err := output.Validate(jsonValue(t, ready)); err != nil {
			t.Fatalf("optional capabilities %v: %v", capabilities, err)
		}
	}
}
