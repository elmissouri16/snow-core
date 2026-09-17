package hostops

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/elmissouri16/snow-core/internal/procgroup"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const helperPayloadLimit = 16 << 10

type helperPayload struct {
	GitExecutable string                              `json:"git_executable"`
	Start         protocol.RPCProjectCloneStartParams `json:"start"`
	Deadline      time.Time                           `json:"deadline"`
}
type helperResult struct {
	Status protocol.HostOperationStatus `json:"status"`
}

// HelperMain runs ONLY in an early, private same-Snow subprocess entrypoint,
// before application/config initialization. It changes process-wide cwd via
// fd3, supervises Git instead of exec-replacing itself, and monitors fd4 EOF.
// Its input is private bounded stdin JSON, not a public command protocol. The
// schema is not a security boundary against other processes of the same OS user.
// stdout receives one small fixed-status object; Git logs are never emitted.
// Ownership transfers only after both raw descriptors validate as a directory
// and read-only FIFO. Rejected descriptors are never wrapped, changed or closed.
func HelperMain(ctx context.Context, input io.Reader, output io.Writer, childFD, lifeFD uintptr) int {
	status := protocol.HostOperationFailed
	// Do not wrap, close, or mutate either descriptor until the complete pair
	// has been checked using raw non-owning syscalls. Missing inherited fds can
	// already belong to Go's runtime after package initialization (BUG-158).
	if validHelperDescriptors(childFD, lifeFD) {
		child := os.NewFile(childFD, "host-clone-directory")
		life := os.NewFile(lifeFD, "host-clone-liveness")
		status = runHelper(ctx, input, child, life)
	}
	payload, err := json.Marshal(helperResult{Status: status})
	if err != nil {
		return 1
	}
	if _, err := output.Write(append(payload, '\n')); err != nil {
		return 1
	}
	return 0
}

func runHelper(parent context.Context, input io.Reader, child, life *os.File) protocol.HostOperationStatus {
	defer child.Close()
	defer life.Close()
	// Validate the required private descriptor shapes before changing cwd or
	// reading untrusted stdin. The liveness watcher starts before that read.
	if err := enterHelperDirectory(child, life); err != nil {
		return protocol.HostOperationFailed
	}
	ctx, cancel := context.WithTimeout(parent, CloneTimeout)
	defer cancel()
	go func() {
		var one [1]byte
		// EOF, an error, or unexpected bytes all revoke execution. No descendant
		// receives the writer; worker exit therefore always produces EOF.
		_, _ = life.Read(one[:])
		cancel()
	}()
	// OS stdin is closed on cancellation to interrupt a partial private payload.
	if closer, ok := input.(io.Closer); ok {
		stop := context.AfterFunc(ctx, func() { _ = closer.Close() })
		defer stop()
	}
	data, err := io.ReadAll(io.LimitReader(input, helperPayloadLimit+1))
	if err != nil || len(data) > helperPayloadLimit || ctx.Err() != nil {
		return protocol.HostOperationFailed
	}
	var payload helperPayload
	if json.Unmarshal(data, &payload, json.RejectUnknownMembers(true)) != nil || !validOperationID(payload.Start.OperationID) || !validIdentity(payload.Start.Child) || !ValidCloneURL(payload.Start.URL) || !trustedExecutable(payload.GitExecutable) || payload.Deadline.IsZero() {
		return protocol.HostOperationFailed
	}
	identity, err := fileIdentity(child, payload.Start.Child.Path)
	if err != nil || identity != payload.Start.Child {
		return protocol.HostOperationFailed
	}
	ctx, deadlineCancel := context.WithDeadline(ctx, payload.Deadline)
	defer deadlineCancel()
	if ctx.Err() != nil {
		return contextStatus(ctx)
	}
	// Refuse any externally populated destination. No adoption, cleaning or
	// deletion. Descriptor-relative cwd, not the user pathname, is authoritative.
	entries, err := child.ReadDir(1)
	if (err != nil && !errors.Is(err, io.EOF)) || len(entries) != 0 {
		return protocol.HostOperationFailed
	}
	private, err := os.MkdirTemp("/tmp", "snow-host-clone-")
	if err != nil {
		return protocol.HostOperationFailed
	}
	// Only helper-owned scratch is removed. The destination is never removed.
	defer os.RemoveAll(private)
	return executeGit(ctx, payload, private)
}

