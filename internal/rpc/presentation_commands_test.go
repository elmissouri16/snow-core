//go:build darwin || linux

package rpc

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRPCPresentationCommandsAreCorrelatedBoundedAndStrict(t *testing.T) {
	a := newRuntimeRPCApp(t)
	var out bytes.Buffer
	s := New(t.Context(), a, &bytes.Buffer{}, &out)
	requests := []Request{
		{ID: "themes", Type: "themes_list"},
		{ID: "keys", Type: "keybindings_get"},
		{ID: "update", Type: "keybindings_update", Params: json.RawMessage(`{"scope":"global","bindings":{"models":["alt+z"]}}`)},
	}
	for _, req := range requests {
		if err := s.handle(t.Context(), req); err != nil {
			t.Fatalf("%s: %v", req.Type, err)
		}
	}
	responses := decodeRuntimeResponses(t, &out)
	if len(responses) != 3 {
		t.Fatalf("responses = %#v", responses)
	}
	if responses[0]["id"] != "themes" || responses[0]["command"] != "themes_list" {
		t.Fatalf("themes response = %#v", responses[0])
	}
	themeData := responses[0]["data"].(map[string]any)
	themes := themeData["themes"].([]any)
	if len(themes) < 4 || len(themes) > 132 {
		t.Fatalf("themes count = %d", len(themes))
	}
	first := themes[0].(map[string]any)
	if first["name"] != "default" || first["scope"] != "builtin" {
		t.Fatalf("first theme = %#v", first)
	}
	if _, exposed := first["path"]; exposed {
		t.Fatalf("theme exposed path: %#v", first)
	}

	updated := responses[2]["data"].(map[string]any)
	actions := updated["actions"].([]any)
	if len(actions) != 31 {
		t.Fatalf("actions count = %d", len(actions))
	}
	found := false
	for _, raw := range actions {
		action := raw.(map[string]any)
		if action["name"] == "models" {
			found = true
			if action["source"] != "global" || action["effective"].([]any)[0] != "alt+z" {
				t.Fatalf("models action = %#v", action)
			}
		}
	}
	if !found {
		t.Fatal("models action missing")
	}
}

func TestRPCPresentationCommandsRejectUnknownFieldsCollisionsAndUntrustedProject(t *testing.T) {
	a := newRuntimeRPCApp(t)
	s := New(t.Context(), a, &bytes.Buffer{}, &bytes.Buffer{})
	cases := []Request{
		{Type: "themes_list", Params: json.RawMessage(`{}`)},
		{Type: "keybindings_get", Secret: "forbidden"},
		{Type: "keybindings_update", Params: json.RawMessage(`{"scope":"global","bindings":{"submit":["ctrl+t"]}}`)},
		{Type: "keybindings_update", Params: json.RawMessage(`{"scope":"project","reset":["models"]}`)},
		{Type: "keybindings_update", Params: json.RawMessage(`{"scope":"global","bindings":{"models":["alt+z"]},"unknown":true}`)},
	}
	for _, req := range cases {
		if err := s.handle(t.Context(), req); err == nil {
			t.Fatalf("%s accepted invalid request %#v", req.Type, req)
		}
	}
}
