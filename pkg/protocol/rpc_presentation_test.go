package protocol

import "testing"

func TestPresentationRPCValuesConformToSchemas(t *testing.T) {
	output := resolveRPCSchema(t, "output.schema.json")
	themeSchema := resolveRPCSchema(t, "theme-catalog.schema.json")
	responseSchema := resolveRPCSchema(t, "response.schema.json")
	pair := RPCAdaptiveColor{Light: "#000000", Dark: "#ffffff"}
	colors := RPCThemeColors{
		Accent: pair, Muted: pair, Foreground: pair, Warning: pair,
		Error: pair, Success: pair, Separator: pair,
	}
	themes := []RPCThemeDescriptor{
		{Name: "default", DisplayName: "Snow", Scope: "builtin", Colors: colors},
		{Name: "frost", DisplayName: "Frost", Scope: "builtin", Colors: colors},
		{Name: "ember", DisplayName: "Ember", Scope: "builtin", Colors: colors},
		{Name: "aurora", DisplayName: "Aurora", Scope: "builtin", Colors: colors},
	}
	if err := themeSchema.Validate(jsonValue(t, RPCThemeCatalog{Selected: "default", Themes: themes})); err != nil {
		t.Fatalf("theme catalog does not conform: %v", err)
	}
	bindings := RPCKeybindings{Actions: make([]RPCKeybindingAction, 0, 31)}
	for _, name := range []string{
		"submit", "follow_up", "newline", "paste", "abort", "quit", "toggle_mode", "thinking", "models", "agents", "processes",
		"page_up", "page_down", "top", "bottom", "line_up", "line_down", "picker_up", "picker_down", "picker_previous", "picker_next",
		"picker_page_up", "picker_page_down", "picker_top", "picker_bottom", "accept", "close", "branch_fork", "branch_rename", "branch_delete", "confirm",
	} {
		bindings.Actions = append(bindings.Actions, RPCKeybindingAction{Name: name, Global: []string{}, Project: []string{}, Effective: []string{"x"}, Source: "default"})
	}
	responses := []RPCResponse{
		{ID: "themes", Type: "response", Command: "themes_list", Success: true, Data: RPCThemeCatalog{Selected: "default", Themes: themes}},
		{ID: "keys", Type: "response", Command: "keybindings_get", Success: true, Data: bindings},
		{ID: "update", Type: "response", Command: "keybindings_update", Success: true, Data: bindings},
	}
	if err := responseSchema.Validate(jsonValue(t, responses[0])); err != nil {
		t.Fatalf("theme response does not conform directly: %v", err)
	}
	for _, response := range responses {
		if err := output.Validate(jsonValue(t, response)); err != nil {
			t.Fatalf("%s response does not conform: %v", response.Command, err)
		}
	}
}
