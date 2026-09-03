//go:build darwin || linux

package rpc

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
)

func TestRPCSettingsGetAndUpdateAreCorrelatedAndSecretFree(t *testing.T) {
	a := newRuntimeRPCApp(t)
	var out bytes.Buffer
	s := New(t.Context(), a, &bytes.Buffer{}, &out)
	if err := s.handle(t.Context(), Request{ID: "get", Type: "settings_get"}); err != nil {
		t.Fatal(err)
	}
	if err := s.handle(t.Context(), Request{ID: "set", Type: "settings_update", Params: json.RawMessage(`{"provider":"fake","model":"fake-1","thinking":"off","reasoning_summary":"detailed","text_verbosity":"high","debug_enabled":true,"subagents_enabled":false,"subagents_max_concurrent":5,"skills_enabled":true,"auto_update":true}`)}); err != nil {
		t.Fatal(err)
	}
	responses := decodeRuntimeResponses(t, &out)
	if len(responses) != 2 {
		t.Fatalf("responses = %#v", responses)
	}
	if responses[0]["id"] != "get" || responses[0]["command"] != "settings_get" {
		t.Fatalf("get response = %#v", responses[0])
	}
	updated := responses[1]["data"].(map[string]any)
	if responses[1]["id"] != "set" || responses[1]["command"] != "settings_update" {
		t.Fatalf("update response = %#v", responses[1])
	}
	if updated["provider"] != "fake" || updated["model"] != "fake-1" || updated["thinking"] != "off" ||
		updated["reasoning_summary"] != "detailed" || updated["text_verbosity"] != "high" || updated["debug_enabled"] != true ||
		updated["subagents_enabled"] != false || updated["subagents_max_concurrent"] != float64(5) ||
		updated["skills_enabled"] != true || updated["restart_required"] != true ||
		updated["auto_update"] != true || updated["update_check_on_startup"] != true {
		t.Fatalf("updated settings = %#v", updated)
	}
	for _, forbidden := range []string{"secret", "api_key", "headers", "environment", "providers"} {
		if _, ok := updated[forbidden]; ok {
			t.Fatalf("settings exposed forbidden field %q: %#v", forbidden, updated)
		}
	}
}

func TestRPCSettingsRejectsInvalidRequests(t *testing.T) {
	a := newRuntimeRPCApp(t)
	s := New(t.Context(), a, &bytes.Buffer{}, &bytes.Buffer{})
	if err := s.handle(t.Context(), Request{Type: "settings_get", Params: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("settings_get accepted params")
	}
	if err := s.handle(t.Context(), Request{Type: "settings_update", Params: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("settings_update accepted empty params")
	}
	if err := s.handle(t.Context(), Request{Type: "settings_get", Secret: "must-not-be-accepted"}); err == nil {
		t.Fatal("settings_get accepted a secret field")
	}
	if err := s.handle(t.Context(), Request{Type: "settings_update", Params: json.RawMessage(`{"api_key":"must-not-be-accepted"}`)}); err == nil {
		t.Fatal("settings_update accepted an unknown secret-bearing field")
	}
}

func TestRPCLiveSettingCommandsPersistCanonicalChoices(t *testing.T) {
	a := newRuntimeRPCApp(t)
	var out bytes.Buffer
	s := New(t.Context(), a, &bytes.Buffer{}, &out)
	for _, req := range []Request{
		{ID: "model", Type: "set_model", Provider: "fake", Model: "fake-1", Thinking: "off"},
		{ID: "thinking", Type: "set_thinking", Thinking: "off"},
		{ID: "summary", Type: "set_reasoning_summary", ReasoningSummary: "concise"},
		{ID: "verbosity", Type: "set_text_verbosity", TextVerbosity: "high"},
		{ID: "debug", Type: "debug_enable"},
	} {
		if err := s.handle(t.Context(), req); err != nil {
			t.Fatalf("%s: %v", req.Type, err)
		}
	}
	responses := decodeRuntimeResponses(t, &out)
	if len(responses) != 5 {
		t.Fatalf("responses = %#v", responses)
	}
	for i, response := range responses {
		if response["success"] != true {
			t.Fatalf("response %d = %#v", i, response)
		}
	}

	persisted, err := config.Load(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := persisted.ProjectSelections[a.CWD()]
	if !ok || selection.Provider != "fake" || selection.Model != "fake-1" || selection.Thinking != "off" {
		t.Fatalf("persisted project selection = %+v, ok=%v", selection, ok)
	}
	if persisted.ReasoningSummary != "concise" || persisted.TextVerbosity != "high" || !persisted.Debug.Enabled {
		t.Fatalf("persisted live settings = summary:%q verbosity:%q debug:%v", persisted.ReasoningSummary, persisted.TextVerbosity, persisted.Debug.Enabled)
	}
}
