package app

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func reasoningTestApp(t *testing.T) *App {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SNOW_HOME", filepath.Join(home, "snow-home"))
	a, err := New(t.Context(), Options{Provider: "fake", Permission: "ask", NoSession: true, CWD: filepath.Join(home, "project"), NoMCP: true, NoPlugins: true, NoSkills: true, ManagedExplicitGoals: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	model := a.Agent.Model()
	model.SupportsThinking = true
	model.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh}
	model.SupportsReasoningSummary = new(true)
	model.SupportsVerbosity = true
	if err := a.Agent.SetModel(model); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.SetThinking(protocol.ThinkingLow); err != nil {
		t.Fatal(err)
	}
	return a
}
func appReasoning(t *testing.T, a *App) protocol.RPCSessionReasoning {
	t.Helper()
	value, err := a.SessionReasoning(t.Context(), protocol.RPCSessionReasoningGetParams{SessionID: a.Session.ID()})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSessionReasoningNoConfigOrHistoryWritesAndModeOverrides(t *testing.T) {
	a := reasoningTestApp(t)
	a.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	a.PersistedCfg.TUI.Theme = "nord"
	if err := config.Save(a.ConfigPath, a.PersistedCfg); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(a.Cfg, json.Deterministic(true))
	persisted, _ := json.Marshal(a.PersistedCfg, json.Deterministic(true))
	tip := a.Session.BranchTip()
	check := func() {
		t.Helper()
		got, err := os.ReadFile(a.ConfigPath)
		if err != nil || !bytes.Equal(got, disk) {
			t.Fatalf("configuration bytes changed: %v", err)
		}
		c, _ := json.Marshal(a.Cfg, json.Deterministic(true))
		p, _ := json.Marshal(a.PersistedCfg, json.Deterministic(true))
		if !bytes.Equal(c, cfg) || !bytes.Equal(p, persisted) {
			t.Fatal("in-memory defaults changed")
		}
		if a.Session.BranchTip() != tip {
			t.Fatal("reasoning changed history tip")
		}
		messages, err := a.Session.Messages()
		if err != nil || len(messages) != 0 {
			t.Fatalf("reasoning wrote history: %v %v", messages, err)
		}
	}
	apply := func(field, value string) protocol.RPCSessionReasoning {
		t.Helper()
		before := appReasoning(t, a)
		want := before.RPCSessionReasoningState
		switch field {
		case "thinking":
			want.Thinking = protocol.ThinkingLevel(value)
		case "reasoning_summary":
			want.ReasoningSummary = protocol.ReasoningSummary(value)
		case "text_verbosity":
			want.TextVerbosity = protocol.TextVerbosity(value)
		}
		after, err := a.SetSessionReasoning(t.Context(), protocol.RPCSessionReasoningSetParams{Expected: before.RPCSessionReasoningState, Field: field, Value: value})
		if err != nil || after.RPCSessionReasoningState != want {
			t.Fatalf("update: %+v %v", after, err)
		}
		check()
		return after
	}
	check()
	apply("thinking", "high")
	if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if got := appReasoning(t, a); got.Thinking != protocol.ThinkingMedium {
		t.Fatalf("Plan effective = %s", got.Thinking)
	}
	apply("thinking", "low")
	apply("reasoning_summary", "detailed")
	apply("text_verbosity", "high")
	if err := a.Agent.SetMode(protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	if got := appReasoning(t, a); got.Thinking != protocol.ThinkingHigh {
		t.Fatalf("Default overwritten: %+v", got)
	}
	if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if got := appReasoning(t, a); got.Thinking != protocol.ThinkingLow {
		t.Fatalf("Plan override lost: %+v", got)
	}
	check()
}

func TestSessionReasoningExactAuthorityRejectsStaleAndUnsafeFields(t *testing.T) {
	a := reasoningTestApp(t)
	before := appReasoning(t, a)
	for name, mutate := range map[string]func(*protocol.RPCSessionReasoningSetParams){
		"session":     func(p *protocol.RPCSessionReasoningSetParams) { p.Expected.SessionID = "other" },
		"branch":      func(p *protocol.RPCSessionReasoningSetParams) { p.Expected.BranchID = "other" },
		"tip":         func(p *protocol.RPCSessionReasoningSetParams) { p.Expected.TipID = "other" },
		"provider":    func(p *protocol.RPCSessionReasoningSetParams) { p.Expected.Provider = "other" },
		"model":       func(p *protocol.RPCSessionReasoningSetParams) { p.Expected.Model = "other" },
		"mode":        func(p *protocol.RPCSessionReasoningSetParams) { p.Expected.Mode = protocol.ModePlan },
		"permissions": func(p *protocol.RPCSessionReasoningSetParams) { p.Expected.PermissionMode = "allow" },
		"thinking":    func(p *protocol.RPCSessionReasoningSetParams) { p.Expected.Thinking = protocol.ThinkingHigh },
		"summary": func(p *protocol.RPCSessionReasoningSetParams) {
			p.Expected.ReasoningSummary = protocol.ReasoningSummaryDetailed
		},
		"verbosity":       func(p *protocol.RPCSessionReasoningSetParams) { p.Expected.TextVerbosity = protocol.TextVerbosityHigh },
		"invalid level":   func(p *protocol.RPCSessionReasoningSetParams) { p.Value = "maximum-ultra" },
		"debug":           func(p *protocol.RPCSessionReasoningSetParams) { p.Field = "debug_enabled" },
		"plugins":         func(p *protocol.RPCSessionReasoningSetParams) { p.Field = "plugins" },
		"arbitrary scope": func(p *protocol.RPCSessionReasoningSetParams) { p.Field = "defaults" },
	} {
		t.Run(name, func(t *testing.T) {
			p := protocol.RPCSessionReasoningSetParams{Expected: before.RPCSessionReasoningState, Field: "thinking", Value: "high"}
			mutate(&p)
			if _, err := a.SetSessionReasoning(t.Context(), p); err == nil {
				t.Fatal("unsafe mutation accepted")
			}
			if got := appReasoning(t, a); got.RPCSessionReasoningState != before.RPCSessionReasoningState {
				t.Fatalf("rejection mutated: %+v", got)
			}
		})
	}
}

func TestSessionReasoningInvalidatesPreparedHistoryOnlyAfterMutation(t *testing.T) {
	a := reasoningTestApp(t)
	before := appReasoning(t, a)
	a.messageEdits = map[string]messageEditAuthorization{"prepared": {}}
	a.branchRestores = map[string]branchRestoreAuthorization{"prepared": {}}
	if _, err := a.SetSessionReasoning(t.Context(), protocol.RPCSessionReasoningSetParams{Expected: before.RPCSessionReasoningState, Field: "plugins", Value: "true"}); !errors.Is(err, agent.ErrSessionReasoningUnsupported) {
		t.Fatal(err)
	}
	if len(a.messageEdits) != 1 || len(a.branchRestores) != 1 {
		t.Fatal("rejected update consumed preparations")
	}
	if _, err := a.SetSessionReasoning(t.Context(), protocol.RPCSessionReasoningSetParams{Expected: before.RPCSessionReasoningState, Field: "thinking", Value: "high"}); err != nil {
		t.Fatal(err)
	}
	if len(a.messageEdits) != 0 || len(a.branchRestores) != 0 {
		t.Fatal("old preparations retained after reasoning changed")
	}
}

func TestSessionReasoningUnknownCapabilitiesAreDisabled(t *testing.T) {
	a := reasoningTestApp(t)
	model := a.Agent.Model()
	model.SupportsReasoningSummary = nil
	model.SupportsVerbosity = false
	model.SupportsThinking = false
	if err := a.Agent.SetModel(model); err != nil {
		t.Fatal(err)
	}
	before := appReasoning(t, a)
	if len(before.ReasoningSummaries) != 0 || len(before.TextVerbosities) != 0 || len(before.ThinkingLevels) != 1 || before.ThinkingLevels[0] != protocol.ThinkingOff {
		t.Fatalf("guessed support: %+v", before)
	}
	for _, p := range []protocol.RPCSessionReasoningSetParams{{Expected: before.RPCSessionReasoningState, Field: "thinking", Value: "high"}, {Expected: before.RPCSessionReasoningState, Field: "reasoning_summary", Value: "detailed"}, {Expected: before.RPCSessionReasoningState, Field: "text_verbosity", Value: "high"}} {
		if _, err := a.SetSessionReasoning(t.Context(), p); !errors.Is(err, agent.ErrSessionReasoningUnsupported) {
			t.Fatalf("unknown support accepted: %v", err)
		}
	}
}
