//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/session"
)

func newProcessTestApp(t *testing.T, tools []string) *App {
	t.Helper()
	app, err := New(context.Background(), Options{
		Provider: "fake", Permission: "allow", NoSession: true, CWD: t.TempDir(),
		NoMCP: true, NoPlugins: true, NoSkills: true, Tools: tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestAppCloseStopsManagedProcessDescendants(t *testing.T) {
	app := newProcessTestApp(t, nil)
	marker := filepath.Join(t.TempDir(), "escaped")
	command := fmt.Sprintf("(sleep 0.5; printf escaped > %q) & sleep 10", marker)
	if _, err := app.ProcessManager.Start(context.Background(), managedprocess.StartRequest{Command: command}, nil); err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed descendant survived app close: %v", err)
	}
}

func TestAppSessionSwitchBlockedWhileManagedProcessRuns(t *testing.T) {
	app := newProcessTestApp(t, nil)
	defer app.Close()
	state, err := app.ProcessManager.Start(context.Background(), managedprocess.StartRequest{Command: "sleep 10"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	next := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	if err := app.SetSession(next); err == nil || !strings.Contains(err.Error(), "managed processes") {
		t.Fatalf("session switch error = %v", err)
	}
	if _, err := app.ProcessManager.Stop(context.Background(), state.ProcessID, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := app.SetSession(next); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ProcessManager.Status(state.ProcessID); err == nil {
		t.Fatal("old session process remained visible after rebind")
	}
}

func TestManagedProcessUIFacadesListAndReadLogs(t *testing.T) {
	app := newProcessTestApp(t, nil)
	defer app.Close()
	state, err := app.ProcessManager.Start(context.Background(), managedprocess.StartRequest{
		Command:   "printf 'server ready\\n'; sleep 10",
		Readiness: &managedprocess.ReadinessRequest{Type: "log", Pattern: "server ready", TimeoutMS: 1000},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, err := app.ListManagedProcesses(context.Background())
	if err != nil || len(list) != 1 || list[0].ProcessID != state.ProcessID {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	logs, err := app.ManagedProcessLogs(context.Background(), state.ProcessID, nil, 4096)
	if err != nil || !strings.Contains(logs.Output, "server ready") {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
}

func TestManagedProcessToolsRespectExplicitAllowlist(t *testing.T) {
	app := newProcessTestApp(t, []string{"process_list"})
	defer app.Close()
	if _, ok := app.Registry.Get("process_list"); !ok {
		t.Fatal("allowed process_list missing")
	}
	for _, name := range []string{"process_start", "process_status", "process_logs", "process_stop"} {
		if _, ok := app.Registry.Get(name); ok {
			t.Fatalf("disallowed tool %s is registered", name)
		}
	}
}
