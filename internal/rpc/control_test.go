package rpc

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/hostcontrol"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type controlTestService struct {
	calls    []string
	err      error
	large    bool
	deadline time.Time
}

func (s *controlTestService) record(ctx context.Context, name string) {
	s.calls = append(s.calls, name)
	s.deadline, _ = ctx.Deadline()
}
func (s *controlTestService) GetDefaults(ctx context.Context, request protocol.HostDefaultsRequest) (protocol.HostDefaultsResponse, error) {
	s.record(ctx, "get")
	return protocol.HostDefaultsResponse{Scope: request.Scope, CWD: request.CWD, Revision: "revision", AppliesTo: "future_runtime"}, s.err
}
func (s *controlTestService) UpdateDefaults(ctx context.Context, request protocol.HostDefaultsUpdateRequest) (protocol.HostDefaultsResponse, error) {
	s.record(ctx, "update")
	return protocol.HostDefaultsResponse{Scope: request.Scope, Revision: "updated"}, s.err
}
func (s *controlTestService) ProviderStatus(ctx context.Context) (protocol.HostProviderStatusResponse, error) {
	s.record(ctx, "status")
	response := protocol.HostProviderStatusResponse{CheckedLocally: true}
	if s.large {
		response.Providers = []protocol.HostProviderStatus{{ProviderID: strings.Repeat("secret-oversized-data", 10000)}}
	}
	return response, s.err
}

func TestControlReadyAndSerialAllowlist(t *testing.T) {
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project, _ := json.Marshal(protocol.HostDefaultsRequest{Scope: "project", CWD: cwd})
	input := strings.Join([]string{
		`{"id":"g","type":"defaults_get","params":{"scope":"global"}}`,
		`{"id":"p","type":"defaults_get","params":` + string(project) + `}`,
		`{"id":"u","type":"defaults_update","params":{"scope":"global","revision":"r","global":{"thinking":{"op":"set","value":"high"}}}}`,
		`{"id":"s","type":"provider_status_list","params":{}}`,
	}, "\n")
	var output bytes.Buffer
	service := &controlTestService{}
	if err := ServeControl(t.Context(), strings.NewReader(input), &output, cwd, "control-version", service); err != nil {
		t.Fatal(err)
	}
	frames := catalogFrames(t, output.String())
	if len(frames) != 5 {
		t.Fatalf("frames=%v", frames)
	}
	ready := frames[0]
	if ready["snow_version"] != "control-version" || ready["max_input_bytes"] != float64(protocol.RPCControlMaxInputBytes) ||
		!reflect.DeepEqual(ready["capabilities"], []any{"runtime_free_control", "defaults_control", "provider_status"}) {
		t.Fatalf("ready=%v", ready)
	}
	for _, frame := range frames[1:] {
		if frame["success"] != true {
			t.Fatalf("response=%v", frame)
		}
	}
	if !reflect.DeepEqual(service.calls, []string{"get", "get", "update", "status"}) {
		t.Fatalf("calls=%v", service.calls)
	}
	if remaining := time.Until(service.deadline); remaining <= 0 || remaining > controlOperationTimeout {
		t.Fatalf("operation deadline=%v", remaining)
	}
}

