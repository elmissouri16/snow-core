package protocol

import "testing"

func TestPluginManagementSchemas(t *testing.T) {
	request := resolveRPCSchema(t, "request.schema.json")
	response := resolveRPCSchema(t, "response.schema.json")
	status := PluginStatus{ID: "demo", Path: "/plugins/demo", Scope: "global", Enabled: false, Loaded: true, CanToggle: true, RestartRequired: true}
	for _, command := range []string{"plugin_enable", "plugin_disable"} {
		if err := request.Validate(jsonValue(t, RPCRequest{ID: "toggle", Type: command, Params: []byte(`{"id":"demo"}`)})); err != nil {
			t.Fatal(err)
		}
		if err := response.Validate(jsonValue(t, RPCResponse{ID: "toggle", Type: "response", Command: command, Success: true, Data: status})); err != nil {
			t.Fatal(err)
		}
		if err := request.Validate(jsonValue(t, RPCRequest{Type: command, Params: []byte(`{}`)})); err == nil {
			t.Fatal("schema accepted missing plugin id")
		}
	}
	if err := response.Validate(jsonValue(t, RPCResponse{Type: "response", Command: "plugin_statuses", Success: true, Data: []PluginStatus{status}})); err != nil {
		t.Fatal(err)
	}
	for command, data := range map[string]any{
		"plugins_list":    []PluginInfo{{ID: "demo", Name: "Demo", Version: "1", APIVersion: 2}},
		"plugin_commands": []PluginCommand{}, "plugin_views": []PluginView{},
		"plugin_command_cancel": map[string]bool{"canceled": true},
		"plugin_command_run":    map[string]any{"content": []ContentBlock{}, "is_error": false},
	} {
		if err := response.Validate(jsonValue(t, RPCResponse{Type: "response", Command: command, Success: true, Data: data})); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
	}
}
