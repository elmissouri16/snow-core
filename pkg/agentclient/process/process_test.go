package process

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const helperMarker = "--snow-worker-helper"

// TestWorkerHelper executes only when explicitly invoked as our subprocess.
// All fixtures are local; no runtime, plugins, providers, or network are used.
func TestWorkerHelper(t *testing.T) {
	i := slices.Index(os.Args, helperMarker)
	if i < 0 {
		return
	}
	os.Exit(runHelper(os.Args[i+1:]))
}

func runHelper(args []string) int {
	mode := args[0]
	if len(args) > 1 && args[1] != "" {
		if err := os.WriteFile(args[1], []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			return 30
		}
	}
	if mode == "silent" {
		time.Sleep(time.Hour)
		return 0
	}
	if mode == "malformed" {
		fmt.Fprintln(os.Stdout, "not JSON")
		time.Sleep(time.Hour)
		return 0
	}
	if mode == "stderr-exit" {
		fmt.Fprintln(os.Stderr, "PRIVATE-STDERR-MUST-NOT-ESCAPE")
		return 23
	}
	if mode == "stderr-flood" {
		chunk := strings.Repeat("PRIVATE-STDERR-MUST-NOT-ESCAPE", 4096)
		for range 128 {
			fmt.Fprint(os.Stderr, chunk)
		}
	}
	ready, err := json.Marshal(protocol.NewRPCReady("helper"))
	if err != nil {
		return 31
	}
	fmt.Fprintln(os.Stdout, string(ready))
	if mode == "ignore-eof" {
		time.Sleep(time.Hour)
		return 0
	}
	if mode == "exit" {
		return 23
	}
	r := bufio.NewScanner(os.Stdin)
	for r.Scan() {
		var req protocol.RPCRequest
		if err := json.Unmarshal(r.Bytes(), &req); err != nil {
			return 32
		}
		cwd, _ := os.Getwd()
		data := map[string]any{"cwd": cwd, "env": os.Getenv("SNOW_PROCESS_TEST_ENV"), "args": args[2:], "message": req.Message}
		response, err := json.Marshal(protocol.RPCResponse{Type: "response", ID: req.ID, Command: req.Type, Success: true, Data: data})
		if err != nil {
			return 33
		}
		if _, err := fmt.Fprintln(os.Stdout, string(response)); err != nil {
			return 34
		}
	}
	if r.Err() != nil {
		return 35
	}
	if mode == "final-event" {
		frame, _ := json.Marshal(protocol.AgentEvent{Type: protocol.EvTurnDone})
		fmt.Fprintln(os.Stdout, string(frame))
	}
	if mode == "eof-marker" {
		if err := os.WriteFile(args[2], []byte("EOF"), 0o600); err != nil {
			return 36
		}
	}
	return 0
}

func helperOptions(t *testing.T, mode string, extra ...string) Options {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestWorkerHelper$", "--", helperMarker, mode, ""}
	return Options{Executable: executable, Args: append(args, extra...), Env: []string{"GORACE=atexit_sleep_ms=0"}, RPC: rpc.Options{HandshakeTimeout: 3 * time.Second}, ShutdownTimeout: 200 * time.Millisecond}
}

func startHelper(t *testing.T, ctx context.Context, opts Options) *Worker {
	t.Helper()
	w, err := Start(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("worker cleanup timed out")
	}
}

func TestStartArgumentsDirectoryEnvironment(t *testing.T) {
	// Deliberately include shell metacharacters: this is one literal argument.
	argument := "a b; $(touch must-not-exist) ' \""
	t.Setenv("SNOW_PROCESS_TEST_ENV", "inherited")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	for _, tt := range []struct {
		name string
		env  []string
		want string
	}{
		{"inherit", nil, "inherited"},
		{"replace", []string{"SNOW_PROCESS_TEST_ENV=explicit", "GORACE=atexit_sleep_ms=0"}, "explicit"},
		{"empty", []string{}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := helperOptions(t, "echo", argument)
			opts.Dir, opts.Env = t.TempDir(), tt.env
			// The race runtime's default exit sleep also applies to empty Env.
			opts.ShutdownTimeout = 2 * time.Second
			w := startHelper(t, t.Context(), opts)
			if got := w.Client.Ready().SnowVersion; got != "helper" {
				t.Fatalf("ready version = %q", got)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			response, err := w.Client.Call(ctx, protocol.RPCRequest{Type: "inspect", Message: "hello"})
			if err != nil {
				t.Fatal(err)
			}
			data := response.Data.(map[string]any)
			wantDir, err := filepath.EvalSymlinks(opts.Dir)
			if err != nil {
				t.Fatal(err)
			}
			gotDir, err := filepath.EvalSymlinks(data["cwd"].(string))
			if err != nil {
				t.Fatal(err)
			}
			if gotDir != wantDir || data["env"] != tt.want || data["message"] != "hello" || data["args"].([]any)[0] != argument {
				t.Fatalf("unexpected response: %#v", data)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if w.cmd.ProcessState == nil || !w.cmd.ProcessState.Success() {
				t.Fatal("child was not reaped successfully")
			}
		})
	}
}

func TestCloseGracefulEOFAndConcurrentCalls(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "eof")
	w := startHelper(t, t.Context(), helperOptions(t, "eof-marker", marker))
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := w.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "EOF" {
		t.Fatalf("graceful EOF marker = %q, %v", data, err)
	}
	if w.cmd.ProcessState == nil || !w.cmd.ProcessState.Success() {
		t.Fatal("graceful child was not reaped")
	}
}

