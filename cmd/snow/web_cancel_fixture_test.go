//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const cancelFixtureEnv = "SNOW_WEB_CANCEL_FIXTURE_DIR"

func init() {
	if os.Getenv(cancelFixtureEnv) != "" && slices.Contains(os.Args, "--mode") {
		code := runCancelFixtureWorker()
		_ = os.WriteFile(filepath.Join(os.Getenv(cancelFixtureEnv), "worker-exit"), []byte(fmt.Sprint(code)), 0600)
		os.Exit(code)
	}
}

// Only admission timing is injected. Options, application, RPC dispatch, prompt
// lifecycle and cancellation remain production code; no provider network exists.
func runCancelFixtureWorker() int {
	directory := os.Getenv(cancelFixtureEnv)
	cwd, err := os.Getwd()
	if err != nil {
		return 90
	}
	if _, err := permissionFixtureProject(directory, cwd); err != nil {
		return 91
	}
	opts, err := policyFixtureOptions(os.Args[1:])
	if err != nil || opts.Provider != "fake" || opts.Model != "fake-1" || !opts.NoSession || !opts.NoPlugins || !opts.NoMCP || !opts.NoSkills || opts.Subagents == nil || *opts.Subagents {
		return 92
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	a, err := app.New(ctx, opts)
	if err != nil {
		return 93
	}
	defer a.Close()
	a.Providers["fake"] = &streamFixtureProvider{Provider: fake.New(nil), directory: directory}
	if err := a.SetProvider("fake"); err != nil {
		return 94
	}
	output := &cancelFixtureOutput{ctx: ctx, directory: directory, out: permissionFixtureOutput{out: os.Stdout}}
	defer func() { cancel(); output.pending.Wait() }()
	unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
		data, err := json.Marshal(event)
		if err != nil {
			cancel()
			return
		}
		if _, err := output.Write(append(data, '\n')); err != nil {
			cancel()
		}
	})
	defer unsubscribe()
	if err := rpc.New(ctx, a, permissionFixtureInput{ReadCloser: os.Stdin}, output).Serve(ctx); err != nil {
		return 95
	}
	return 0
}

type cancelFixtureOutput struct {
	ctx        context.Context
	directory  string
	out        permissionFixtureOutput
	admissions atomic.Uint64
	aborts     atomic.Uint64
	heldAbort  atomic.Bool
	pending    sync.WaitGroup
}

// The underlying fixture writer is bounded and the injected gate has the
// worker's finite context deadline; production requires this explicit contract.
func (*cancelFixtureOutput) RPCWriteBounded() bool { return true }

func (w *cancelFixtureOutput) Write(data []byte) (int, error) {
	var response struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
	}
	if json.Unmarshal(data, &response) == nil && response.Type == "response" && response.Success {
		if response.Command == "prompt" && w.admissions.Add(1) == 1 {
			if err := os.WriteFile(filepath.Join(w.directory, "admission-held"), []byte("held\n"), 0600); err != nil {
				return 0, err
			}
			if err := permissionFixtureGate(w.ctx, filepath.Join(w.directory, "release-admission")); err != nil {
				return 0, err
			}
		}
		if response.Command == "abort" {
			if err := os.WriteFile(filepath.Join(w.directory, "abort-count"), []byte(strconv.FormatUint(w.aborts.Add(1), 10)), 0600); err != nil {
				return 0, err
			}
			if w.admissions.Load() > 0 && w.heldAbort.CompareAndSwap(false, true) {
				if err := os.WriteFile(filepath.Join(w.directory, "cancel-response-held"), []byte("held\n"), 0600); err != nil {
					return 0, err
				}
				// Buffer just this response and return to release RPC's writeMu.
				// prompt_completed may then pass before the delayed abort ack.
				held := bytes.Clone(data)
				w.pending.Go(func() {
					if permissionFixtureGate(w.ctx, filepath.Join(w.directory, "release-cancel-ack")) == nil {
						_, _ = w.out.Write(held)
					}
				})
				return len(data), nil
			}
		}
	}
	return w.out.Write(data)
}

