package agent

import (
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestMessageEditRejectsRecoveredInputAndRunningState(t *testing.T) {
	store := session.NewMemoryStore(session.Options{})
	p := &scriptedProvider{}
	a, err := New(Options{Provider: p, Registry: tools.NewRegistry(), Session: store, Permission: permission.NewService(permission.ModeDeny, nil), Model: protocol.Model{Provider: p.ID(), ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	unlock := a.LockAdmission()
	defer unlock()
	a.mu.Lock()
	a.queuedInputs = []protocol.QueuedInput{{ID: "recovered", Text: "do not discard"}}
	a.mu.Unlock()
	if err := a.MessageEditReadyAdmitted(); err == nil {
		t.Fatal("edit consumed pending recovery")
	}
	if pending := a.PendingInputs(); len(pending.Items) != 1 {
		t.Fatal("readiness cleared pending input")
	}
	a.mu.Lock()
	a.queuedInputs = nil
	a.running = true
	a.mu.Unlock()
	if err := a.MessageEditReadyAdmitted(); err == nil {
		t.Fatal("edit accepted running agent")
	}
	a.mu.Lock()
	a.running = false
	a.autoRunning = true
	a.mu.Unlock()
	if err := a.MessageEditReadyAdmitted(); err == nil {
		t.Fatal("edit accepted automatic continuation")
	}
	a.mu.Lock()
	a.autoRunning = false
	a.mu.Unlock()
}
