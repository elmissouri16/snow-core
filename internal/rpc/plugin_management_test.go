package rpc

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func TestRPCPluginManagementAndValidation(t *testing.T) {
	a := newRuntimeRPCApp(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"snow-plugin.json": `{"id":"demo","name":"Demo","version":"1","api_version":2,"entry":"main.js"}`,
		"main.js":          `throw new Error("must not execute");`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.UpdateJavaScriptPlugins(a.ConfigPath, true, func(specs map[string]plugin.JavaScriptSpec) error {
		specs["demo"] = plugin.JavaScriptSpec{Path: dir, Disabled: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	s := New(t.Context(), a, &bytes.Buffer{}, &out)
	for _, req := range []Request{
		{ID: "list", Type: "plugin_statuses"},
		{ID: "enable", Type: "plugin_enable", Params: []byte(`{"id":"demo"}`)},
		{ID: "disable", Type: "plugin_disable", Params: []byte(`{"id":"demo"}`)},
	} {
		if err := s.handle(t.Context(), req); err != nil {
			t.Fatal(err)
		}
	}
	responses := decodeRuntimeResponses(t, &out)
	if len(responses) != 3 || responses[0]["id"] != "list" || responses[1]["id"] != "enable" || responses[2]["id"] != "disable" {
		t.Fatalf("correlation=%+v", responses)
	}
	listed := responses[0]["data"].([]any)[0].(map[string]any)
	if listed["enabled"] != false || listed["loaded"] != false || listed["can_toggle"] != true {
		t.Fatalf("list=%+v", listed)
	}
	for i, enabled := range []bool{true, false} {
		data := responses[i+1]["data"].(map[string]any)
		if data["id"] != "demo" || data["enabled"] != enabled || data["loaded"] != false || data["restart_required"] != enabled {
			t.Fatalf("toggle=%+v", data)
		}
	}
	for _, raw := range []string{`{}`, `null`, `{"id":""}`, `{"id":"missing"}`, `{"id":"demo","enabled":true}`, `{"command":"demo"}`, `{"id":true}`} {
		if err := s.handle(t.Context(), Request{Type: "plugin_enable", Params: []byte(raw)}); err == nil {
			t.Fatalf("accepted invalid params %s", raw)
		}
	}
	if err := s.handle(t.Context(), Request{Type: "plugin_statuses", Params: []byte(`{"id":"demo"}`)}); err == nil {
		t.Fatal("list accepted unknown params")
	}
}
