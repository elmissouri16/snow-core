package web

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRuntimeWorkflowRejectedSwitchRemainsTerminal(t *testing.T) {
	for _, target := range []string{"saved", ""} {
		t.Run("target-"+target, func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, "workflow-switch-rejected")
			p := projects[0]
			initial, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Switch(t.Context(), p.ID, initial.InstanceID, target, false)
			if !errors.Is(err, ErrRuntimeUnavailable) {
				t.Fatalf("switch error: %v", err)
			}
			current, _ := m.Snapshot(p.ID)
			if current.Status != "failed" {
				t.Fatalf("rejected switch reopened admission: %+v", current)
			}
			before, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(before), "bound:") {
				t.Fatal("fixture did not change its active binding")
			}
			actions := []func() error{
				func() error { return m.Prompt(t.Context(), p.ID, initial.InstanceID, "stale") },
				func() error { return m.Abort(t.Context(), p.ID, initial.InstanceID) },
				func() error { return m.SetModel(t.Context(), p.ID, initial.InstanceID, "other", "next") },
				func() error { return m.SetMode(t.Context(), p.ID, initial.InstanceID, "plan") },
				func() error { return m.Rename(t.Context(), p.ID, initial.InstanceID, "Stale") },
				func() error {
					return m.ReplyPermission(t.Context(), p.ID, initial.InstanceID, "permission-1", protocol.PermissionAllow)
				},
				func() error { return m.ReplyInput(t.Context(), p.ID, initial.InstanceID, protocol.UserInputResponse{}) },
				func() error { _, err := m.Switch(t.Context(), p.ID, initial.InstanceID, "", false); return err },
				func() error { _, err := m.Choices(t.Context(), p.ID, initial.InstanceID); return err },
			}
			for _, action := range actions {
				if err := action(); err == nil {
					t.Fatal("old control admitted after uncertain binding")
				}
			}
			after, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatalf("terminal control reached worker: %q", after)
			}
		})
	}
}

func TestRuntimeWorkflowMutationVerificationFailureIsTerminal(t *testing.T) {
	for _, action := range []string{"mode", "model", "rename", "switch"} {
		t.Run(action, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, "workflow-info-rejected")
			p := projects[0]
			initial, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "mode":
				err = m.SetMode(t.Context(), p.ID, initial.InstanceID, "plan")
			case "model":
				if _, err = m.Choices(t.Context(), p.ID, initial.InstanceID); err != nil {
					t.Fatal(err)
				}
				err = m.SetModel(t.Context(), p.ID, initial.InstanceID, "other", "next")
			case "rename":
				err = m.Rename(t.Context(), p.ID, initial.InstanceID, "Renamed")
			case "switch":
				_, err = m.Switch(t.Context(), p.ID, initial.InstanceID, "saved", false)
			}
			if !errors.Is(err, ErrRuntimeUnavailable) {
				t.Fatalf("unverified mutation: %v", err)
			}
			current, _ := m.Snapshot(p.ID)
			if current.Status != "failed" {
				t.Fatalf("unverified metadata stayed usable: %+v", current)
			}
			if err := m.Prompt(t.Context(), p.ID, current.InstanceID, "follow-up"); err == nil {
				t.Fatal("prompt admitted without authoritative metadata")
			}
		})
	}
}

func TestRuntimeWorkflowPublishSwitchPreservesTerminalState(t *testing.T) {
	// Call the extracted final publication boundary directly so EOF/cancellation
	// deterministically precedes the exact lock acquisition used by Switch. No
	// sleeps, scheduler assumptions, or production-only test hooks are needed.
	for _, terminal := range []string{"eof", "canceled", "closing"} {
		t.Run(terminal, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, "workflow")
			p := projects[0]
			initial, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			r, err := m.runtime(p.ID, initial.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			r.control.Lock()
			defer r.control.Unlock()
			r.mu.Lock()
			r.transitioning = true
			r.snapshot.Status = "switching"
			r.mu.Unlock()
			switch terminal {
			case "eof":
				if err := r.worker.Close(); err != nil {
					t.Fatal(err)
				}
				<-r.drained
			case "canceled":
				r.cancel()
			case "closing":
				r.mu.Lock()
				r.snapshot.Status = "closing"
				r.mu.Unlock()
			}
			if _, err := r.publishSwitch(protocol.RPCSessionInfo{SessionID: "saved", Name: "Late publication", Provider: "late-provider", Model: "late-model"}); !errors.Is(err, ErrRuntimeClosed) {
				t.Fatalf("terminal publication: %v", err)
			}
			current, _ := m.Snapshot(p.ID)
			if current.Status == "idle" || current.SessionName == "Late publication" || current.Provider == "late-provider" {
				t.Fatalf("terminal state overwritten: %+v", current)
			}
		})
	}
}