func TestControlRejectsOriginalFrameAndLegacyCommands(t *testing.T) {
	requests := []string{
		`{"type":"defaults_get","message":"secret-input","params":{"scope":"global"}}`,
		`{"type":"defaults_get","params":{"scope":"global","secret":"input"}}`,
		`{"type":"defaults_get","params":{"scope":"global","scope":"project"}}`,
		`{"type":"defaults_get","type":"provider_status_list"}`,
		`{"type":"defaults_get","params":{"scope":"global"},"params":{}}`,
		`{"type":"defaults_get","params":{"scope":"global","cwd":"/secret-path"}}`,
		`{"type":"defaults_get","params":{"scope":"project","cwd":"/secret-path"}}`,
		`{"type":"defaults_get","params":{}}`,
		`{"type":"defaults_get"}`,
		`{"type":"defaults_get","params":null}`,
		`{"type":"provider_status_list","params":{"refresh":true}}`,
		`{"type":"provider_status_list","params":[]}`,
		`{"type":"provider_status_list","id":null}`,
		`{"type":"provider_status_list","id":"` + strings.Repeat("x", 129) + `"}`,
		`{"type":"` + strings.Repeat("x", 65) + `"}`,
		`{"type":"defaults_update","params":{"scope":"global","revision":"r","global":{"thinking":{"op":"set","value":null}}}}`,
		`{"type":"defaults_update","params":{"scope":"global","revision":"r","global":{"thinking":{"op":"set","value":"` + strings.Repeat("x", 33) + `"}}}}`,
		`{"type":"defaults_update","params":{"scope":"global","revision":"r","global":{"thinking":{"op":"set","value":"high","secret":"input"}}}}`,
		`{"type":"defaults_update","params":{"scope":"global","revision":"r","global":{"thinking":{"op":"set","op":"reset","value":"high"}}}}`,
		`{"type":"defaults_update","params":{"scope":"global","revision":"r","project":{}}}`,
		`{"type":"defaults_update","params":{"scope":"global","revision":"r","global":{},"project":{}}}`,
		`{"type":"defaults_update","params":{"scope":"global","revision":"` + strings.Repeat("x", 257) + `","global":{}}}`,
		`{"type":"defaults_update","params":{"scope":"global","revision":"r","global":{"provider_model":{"op":"set","value":{"provider":"fake","model":"` + strings.Repeat("x", 257) + `"}}}}}`,
		`{"type":"defaults_get","params":{"scope":"project","cwd":"` + strings.Repeat("x", 4097) + `"}}`,
		`{"type":"provider_status_list","params":{` + strings.Repeat(" ", 16*1024) + `}}`,
		`null`, `[]`, `{} {}`, "\xff",
	}
	for _, command := range []string{"prompt", "session_create", "session_open", "sessions_list", "catalog_sessions", "models_discover", "models_list", "auth_login_start", "auth_providers", "auth_logout", "provider_set", "tools_list", "plugin_reload", "mcp_servers", "api_key_set"} {
		requests = append(requests, `{"type":"`+command+`"}`)
	}
	for _, request := range requests {
		t.Run(request[:min(len(request), 90)], func(t *testing.T) {
			var output bytes.Buffer
			service := &controlTestService{}
			if err := ServeControl(t.Context(), strings.NewReader(request+"\n"), &output, t.TempDir(), "", service); err != nil {
				t.Fatal(err)
			}
			frames := catalogFrames(t, output.String())
			if len(frames) != 2 || frames[1]["success"] != false || len(service.calls) != 0 {
				t.Fatalf("accepted request: %v, calls=%v", frames, service.calls)
			}
			if strings.Contains(output.String(), "secret-") {
				t.Fatal("reflected private payload")
			}
		})
	}
}

func TestControlErrorsAndResponseBound(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		large bool
		code  string
	}{
		{"raw", errors.New("secret-path secret-key"), false, "invalid"},
		{"unavailable", hostcontrol.ErrUnavailable, false, "unavailable"},
		{"conflict", hostcontrol.ErrRevisionConflict, false, "revision_conflict"},
		{"canceled", context.Canceled, false, "canceled"},
		{"deadline", context.DeadlineExceeded, false, "canceled"},
		{"bound", nil, true, "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			service := &controlTestService{err: tc.err, large: tc.large}
			if err := ServeControl(t.Context(), strings.NewReader(`{"id":"s","type":"provider_status_list"}`), &output, t.TempDir(), "", service); err != nil {
				t.Fatal(err)
			}
			frames := catalogFrames(t, output.String())
			if frames[1]["error_code"] != tc.code || frames[1]["id"] != "s" || strings.Contains(output.String(), "secret-") {
				t.Fatalf("frames=%v", frames)
			}
			for line := range strings.SplitSeq(strings.TrimSpace(output.String()), "\n") {
				if len(line)+1 > protocol.RPCControlMaxOutputBytes {
					t.Fatal("unbounded frame")
				}
			}
		})
	}
}