func TestKillAndAutomaticCleanup(t *testing.T) {
	for _, trigger := range []string{"close", "cancel", "client-close"} {
		t.Run(trigger, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			w := startHelper(t, ctx, helperOptions(t, "ignore-eof"))
			start := time.Now()
			switch trigger {
			case "close":
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			case "client-close":
				_ = w.Client.Close()
			}
			await(t, w.done)
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Fatalf("shutdown took %v", elapsed)
			}
			if w.cmd.ProcessState == nil || w.cmd.ProcessState.Success() {
				t.Fatal("uncooperative child was not killed and reaped")
			}
			await(t, w.Client.Done())
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStartupFailuresAreBoundedAndPrivate(t *testing.T) {
	for _, mode := range []string{"silent", "malformed", "stderr-exit"} {
		t.Run(mode, func(t *testing.T) {
			opts := helperOptions(t, mode)
			pidPath := filepath.Join(t.TempDir(), "pid")
			opts.Args[len(opts.Args)-1] = pidPath
			if mode == "silent" {
				opts.RPC.HandshakeTimeout = 300 * time.Millisecond
			}
			start := time.Now()
			w, err := Start(t.Context(), opts)
			if err == nil || w != nil {
				if w != nil {
					_ = w.Close()
				}
				t.Fatal("unexpected startup success")
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("startup failure exceeded bounds")
			}
			if strings.Contains(err.Error(), "PRIVATE-STDERR") {
				t.Fatal("stderr leaked in startup error")
			}
			pidData, readErr := os.ReadFile(pidPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			pid, parseErr := strconv.Atoi(string(pidData))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			assertReaped(t, pid)
		})
	}
}

func TestStderrFloodDoesNotBlockReadiness(t *testing.T) {
	w := startHelper(t, t.Context(), helperOptions(t, "stderr-flood"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSpontaneousExitCleansUp(t *testing.T) {
	w, err := Start(t.Context(), helperOptions(t, "exit"))
	if err != nil {
		// Exit can win the handshake selection; failed startup also reaps.
		return
	}
	await(t, w.done)
	if err := w.Close(); err == nil {
		t.Fatal("nonzero spontaneous exit was not reported")
	}
	if w.cmd.ProcessState == nil {
		t.Fatal("child was not reaped")
	}
}

func TestInvalidAndCanceledStart(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w, err := Start(ctx, helperOptions(t, "echo"))
	if w != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Start = %v, %v", w, err)
	}
	for _, opts := range []Options{{}, {Executable: "does-not-exist-snow-test"}, {Executable: "unused", ShutdownTimeout: -1}, {Executable: "unused", Dir: filepath.Join(t.TempDir(), "missing")}} {
		if w, err := Start(t.Context(), opts); err == nil || w != nil {
			t.Fatalf("invalid Start = %v, %v", w, err)
		}
	}
	if w, err := Start(nil, helperOptions(t, "echo")); w != nil || err == nil {
		t.Fatalf("nil context Start = %v, %v", w, err)
	}
	opts := helperOptions(t, "ignore-eof")
	opts.RPC.EventBuffer = -1
	if w, err := Start(t.Context(), opts); w != nil || err == nil {
		t.Fatalf("invalid RPC options Start = %v, %v", w, err)
	}
}

func TestCancellationDuringHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	w, err := Start(ctx, helperOptions(t, "silent"))
	if w != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start = %v, %v", w, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("canceled handshake did not clean up promptly")
	}
}

func TestBlockedRequestWriteIsBounded(t *testing.T) {
	opts := helperOptions(t, "ignore-eof")
	opts.RPC.WriteTimeout = 50 * time.Millisecond
	w := startHelper(t, t.Context(), opts)
	_, err := w.Client.Call(t.Context(), protocol.RPCRequest{Type: "prompt", Message: strings.Repeat("x", 2*1024*1024)})
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("blocked write error = %v", err)
	}
	await(t, w.done)
}

func TestCloseDrainsFinalFrames(t *testing.T) {
	w := startHelper(t, t.Context(), helperOptions(t, "final-event"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	event, ok := <-w.Client.Events()
	if !ok || event.AgentEvent == nil || event.AgentEvent.Type != protocol.EvTurnDone {
		t.Fatalf("final frame lost during graceful exit: %+v, %v", event, ok)
	}
}
