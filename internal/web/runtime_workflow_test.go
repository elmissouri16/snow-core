package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRuntimeWorkflowChoicesModelModeRenameAndPlan(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "workflow")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionName != "Original" || s.Mode != "default" || s.Thinking != "high" || s.Telemetry == nil || !s.Telemetry.Available || s.Telemetry.TotalTokens != 120 || !s.Telemetry.Estimated {
		t.Fatalf("snapshot %+v", s)
	}
	if err := m.SetModel(t.Context(), p.ID, s.InstanceID, "other", "next"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("undiscovered model %v", err)
	}
	choices, err := m.Choices(t.Context(), p.ID, s.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(choices.Models) != 2 || len(choices.Sessions) != 2 || !choices.ModelsPartial {
		t.Fatalf("choices %+v", choices)
	}
	if err := m.SetModel(t.Context(), p.ID, s.InstanceID, "host-provider", "next"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("mixed pair %v", err)
	}
	if err := m.SetModel(t.Context(), p.ID, s.InstanceID, "other", "next"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMode(t.Context(), p.ID, s.InstanceID, "plan"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMode(t.Context(), p.ID, s.InstanceID, "allow"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatal(err)
	}
	if err := m.Rename(t.Context(), p.ID, s.InstanceID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "plan"); err != nil {
		t.Fatal(err)
	}
	s = runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	if s.Provider != "other" || s.Model != "next" || s.Mode != "plan" || s.SessionName != "Renamed" {
		t.Fatalf("snapshot %+v", s)
	}
	if s.Telemetry.TotalTokens != 138 || s.Telemetry.InputTokens != 115 || s.Telemetry.ContextTokens != 15 || s.Telemetry.Estimated {
		t.Fatalf("usage %+v", s.Telemetry)
	}
	found := false
	for _, msg := range s.Messages {
		if msg.Role == "plan" && msg.Text == "Proposed plan" {
			found = true
		}
	}
	if !found {
		t.Fatalf("plan discarded %+v", s.Messages)
	}
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "PRIVATE") {
		t.Fatal("private context exposed")
	}
	before, _ := os.ReadFile(log)
	for range 5 {
		m.Snapshot(p.ID)
	}
	after, _ := os.ReadFile(log)
	if string(before) != string(after) {
		t.Fatal("polling invoked RPC")
	}
	s.Telemetry.TotalTokens = 999
	fresh, _ := m.Snapshot(p.ID)
	if fresh.Telemetry.TotalTokens == 999 {
		t.Fatal("telemetry aliases")
	}
}

func TestRuntimeWorkflowRejectsSwitchWithIdleChildPermission(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "workflow")
	p := projects[0]
	snapshot, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.runtime(p.ID, snapshot.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.busy = false
	r.snapshot.Status = "permission"
	r.snapshot.Permission = &RuntimePermission{ID: "child-permission", AgentPath: "/root/child", Tool: "bash"}
	r.permissionAgent = &protocol.AgentRef{ThreadID: "child-thread", ParentThreadID: "root-thread", Path: "/root/child", ParentPath: protocol.RootAgentPath, Depth: 1}
	r.mu.Unlock()
	before, _ := os.ReadFile(log)
	if _, err := m.Switch(t.Context(), p.ID, snapshot.InstanceID, "saved", true); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("switch with child permission = %v", err)
	}
	after, _ := os.ReadFile(log)
	if string(before) != string(after) {
		t.Fatal("blocked child-permission switch reached RPC")
	}

	r.mu.Lock()
	r.clearPermissionLocked()
	r.snapshot.Status = "idle"
	r.activeChildren = map[string]protocol.AgentStatus{"child-thread": protocol.AgentRunning}
	r.mu.Unlock()
	if _, err := m.Switch(t.Context(), p.ID, snapshot.InstanceID, "saved", false); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("switch with running child = %v", err)
	}
	afterActive, _ := os.ReadFile(log)
	if string(after) != string(afterActive) {
		t.Fatal("blocked running-child switch reached RPC")
	}
}