func TestControlProjectPinRejectsReplacement(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	pin, err := newControlProjectPin(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !pin.matches("project", pin.cwd) {
		t.Fatal("original project rejected")
	}
	if err := os.Rename(cwd, cwd+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	if pin.matches("project", pin.cwd) {
		t.Fatal("replacement accepted")
	}
	if !pin.matches("global", "") {
		t.Fatal("global scope depends on project")
	}
}

func TestControlCancellationInterruptsInput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	input, writer := io.Pipe()
	defer writer.Close()
	done := make(chan error, 1)
	cwd := t.TempDir()
	go func() { done <- ServeControl(ctx, input, io.Discard, cwd, "", &controlTestService{}) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("input was not interrupted")
	}
}

func TestControlOversizedInputAndUnsafeTransport(t *testing.T) {
	cwd := t.TempDir()
	service := &controlTestService{}
	var output bytes.Buffer
	if err := ServeControl(t.Context(), strings.NewReader(strings.Repeat("x", protocol.RPCControlMaxInputBytes+1)+"\n"), &output, cwd, "", service); !errors.Is(err, errControlUnavailable) {
		t.Fatalf("error=%v", err)
	}
	if len(service.calls) != 0 {
		t.Fatal("oversized request executed")
	}
	for _, tc := range []struct {
		in  io.Reader
		out io.Writer
	}{
		{&unavailableDeadlineReader{}, io.Discard},
		{strings.NewReader(""), &unavailableDeadlineWriter{}},
	} {
		if err := ServeControl(t.Context(), tc.in, tc.out, cwd, "", service); !errors.Is(err, errControlUnavailable) {
			t.Fatalf("unsafe transport error=%v", err)
		}
	}
}

type controlDeadlineWriter struct {
	bytes.Buffer
	deadlines []time.Time
}

func (w *controlDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestControlUsesIndependentFiveSecondWriteBudget(t *testing.T) {
	writer := &controlDeadlineWriter{}
	start := time.Now()
	if err := ServeControl(t.Context(), strings.NewReader(`{"type":"provider_status_list"}`), writer, t.TempDir(), "", &controlTestService{}); err != nil {
		t.Fatal(err)
	}
	if len(writer.deadlines) != 4 {
		t.Fatalf("write/clear deadlines=%v", writer.deadlines)
	}
	for i, deadline := range writer.deadlines {
		if i%2 == 1 {
			if !deadline.IsZero() {
				t.Fatal("write deadline not cleared")
			}
			continue
		}
		budget := deadline.Sub(start)
		if budget < 5*time.Second || budget > 6*time.Second {
			t.Fatalf("control inherited live RPC deadline: %v", budget)
		}
	}
}

type controlWaitingService struct {
	controlTestService
	started chan struct{}
}

func (s *controlWaitingService) ProviderStatus(ctx context.Context) (protocol.HostProviderStatusResponse, error) {
	close(s.started)
	<-ctx.Done()
	return protocol.HostProviderStatusResponse{}, ctx.Err()
}

func TestControlCancellationReachesHostOperation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service := &controlWaitingService{started: make(chan struct{})}
	cwd := t.TempDir()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- ServeControl(ctx, strings.NewReader(`{"id":"wait","type":"provider_status_list"}`), &output, cwd, "", service)
	}()
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("host operation did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("host operation did not cancel")
	}
	frames := catalogFrames(t, output.String())
	if len(frames) != 2 || frames[1]["error_code"] != "canceled" {
		t.Fatalf("frames=%v", frames)
	}
}
