//go:build darwin || linux

package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/permission"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/tools"
)

func TestRegisterProcessToolsContractsAndRisks(t *testing.T) {
	manager := managedprocess.NewManager(managedprocess.Options{CWD: t.TempDir(), RetainedOutputBytes: 64 << 10})
	if err := manager.BindSession("session"); err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	registry := tools.NewRegistry()
	if err := RegisterProcessTools(registry, manager); err != nil {
		t.Fatal(err)
	}
	want := map[string]permission.Risk{
		"process_start":  permission.RiskExec,
		"process_status": permission.RiskRead,
		"process_logs":   permission.RiskRead,
		"process_stop":   permission.RiskExec,
		"process_list":   permission.RiskRead,
	}
	for name, risk := range want {
		descriptor, ok := registry.Descriptor(name)
		if !ok || descriptor.Risk != risk || descriptor.Schema.Discovery != nil {
			t.Fatalf("descriptor %s = %+v", name, descriptor)
		}
	}
	start, _ := registry.Descriptor("process_start")
	for _, phrase := range []string{"development servers", "watchers", "long-running commands", "stable name", "startup log marker is sufficient", "prefer log readiness", "do not reconfirm"} {
		if !strings.Contains(start.Schema.Description, phrase) {
			t.Fatalf("process_start description missing %q: %q", phrase, start.Schema.Description)
		}
	}
	parameters := string(start.Schema.Parameters)
	for _, phrase := range []string{"startup evidence", "stable log marker is sufficient and preferred", "explicitly requests service/network health", "no reliable log marker exists", "do not guess", "without shell backgrounding"} {
		if !strings.Contains(parameters, phrase) {
			t.Fatalf("process_start parameters missing %q: %s", phrase, parameters)
		}
	}
	logOption := strings.Index(parameters, `"const":"log"`)
	tcpOption := strings.Index(parameters, `"const":"tcp"`)
	httpOption := strings.Index(parameters, `"const":"http"`)
	if logOption < 0 || tcpOption < 0 || httpOption < 0 || logOption > tcpOption || logOption > httpOption {
		t.Fatalf("log readiness must be presented first: %s", parameters)
	}
	list, _ := registry.Descriptor("process_list")
	if !strings.Contains(list.Schema.Description, "avoid duplicates") {
		t.Fatalf("process_list description = %q", list.Schema.Description)
	}
}

func TestProcessStartReadinessErrorRetainsOpaqueHandle(t *testing.T) {
	manager := managedprocess.NewManager(managedprocess.Options{CWD: t.TempDir(), RetainedOutputBytes: 64 << 10})
	if err := manager.BindSession("session"); err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	tool := &processStartTool{manager: manager}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"command":"sleep 10","readiness":{"type":"log","pattern":"never","timeout_ms":10}}`), stubHost{cwd: t.TempDir()})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].Text, `"process_id":"proc_`) {
		t.Fatalf("start failure result=%+v err=%v", result, err)
	}
}

func TestProcessToolsStartListLogsStop(t *testing.T) {
	manager := managedprocess.NewManager(managedprocess.Options{CWD: t.TempDir(), RetainedOutputBytes: 64 << 10, MaxLogReadBytes: 64 << 10})
	if err := manager.BindSession("session"); err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	registry := tools.NewRegistry()
	if err := RegisterProcessTools(registry, manager); err != nil {
		t.Fatal(err)
	}
	host := stubHost{cwd: t.TempDir()}
	start, _ := registry.Get("process_start")
	result, err := start.Run(context.Background(), json.RawMessage(`{"command":"printf hello; sleep 10","name":"server"}`), host)
	if err != nil || result.IsError {
		t.Fatalf("start result=%+v err=%v", result, err)
	}
	var state managedprocess.State
	if err := json.Unmarshal([]byte(result.Content[0].Text), &state); err != nil {
		t.Fatal(err)
	}
	if state.ProcessID == "" || strings.Contains(result.Content[0].Text, "printf hello") {
		t.Fatalf("unsafe start result %q", result.Content[0].Text)
	}

	list, _ := registry.Get("process_list")
	result, _ = list.Run(context.Background(), json.RawMessage(`{}`), host)
	if result.IsError || !strings.Contains(result.Content[0].Text, state.ProcessID) || strings.Contains(result.Content[0].Text, "printf hello") {
		t.Fatalf("list result = %+v", result)
	}

	logs, _ := registry.Get("process_logs")
	args, _ := json.Marshal(map[string]any{"process_id": state.ProcessID, "wait_ms": 1000})
	result, _ = logs.Run(context.Background(), args, host)
	if result.IsError || !strings.Contains(result.Content[0].Text, "hello") {
		t.Fatalf("logs result = %+v", result)
	}

	stop, _ := registry.Get("process_stop")
	args, _ = json.Marshal(map[string]any{"process_id": state.ProcessID, "grace_ms": int((50 * time.Millisecond) / time.Millisecond)})
	result, _ = stop.Run(context.Background(), args, host)
	if result.IsError || !strings.Contains(result.Content[0].Text, `"status":"stopped"`) {
		t.Fatalf("stop result = %+v", result)
	}
}
