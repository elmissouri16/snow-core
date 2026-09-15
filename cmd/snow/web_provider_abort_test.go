//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/web"
)

// A real subprocess runs App/Agent/RPC through the production process client
// and manager projection. Only the provider's terminal signal is injected.
func TestWebProviderAbortRealWorker(t *testing.T) {
	manager, project, initial, directory := newMessageRegenerateFixture(t, false)
	marker := filepath.Join(directory, "provider-abort")
	if err := os.WriteFile(marker, []byte("abort\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Prompt(t.Context(), project.ID, initial.InstanceID, "provider aborts without Stop"); err != nil {
		t.Fatal(err)
	}
	stopped := policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		return s.Status == "idle" && s.Recovery.State == web.RecoveryCanceled
	})
	if stopped.CancelRequested || stopped.CancelToken != "" || len(stopped.Messages) != 2 {
		t.Fatalf("provider abort did not settle the owning prompt: %+v", stopped)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := manager.Prompt(t.Context(), project.ID, stopped.InstanceID, "continue explicitly"); err != nil {
		t.Fatal(err)
	}
	policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		return s.Status == "idle" && s.Recovery.State != web.RecoveryCanceled && len(s.Messages) == 4
	})
	// There was no automatic retry of the aborted request.
	if _, err := os.Stat(filepath.Join(directory, "context-3.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected third provider call: %v", err)
	}
}
