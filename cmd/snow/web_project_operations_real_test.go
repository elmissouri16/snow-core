//go:build darwin || linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/hostcontrol"
	"github.com/elmissouri16/snow-core/internal/hostops"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const realProjectCanary = "FICTIONAL_GIT_OUTPUT_MUST_NEVER_BE_PUBLIC"

// Only the test executable recognizes these private fixture arguments. The
// production host helper early hook (in host_clone_helper_test.go) remains the
// actual implementation, with real descriptor and liveness-pipe supervision.
func init() {
	if len(os.Args) < 3 || !strings.HasPrefix(os.Args[1], "--fixture-real-project-") {
		return
	}
	root := os.Args[2]
	info, err := os.Stat(root)
	marker, markerErr := os.ReadFile(filepath.Join(root, "fixture-only"))
	if !filepath.IsAbs(root) || err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || markerErr != nil || string(marker) != "PRIVATE_PROJECT_OPERATIONS_FIXTURE" {
		os.Exit(90)
	}
	code := 91
	switch os.Args[1] {
	case "--fixture-real-project-manager":
		code = realProjectManager(root)
	case "--fixture-real-project-git":
		code = realProjectGit(root)
	case "--fixture-real-project-control", "--fixture-real-project-root":
		if slices.Equal(os.Args[3:], []string{"--mode", "rpc", "--rpc-startup", "control"}) {
			if os.Args[1] == "--fixture-real-project-root" {
				os.Args = append([]string{"snow"}, os.Args[3:]...)
				if run() == nil {
					code = 0
				}
			} else {
				code = realProjectControl(root)
			}
		}
	}
	os.Exit(code)
}

func realProjectManager(root string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); cancel() }()
	if web.Run(ctx, web.Options{Listen: "127.0.0.1:0", Version: "real-project-operations-fixture", ManagerDir: filepath.Join(root, "manager"),
		Executable: filepath.Join(root, "worker"), SessionsRoot: filepath.Join(root, "sessions")}, os.Stdout) != nil {
		return 92
	}
	return 0
}

func realProjectControl(root string) int {
	cwd, err := os.Getwd()
	if err != nil {
		return 93
	}
	if appendRealProjectRecord(filepath.Join(root, "worker-pids"), []byte(strconv.Itoa(os.Getpid()))) != nil {
		return 94
	}
	service, err := hostcontrol.New(hostcontrol.Options{})
	if err != nil {
		return 95
	}
	self, err := os.Executable()
	if err != nil {
		return 96
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	output := &realProjectRPCOutput{root: root, out: &permissionFixtureOutput{out: os.Stdout}}
	// The only service substitution is the trusted Git executable. All request
	// decoding, admission, Prepare/Start/Release, helper execution and completion
	// frames remain the actual production implementations.
	err = rpc.ServeControl(ctx, permissionFixtureInput{ReadCloser: os.Stdin}, output, cwd, "real-project-fixture", service,
		rpc.ControlOptions{HostOperations: rpc.NewControlHostOperations(hostops.Options{GitExecutable: filepath.Join(root, "git"), HelperExecutable: self})})
	if err != nil {
		return 97
	}
	return 0
}

type realProjectRPCOutput struct {
	root string
	out  *permissionFixtureOutput
}

func (*realProjectRPCOutput) RPCWriteBounded() bool { return true }
func (w *realProjectRPCOutput) Write(data []byte) (int, error) {
	n, err := w.out.Write(data)
	if err != nil {
		return n, err
	}
	if err := appendRealProjectRecord(filepath.Join(w.root, "rpc-wire"), bytes.TrimSpace(data)); err != nil {
		return n, err
	}
	var frame struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
	}
	if json.Unmarshal(data, &frame) == nil && frame.Type == "response" && frame.Command == "project_clone_start" && frame.Success {
		// This marker is recorded only AFTER the actual ACK bytes were written,
		// and before Write returns to CONTROL's Release call.
		if err := os.WriteFile(filepath.Join(w.root, "clone-ack"), data, 0600); err != nil {
			return n, err
		}
	}
	return n, nil
}

