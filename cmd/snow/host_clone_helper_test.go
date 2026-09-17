//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/hostops"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// These private fictional fixtures exist only in the test binary. The production
// helper is called unchanged, before CLI/app setup, with real fd3/fd4 plumbing.
func init() {
	if len(os.Args) == 2 && os.Args[1] == "--fictional-host-descriptor-probe" {
		os.Exit(fictionalHostDescriptorProbe())
	}
	if handled, code := runHostCloneHelperEarly(os.Args[1:]); handled {
		os.Exit(code)
	}
	if len(os.Args) >= 4 && os.Args[1] == "--fictional-host-git" {
		os.Exit(fictionalHostGit(os.Args[2], os.Args[3], os.Args[4:]))
	}
	if len(os.Args) == 4 && os.Args[1] == "--fictional-host-worker" {
		os.Exit(fictionalHostWorker(os.Args[2], os.Args[3]))
	}
}

type hostFixtureRecord struct {
	Environment []string                       `json:"environment"`
	Arguments   []string                       `json:"arguments"`
	Directory   protocol.HostDirectoryIdentity `json:"directory"`
}

func fixtureHostIdentity(path string) (protocol.HostDirectoryIdentity, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return protocol.HostDirectoryIdentity{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return protocol.HostDirectoryIdentity{}, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	return protocol.HostDirectoryIdentity{Path: canonical, Device: strconv.FormatUint(uint64(stat.Dev), 10), Inode: strconv.FormatUint(uint64(stat.Ino), 10)}, nil
}

func fictionalHostGit(mode, fixture string, args []string) int {
	// An explicit private fixture marker prevents accidental use as a real Git.
	if data, err := os.ReadFile(filepath.Join(fixture, "fictional-only")); err != nil || string(data) != "FICTIONAL_HOST_CLONE" {
		return 90
	}
	if mode == "grandchild" {
		for {
			_ = os.WriteFile(filepath.Join(fixture, "grandchild-tick"), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0600)
			time.Sleep(10 * time.Millisecond)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 91
	}
	identity, err := fixtureHostIdentity(cwd)
	if err != nil {
		return 92
	}
	data, _ := json.Marshal(hostFixtureRecord{Environment: os.Environ(), Arguments: args, Directory: identity})
	if os.WriteFile(filepath.Join(fixture, "record"), data, 0600) != nil {
		return 93
	}
	if os.WriteFile("FICTIONAL_PARTIAL", []byte("not a real repository"), 0600) != nil {
		return 94
	}
	switch mode {
	case "success":
		return 0
	case "failure":
		_, _ = fmt.Fprintln(os.Stderr, "FICTIONAL_SECRET_DO_NOT_PUBLISH")
		return 1
	case "flood":
		block := bytes.Repeat([]byte("FICTIONAL_SECRET_DO_NOT_PUBLISH"), 4096)
		for {
			_, _ = os.Stdout.Write(block)
			_, _ = os.Stderr.Write(block)
		}
	case "hang", "term-resistant":
		// Grandchild remains in Git's process group. Git's signal handler reaps
		// it, avoiding orphan zombies on container PID1 implementations.
		binary, _ := os.Executable()
		child := exec.Command(binary, "--fictional-host-git", "grandchild", fixture)
		child.Stdout, child.Stderr = io.Discard, io.Discard
		if child.Start() != nil {
			return 95
		}
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		defer signal.Stop(signals)
		for {
			select {
			case <-signals:
				_ = child.Process.Kill()
				_ = child.Wait()
				if mode == "term-resistant" {
					signal.Ignore(syscall.SIGTERM)
					for {
						time.Sleep(time.Second)
					}
				}
				return 0
			default:
				_ = os.WriteFile(filepath.Join(fixture, "child-tick"), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0600)
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
	return 96
}

func fictionalHostWorker(fixture, git string) int {
	parent, err := fixtureHostIdentity(fixture)
	if err != nil {
		return 90
	}
	service, err := hostops.New(hostops.Options{GitExecutable: git})
	if err != nil {
		return 91
	}
	prepared, err := service.Prepare(context.Background(), protocol.RPCProjectPrepareParams{OperationID: "fictional-worker", Parent: parent, Leaf: "worker-project"})
	if err != nil {
		return 92
	}
	defer prepared.Close()
	clone, err := prepared.StartClone(context.Background(), protocol.RPCProjectCloneStartParams{OperationID: "fictional-worker", Child: prepared.Result().Child, URL: "https://example.invalid/fictional"})
	if err != nil {
		return 93
	}
	clone.Release()
	<-clone.Done()
	return 0
}

func newHostCloneFixture(t *testing.T, mode string) (fixture, git string) {
	t.Helper()
	fixture, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "fictional-only"), []byte("FICTIONAL_HOST_CLONE"), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	git = filepath.Join(fixture, "fictional-git")
	script := "#!/bin/sh\nexec " + quote(binary) + " --fictional-host-git " + quote(mode) + " " + quote(fixture) + " \"$@\"\n"
	if err := os.WriteFile(git, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return fixture, git
}

func prepareHostFixture(t *testing.T, git string) *hostops.Prepared {
	t.Helper()
	parent, err := fixtureHostIdentity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := hostops.New(hostops.Options{GitExecutable: git})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(t.Context(), protocol.RPCProjectPrepareParams{OperationID: "fictional-operation", Parent: parent, Leaf: "project"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prepared.Close() })
	return prepared
}

func startHostFixture(t *testing.T, ctx context.Context, prepared *hostops.Prepared) *hostops.Clone {
	t.Helper()
	result := prepared.Result()
	clone, err := prepared.StartClone(ctx, protocol.RPCProjectCloneStartParams{OperationID: result.OperationID, Child: result.Child, URL: "https://example.invalid/fictional"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		clone.Cancel()
		select {
		case <-clone.Done():
		case <-time.After(12 * time.Second):
			t.Error("fixture cleanup timed out")
		}
	})
	return clone
}

func awaitHostCompletion(t *testing.T, clone *hostops.Clone) protocol.RPCProjectCompletion {
	t.Helper()
	select {
	case <-clone.Done():
	case <-time.After(12 * time.Second):
		t.Fatal("clone did not finish bounded cleanup")
	}
	result, ready := clone.Completion()
	if !ready {
		t.Fatal("completion missing after Done")
	}
	data, _ := json.Marshal(result)
	if bytes.Contains(data, []byte("FICTIONAL_SECRET")) || bytes.Contains(data, []byte("pid")) {
		t.Fatal("private output leaked", string(data))
	}
	return result
}

func awaitHostFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fixture never created %s", path)
}

type hostFixtureDeadlineContext struct {
	context.Context
	done chan struct{}
	err  error
	once sync.Once
}

func newHostFixtureDeadlineContext(t *testing.T) *hostFixtureDeadlineContext {
	t.Helper()
	ctx := &hostFixtureDeadlineContext{Context: t.Context(), done: make(chan struct{})}
	stop := context.AfterFunc(t.Context(), func() { ctx.finish(t.Context().Err()) })
	t.Cleanup(func() { stop() })
	return ctx
}

func (c *hostFixtureDeadlineContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *hostFixtureDeadlineContext) Done() <-chan struct{}       { return c.done }
func (c *hostFixtureDeadlineContext) Err() error {
	select {
	case <-c.done:
		return c.err
	default:
		return nil
	}
}

func (c *hostFixtureDeadlineContext) expire() { c.finish(context.DeadlineExceeded) }

func (c *hostFixtureDeadlineContext) finish(err error) {
	c.once.Do(func() {
		c.err = err
		close(c.done)
	})
}

func TestHostCloneHelperGateAndPrivateEnvironment(t *testing.T) {
	fixture, git := newHostCloneFixture(t, "success")
	poison := t.TempDir()
	for _, path := range []string{".gitconfig", ".netrc", ".curlrc"} {
		if err := os.WriteFile(filepath.Join(poison, path), []byte("FICTIONAL_POISON"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_ASKPASS", "SSH_ASKPASS", "GIT_EXEC_PATH", "GIT_TEMPLATE_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_SSH_COMMAND", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy", "NO_PROXY", "CURL_HOME", "GIT_SSL_NO_VERIFY", "GIT_TRACE", "GIT_TRACE2", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES"} {
		t.Setenv(name, poison)
	}
	prepared := prepareHostFixture(t, git)
	clone := startHostFixture(t, t.Context(), prepared)
	time.Sleep(40 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(fixture, "record")); !os.IsNotExist(err) {
		t.Fatal("Git ran before ACK/release", err)
	}
	clone.Release()
	result := awaitHostCompletion(t, clone)
	if result.Status != protocol.HostOperationSucceeded {
		t.Fatal(result)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "record"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(poison)) {
		t.Fatal("inherited poisoned environment")
	}
	var record hostFixtureRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Directory != prepared.Result().Child {
		t.Fatal(record.Directory, prepared.Result().Child)
	}
	for _, want := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_ALLOW_PROTOCOL=https", "GIT_TERMINAL_PROMPT=0", "PATH=/usr/bin:/bin"} {
		if !slices.Contains(record.Environment, want) {
			t.Error("missing controlled environment", want)
		}
	}
	for _, want := range []string{"credential.helper=", "http.followRedirects=false", "http.lowSpeedTime=15", "protocol.allow=never", "protocol.https.allow=always", "transfer.bundleURI=false", "--no-recurse-submodules", "--template="} {
		if !slices.Contains(record.Arguments, want) {
			t.Error("missing fixed Git option", want)
		}
	}
	last := record.Arguments[len(record.Arguments)-3:]
	if !slices.Equal(last, []string{"--", "https://example.invalid/fictional", "."}) {
		t.Fatal(last)
	}
}

func TestHostCloneHelperFailureAndFloodPreservePartialDestination(t *testing.T) {
	for _, mode := range []string{"failure", "flood"} {
		t.Run(mode, func(t *testing.T) {
			_, git := newHostCloneFixture(t, mode)
			prepared := prepareHostFixture(t, git)
			clone := startHostFixture(t, t.Context(), prepared)
			clone.Release()
			result := awaitHostCompletion(t, clone)
			want := protocol.HostOperationFailed
			if mode == "flood" {
				want = protocol.HostOperationOutputLimit
			}
			if result.Status != want {
				t.Fatal(result)
			}
			if _, err := os.Stat(filepath.Join(prepared.Result().Child.Path, "FICTIONAL_PARTIAL")); err != nil {
				t.Fatal("partial destination removed", err)
			}
		})
	}
}

func TestHostCloneHelperCancelTimeoutAndKillStopDescendants(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "term-resistant"} {
		t.Run(mode, func(t *testing.T) {
			gitMode := "hang"
			if mode == "term-resistant" {
				gitMode = mode
			}
			fixture, git := newHostCloneFixture(t, gitMode)
			prepared := prepareHostFixture(t, git)
			ctx := t.Context()
			var deadline *hostFixtureDeadlineContext
			if mode == "timeout" {
				deadline = newHostFixtureDeadlineContext(t)
				ctx = deadline
			}
			clone := startHostFixture(t, ctx, prepared)
			clone.Release()
			awaitHostFile(t, filepath.Join(fixture, "child-tick"))
			awaitHostFile(t, filepath.Join(fixture, "grandchild-tick"))
			if deadline != nil {
				deadline.expire()
			} else {
				clone.Cancel()
			}
			result := awaitHostCompletion(t, clone)
			want := protocol.HostOperationCanceled
			if mode == "timeout" {
				want = protocol.HostOperationTimedOut
			}
			if result.Status != want {
				t.Fatal(result)
			}
			assertHostMarkersStopped(t, fixture)
			if _, err := os.Stat(prepared.Result().Child.Path); err != nil {
				t.Fatal("destination removed", err)
			}
		})
	}
}

func assertHostMarkersStopped(t *testing.T, fixture string) {
	t.Helper()
	for _, name := range []string{"child-tick", "grandchild-tick"} {
		path := filepath.Join(fixture, name)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(120 * time.Millisecond)
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("descendant survived terminal completion", name, err)
		}
	}
}

func TestHostCloneHelperWorkerDeathClosesLiveness(t *testing.T) {
	fixture, git := newHostCloneFixture(t, "hang")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	worker := exec.Command(binary, "--fictional-host-worker", fixture, git)
	worker.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + fixture}
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Process.Kill() })
	awaitHostFile(t, filepath.Join(fixture, "child-tick"))
	awaitHostFile(t, filepath.Join(fixture, "grandchild-tick"))
	if err := worker.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = worker.Wait()
	// No explicit clone cancellation exists in this test: only worker death
	// closes fd4's sole writer. Give bounded TERM/KILL/reap time, then sample.
	time.Sleep(6 * time.Second)
	assertHostMarkersStopped(t, fixture)
	if _, err := os.Stat(filepath.Join(fixture, "worker-project", "FICTIONAL_PARTIAL")); err != nil {
		t.Fatal("partial child removed", err)
	}
}
