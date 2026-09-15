# Real permission-execution browser regression

From the repository root:

```sh
node scripts/tests/browser/permission-workflow/run.mjs
```

Requires the checkout's Go toolchain, Node 22+ (built-in WebSocket), and installed
Chrome/Chromium. `SNOW_CHROME_BIN` overrides the shared runner's fixed browser
paths. No npm packages, provider credentials, external services, or installed
Snow binary are required. Network traffic is loopback HTTP/CDP only.

## What is real

```text
local scripted fake provider
  -> app.New / serial agent loop / real permission broker
  -> existing builtin write / pinned project file guard / durable session
  -> external RPC worker / RuntimeManager / production HTTP and SSE
  -> Chrome with unmodified production HTML and JavaScript
```

The implementation-dependent fixture lives in
`cmd/snow/web_permission_fixture{,_helpers}_test.go`, not in `internal/web` or
`pkg/agentclient`. No second agent loop, simulated permission event, fake RPC
reply, or fabricated public snapshot is used. The only tool is the existing
builtin `write`, under `ask` with no extra allowed roots. A test-only decorator
preserves its admission descriptor and delegates its actual filesystem operation;
it records execution counts and supplies deterministic before/after gates.

## Coverage

The workflow checks:

- Both explicitly activated projects start without provider calls. Their first
  real writes have colliding worker-local permission IDs; mixed project/instance
  authority cannot authorize either one.
- Pending requests leave the tool runner unentered. Allow once produces exact
  expected file bytes exactly once; Deny leaves files absent and persists a real
  error result. A subsequent write needs a fresh decision.
- Duplicate decisions cannot authorize a new request. After close/resume, a
  reused permission ID cannot be approved with the old instance authority.
- A foreground browser's real in-flight request is stopped through CDP while
  its network is offline. (Offline emulation alone can leave localhost streams
  alive.) Production reconnect obtains the same pending approval without
  replaying a prompt or provider request. The other project remains usable.
  Tests deliberately foreground each tab before operating its controls, so
  hidden-tab pause is not mistaken for a network failure.
- Real worker death while approval is pending leaves the write unentered and
  the other project's pending approval valid. Explicit close while awaiting
  approval likewise does not restore usable authority or replay the write.
- After approval, worker death is gated immediately before the actual write,
  and after committed file bytes but before the tool returns its result.
  The first leaves no file; the second leaves exactly one write. Both retain
  unknown outcomes, not fabricated completion or cancellation.
- Close browses the exact real saved session; explicit resume has fresh instance
  authority, stable saved tool identities/public output, and no old activities
  or approvals. Newly synthesized interruption records remain unresolved rather
  than being presented as definitive execution failures.
- Durable provider-step and execution logs, actual file bytes, and observed
  browser prompt-POST counts establish no replay across these scenarios.

Reports and screenshots are written to `.snow/browser/permission-workflow/`;
`--output-dir PATH` selects a separate evidence directory. The report is marked
`running` before compilation and `failed` on exceptions, invalidating any older
success. Only after the fixture manager exits successfully, workers are joined,
and temporary-directory cleanup finishes does the runner publish `passed` and
print success. The final process exit status remains authoritative.

The expected-prerequisite-failure regression uses a separate temporary evidence
directory and verifies stale-success invalidation without a Chrome installation:

```sh
node scripts/tests/browser/permission-workflow/failure-report.mjs
```

Run it before the normal workflow when changing fixture/report lifecycle code.

## Isolation and lifetime

The runner compiles a test binary into a private mode-0700 temporary directory.
It does not forward the caller's auth/config/provider environment. HOME,
SNOW_HOME, session stores, manager registry, projects A/B, and Chrome profile are
all temporary. Neither project is activated by fixture setup.

`SNOW_WEB_PERMISSION_FIXTURE_DIR` and `TestWebPermissionFixture` are private test
IPC, not production commands or a fixture service to start manually. The real
loopback pairing endpoint creates the browser cookie; the runner consumes the
cookie-bearing READY frame without printing it. Production has no kill/gate
endpoint. Fixed private marker files affect only the test's own eager worker,
not arbitrary PIDs; catalog workers cannot consume kill markers. Counters are
bounded and synced independently of reached markers, so repeated execution
cannot disappear behind an overwritten marker. Cleanup removes only this
runner's temporary directory and stops only its own fixture/Chrome children.

Ordinary `go test ./cmd/snow` skips the interactive browser fixture but runs
network-free tests for exact worker argument/CWD guards, context cancellation,
real broker-to-write admission and both execution gate boundaries.

## Limits

This is deterministic local-fake-provider coverage, not a remote-provider smoke
check or proof of arbitrary tool/browser/network failure behavior. It does not
exercise Bash, plugins, MCP, managed processes, or detached side effects. No
exactly-once delivery guarantee, OS sandbox, automatic recovery, remembered
permission grant, or production tool/permission expansion is added. Legacy
interruption records lacking explicit unknown-outcome provenance are not
backfilled or classified by matching private message text. Layout/cross-browser,
physical mobile keyboard, and deployment-security checks remain separate gates.
