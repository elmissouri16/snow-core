//go:build darwin || linux

package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/permission"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestProcessControlOwnershipPolicyAndLimits(t *testing.T) {
	a := newProcessTestApp(t, []string{"read"})
	t.Cleanup(func() { _ = a.Close() })
	state, err := a.ProcessManager.Start(t.Context(), managedprocess.StartRequest{Command: "PRIVATE_COMMAND_MARKER=secret; printf 'public-ready\\n'; sleep 30", Name: "fixture", Readiness: &managedprocess.ReadinessRequest{Type: "log", Pattern: "public-ready", TimeoutMS: 1000}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := a.Session.ID()
	list, err := a.ProcessControlList(t.Context(), protocol.RPCProcessControlListParams{SessionID: sessionID})
	if err != nil || len(list.Processes) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	encoded, _ := json.Marshal(list)
	if strings.Contains(string(encoded), "PRIVATE_COMMAND_MARKER") || strings.Contains(string(encoded), "command") || strings.Contains(string(encoded), "pid") {
		t.Fatalf("private inventory: %s", encoded)
	}
	logsParams := protocol.RPCProcessControlLogsParams{SessionID: sessionID, ProcessID: state.ProcessID}
	logs, err := a.ProcessControlLogs(t.Context(), logsParams)
	if err != nil || !strings.Contains(logs.Output, "public-ready") || len(logs.Output) > 32768 {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
	for _, count := range []int{-1, 1, 3, 32769} {
		p := logsParams
		p.MaxBytes = count
		if _, err := a.ProcessControlLogs(t.Context(), p); err == nil {
			t.Errorf("accepted limit %d", count)
		}
	}
	for _, cursor := range []int64{-1, 1 << 62} {
		p := logsParams
		p.Cursor = new(cursor)
		if _, err := a.ProcessControlLogs(t.Context(), p); err == nil {
			t.Errorf("accepted cursor %d", cursor)
		}
	}
	stop := protocol.RPCProcessControlStopParams{SessionID: sessionID, ProcessID: state.ProcessID, GraceMS: 50}
	for _, mode := range []permission.Mode{permission.ModeDeny, permission.ModeAsk} {
		if err := a.SetPermissionMode(mode); err != nil {
			t.Fatal(err)
		}
		if _, err := a.ProcessControlStop(t.Context(), stop); !errors.Is(err, ErrProcessControlDenied) {
			t.Fatalf("%s stop=%v", mode, err)
		}
	}
	if err := a.SetPermissionMode(permission.ModeAllow); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ProcessControlStop(t.Context(), stop); !errors.Is(err, ErrProcessControlDenied) {
		t.Fatalf("plan stop=%v", err)
	}
	if _, err := a.ProcessControlLogs(t.Context(), logsParams); err != nil {
		t.Fatal("Plan inspection denied", err)
	}
	if err := a.Agent.SetMode(protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	for _, grace := range []int{-1, 5001} {
		p := stop
		p.GraceMS = grace
		if _, err := a.ProcessControlStop(t.Context(), p); err == nil {
			t.Errorf("accepted grace %d", grace)
		}
	}
	foreign := stop
	foreign.ProcessID = "proc_" + strings.Repeat("0", 32)
	if _, err := a.ProcessControlStop(t.Context(), foreign); !errors.Is(err, ErrProcessControlRejected) {
		t.Fatalf("foreign process=%v", err)
	}
	stale := stop
	stale.SessionID = "not-current"
	if _, err := a.ProcessControlStop(t.Context(), stale); !errors.Is(err, ErrProcessControlRejected) {
		t.Fatalf("stale session=%v", err)
	}
	result, err := a.ProcessControlStop(t.Context(), stop)
	if err != nil || result.Process.Status == "running" {
		t.Fatalf("stop=%+v %v", result, err)
	}
	next := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	if err := a.SetSession(next); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ProcessControlList(t.Context(), protocol.RPCProcessControlListParams{SessionID: sessionID}); err == nil {
		t.Fatal("stale inventory accepted")
	}
	logsParams.SessionID = next.ID()
	if _, err := a.ProcessControlLogs(t.Context(), logsParams); err == nil {
		t.Fatal("old handle crossed session")
	}
	current, err := a.ProcessControlList(t.Context(), protocol.RPCProcessControlListParams{SessionID: next.ID()})
	if err != nil || len(current.Processes) != 0 {
		t.Fatalf("rebind inventory=%+v %v", current, err)
	}
}

// A provider turn does not hold admission for its lifetime. Inspection may
// acquire admission while it is streaming, but SetSession must still reject a
// running turn without rebinding the manager or invalidating owned handles.
func TestProcessControlReadDuringPromptRejectsSessionRebind(t *testing.T) {
	a := newProcessTestApp(t, []string{"read"})
	t.Cleanup(func() { _ = a.Close() })
	state, err := a.ProcessManager.Start(t.Context(), managedprocess.StartRequest{Command: "printf 'live-ready\\n'; sleep 30", Name: "live-read", Readiness: &managedprocess.ReadinessRequest{Type: "log", Pattern: "live-ready", TimeoutMS: 1000}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &reviewQueueProvider{Provider: a.Provider, started: make(chan struct{})}
	if err := a.Agent.SetProvider(p); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Agent.Prompt(ctx, "hold provider while inspecting processes") }()
	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	sessionID := a.Session.ID()
	inspectCtx, inspectCancel := context.WithTimeout(t.Context(), time.Second)
	defer inspectCancel()
	list, err := a.ProcessControlList(inspectCtx, protocol.RPCProcessControlListParams{SessionID: sessionID})
	if err != nil || len(list.Processes) != 1 {
		t.Fatalf("active-turn list=%+v err=%v", list, err)
	}
	logs, err := a.ProcessControlLogs(inspectCtx, protocol.RPCProcessControlLogsParams{SessionID: sessionID, ProcessID: state.ProcessID})
	if err != nil || !strings.Contains(logs.Output, "live-ready") {
		t.Fatalf("active-turn logs=%+v err=%v", logs, err)
	}
	next := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	t.Cleanup(func() { _ = next.Close() })
	if err := a.SetSession(next); err == nil {
		t.Fatal("session rebound during active prompt")
	}
	if a.Session.ID() != sessionID {
		t.Fatal("rejected transition changed session")
	}
	if _, err := a.ProcessManager.Status(state.ProcessID); err != nil {
		t.Fatal("rejected rebind invalidated current handle", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not cancel")
	}
	if err := a.SetSession(next); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ProcessControlLogs(t.Context(), protocol.RPCProcessControlLogsParams{SessionID: sessionID, ProcessID: state.ProcessID}); err == nil {
		t.Fatal("old read authority survived committed rebind")
	}
}