func baseEnvironment() []string {
	return []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
}

func gitEnvironment(private string) []string {
	return append(baseEnvironment(),
		"HOME="+private, "XDG_CONFIG_HOME="+private, "XDG_CACHE_HOME="+private,
		"TMPDIR="+private,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/usr/bin/false",
		"SSH_ASKPASS=/usr/bin/false", "GIT_ALLOW_PROTOCOL=https", "GIT_PROTOCOL_FROM_USER=0",
		"GIT_CEILING_DIRECTORIES=/", "GIT_DISCOVERY_ACROSS_FILESYSTEM=0",
	)
}

func gitArguments(url, private string) []string {
	return []string{
		"-c", "credential.helper=", "-c", "credential.interactive=false",
		"-c", "core.askPass=/usr/bin/false", "-c", "core.hooksPath=" + filepath.Join(private, "hooks-disabled"),
		"-c", "core.attributesFile=/dev/null", "-c", "core.fsmonitor=false",
		"-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"-c", "http.proxy=", "-c", "http.followRedirects=false", "-c", "http.sslVerify=true",
		"-c", "http.lowSpeedLimit=1", "-c", "http.lowSpeedTime=15",
		"-c", "http.extraHeader=", "-c", "http.cookieFile=", "-c", "http.saveCookies=false",
		"-c", "http.emptyAuth=false",
		"-c", "submodule.recurse=false", "-c", "fetch.recurseSubmodules=false",
		"-c", "transfer.bundleURI=false", "-c", "fetch.bundleURI=",
		"-c", "gc.auto=0", "-c", "maintenance.auto=false",
		"clone", "--no-local", "--no-hardlinks", "--no-recurse-submodules",
		"--template=", "--", url, ".",
	}
}

func executeGit(parent context.Context, payload helperPayload, private string) protocol.HostOperationStatus {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	cmd := exec.Command(payload.GitExecutable, gitArguments(payload.Start.URL, private)...)
	// Deliberately NO Cmd.Dir: helper already fchdir'ed its pinned fd. Naming
	// the original directory again would reintroduce the replacement race.
	cmd.Env = gitEnvironment(private)
	cmd.Stdin = nil
	output := &limitedCapture{limit: OutputBudget, discard: true, cancel: cancel}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.WaitDelay = time.Second
	if procgroup.Configure(cmd) != nil {
		return protocol.HostOperationFailed
	}
	if ctx.Err() != nil {
		return contextStatus(ctx)
	}
	if cmd.Start() != nil {
		return protocol.HostOperationFailed
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	var waitErr error
	reaped := false
	select {
	case waitErr = <-wait:
		reaped = true
	case <-ctx.Done():
	}
	// Reap the direct child before testing whether its process group disappeared.
	// A zombie group leader remains observable to kill(-pgid, 0), so polling the
	// group before Wait completes can consume the cleanup bound without proving
	// whether Git exited. Descendants remain in Git's original process group and
	// are terminated below even after the leader has been reaped.
	var cleanupErr error
	if !reaped {
		cleanupErr = procgroup.Terminate(cmd.Process)
		select {
		case waitErr = <-wait:
			reaped = true
		case <-time.After(2 * time.Second):
		}
	}
	if !reaped {
		cleanupErr = errors.Join(cleanupErr, procgroup.Kill(cmd.Process))
		select {
		case waitErr = <-wait:
			reaped = true
		case <-time.After(2 * time.Second):
			return protocol.HostOperationCleanupFailed
		}
	}
	// The outer helper allows eight seconds for worker-loss cleanup. Keep the
	// residual TERM/KILL group proof within three seconds after the at-most-four
	// second leader reap above.
	cleanupErr = errors.Join(cleanupErr, procgroup.Shutdown(cmd.Process, 1500*time.Millisecond))
	if cleanupErr != nil {
		return protocol.HostOperationCleanupFailed
	}
	if output.exceeded() {
		return protocol.HostOperationOutputLimit
	}
	if parent.Err() != nil {
		return contextStatus(parent)
	}
	if waitErr != nil {
		return protocol.HostOperationFailed
	}
	return protocol.HostOperationSucceeded
}