func TestRuntimeWorkflowSwitchConfirmationRotationAndStaleControls(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "workflow")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(log)
	if _, err := m.Switch(t.Context(), p.ID, s.InstanceID, "saved", false); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(log)
	if string(before) != string(after) {
		t.Fatal("unconfirmed switch sent RPC")
	}
	for _, action := range []func() error{func() error { return m.SetMode(t.Context(), p.ID, s.InstanceID, "plan") }, func() error { return m.Rename(t.Context(), p.ID, s.InstanceID, "Busy") }} {
		if err := action(); !errors.Is(err, ErrRuntimeBusy) {
			t.Fatal(err)
		}
	}
	start := time.Now()
	next, err := m.Switch(t.Context(), p.ID, s.InstanceID, "saved", true)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 60*time.Millisecond {
		t.Fatal("switch skipped definitive completion")
	}
	if next.InstanceID == s.InstanceID || next.SessionID != "saved" || next.Status != "idle" || next.Telemetry.TotalTokens != 120 {
		t.Fatalf("switch %+v", next)
	}
	stale := []func() error{
		func() error { return m.Prompt(t.Context(), p.ID, s.InstanceID, "stale") }, func() error { return m.Abort(t.Context(), p.ID, s.InstanceID) }, func() error { return m.CloseProject(t.Context(), p.ID, s.InstanceID) },
		func() error { return m.SetMode(t.Context(), p.ID, s.InstanceID, "plan") }, func() error { return m.SetModel(t.Context(), p.ID, s.InstanceID, "other", "next") }, func() error { return m.Rename(t.Context(), p.ID, s.InstanceID, "Stale") },
		func() error { _, err := m.Choices(t.Context(), p.ID, s.InstanceID); return err }, func() error { _, err := m.Switch(t.Context(), p.ID, s.InstanceID, "", false); return err },
		func() error {
			return m.ReplyPermission(t.Context(), p.ID, s.InstanceID, "request", protocol.PermissionAllow)
		}, func() error { return m.ReplyInput(t.Context(), p.ID, s.InstanceID, protocol.UserInputResponse{}) },
	}
	before, _ = os.ReadFile(log)
	for _, action := range stale {
		if err := action(); !errors.Is(err, ErrRuntimeInvalid) {
			t.Fatalf("stale control %v", err)
		}
	}
	after, _ = os.ReadFile(log)
	if string(before) != string(after) {
		t.Fatal("stale control reached worker")
	}
	if err := m.Prompt(t.Context(), p.ID, next.InstanceID, "answer"); err != nil {
		t.Fatal(err)
	}
	final := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	data, _ := json.Marshal(final)
	if strings.Contains(string(data), "STALE") {
		t.Fatal("late epoch event exposed")
	}
	newer, err := m.Switch(t.Context(), p.ID, next.InstanceID, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if newer.InstanceID == next.InstanceID || newer.SessionID == next.SessionID {
		t.Fatal("new session not rotated")
	}
}

func TestRuntimeWorkflowTargetValidationAndLegacy(t *testing.T) {
	for _, mode := range []string{"workflow-legacy", "workflow-large", "workflow-wrong-model"} {
		t.Run(mode, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, mode)
			p := projects[0]
			s, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			choices, err := m.Choices(t.Context(), p.ID, s.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "workflow-legacy" && (!choices.ModelsPartial || len(choices.Models) != 1) {
				t.Fatalf("legacy %+v", choices)
			}
			if mode == "workflow-large" && (len(choices.Sessions) != 100 || !choices.SessionsTruncated || !choices.ModelsTruncated || len(choices.Models) != 3) {
				t.Fatalf("unbounded %+v", choices)
			}
			if _, err := m.Switch(t.Context(), p.ID, s.InstanceID, "unlisted", false); !errors.Is(err, ErrRuntimeInvalid) {
				t.Fatal(err)
			}
			if mode == "workflow-wrong-model" {
				if _, err := m.Switch(t.Context(), p.ID, s.InstanceID, "saved", false); !errors.Is(err, ErrRuntimeUnavailable) {
					t.Fatalf("wrong provider %v", err)
				}
				current, _ := m.Snapshot(p.ID)
				if current.Status != "failed" || current.InstanceID == s.InstanceID {
					t.Fatal("uncertain switch not fenced")
				}
			}
		})
	}
}

func TestRuntimeWorkflowAdmittedSwitchIgnoresBrowserCancellation(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "workflow")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "hold"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := m.Switch(ctx, p.ID, s.InstanceID, "saved", true); done <- err }()
	runtimeWait(t, m, p.ID, func(_ RuntimeSnapshot) bool {
		r, _ := m.runtime(p.ID, s.InstanceID)
		if r == nil {
			return true
		}
		if r.control.TryLock() {
			r.control.Unlock()
			return false
		}
		return true
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeWorkflowLegacyUnsupportedTelemetry(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Telemetry == nil || s.Telemetry.Available || s.Telemetry.ContextAvailable {
		t.Fatalf("unsupported telemetry %+v", s.Telemetry)
	}
	choices, err := m.Choices(t.Context(), p.ID, s.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(choices.Models) != 0 || !choices.ModelsPartial || choices.SessionsAvailable {
		t.Fatalf("unsupported choices %+v", choices)
	}
}

func TestRuntimeWorkflowControlRevalidatesSessionPointer(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "workflow")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pointer, err := m.runtime(p.ID, s.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := m.Switch(t.Context(), p.ID, s.InstanceID, "saved", false)
	if err != nil {
		t.Fatal(err)
	}
	pointer.control.Lock()
	if err := m.revalidate(pointer, p.ID, s.InstanceID); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("stale pointer admitted: %v", err)
	}
	if err := m.CloseProject(t.Context(), p.ID, next.InstanceID); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("close bypassed control gate: %v", err)
	}
	pointer.control.Unlock()
	if err := m.CloseProject(t.Context(), p.ID, s.InstanceID); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("stale close admitted: %v", err)
	}
}

func TestRuntimeWorkflowNeverUsesPersistentModelControl(t *testing.T) {
	for _, mode := range []string{"workflow", "workflow-no-model-selection"} {
		t.Run(mode, func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, mode)
			p := projects[0]
			s, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Choices(t.Context(), p.ID, s.InstanceID); err != nil {
				t.Fatal(err)
			}
			err = m.SetModel(t.Context(), p.ID, s.InstanceID, "other", "next")
			if mode == "workflow-no-model-selection" {
				if !errors.Is(err, ErrRuntimeInvalid) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(log)
			for line := range strings.SplitSeq(string(data), "\n") {
				if line == "set_model" {
					t.Fatal("conversation control wrote host configuration")
				}
			}
			if mode == "workflow" && !strings.Contains(string(data), "session_set_model\n") {
				t.Fatal("missing session-local command")
			}
		})
	}
}
