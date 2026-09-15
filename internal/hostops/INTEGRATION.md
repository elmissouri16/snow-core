# Runtime-free host operation integration

This package does not wire CLI, RPC control, manager persistence/reconciliation,
or UI. Focused verification is recorded below; full integration remains separate.

## Hooks available now

- `hostops.New(hostops.Options{GitExecutable: "/trusted/absolute/git"})`.
  The default helper is `os.Executable()`, the same Snow binary. The optional
  `HelperExecutable` is trusted packaging/test composition, never request input.
  The constructor fails closed outside Darwin/Linux. No PATH search occurs.
  `New(Options{})` supports CREATE without Git. A configured missing,
  non-executable, non-regular or relative Git selection also cannot block CREATE.
  Git is validated at StartClone admission; clone fails closed unless the
  operator-selected fixed absolute executable is available then. No PATH fallback.
- `service.Prepare(ctx, protocol.RPCProjectPrepareParams{OperationID, Parent,
  Leaf}) -> (*hostops.Prepared, error)`; `prepared.Result()` returns
  `protocol.RPCProjectPrepared{OperationID, Parent, Child, Leaf}`.
- `Parent` and `Child` use `protocol.HostDirectoryIdentity{Path, Device, Inode}`.
  Device/inode are canonical **decimal strings**, avoiding browser precision loss.
  The parent path must be absolute/canonical, and match the opened directory.
- **Durably save `Result()` before ACKing creation or requesting clone.** Creation
  is only `os.Root.Mkdir(leaf, 0700)`; no command/network execution. All existing
  destinations are rejected. The manager owns recovery when a worker dies after
  mkdir but before its identity result is durably recorded. Never infer ownership
  from a directory name or silently adopt it after interruption.
- `prepared.StartClone(ctx, protocol.RPCProjectCloneStartParams{OperationID,
  Child, URL}) -> (*hostops.Clone, error)` consumes preparation once. Matching the
  exact child and operation ID is the durable identity acknowledgement. The
  request URL is the reviewed anonymous HTTPS URL; no command/flags API exists.
- **Write the start RPC ACK successfully before `clone.Release()`.** Release is
  idempotent. A disconnected/failed ACK must call `Cancel`, not Release.
  `Cancel()` works both before and after Release; `<-Done()` waits bounded cleanup;
  `Completion() (protocol.RPCProjectCompletion, bool)` exposes fixed public status
  only after completion. `cleanup_failed` is not a successfully reaped cancel.
- `prepared.Close()` only releases descriptors. It never deletes or cancels an
  already admitted clone, which owns an independent pinned child descriptor.
  Cancel all admitted clones on worker shutdown, then await their Done channels.
- Manager-side terminal persistence, identity reconciliation, registration and
  activation are separate. Success does not prove the pathname remains bound to
  the original inode; revalidate before registration. Never activate implicitly.

In `cmd/snow/cli.go`, before Cobra/app/config initialization and before ordinary
error rendering:

```go
if handled, code := runHostCloneHelperEarly(os.Args[1:]); handled {
    os.Exit(code)
}
```

The new `cmd/snow/host_clone_helper.go` provides that hook. The private entry is
`--host-clone-helper-only` with exactly no other arguments. fd3 must be the child
folder, fd4 a read-only liveness pipe. Strict <=16 KiB stdin JSON contains only
private fixed execution composition, deadline and operation binding; this is not
an OS-user security boundary. No browser secret or credential is a CLI argument.

## Containment and limits

The worker owns the **only** pipe writer. The helper marks private fds close-on-
exec, fchdirs to fd3, then supervises a new Git process group via `procgroup`.
It never exec-replaces itself. Worker death produces fd4 EOF, canceling/reaping
Git and descendants without requiring the dying worker to run cleanup code.
No child pathname is reopened as `Cmd.Dir` at Git launch.

Git gets no inherited environment beyond a fixed PATH/locale. Its private 0700
HOME/XDG/scratch replaces user config/netrc/curlrc state. Global/system Git config,
credential prompting/helpers, hooks, templates, system attributes, inherited
proxies, rewrites, filters, SSH and other transports are excluded/disabled. Git's
installed executable/helpers and OS certificate store are trusted operator
composition. A malicious same-OS-user process can still mutate executables,
private directories or processes; this is not a whole-process sandbox. In
particular, mkdir and the first stat/open are distinct syscalls; descriptor and
binding checks do not create an atomic mkdir-and-return-fd primitive against
hostile same-user mutation.

Only reviewed `https://host/path` without userinfo/query/fragment is accepted.
No SSH is available until a separately approved host profile is implemented.
No recursive submodules, redirects, reference repositories, bundle-URI requests,
arbitrary flags, URL rewrites or non-HTTPS Git transports. This does **not** impose
an IP/address egress policy or prove a remote host harmless. OS/container network
containment is required for that policy.

