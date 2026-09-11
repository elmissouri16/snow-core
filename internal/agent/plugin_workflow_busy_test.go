package agent

import (
	"strings"
	"testing"
)

func TestPluginWorkflowBusyAdmittedRejectsAutomaticInterturnGaps(t *testing.T) {
	for _, state := range []string{"idle", "running", "automatic-running", "automatic-pending", "closed"} {
		t.Run(state, func(t *testing.T) {
			a := &Agent{}
			switch state {
			case "running":
				a.running = true
			case "automatic-running":
				a.autoRunning = true
			case "automatic-pending":
				a.autoPending = true
			case "closed":
				a.closed = true
			}
			canceled := false
			a.activeCancel = func() { canceled = true }
			unlock := a.LockAdmission()
			defer unlock()
			err := a.PluginWorkflowBusyAdmitted()
			if state == "idle" {
				if err != nil {
					t.Fatalf("idle refused: %v", err)
				}
			} else if err == nil {
				t.Fatalf("%s admitted", state)
			} else if state == "closed" && !strings.Contains(err.Error(), "closed") {
				t.Fatalf("closed error: %v", err)
			}
			if canceled || a.autoStop || a.running != (state == "running") || a.autoRunning != (state == "automatic-running") || a.autoPending != (state == "automatic-pending") || a.closed != (state == "closed") {
				t.Fatal("read-only workflow admission changed worker state")
			}
		})
	}
}
