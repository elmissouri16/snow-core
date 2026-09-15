package web

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/agentclient/process"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func hostTestDefaults(scope string) protocol.HostDefaultsResponse {
	pair := protocol.HostProviderModelDefault{Effective: protocol.HostProviderModel{Provider: "opencode-zen"}, Source: "builtin"}
	thinking := protocol.HostStringDefault{Effective: "off", Source: "builtin"}
	result := protocol.HostDefaultsResponse{Scope: scope, Revision: strings.Repeat("a", 64), AppliesTo: "future_runtime"}
	if scope == "global" {
		result.Global = &protocol.HostGlobalDefaults{ProviderModel: pair, Thinking: thinking, ReasoningSummary: protocol.HostStringDefault{Effective: "auto", Source: "builtin"}, TextVerbosity: protocol.HostStringDefault{Effective: "low", Source: "builtin"}}
	} else {
		result.Project = &protocol.HostProjectDefaults{ProviderModel: pair, Thinking: thinking}
	}
	return result
}
func TestHostControlWorkerFixture(t *testing.T) {
	if os.Getenv("SNOW_HOST_CONTROL_FIXTURE") != "1" {
		return
	}
	emit := func(value any) {
		data, err := json.Marshal(value)
		if err != nil {
			os.Exit(9)
		}
		_, _ = os.Stdout.Write(append(data, '\n'))
	}
	mode := os.Getenv("SNOW_HOST_CONTROL_MODE")
	ready := protocol.NewRPCReady("fixture")
	ready.Capabilities = []string{"runtime_free_control", "defaults_control", "provider_status"}
	if mode == "eager" {
		ready.Capabilities = []string{"defaults_control", "provider_status"}
	}
	emit(ready)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request protocol.RPCRequest
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(10)
		}
		if log := os.Getenv("SNOW_HOST_CONTROL_LOG"); log != "" {
			_ = os.WriteFile(log, scanner.Bytes(), 0600)
		}
		if mode == "hang" {
			time.Sleep(10 * time.Second)
			continue
		}
		var input protocol.HostDefaultsRequest
		_ = json.Unmarshal(request.Params, &input)
		raw := hostTestDefaults(input.Scope)
		raw.CWD = input.CWD
		response := protocol.RPCResponse{ID: request.ID, Type: "response", Command: request.Type, Success: true, Data: raw}
		if request.Type == "provider_status_list" {
			response.Data = protocol.HostProviderStatusResponse{CheckedLocally: true, Providers: []protocol.HostProviderStatus{{ProviderID: "opencode-zen", State: "configured", Reason: "anonymous_access", CheckedLocally: true}}}
		}
		switch mode {
		case "secret":
			response.Success = false
			response.Error = "SECRET_TOKEN /private/credentials"
		case "oversized":
			response.Data = map[string]any{"secret": strings.Repeat("x", 65<<10)}
		case "extensions":
			response.Data = map[string]any{"scope": "global", "revision": raw.Revision, "applies_to": "future_runtime", "global": raw.Global, "auth": "SECRET_TOKEN", "cwd": "/private/path"}
		}
		emit(response)
	}
	os.Exit(0)
}
func hostTestWorker(t *testing.T, mode string) (*WorkerControl, string) {
	t.Helper()
	base := t.TempDir()
	registry := registryTestOpen(t, filepath.Join(base, "manager"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := NewWorkerControl(exe, registry.path, registry)
	log := filepath.Join(base, "worker.log")
	c.start = func(ctx context.Context, opts process.Options) (*process.Worker, error) {
		if !reflect.DeepEqual(opts.Args, []string{"--mode", "rpc", "--rpc-startup", "control"}) || opts.RPC.MaxFrameBytes != 64<<10 || opts.RPC.MaxPending != 1 {
			t.Error("unsafe worker arguments or limits")
		}
		opts.Args = []string{"-test.run=^TestHostControlWorkerFixture$"}
		opts.Env = append(os.Environ(), "SNOW_HOST_CONTROL_FIXTURE=1", "SNOW_HOST_CONTROL_MODE="+mode, "SNOW_HOST_CONTROL_LOG="+log)
		return process.Start(ctx, opts)
	}
	return c, log
}
func TestHostControlShortWorkerAndProjection(t *testing.T) {
	for _, mode := range []string{"", "extensions", "eager", "secret", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			c, log := hostTestWorker(t, mode)
			if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("construction started a worker")
			}
			result, err := c.Defaults(t.Context(), "global", "")
			if mode == "" || mode == "extensions" {
				if err != nil || result.Global == nil || result.Global.ProviderModel.Effective.Model != "" {
					t.Fatalf("fresh defaults: %+v %v", result, err)
				}
				data, _ := json.Marshal(result)
				if strings.Contains(string(data), "SECRET") || strings.Contains(string(data), "cwd") || strings.Contains(string(data), "private") {
					t.Fatalf("private output: %s", data)
				}
				request, err := os.ReadFile(log)
				if err != nil || !strings.Contains(string(request), "defaults_get") || strings.Contains(string(request), "cwd") {
					t.Fatalf("request: %s %v", request, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("untrusted worker: %v", err)
			}
			if len(c.slots) != 0 {
				t.Fatal("worker retained after operation")
			}
		})
	}
}
func TestHostControlProjectIdentityBeforeAndAfterSpawn(t *testing.T) {
	for _, phase := range []string{"before", "after"} {
		t.Run(phase, func(t *testing.T) {
			c, log := hostTestWorker(t, "")
			dir := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			project, err := c.registry.Add(t.Context(), "project", dir)
			if err != nil {
				t.Fatal(err)
			}
			replace := func() {
				t.Helper()
				if err := os.Rename(dir, dir+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			start := c.start
			starts := 0
			c.start = func(ctx context.Context, opts process.Options) (*process.Worker, error) {
				starts++
				if opts.Dir != project.Path {
					t.Error("CWD not registry selected")
				}
				worker, err := start(ctx, opts)
				if err == nil && phase == "after" {
					replace()
				}
				return worker, err
			}
			if phase == "before" {
				replace()
			}
			_, err = c.Defaults(t.Context(), "project", project.ID)
			if !errors.Is(err, ErrHostControlUnavailable) || phase == "before" && starts != 0 {
				t.Fatalf("replacement admitted: starts=%d err=%v", starts, err)
			}
			if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("operation sent after replacement")
			}
		})
	}
}
func TestHostControlAdmissionAndDeadlines(t *testing.T) {
	c := NewWorkerControl("/operator/snow", "/operator/manager", nil)
	entered := make(chan time.Duration, 2)
	release := make(chan struct{})
	var active atomic.Int32
	c.start = func(ctx context.Context, opts process.Options) (*process.Worker, error) {
		active.Add(1)
		defer active.Add(-1)
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("missing deadline")
		}
		entered <- time.Until(deadline)
		<-release
		return nil, errors.New("fixture unavailable")
	}
	finished := make(chan struct{}, 2)
	for range 2 {
		go func() { _, _ = c.Defaults(t.Context(), "global", ""); finished <- struct{}{} }()
	}
	for range 2 {
		if remaining := <-entered; remaining <= 0 || remaining > 3*time.Second {
			t.Errorf("read deadline %v", remaining)
		}
	}
	if _, err := c.Defaults(t.Context(), "global", ""); !errors.Is(err, ErrHostControlBusy) || active.Load() != 2 {
		t.Fatalf("queued third worker: %v", err)
	}
	close(release)
	for range 2 {
		<-finished
	}
	entered = make(chan time.Duration, 2)
	release = make(chan struct{})
	input := protocol.HostDefaultsUpdateRequest{Scope: "global", Revision: strings.Repeat("a", 64), Global: &protocol.HostGlobalDefaultsPatch{Thinking: &protocol.HostStringOperation{Op: "reset"}}}
	go func() { _, _ = c.UpdateDefaults(t.Context(), "global", "", input); finished <- struct{}{} }()
	if remaining := <-entered; remaining <= 3*time.Second || remaining > 5*time.Second {
		t.Errorf("write deadline %v", remaining)
	}
	if _, err := c.UpdateDefaults(t.Context(), "global", "", input); !errors.Is(err, ErrHostControlBusy) {
		t.Fatalf("concurrent write admitted: %v", err)
	}
	close(release)
	<-finished
	if err := c.call(t.Context(), "global", "", "settings_update", struct{}{}, nil); !errors.Is(err, ErrHostControlInvalid) {
		t.Fatal("arbitrary command admitted")
	}
}

func TestHostControlPinsOperatorHomeBeforeProjectCWD(t *testing.T) {
	t.Setenv("SNOW_HOME", "relative-operator-home")
	expected, err := filepath.Abs("relative-operator-home")
	if err != nil {
		t.Fatal(err)
	}
	c := NewWorkerControl("/operator/snow", "/operator/manager", nil)
	// Constructor snapshots environment without reading any configuration file.
	t.Setenv("SNOW_HOME", "different-home")
	found := 0
	for _, value := range c.env {
		if strings.HasPrefix(value, "SNOW_HOME=") {
			found++
			if value != "SNOW_HOME="+expected {
				t.Fatalf("unfixed config root: %q", value)
			}
		}
	}
	if found != 1 {
		t.Fatalf("SNOW_HOME entries = %d", found)
	}
}

func TestHostControlProjectRequestAndCancellation(t *testing.T) {
	c, log := hostTestWorker(t, "")
	project, err := c.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Defaults(t.Context(), "project", project.ID)
	if err != nil || result.ProjectID != project.ID || result.Project == nil {
		t.Fatalf("project defaults: %+v %v", result, err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var request protocol.RPCRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	var params protocol.HostDefaultsRequest
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.CWD != project.Path || params.Scope != "project" {
		t.Fatalf("project not resolved on host: %+v", params)
	}
	hanging, _ := hostTestWorker(t, "hang")
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = hanging.Defaults(ctx, "global", "")
	if err == nil || time.Since(started) > 2*time.Second || len(hanging.slots) != 0 {
		t.Fatalf("canceled worker not reaped: %v", err)
	}
}