func TestWebCancelRealWorkerDuringAdmission(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(directory, "project-a")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", directory)
	t.Setenv("SNOW_HOME", filepath.Join(directory, "home"))
	t.Setenv(cancelFixtureEnv, directory)
	t.Setenv(policyFixtureEnv, "")
	t.Setenv(permissionFixtureEnv, "")
	t.Setenv(streamFixtureEnv, "")
	registry, err := web.OpenRegistry(t.Context(), filepath.Join(directory, "manager"))
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	project, err := registry.Add(t.Context(), "cancel", cwd)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewRuntimeManager(t.Context(), executable, filepath.Join(directory, "sessions"))
	var work sync.WaitGroup
	defer func() { manager.Close(); work.Wait() }()
	initial, err := manager.Open(t.Context(), project, "", "fake", "fake-1")
	if err != nil {
		exit, _ := os.ReadFile(filepath.Join(directory, "worker-exit"))
		t.Fatalf("open: %v; fixture exit: %s", err, exit)
	}
	// Opening binds the runtime with an existing startup abort. Count only
	// cancellation RPCs after that authoritative bootstrap has completed.
	bootstrap, err := os.ReadFile(filepath.Join(directory, "abort-count"))
	if err != nil {
		t.Fatal(err)
	}
	initialAborts, err := strconv.ParseUint(string(bootstrap), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	admitted := make(chan error, 1)
	work.Go(func() { admitted <- manager.Prompt(t.Context(), project.ID, initial.InstanceID, "cancel before text") })
	running := policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		_, err := os.Stat(filepath.Join(directory, "admission-held"))
		return err == nil && s.Status == "running" && s.CancelToken != ""
	})
	for range 2 {
		if err := manager.CancelTurn(t.Context(), project.ID, running.InstanceID, running.CancelToken); err != nil {
			t.Fatal(err)
		}
	}
	requested, _ := manager.Snapshot(project.ID)
	if !requested.CancelRequested || requested.Status != "running" {
		t.Fatalf("cancel must be requested, not fake completion: %+v", requested)
	}
	select {
	case err := <-admitted:
		t.Fatalf("admission unexpectedly finished before release: %v", err)
	default:
	}
	if err := os.WriteFile(filepath.Join(directory, "release-admission"), []byte("release\n"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-admitted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("admission remained blocked")
	}
	// RPC emits prompt_completed before acknowledging Abort. The UI must not
	// advertise Send as ready while that final cancellation control is held.
	settling := policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		_, err := os.Stat(filepath.Join(directory, "cancel-response-held"))
		return err == nil && s.Status == "idle" && s.CancelRequested
	})
	if err := manager.Prompt(t.Context(), project.ID, settling.InstanceID, "must not be admitted"); !errors.Is(err, web.ErrRuntimeBusy) {
		t.Fatalf("pending cancellation admitted another prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "release-cancel-ack"), []byte("release\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stopped := policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		return s.Status == "idle" && !s.CancelRequested && s.Recovery.State == web.RecoveryCanceled
	})
	count, err := os.ReadFile(filepath.Join(directory, "abort-count"))
	if err != nil || string(count) != strconv.FormatUint(initialAborts+1, 10) {
		t.Fatalf("duplicate cancellation dispatched multiple aborts: %s %v", count, err)
	}
	if err := manager.Prompt(t.Context(), project.ID, stopped.InstanceID, "second turn"); err != nil {
		t.Fatal(err)
	}
	next := policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		return s.Status == "running" && s.CancelToken != "" && s.CancelToken != running.CancelToken
	})
	if err := manager.CancelTurn(t.Context(), project.ID, next.InstanceID, running.CancelToken); err == nil {
		t.Fatal("stale cancellation accepted for later prompt")
	}
	if next.CancelRequested {
		t.Fatal("new prompt inherited cancellation intent")
	}
	if err := manager.CancelTurn(t.Context(), project.ID, next.InstanceID, next.CancelToken); err != nil {
		t.Fatal(err)
	}
	policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		return s.Status == "idle" && !s.CancelRequested && s.Recovery.State == web.RecoveryCanceled
	})
	count, err = os.ReadFile(filepath.Join(directory, "abort-count"))
	if err != nil || string(count) != strconv.FormatUint(initialAborts+2, 10) {
		t.Fatalf("later cancellation dispatch count: %s %v", count, err)
	}
}
