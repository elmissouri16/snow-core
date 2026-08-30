//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testManager(t *testing.T, options ...func(*Options)) *Manager {
	t.Helper()
	opts := Options{CWD: t.TempDir(), MaxRunning: 4, MaxRecords: 8, RetainedOutputBytes: 64 << 10, MaxLogReadBytes: 64 << 10}
	for _, apply := range options {
		apply(&opts)
	}
	manager := NewManager(opts)
	if err := manager.BindSession("session-one"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close manager: %v", err)
		}
	})
	return manager
}

func TestManagerStartLogsStatusAndStop(t *testing.T) {
	manager := testManager(t)
	state, err := manager.Start(context.Background(), StartRequest{Command: "printf hello; sleep 10", Name: "dev-server"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "running" || state.ProcessID == "" || state.Name != "dev-server" {
		t.Fatalf("start state = %+v", state)
	}
	logs, err := manager.Logs(context.Background(), LogsRequest{ProcessID: state.ProcessID, Wait: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Output != "hello" || logs.NextCursor != 5 || logs.EOF {
		t.Fatalf("logs = %+v", logs)
	}
	stopped, err := manager.Stop(context.Background(), state.ProcessID, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != "stopped" || stopped.Reason != "stop_requested" || stopped.FinishedAt == 0 {
		t.Fatalf("stopped state = %+v", stopped)
	}
	logs, err = manager.Logs(context.Background(), LogsRequest{ProcessID: state.ProcessID, Cursor: &logs.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if !logs.EOF {
		t.Fatalf("terminal logs = %+v", logs)
	}
}

func TestManagerNaturalNonzeroExitIsState(t *testing.T) {
	manager := testManager(t)
	state, err := manager.Start(context.Background(), StartRequest{Command: "exit 7"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for state.Status == "running" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		state, err = manager.Status(state.ProcessID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if state.Status != "exited" || state.ExitCode == nil || *state.ExitCode != 7 || state.Reason != "natural" {
		t.Fatalf("exit state = %+v", state)
	}
}

func TestManagerLogReadReportsRollover(t *testing.T) {
	manager := testManager(t, func(opts *Options) { opts.RetainedOutputBytes = 64 << 10 })
	state, err := manager.Start(context.Background(), StartRequest{Command: "head -c 70000 /dev/zero | tr '\\0' x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		current, statusErr := manager.Status(state.ProcessID)
		if statusErr != nil {
			t.Fatal(statusErr)
		}
		if current.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("process did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	cursor := int64(0)
	logs, err := manager.Logs(context.Background(), LogsRequest{ProcessID: state.ProcessID, Cursor: &cursor, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Omitted != 70000-(64<<10) || len(logs.Output) != 1024 {
		t.Fatalf("rollover logs = %+v output=%d", logs, len(logs.Output))
	}
}

func TestManagerLogReadiness(t *testing.T) {
	manager := testManager(t)
	var progress []string
	state, err := manager.Start(context.Background(), StartRequest{
		Command:   "sleep 0.05; printf 'server READY'; sleep 10",
		Name:      "ready-server",
		Readiness: &ReadinessRequest{Type: "log", Pattern: `READY`, TimeoutMS: 2000},
	}, func(message string) { progress = append(progress, message) })
	if err != nil {
		t.Fatal(err)
	}
	if !state.Ready || !strings.Contains(strings.Join(progress, "|"), "process ready") {
		t.Fatalf("state=%+v progress=%v", state, progress)
	}
}

func TestManagerHTTPReadinessIsLoopbackOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	manager := testManager(t)
	state, err := manager.Start(context.Background(), StartRequest{
		Command: "sleep 10", Readiness: &ReadinessRequest{Type: "http", URL: server.URL, TimeoutMS: 2000},
	}, nil)
	if err != nil || !state.Ready {
		t.Fatalf("loopback readiness state=%+v err=%v", state, err)
	}
	_, err = manager.Start(context.Background(), StartRequest{
		Command: "sleep 10", Readiness: &ReadinessRequest{Type: "http", URL: "http://192.0.2.1/", TimeoutMS: 100},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("external readiness error = %v", err)
	}
}

func TestManagerReadinessFailureCleansUp(t *testing.T) {
	manager := testManager(t)
	state, err := manager.Start(context.Background(), StartRequest{
		Command: "sleep 10", Readiness: &ReadinessRequest{Type: "log", Pattern: "never", TimeoutMS: 50},
	}, nil)
	if err == nil || state.Status != "stopped" || manager.HasRunning() {
		t.Fatalf("state=%+v running=%t err=%v", state, manager.HasRunning(), err)
	}
}

func TestManagerCloseKillsDescendantGroup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "escaped")
	manager := NewManager(Options{CWD: dir, MaxRunning: 2, MaxRecords: 4, RetainedOutputBytes: 64 << 10})
	if err := manager.BindSession("session-one"); err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf("(trap '' TERM; sleep 3; printf escaped > %q) & wait", marker)
	if _, err := manager.Start(context.Background(), StartRequest{Command: command}, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant escaped cleanup: %v", err)
	}
}

func TestManagerRebindKillsDescendantGroup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "escaped-after-switch")
	manager := testManager(t)
	command := fmt.Sprintf("(trap '' TERM; sleep 3; printf escaped > %q) & wait", marker)
	state, err := manager.Start(context.Background(), StartRequest{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := manager.lookup(state.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RebindSession(context.Background(), "session-two"); err != nil {
		t.Fatal(err)
	}
	stopped := record.state()
	if stopped.Status != "stopped" || stopped.Reason != "session_switch" {
		t.Fatalf("stopped state = %+v", stopped)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed descendant survived session switch: %v", err)
	}
}

func TestManagerRebindStopsLiveGroupAfterLeaderExit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "escaped-after-leader")
	manager := testManager(t)
	command := fmt.Sprintf("(sleep 3; printf escaped > %q) >/dev/null 2>&1 &", marker)
	state, err := manager.Start(context.Background(), StartRequest{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := manager.lookup(state.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for record.state().Status == "running" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if record.state().Status == "running" || !record.hasLiveGroup() {
		t.Fatalf("test did not produce terminal leader with live group: state=%+v live=%t", record.state(), record.hasLiveGroup())
	}
	if err := manager.RebindSession(context.Background(), "session-two"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3200 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant of terminal leader survived session switch: %v", err)
	}
}

func TestManagerFailedRebindRetainsOldInventoryForRetry(t *testing.T) {
	manager := testManager(t)
	state, err := manager.Start(context.Background(), StartRequest{
		Command:   "(trap '' TERM; printf READY; sleep 10) & wait",
		Readiness: &ReadinessRequest{Type: "log", Pattern: "READY", TimeoutMS: 1000},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := manager.RebindSession(ctx, "session-two"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("rebind error = %v", err)
	}
	manager.mu.Lock()
	sessionID := manager.sessionID
	manager.mu.Unlock()
	if sessionID != "session-one" {
		t.Fatalf("session id = %q, want session-one", sessionID)
	}
	stopped, err := manager.Status(state.ProcessID)
	if err != nil {
		t.Fatalf("old inventory unavailable after failed rebind: %v", err)
	}
	if stopped.Status != "stopped" || stopped.Reason != "session_switch" {
		t.Fatalf("stopped state = %+v", stopped)
	}
	if err := manager.RebindSession(context.Background(), "session-two"); err != nil {
		t.Fatalf("retry rebind: %v", err)
	}
	if _, err := manager.Status(state.ProcessID); err == nil {
		t.Fatal("old inventory remained after successful retry")
	}
}

func TestManagerCloseDuringRebindPreventsNewStartsAndBinding(t *testing.T) {
	manager := testManager(t)
	if _, err := manager.Start(context.Background(), StartRequest{
		Command:   "(trap '' TERM; printf READY; sleep 10) & wait",
		Readiness: &ReadinessRequest{Type: "log", Pattern: "READY", TimeoutMS: 1000},
	}, nil); err != nil {
		t.Fatal(err)
	}
	rebindDone := make(chan error, 1)
	go func() { rebindDone <- manager.RebindSession(context.Background(), "session-two") }()
	deadline := time.Now().Add(time.Second)
	for {
		manager.mu.Lock()
		rebinding := manager.rebinding
		manager.mu.Unlock()
		if rebinding {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rebind did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := manager.Start(context.Background(), StartRequest{Command: "sleep 10"}, nil); err == nil || !strings.Contains(err.Error(), "session switch") {
		t.Fatalf("start during rebind error = %v", err)
	}
	closeCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := manager.Close(closeCtx); err != nil {
		t.Fatalf("close during rebind: %v", err)
	}
	if err := <-rebindDone; err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("rebind after close error = %v", err)
	}
	if err := manager.BindSession("session-three"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("bind after close error = %v", err)
	}
}

func TestManagerStopCancellationEscalatesImmediately(t *testing.T) {
	manager := testManager(t)
	state, err := manager.Start(context.Background(), StartRequest{
		Command:   "(trap '' TERM; printf READY; sleep 10) & wait",
		Readiness: &ReadinessRequest{Type: "log", Pattern: "READY", TimeoutMS: 1000},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	stopped, err := manager.Stop(ctx, state.ProcessID, MaxStopGrace)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop error = %v", err)
	}
	if time.Since(started) > 3*time.Second || stopped.Status != "stopped" {
		t.Fatalf("stop took %s state=%+v", time.Since(started), stopped)
	}
}

func TestManagerRebindStopsProcessesClearsInventoryAndEnforcesLimit(t *testing.T) {
	manager := testManager(t, func(opts *Options) { opts.MaxRunning = 1 })
	state, err := manager.Start(context.Background(), StartRequest{Command: "sleep 10", Name: "one"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.BindSession("session-two"); err == nil {
		t.Fatal("plain session bind succeeded with running process")
	}
	if _, err := manager.Start(context.Background(), StartRequest{Command: "sleep 10", Name: "two"}, nil); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("limit error = %v", err)
	}
	if err := manager.RebindSession(context.Background(), "session-two"); err != nil {
		t.Fatal(err)
	}
	if manager.HasRunning() {
		t.Fatal("process remained running after session rebind")
	}
	if _, err := manager.Status(state.ProcessID); err == nil {
		t.Fatal("old-session process handle remained visible")
	}
	replacement, err := manager.Start(context.Background(), StartRequest{Command: "sleep 10", Name: "two"}, nil)
	if err != nil {
		t.Fatalf("start after rebind: %v", err)
	}
	if _, err := manager.Stop(context.Background(), replacement.ProcessID, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
