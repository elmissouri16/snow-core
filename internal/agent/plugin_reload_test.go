package agent

import (
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"testing"
)

func TestPluginReloadAdmissionRejectsAutomaticGapsWithoutStopping(t *testing.T) {
	for _, kind := range []string{"turn", "automatic-gap", "pending", "closed"} {
		t.Run(kind, func(t *testing.T) {
			a := &Agent{}
			switch kind {
			case "turn":
				a.running = true
			case "automatic-gap":
				a.autoRunning = true
			case "pending":
				a.autoPending = true
			case "closed":
				a.closed = true
			}
			unlock, err := a.LockPluginReloadAdmission()
			if err == nil || unlock != nil {
				t.Fatal("busy catalog admission accepted")
			}
			if a.autoStop || a.activeCancel != nil {
				t.Fatal("reload changed work state")
			}
			// Rejection must release the admission token.
			a.running, a.autoRunning, a.autoPending, a.closed = false, false, false, false
			unlock, err = a.LockPluginReloadAdmission()
			if err != nil {
				t.Fatal(err)
			}
			if second, err := a.LockPluginReloadAdmission(); err == nil || second != nil {
				t.Fatal("nested admission accepted")
			}
			unlock()
		})
	}
}
func TestPluginReloadAdmissionDoesNotPauseActiveGoal(t *testing.T) {
	a, controller, _ := goalAgent(t, &scriptedProvider{})
	if _, err := controller.Create("objective", nil, false); err != nil {
		t.Fatal(err)
	}
	if unlock, err := a.LockPluginReloadAdmission(); err == nil || unlock != nil {
		t.Fatal("active goal reload accepted")
	}
	goal, err := controller.Get()
	if err != nil || goal.Status != protocol.GoalActive {
		t.Fatalf("goal changed %+v %v", goal, err)
	}
}
