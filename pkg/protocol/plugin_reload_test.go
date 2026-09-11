package protocol

import "testing"

func TestPluginReloadRequestAndReceiptSchemas(t *testing.T) {
	request := resolveRPCSchema(t, "request.schema.json")
	for _, params := range []map[string]any{{}, {"id": ""}, {"id": true}, {"id": "demo", "extra": 1}} {
		if err := request.Validate(map[string]any{"type": "plugin_reload", "params": params}); err == nil {
			t.Fatalf("invalid reload accepted %+v", params)
		}
	}
	if err := request.Validate(map[string]any{"type": "plugin_reload", "params": map[string]any{"id": "demo"}}); err != nil {
		t.Fatal(err)
	}
	response := resolveRPCSchema(t, "response.schema.json")
	receipt := PluginReloadResult{PluginID: "demo", Applied: true, Generation: 2, Fingerprint: "test", Diagnostics: []PluginReloadDiagnostic{{Phase: "ready", Message: "test"}}}
	if err := response.Validate(jsonValue(t, map[string]any{"type": "response", "command": "plugin_reload", "success": true, "data": receipt})); err != nil {
		t.Fatal(err)
	}
	if err := response.Validate(map[string]any{"type": "response", "command": "plugin_reload", "success": true, "data": map[string]any{"applied": true}}); err == nil {
		t.Fatal("incomplete reload receipt accepted")
	}
}
