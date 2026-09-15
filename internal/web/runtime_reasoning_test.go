package web

import (
	"encoding/json/v2"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func reasoningTestManager(t *testing.T, mode string) (*RuntimeManager, Project, RuntimeSnapshot, string) {
	t.Helper()
	m, projects, log := runtimeTestManager(t, mode)
	m.env = append(m.env, "SNOW_WEB_REASONING_TEST_CHILD=1")
	snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return m, projects[0], snapshot, log
}
func reasoningLog(t *testing.T, path string) string {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func reasoningInspect(t *testing.T, m *RuntimeManager, p Project, s RuntimeSnapshot) RuntimeReasoning {
	t.Helper()
	v, e := m.InspectReasoning(t.Context(), p.ID, s.InstanceID, s.SessionID)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func reasoningInput(view RuntimeReasoning, field, value string) RuntimeReasoningInput {
	return RuntimeReasoningInput{Expected: view, Scope: "session", Field: field, Value: value, Confirm: true}
}

func TestReasoningCapabilitiesDoNotGuessFromModelNames(t *testing.T) {
	for _, mode := range []string{"", "unsupported", "unknown-summary", "no-discovery"} {
		t.Run(mode, func(t *testing.T) {
			m, p, s, log := reasoningTestManager(t, mode)
			view := reasoningInspect(t, m, p, s)
			if !view.CurrentSessionAvailable || view.DefaultsAvailable {
				t.Fatalf("scope: %+v", view)
			}
			if mode == "unsupported" {
				if len(view.ThinkingLevels)+len(view.ReasoningSummaries)+len(view.TextVerbosities) != 0 {
					t.Fatalf("guessed: %+v", view)
				}
			} else {
				if !slices.Equal(view.ThinkingLevels, []string{"off", "low", "medium", "high"}) || !slices.Equal(view.TextVerbosities, []string{"low", "medium", "high"}) {
					t.Fatalf("options: %+v", view)
				}
			}
			if mode == "unknown-summary" && len(view.ReasoningSummaries) != 0 {
				t.Fatal("nil summary support guessed")
			}
			data, _ := json.Marshal(view)
			if strings.Contains(string(data), "debug") || strings.Contains(string(data), "skills") || strings.Contains(string(data), "subagent") {
				t.Fatal("unrelated settings exposed")
			}
			if strings.Contains(reasoningLog(t, log), "session_reasoning_set") || strings.Contains(reasoningLog(t, log), "settings_") || strings.Contains(reasoningLog(t, log), "models_discover") {
				t.Fatal("inspection mutated or used persisted settings/network discovery")
			}
		})
	}
}

func TestReasoningSingleFieldSessionChangesPreserveModeOverrides(t *testing.T) {
	m, p, s, log := reasoningTestManager(t, "")
	apply := func(field, value string) RuntimeReasoning {
		t.Helper()
		view := reasoningInspect(t, m, p, s)
		before := reasoningLog(t, log)
		got, err := m.SetReasoning(t.Context(), p.ID, s.InstanceID, reasoningInput(view, field, value))
		if err != nil {
			t.Fatal(err)
		}
		delta := strings.TrimPrefix(reasoningLog(t, log), before)
		if strings.Count(delta, "session_reasoning_set\n") != 1 || !strings.Contains(delta, `"field":"`+field+`","value":"`+value+`"`) {
			t.Fatalf("mutation: %s", delta)
		}
		if got.Revision <= view.Revision || got.PermissionMode != "ask" || got.Provider != "fixture" {
			t.Fatalf("authority: %+v", got)
		}
		return got
	}
	apply("thinking", "high")
	if err := m.SetMode(t.Context(), p.ID, s.InstanceID, "plan"); err != nil {
		t.Fatal(err)
	}
	if v := reasoningInspect(t, m, p, s); v.Thinking != "medium" {
		t.Fatalf("plan inherited base rather than authority: %+v", v)
	}
	apply("thinking", "low")
	apply("reasoning_summary", "detailed")
	apply("text_verbosity", "high")
	if err := m.SetMode(t.Context(), p.ID, s.InstanceID, "default"); err != nil {
		t.Fatal(err)
	}
	if v := reasoningInspect(t, m, p, s); v.Thinking != "high" || v.ReasoningSummary != "detailed" || v.TextVerbosity != "high" {
		t.Fatalf("default overwritten: %+v", v)
	}
	if err := m.SetMode(t.Context(), p.ID, s.InstanceID, "plan"); err != nil {
		t.Fatal(err)
	}
	if v := reasoningInspect(t, m, p, s); v.Thinking != "low" {
		t.Fatalf("plan override lost: %+v", v)
	}
}

func TestReasoningRejectsStaleScopeUnconfirmedAndUnsafeFields(t *testing.T) {
	m, p, s, log := reasoningTestManager(t, "")
	view := reasoningInspect(t, m, p, s)
	before := reasoningLog(t, log)
	for _, alter := range []func(*RuntimeReasoningInput){
		func(i *RuntimeReasoningInput) { i.Confirm = false }, func(i *RuntimeReasoningInput) { i.Scope = "defaults" }, func(i *RuntimeReasoningInput) { i.Scope = "" },
		func(i *RuntimeReasoningInput) { i.Field = "debug_enabled" }, func(i *RuntimeReasoningInput) { i.Field = "skills_enabled" }, func(i *RuntimeReasoningInput) { i.Field = "subagents_enabled" }, func(i *RuntimeReasoningInput) { i.Field = "provider" }, func(i *RuntimeReasoningInput) { i.Field = "plugins" }, func(i *RuntimeReasoningInput) { i.Field = "mcp" },
		func(i *RuntimeReasoningInput) { i.Expected.SessionID = "other" }, func(i *RuntimeReasoningInput) { i.Expected.InstanceID = "other" }, func(i *RuntimeReasoningInput) { i.Expected.ProjectID = "other" }, func(i *RuntimeReasoningInput) { i.Expected.Revision++ },
	} {
		input := reasoningInput(view, "thinking", "high")
		alter(&input)
		if _, err := m.SetReasoning(t.Context(), p.ID, s.InstanceID, input); err == nil {
			t.Fatalf("accepted %+v", input)
		}
	}
	if reasoningLog(t, log) != before {
		t.Fatal("invalid structure reached worker")
	}
	for _, alter := range []func(*RuntimeReasoningInput){func(i *RuntimeReasoningInput) { i.Value = "ultra" }, func(i *RuntimeReasoningInput) { i.Expected.PermissionMode = "allow" }, func(i *RuntimeReasoningInput) { i.Expected.Provider = "other" }, func(i *RuntimeReasoningInput) { i.Expected.Model = "other" }, func(i *RuntimeReasoningInput) { i.Expected.Mode = "plan" }, func(i *RuntimeReasoningInput) { i.Expected.Thinking = "medium" }, func(i *RuntimeReasoningInput) { i.Expected.ReasoningSummary = "off" }, func(i *RuntimeReasoningInput) { i.Expected.TextVerbosity = "medium" }} {
		input := reasoningInput(view, "thinking", "high")
		alter(&input)
		if _, err := m.SetReasoning(t.Context(), p.ID, s.InstanceID, input); err == nil {
			t.Fatalf("accepted stale facts %+v", input)
		}
	}
	if strings.Contains(strings.TrimPrefix(reasoningLog(t, log), before), "session_reasoning_set") {
		t.Fatal("stale facts mutated")
	}
}

func TestReasoningIdleAndQueueHistoryFences(t *testing.T) {
	m, p, s, log := reasoningTestManager(t, "")
	view := reasoningInspect(t, m, p, s)
	r, _ := m.runtime(p.ID, s.InstanceID)
	cases := map[string]func(){
		"busy": func() { r.busy = true }, "transition": func() { r.transitioning = true }, "cancel": func() { r.snapshot.CancelRequested = true }, "goal pending": func() { r.goal.pending = true }, "goal active": func() { r.goal.active = true }, "permission": func() { r.snapshot.Permission = &RuntimePermission{ID: "pending"} }, "input": func() { r.snapshot.Input = &protocol.UserInputRequest{ID: "pending"} }, "queue pending": func() { r.queue.control = &protocol.QueueControl{Items: []protocol.QueueControlItem{{ID: "pending"}}} }, "queue review": func() {
			r.queue.control = &protocol.QueueControl{ReviewItems: []protocol.QueueControlItem{{ID: "review"}}}
		}, "unknown": func() { r.snapshot.Recovery.State = RecoveryAdmissionUnknown },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			before := reasoningLog(t, log)
			r.mu.Lock()
			original := r.snapshot.clone()
			mutate()
			r.mu.Unlock()
			if _, err := m.SetReasoning(t.Context(), p.ID, s.InstanceID, reasoningInput(view, "thinking", "high")); err == nil {
				t.Fatal("unsafe admission")
			}
			r.mu.Lock()
			r.busy = false
			r.transitioning = false
			r.goal = runtimeGoalState{}
			r.queue = runtimeQueueState{}
			r.snapshot = original
			r.mu.Unlock()
			if reasoningLog(t, log) != before {
				t.Fatal("unsafe state reached RPC")
			}
		})
	}
	r.mu.Lock()
	r.messageEdit.preparation = &protocol.RPCMessageEditPrepared{EditToken: "old"}
	r.versions.preparation = &protocol.RPCBranchRestorePrepared{}
	r.queue.token = "old"
	r.mu.Unlock()
	if _, err := m.SetReasoning(t.Context(), p.ID, s.InstanceID, reasoningInput(view, "thinking", "high")); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.messageEdit.preparation != nil || r.versions.preparation != nil || r.queue.token != "" {
		t.Fatal("history/queue prepare survived")
	}
}

func TestReasoningUnknownOutcomesFailClosedWithoutRetry(t *testing.T) {
	for _, mode := range []string{"exit", "rejected", "mismatch", "wrong-session", "read-rejected"} {
		t.Run(mode, func(t *testing.T) {
			m, p, s, log := reasoningTestManager(t, mode)
			view := reasoningInspect(t, m, p, s)
			_, err := m.SetReasoning(t.Context(), p.ID, s.InstanceID, reasoningInput(view, "thinking", "high"))
			if !errors.Is(err, ErrRuntimeUnavailable) {
				t.Fatalf("error %v", err)
			}
			after, _ := m.Snapshot(p.ID)
			if after.Status != "failed" {
				t.Fatalf("revived: %+v", after)
			}
			_, _ = m.SetReasoning(t.Context(), p.ID, s.InstanceID, reasoningInput(view, "thinking", "high"))
			if strings.Count(reasoningLog(t, log), "session_reasoning_set\n") != 1 {
				t.Fatal("mutation retried")
			}
		})
	}
}

func TestReasoningNoSettingsAndUnknownPendingInputs(t *testing.T) {
	for _, mode := range []string{"no-settings", "pending-input"} {
		t.Run(mode, func(t *testing.T) {
			m, p, s, log := reasoningTestManager(t, mode)
			if _, err := m.InspectReasoning(t.Context(), p.ID, s.InstanceID, s.SessionID); err == nil {
				t.Fatal("unsupported authority trusted")
			}
			if strings.Contains(reasoningLog(t, log), "session_reasoning_set") {
				t.Fatal("mutated")
			}
		})
	}
}