func appendRealProjectRecord(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(bytes.Clone(data), '\n'))
	return err
}

// The fictional Git executable never invokes Git or accesses a network. It
// checks both the already-written transport ACK and the real committed manager
// row before recording its first execution marker, then waits on a private gate.
func realProjectGit(root string) int {
	fail := func(code int) int {
		_ = os.WriteFile(filepath.Join(root, "git-fixture-error"), []byte(strconv.Itoa(code)), 0600)
		return code
	}
	ackData, err := os.ReadFile(filepath.Join(root, "clone-ack"))
	if err != nil {
		return fail(100)
	}
	var ack struct {
		ID      string                      `json:"id"`
		Success bool                        `json:"success"`
		Data    protocol.RPCProjectPrepared `json:"data"`
	}
	if json.Unmarshal(ackData, &ack) != nil || !ack.Success || ack.ID == "" {
		return fail(101)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(102)
	}
	identity, err := fixtureHostIdentity(cwd)
	if err != nil || identity != ack.Data.Child {
		return fail(103)
	}
	dsn := url.URL{Scheme: "file", Path: filepath.Join(root, "manager", "manager.db"), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return fail(104)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var record string
	if db.QueryRowContext(ctx, `SELECT record FROM manager_operations WHERE id=? AND state='running'`, ack.Data.OperationID).Scan(&record) != nil {
		return fail(105)
	}
	var durable web.ProjectOperation
	if json.Unmarshal([]byte(record), &durable) != nil || durable.Outcome != "observed" || durable.Child.Path != identity.Path || durable.Child.Inode != identity.Inode || durable.Child.Device != identity.Device {
		return fail(106)
	}
	evidence, _ := json.Marshal(map[string]any{"request_id": ack.ID, "operation_id": durable.ID, "durable_state": durable.State, "child": identity})
	if appendRealProjectRecord(filepath.Join(root, "git-starts"), evidence) != nil {
		return fail(107)
	}
	if os.WriteFile(filepath.Join(root, "git-pid"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
		return fail(108)
	}
	if os.WriteFile("FICTIONAL_PARTIAL", []byte("private fixture, not a repository"), 0600) != nil {
		return fail(109)
	}
	fmt.Fprintln(os.Stdout, realProjectCanary)
	fmt.Fprintln(os.Stderr, realProjectCanary)
	for {
		_ = os.WriteFile(filepath.Join(root, "git-tick"), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0600)
		if _, err := os.Stat(filepath.Join(root, "release-git")); err == nil {
			return 0
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type realProjectHTTP struct {
	*managerExecutionHTTP
	cmd     *exec.Cmd
	input   io.WriteCloser
	done    chan error
	stderr  bytes.Buffer
	stopped bool
}

func newRealProjectRoot(t *testing.T, actualRoot bool) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"home", "snow", "parent"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "fixture-only"), []byte("PRIVATE_PROJECT_OPERATIONS_FIXTURE"), 0600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	workerMode := "--fixture-real-project-control"
	if actualRoot {
		workerMode = "--fixture-real-project-root"
	}
	for name, mode := range map[string]string{"worker": workerMode, "git": "--fixture-real-project-git"} {
		script := "#!/bin/sh\nexec " + quote(self) + " " + mode + " " + quote(root) + " \"$@\"\n"
		if err := os.WriteFile(filepath.Join(root, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func startRealProjectHTTP(t *testing.T, root string) *realProjectHTTP {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f := &realProjectHTTP{managerExecutionHTTP: &managerExecutionHTTP{t: t, directory: root, client: &http.Client{Timeout: 10 * time.Second}}, done: make(chan error, 1)}
	processCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 100*time.Second)
	t.Cleanup(cancel)
	f.cmd = exec.CommandContext(processCtx, self, "--fixture-real-project-manager", root)
	f.cmd.Dir = root
	// No inherited provider credentials, proxies, endpoints, plugin fixture
	// sentinels, real HOME, or Snow paths reach this production manager/worker.
	f.cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(root, "home"), "SNOW_HOME=" + filepath.Join(root, "snow"), "SNOW_SESSIONS_DIR=" + filepath.Join(root, "sessions"), "TMPDIR=" + root, "GOMAXPROCS=2"}
	f.input, err = f.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := f.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	f.cmd.Stderr = &f.stderr
	if err := f.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { f.done <- f.cmd.Wait() }()
	t.Cleanup(func() { f.stop(false) })
	startup := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		var lines []string
		for len(lines) < 64 && scanner.Scan() {
			lines = append(lines, scanner.Text())
			if _, ok := fixturePairingCode(strings.Join(lines, "\n")); ok {
				break
			}
		}
		startup <- strings.Join(lines, "\n")
	}()
	var output string
	select {
	case output = <-startup:
	case <-time.After(8 * time.Second):
		t.Fatal("real project manager startup timed out")
	}
	f.origin, f.cookie = permissionFixturePair(t, t.Context(), output)
	status, body := f.request("GET", "/", nil)
	if status != http.StatusOK {
		t.Fatalf("paired manager status=%d", status)
	}
	csrf, err := fixturePageCSRF(body)
	if err != nil {
		t.Fatal(err)
	}
	f.csrf = csrf
	return f
}

func (f *realProjectHTTP) stop(kill bool) {
	if f.stopped {
		return
	}
	f.stopped = true
	if kill {
		_ = f.cmd.Process.Kill()
	}
	_ = f.input.Close()
	select {
	case err := <-f.done:
		if !kill && err != nil {
			f.t.Errorf("real project manager exit=%v", err)
		}
	case <-time.After(12 * time.Second):
		_ = f.cmd.Process.Kill()
		<-f.done
		f.t.Error("real project manager shutdown exceeded bound")
	}
	if bytes.Contains(f.stderr.Bytes(), []byte(realProjectCanary)) {
		f.t.Error("manager stderr leaked Git output")
	}
}
func (f *realProjectHTTP) request(method, path string, form url.Values) (int, []byte) {
	f.t.Helper()
	status, body := f.managerExecutionHTTP.request(method, path, form)
	if bytes.Contains(body, []byte(realProjectCanary)) {
		f.t.Fatal("HTTP exposed Git output")
	}
	return status, body
}
func (f *realProjectHTTP) admit(kind, name string) (web.ProjectOperation, url.Values) {
	f.t.Helper()
	status, body := f.request("POST", "/projects/folders/select", url.Values{"path": {filepath.Join(f.directory, "parent")}})
	var grant web.ParentSelection
	if status != http.StatusOK || json.Unmarshal(body, &grant) != nil {
		f.t.Fatalf("select parent: %d %s", status, body)
	}
	form := url.Values{"operation_id": {grant.OperationID}, "name": {name}}
	if kind == "clone" {
		form.Set("remote", "https://example.invalid/fictional/repo.git")
	}
	status, body = f.request("POST", "/projects/"+kind, form)
	var op web.ProjectOperation
	if status != http.StatusAccepted || json.Unmarshal(body, &op) != nil {
		f.t.Fatalf("admit: %d %s", status, body)
	}
	return op, form
}
func (f *realProjectHTTP) get(id string) web.ProjectOperation {
	f.t.Helper()
	status, body := f.request("GET", "/operations/"+id, nil)
	var op web.ProjectOperation
	if status != http.StatusOK || json.Unmarshal(body, &op) != nil {
		f.t.Fatalf("operation read: %d %s", status, body)
	}
	return op
}
func (f *realProjectHTTP) await(id string, states ...string) web.ProjectOperation {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var op web.ProjectOperation
	for time.Now().Before(deadline) {
		op = f.get(id)
		if slices.Contains(states, op.State) {
			return op
		}
		time.Sleep(15 * time.Millisecond)
	}
	fixtureError, _ := os.ReadFile(filepath.Join(f.directory, "git-fixture-error"))
	f.t.Fatalf("operation never reached %v: state=%s error=%s fixture=%s", states, op.State, op.Error, fixtureError)
	return op
}
func (f *realProjectHTTP) control(id, action string, revision int64) (int, web.ProjectOperation) {
	f.t.Helper()
	status, body := f.request("POST", "/operations/"+id+"/"+action, url.Values{"revision": {strconv.FormatInt(revision, 10)}})
	var op web.ProjectOperation
	if status == http.StatusOK && json.Unmarshal(body, &op) != nil {
		f.t.Fatal("invalid operation control JSON")
	}
	return status, op
}
func waitRealProjectFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fixture evidence missing: %s", filepath.Base(path))
	return nil
}
func assertRealProjectNoRuntime(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "sessions")); !os.IsNotExist(err) {
		t.Fatal("project operation initialized sessions")
	}
	for _, dir := range []string{"home", "snow"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil || len(entries) != 0 {
			t.Fatalf("project operation initialized %s runtime config/auth", dir)
		}
	}
	wire, _ := os.ReadFile(filepath.Join(root, "rpc-wire"))
	if bytes.Contains(wire, []byte(realProjectCanary)) {
		t.Fatal("recorded RPC exposed Git output")
	}
}
func assertRealProjectGitStopped(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "git-tick")
	pid, err := strconv.Atoi(string(waitRealProjectFile(t, filepath.Join(root, "git-pid"))))
	if err != nil || pid < 1 {
		t.Fatal("invalid private fictional Git PID")
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		before, _ := os.ReadFile(path)
		time.Sleep(150 * time.Millisecond)
		after, _ := os.ReadFile(path)
		if len(before) > 0 && bytes.Equal(before, after) && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
	}
	t.Fatal("fictional Git remained active after cancellation/worker loss")
}