A hard ten-minute context bound applies from StartClone (including ACK wait).
Combined stdout/stderr is drained and counted, not returned; exceeding 64 KiB
cancels the process. Cleanup sends TERM, waits two seconds, KILL, then bounded
reap. Fixed public statuses distinguish success/failure/cancel/timeout/output
limit/cleanup failure; no stderr, raw errors or PIDs escape.

Current Git docs verify `http.lowSpeedLimit=1` and `http.lowSpeedTime=15` and
`http.followRedirects=false`. **No supported Git 15-second connect-timeout option
was established by the current documentation lookup.** No invented
`http.connectTimeout` setting is used. Slow transfers have the verified 15-second
low-speed control; connect/DNS/TLS blocking is additionally bounded by the hard
operation deadline, not claimed to have a separate 15-second connect guarantee.

These are **not disk-space or network-byte quotas**. Failed/canceled clones may
leave partial contents and are never automatically deleted. Only helper-owned
private scratch is removed; no cache/config/install cleanup occurs.

## Focused verification

```sh
gofmt -w internal/hostops/*.go pkg/protocol/rpc_host_operations.go cmd/snow/host_clone_helper*.go
go test ./internal/hostops
go test ./cmd/snow -run 'TestHostCloneHelper' -count=1
go test -race ./internal/hostops ./cmd/snow -run 'TestHostCloneHelper|TestPrepare|TestStartClone|TestCloneGate|TestLeaf|TestAnonymous|TestOutputBudget' -count=1
```

Execution-dependent fixtures live only in `cmd/snow/host_clone_helper*_test.go`.
They use an absolute fictional Git executable with the production same-binary
helper, not Git/network/provider credentials. Unit cases cover exact grants,
existing destinations, replaced parent/child bindings, strict bounded payloads,
fd-pinned launch after rename, isolated environment/options, release/cancel gate,
output flood, short caller deadline, TERM-resistant children, grandchild markers,
worker EOF and preservation of partial destinations. Full suite, vet and platform
verification remain the integrating agent's release gates.

Git documentation consulted through find-docs / Context7 `/git/htmldocs`:
`git-config.html` (protocol, credential, HTTP, bundle URI), `git-clone.html`
(submodules) and `Documentation/git.adoc` (config environment isolation).

### Executed focused checks

The initial explicit-file helper run passed but did not load the full CLI's
package initialization. After shared compilation edits settled, the actual full
`cmd/snow` package reproduced BUG-158: missing fd3/fd4 could alias Go runtime
poller descriptors, and rejection closed those unrelated descriptors. The
explicit-file pass was insufficient to verify early-helper integration.

The fix validates both raw descriptors with non-owning `fstat`/`F_GETFL` calls
before constructing `os.File` wrappers, changing flags/cwd, or registering any
close/finalizer ownership. A rejected pair remains entirely untouched.

After the fix, these **full-package** focused commands passed:

```sh
GOMAXPROCS=2 go test -p 1 ./internal/hostops -count=1
GOMAXPROCS=2 go test -p 1 ./cmd/snow -run '^TestHostCloneHelper' -count=1 -v
```

All nine helper test groups passed, including private environment poisoning,
output flood, deadline/cancel/TERM-to-KILL, worker-death EOF, parent/child descriptor
identity, populated destinations, strict bounded payloads, and the original
missing-descriptor reproduction. The new
`TestHostCloneHelperRejectsUnownedFDsAfterRuntimePollInit` covers absent/swapped/
wrong/writable descriptors after warming the actual runtime poller and timers.
It checks continued pipe polling after helper rejection and GC, unchanged fd
identity/flags, and output containing only the fixed failure status. No network
listener, remote Git, provider or credentials are used.

No race/full-suite/install check was run in this focused verification pass.

### BUG-161: CREATE is independent of Git availability

Before the fix, tests reproduced `New` rejecting a missing/non-executable Git
selection even with a valid helper, and the production lazy control adapter
therefore rejecting `project_prepare` before creating anything. A separate test
also reproduced a clone handle being admitted after Git was removed following
construction. Git executable validation now belongs to StartClone admission, not
service construction. CREATE remains available, clone rejects unusable Git before
allocating a handle, and no relative/PATH fallback is introduced.

These focused normal checks passed after the fix:

```sh
GOMAXPROCS=2 go test -p 1 ./internal/hostops -count=1
GOMAXPROCS=2 go test -p 1 ./internal/rpc -run '^TestControlHost' -count=1 -v
GOMAXPROCS=2 go test -p 1 ./cmd/snow -run '^TestHostCloneHelper' -count=1
```

`TestCreateWithUnavailableConfiguredGit` covers missing, non-executable, directory
and relative selections (including an executable relative name present in PATH).
`TestStartCloneRevalidatesConfiguredGitBeforeAdmittingExecution` removes Git after
service construction. `TestControlHostCreateWithUnavailableConfiguredGit` drives
the production lazy adapter and ServeControl: CREATE succeeds, clone returns
fixed `unavailable`, and closing the preparation preserves its empty child.
All tests remain credential/network-free; no negative clone gate is released.