func (f *realProjectHTTP) assertAwaitingRegistration(op web.ProjectOperation) {
	f.t.Helper()
	// Observe a settled success, not a transient state between a terminal commit
	// and an incorrect automatic registration transaction.
	time.Sleep(100 * time.Millisecond)
	settled := f.get(op.ID)
	if settled.State != "awaiting_registration" || settled.ProjectID != "" || settled.Revision != op.Revision {
		f.t.Fatalf("successful operation changed without explicit registration: state=%s", settled.State)
	}
	assertRealProjectRegistryCount(f.t, f.directory, 0)
}

func assertRealProjectRegistryCount(t *testing.T, root string, want int) {
	t.Helper()
	dsn := url.URL{Scheme: "file", Path: filepath.Join(root, "manager", "manager.db"), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		t.Fatal("private fixture registry unavailable")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM projects`).Scan(&count); err != nil {
		t.Fatal("private registry count unavailable")
	}
	if count != want {
		t.Fatalf("registered project count=%d, want %d", count, want)
	}
	rows, err := db.QueryContext(ctx, `SELECT record FROM manager_operations`)
	if err != nil {
		t.Fatal("private operation records unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var record string
		if err := rows.Scan(&record); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(record, realProjectCanary) {
			t.Fatal("durable operation record leaked Git output")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func assertRealProjectTerminalCorrelation(t *testing.T, root, operationID string) {
	t.Helper()
	var ack struct {
		ID   string                      `json:"id"`
		Data protocol.RPCProjectPrepared `json:"data"`
	}
	if json.Unmarshal(waitRealProjectFile(t, filepath.Join(root, "clone-ack")), &ack) != nil {
		t.Fatal("invalid private clone ACK")
	}
	wire := waitRealProjectFile(t, filepath.Join(root, "rpc-wire"))
	found := false
	for line := range bytes.SplitSeq(wire, []byte("\n")) {
		var frame struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &frame) != nil || frame.Type != protocol.RPCTypeProjectCompleted {
			continue
		}
		var terminal protocol.RPCProjectCompleted
		if json.Unmarshal(line, &terminal) != nil || terminal.RequestID != ack.ID || terminal.OperationID != operationID || terminal.Child != ack.Data.Child {
			t.Fatal("terminal frame lost exact clone-start correlation")
		}
		found = true
	}
	if !found {
		t.Fatal("clone completed without a recorded correlated terminal frame")
	}
}

func TestWebProjectOperationsRealCreateActualRoot(t *testing.T) {
	root := newRealProjectRoot(t, true)
	f := startRealProjectHTTP(t, root)
	admitted, _ := f.admit("create", "created")
	op := f.await(admitted.ID, "awaiting_registration", "succeeded")
	if op.State != "awaiting_registration" || op.ProjectID != "" {
		t.Fatalf("creation registered without explicit review: state=%s project_id_present=%t", op.State, op.ProjectID != "")
	}
	f.assertAwaitingRegistration(op)
	identity, err := fixtureHostIdentity(op.Child.Path)
	if err != nil || identity.Inode != op.Child.Inode || identity.Device != op.Child.Device {
		t.Fatal("prepared child identity was not preserved")
	}
	status, _ := f.control(op.ID, "register", op.Revision-1)
	if status != http.StatusConflict {
		t.Fatalf("stale registration revision accepted: %d", status)
	}
	status, registered := f.control(op.ID, "register", op.Revision)
	if status != http.StatusOK || registered.State != "succeeded" || registered.ProjectID == "" {
		t.Fatalf("explicit registration failed: %d %s", status, registered.State)
	}
	assertRealProjectRegistryCount(t, root, 1)
	if _, err := os.Stat(filepath.Join(root, "git-starts")); !os.IsNotExist(err) {
		t.Fatal("create started Git")
	}
	assertRealProjectNoRuntime(t, root)
}

func TestWebProjectOperationsRealCloneDurableACKDuplicateAndRegistration(t *testing.T) {
	root := newRealProjectRoot(t, false)
	f := startRealProjectHTTP(t, root)
	admitted, form := f.admit("clone", "cloned")
	waitRealProjectFile(t, filepath.Join(root, "git-starts"))
	op := f.get(admitted.ID)
	if op.State != "running" || op.Child.Path == "" || op.ProjectID != "" {
		t.Fatalf("clone lacks durable pre-execution identity: %+v", op)
	}
	status, _ := f.request("POST", "/projects/clone", form)
	if status != http.StatusAccepted {
		t.Fatalf("duplicate admission response=%d", status)
	}
	// No worker commands during this interval: an active clone must survive
	// CONTROL's otherwise five-second idle expiry. HTTP reads are manager-local.
	time.Sleep(5100 * time.Millisecond)
	if f.get(op.ID).State != "running" {
		t.Fatal("active clone expired at the CONTROL idle deadline")
	}
	if err := os.WriteFile(filepath.Join(root, "release-git"), []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	op = f.await(op.ID, "awaiting_registration", "succeeded")
	if op.State != "awaiting_registration" || op.ProjectID != "" {
		t.Fatalf("clone registered before explicit control: %s", op.State)
	}
	f.assertAwaitingRegistration(op)
	assertRealProjectTerminalCorrelation(t, root, op.ID)
	status, _ = f.request("POST", "/projects/clone", form)
	if status != http.StatusAccepted {
		t.Fatalf("settled duplicate admission response=%d", status)
	}
	starts, _ := os.ReadFile(filepath.Join(root, "git-starts"))
	if bytes.Count(starts, []byte("\n")) != 1 {
		t.Fatal("duplicate admission executed another clone")
	}
	status, registered := f.control(op.ID, "register", op.Revision)
	if status != http.StatusOK || registered.ProjectID == "" || registered.State != "succeeded" {
		t.Fatalf("clone registration: %d %s", status, registered.State)
	}
	assertRealProjectRegistryCount(t, root, 1)
	assertRealProjectNoRuntime(t, root)
}

func TestWebProjectOperationsRealCancelRetainsPartialAndCleans(t *testing.T) {
	root := newRealProjectRoot(t, false)
	f := startRealProjectHTTP(t, root)
	admitted, _ := f.admit("clone", "partial")
	waitRealProjectFile(t, filepath.Join(root, "git-tick"))
	op := f.get(admitted.ID)
	status, _ := f.control(op.ID, "cancel", op.Revision)
	if status != http.StatusOK {
		t.Fatalf("cancel status=%d", status)
	}
	op = f.await(op.ID, "canceled")
	if op.ProjectID != "" {
		t.Fatal("canceled operation registered")
	}
	assertRealProjectGitStopped(t, root)
	assertRealProjectRegistryCount(t, root, 0)
	assertRealProjectTerminalCorrelation(t, root, op.ID)
	if _, err := os.Stat(filepath.Join(root, "parent", "partial", "FICTIONAL_PARTIAL")); err != nil {
		t.Fatal("cancellation deleted partial destination")
	}
	assertRealProjectNoRuntime(t, root)
}

func TestWebProjectOperationsRealReplacementRefusesRegistration(t *testing.T) {
	root := newRealProjectRoot(t, false)
	f := startRealProjectHTTP(t, root)
	admitted, _ := f.admit("clone", "replace")
	waitRealProjectFile(t, filepath.Join(root, "git-tick"))
	op := f.get(admitted.ID)
	// Replace after durable ownership and before completion/registration. The
	// helper still owns the old directory descriptor; registration cannot adopt B.
	if err := os.Rename(op.Child.Path, op.Child.Path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(op.Child.Path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "release-git"), []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	op = f.await(op.ID, "awaiting_registration", "needs_review", "failed")
	status, _ := f.control(op.ID, "register", op.Revision)
	if status != http.StatusConflict {
		t.Fatalf("replaced path registered: %d", status)
	}
	if f.get(op.ID).ProjectID != "" {
		t.Fatal("replacement gained registry identity")
	}
	assertRealProjectRegistryCount(t, root, 0)
	assertRealProjectNoRuntime(t, root)
}

func TestWebProjectOperationsRealWorkerDeathRestartNoReplay(t *testing.T) {
	for _, managerDeath := range []bool{false, true} {
		t.Run(fmt.Sprintf("manager_death_%t", managerDeath), func(t *testing.T) {
			root := newRealProjectRoot(t, false)
			f := startRealProjectHTTP(t, root)
			admitted, _ := f.admit("clone", "interrupted")
			waitRealProjectFile(t, filepath.Join(root, "git-tick"))
			if managerDeath {
				f.stop(true)
			} else {
				pidBytes := waitRealProjectFile(t, filepath.Join(root, "worker-pids"))
				pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
				if err != nil {
					t.Fatal(err)
				}
				process, err := os.FindProcess(pid)
				if err != nil {
					t.Fatal(err)
				}
				if err := process.Signal(syscall.SIGKILL); err != nil {
					t.Fatal(err)
				}
				f.await(admitted.ID, "needs_review")
				f.stop(false)
			}
			assertRealProjectGitStopped(t, root)
			startsBefore := waitRealProjectFile(t, filepath.Join(root, "git-starts"))
			workersBefore := waitRealProjectFile(t, filepath.Join(root, "worker-pids"))
			restarted := startRealProjectHTTP(t, root)
			op := restarted.get(admitted.ID)
			if !slices.Contains([]string{"interrupted", "needs_review"}, op.State) || op.Outcome != "unknown" || op.ProjectID != "" {
				t.Fatalf("restart adopted uncertain operation: %+v", op)
			}
			// Read/reconcile cannot replay or register the partial clone.
			status, _ := restarted.control(op.ID, "reconcile", op.Revision)
			if status != http.StatusOK {
				t.Fatalf("reconcile status=%d", status)
			}
			time.Sleep(100 * time.Millisecond)
			startsAfter, _ := os.ReadFile(filepath.Join(root, "git-starts"))
			workersAfter, _ := os.ReadFile(filepath.Join(root, "worker-pids"))
			if !bytes.Equal(startsBefore, startsAfter) || !bytes.Equal(workersBefore, workersAfter) {
				t.Fatal("restart/reconcile replayed a worker or clone")
			}
			if _, err := os.Stat(filepath.Join(root, "parent", "interrupted", "FICTIONAL_PARTIAL")); err != nil {
				t.Fatal("worker loss removed partial work")
			}
			assertRealProjectRegistryCount(t, root, 0)
			assertRealProjectNoRuntime(t, root)
		})
	}
}
