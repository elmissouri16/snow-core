# Lazy MCP Connection Implementation Plan

This document proposes lazy connection and idle shutdown for Snow's Model
Context Protocol (MCP) servers. It records the intended configuration, cache,
runtime state machine, concurrency rules, security boundaries, implementation
phases, and verification plan for a future change. Current MCP behavior remains
documented in [Model Context Protocol](mcp.md).

> **Note:** This is an implementation plan, not current behavior. Snow currently
> connects every enabled MCP server during application startup. The proposed
> `lifecycle` and `idle_timeout_ms` fields do not exist yet.

## On this page

- [Problem and objective](#problem-and-objective)
- [Research basis](#research-basis)
- [Current Snow behavior](#current-snow-behavior)
- [Proposed behavior](#proposed-behavior)
- [Configuration contract](#configuration-contract)
- [Metadata cache](#metadata-cache)
- [Runtime state machine](#runtime-state-machine)
- [Connection and call flow](#connection-and-call-flow)
- [Idle shutdown](#idle-shutdown)
- [Catalog refresh](#catalog-refresh)
- [Permissions and security](#permissions-and-security)
- [Status and observability](#status-and-observability)
- [Code changes](#code-changes)
- [Implementation phases](#implementation-phases)
- [Verification plan](#verification-plan)
- [Acceptance criteria](#acceptance-criteria)
- [Risks and mitigations](#risks-and-mitigations)
- [Deferred work](#deferred-work)
- [Related documents](#related-documents)

## Problem and objective

Snow currently starts or contacts every enabled MCP server before the agent is
constructed. This makes all configured servers part of startup cost even when a
session never uses their tools.

For stdio servers, eager connection can create unnecessary Node.js, Python, or
browser processes. For HTTP servers, it creates unnecessary initialization
traffic and sessions. A resource such as the default Chrome DevTools MCP
profile can also be locked by one Snow process before any browser tool is used.

The objective is to separate three concerns:

- configuration: a server is known and enabled;
- discovery: Snow knows enough bounded metadata to route its tools;
- connection: the server process or network session is live.

A lazy server should remain disconnected while its cached descriptors stay in
Snow's normal registry and BM25 router. Calling one of those descriptors should
run the existing permission gate, connect the server exactly once, validate the
live catalog, and then make the MCP request.

The change must preserve these invariants:

- one shared serial Snow agent loop;
- the existing normalized `protocol.AgentEvent` stream;
- authoritative permission checks before tool execution;
- bounded startup, connection, refresh, call, and shutdown work;
- exact MCP tool names and schemas after a successful live refresh;
- atomic registry and routing-index replacement;
- complete cancellation and application shutdown;
- no credentials in cache files, status, events, logs, or errors.

## Research basis

The design is informed by `pi-mcp-adapter` 2.26.0 at upstream commit
`1bf36719cec478a163bb52e3390182963aab9f85`.

That adapter implements:

- lazy connection by default;
- a persistent metadata cache for tools, resources, prompts, and instructions;
- local search and description without a live MCP connection;
- connection immediately before an actual MCP call;
- `lazy`, `eager`, `keep-alive`, and `lazy-keep-alive` modes;
- a ten-minute default idle timeout;
- periodic health checks and reconnect for keep-alive modes;
- optional direct tools reconstructed from cached metadata;
- a single proxy tool for search, description, and calls.

Its first run has an important exception: if its metadata cache file does not
exist, it connects every enabled server once to populate the cache. Later
sessions can remain fully lazy.

Snow should reuse the lifecycle and cache ideas but not copy the proxy-tool
interface. Snow already has ordinary MCP descriptors, local deferred BM25
routing, and the `search_tools` recovery path. Keeping those surfaces avoids a
second MCP-specific model interaction contract.

## Current Snow behavior

`internal/app.New` currently creates an MCP manager and calls:

```go
mcpManager.ConnectAll(ctx, mcpSpecs)
```

`internal/mcp.Manager.ConnectAll` processes each enabled server and:

1. validates the `pkg/mcp.ServerSpec`;
2. starts the stdio command or contacts the HTTP endpoint;
3. negotiates an MCP session through the official Go SDK;
4. calls `tools/list` and inspects resource and prompt capabilities;
5. creates descriptors and replaces the server owner's registry entries;
6. starts a `tools/list_changed` refresh worker;
7. retains the live session until application shutdown.

The configured `tool_discovery` value controls provider schema exposure only.
The default `deferred` mode still connects the server and registers all full
schemas before the routing index is built.

The current remote tool assumes a live session:

```go
result, err := t.runtime.session.CallTool(ctx, params)
```

The current runtime close operation is final. It marks the runtime closed,
stops refresh processing, closes the SDK session, and unregisters all owner
descriptors. Lazy idle shutdown therefore requires a separate non-final
disconnect operation.

## Proposed behavior

### Startup with a valid cache

For a server configured with `lifecycle: "lazy"`:

1. Validate the declaration without starting a process or making a request.
2. Load a valid bounded cache entry.
3. Create a disconnected runtime.
4. Reconstruct ordinary MCP descriptors from cached metadata.
5. Register the descriptors atomically under `mcp:<server-id>`.
6. Include them in the existing BM25 routing indexes.
7. Report the server as cached and disconnected.

No child process, network request, MCP initialization, or `tools/list` request
occurs during this path.

### First call

When a cached descriptor is executed:

1. The existing registry and permission service approve the descriptor.
2. The descriptor acquires a runtime call lease.
3. The runtime performs a single synchronized connection if disconnected.
4. The runtime refreshes the complete live catalog.
5. The runtime verifies that the requested original tool still exists.
6. The call uses the caller's context and the connected SDK session.
7. The runtime releases the lease and records last activity.

Other callers waiting for the same runtime share the connection attempt rather
than starting another server process.

### Subsequent calls

Calls made while the server remains connected skip initialization and use the
existing MCP session. The server disconnects only after its idle timeout and
only when no calls, refreshes, or connection attempt are active.

### Startup without a valid cache

The first implementation should match the practical Pi behavior:

- connect an uncached lazy server once;
- obtain and validate its catalog;
- persist bounded metadata;
- disconnect it immediately after startup discovery;
- leave its reconstructed descriptors registered.

This is a one-time bootstrap, not fully lazy first use. It must be clearly shown
in status and documentation.

A later phase should add explicit cache priming and optional activation tools so
users can choose a strict mode that never starts uncached servers during normal
startup.

## Configuration contract

Extend `pkg/mcp.ServerSpec` additively:

```go
type ServerSpec struct {
    // Existing fields omitted.

    // Lifecycle is "eager" (default), "lazy", or
    // "lazy-keep-alive".
    Lifecycle string `json:"lifecycle,omitempty"`

    // IdleTimeoutMS overrides the default idle timeout for lazy servers.
    // Zero uses the lifecycle default. A negative value is invalid.
    IdleTimeoutMS int `json:"idle_timeout_ms,omitempty"`
}
```

Initial semantics:

| Lifecycle | Startup | Idle shutdown | Reconnect policy |
|---|---|---|---|
| empty or `eager` | Immediate | Off by default | Next call |
| `lazy` | Cache or bootstrap | Enabled | Next call |
| `lazy-keep-alive` | Cache or bootstrap | Off after use | Deferred |

Keep eager as the initial default for backward compatibility. Existing
configurations must retain current startup and failure behavior.

The initial lazy idle default should be ten minutes:

```text
600000 ms
```

A positive `idle_timeout_ms` overrides the default. For the first release, zero
means use the lifecycle default rather than disable the timeout. If disabling
idle shutdown becomes necessary, add an explicit semantic instead of
conflating omitted and zero values.

Keep lifecycle orthogonal to schema discovery:

```json
{
  "mcp_servers": {
    "chrome-devtools": {
      "transport": "stdio",
      "command": "npx",
      "args": [
        "-y",
        "chrome-devtools-mcp@latest",
        "--isolated"
      ],
      "tool_discovery": "deferred",
      "lifecycle": "lazy",
      "idle_timeout_ms": 600000
    }
  }
}
```

`tool_discovery` continues to control when schemas enter provider context.
`lifecycle` controls when the MCP process or connection becomes live.

Configuration management must support:

```sh
snow mcp add chrome-devtools \
  --lifecycle lazy \
  --idle-timeout 10m \
  -- npx -y chrome-devtools-mcp@latest --isolated
```

Inspection output must show lifecycle and idle timeout without exposing command
arguments classified as sensitive.

## Metadata cache

### Location and ownership

Store cache data under Snow's private application directory, for example:

```text
~/.snow/cache/mcp-v1.json
```

The final path should use the same resolved Snow home/config ownership rules as
other private state. Project-declared servers must not be loaded from cache
before project trust succeeds.

A project-dependent server catalog may vary by canonical working directory or
root. Partition cache entries by:

- server ID;
- trusted configuration scope;
- canonical project-root identity when applicable;
- safe configuration fingerprint;
- cache format version.

Do not let one project's cached server metadata become another project's tool
catalog merely because both declarations use the same server ID.

### File security

The cache writer must:

- create parent directories deliberately;
- write a same-directory temporary file;
- use mode `0600`;
- flush and close before replacement where supported;
- atomically rename over the destination;
- reject symlink and unexpected-file-type targets;
- preserve bounds during read and write;
- never include environment values, headers, tokens, or credentials.

Treat cache content as untrusted on read. A local process running as the user
can alter it, and cached MCP text is external context rather than trusted
instructions.

### Proposed schema

A cache entry should contain only data needed to reconstruct descriptors and
status:

```json
{
  "version": 1,
  "written_at": "2026-08-01T12:00:00Z",
  "servers": {
    "entry-key": {
      "server_id": "chrome-devtools",
      "scope": "global",
      "project_identity": "",
      "configuration_fingerprint": "safe-v1:...",
      "cached_at": "2026-08-01T12:00:00Z",
      "protocol_version": "2026-07-28",
      "server_name": "chrome-devtools-mcp",
      "server_version": "1.0.0",
      "capabilities": ["tools"],
      "tools": [
        {
          "name": "take_screenshot",
          "title": "Take screenshot",
          "description": "Take a screenshot of the page.",
          "input_schema": {
            "type": "object",
            "properties": {}
          }
        }
      ]
    }
  }
}
```

Do not persist server-provided instructions in the first implementation. Snow
currently adds live bounded instructions to system context, but persisting them
creates a durable prompt-injection surface and stale-context question that is
not required for lazy tool routing.

Resources and prompts may be cached after tool-only lifecycle behavior is
stable. Capability bridge descriptors can initially require a live bootstrap or
connection.

### Safe fingerprint

The fingerprint exists only to invalidate stale metadata. It is not an
authentication mechanism.

Include non-secret structural identity such as:

- cache format version;
- transport;
- server ID;
- command path or URL without user information;
- working-directory identity;
- argument shape after credential-like values are removed;
- names of environment and header keys, not their values;
- roots and project identity;
- fields that alter filtering or exposed capabilities.

Do not hash raw credentials and persist the digest. A digest of a low-entropy
secret can become an offline verifier. If complete secret-sensitive
invalidation becomes necessary, use a separately protected keyed construction
rather than a plain hash.

A changed credential may therefore leave metadata cached until the next live
call. The first live connection always refreshes and revalidates the catalog
before execution, preserving correctness at the call boundary.

### Bounds

Define constants and test them. Initial conservative limits should cover:

- maximum cache file bytes;
- maximum servers;
- maximum tools per server;
- maximum tool name and description bytes;
- maximum schema bytes per tool;
- maximum aggregate schema bytes per server;
- maximum capability strings;
- maximum cache age.

Reject or skip an invalid entry without making the whole Snow application
unusable. Record a bounded secret-free status message and use bootstrap or eager
connection according to lifecycle policy.

### Cache age

Use a finite default age, initially seven days to match the investigated Pi
adapter. Expiry should trigger bootstrap for the first implementation. A later
strict mode may leave the server uncached and require explicit activation.

Refresh successful entries on:

- startup bootstrap;
- first lazy connection;
- explicit cache refresh;
- `tools/list_changed`;
- server reconnection that changes capabilities.

## Runtime state machine

### States

Use explicit internal states rather than deriving lifecycle from a nullable
session pointer:

```go
type runtimeState uint8

const (
    stateConfigured runtimeState = iota
    stateCached
    stateConnecting
    stateConnected
    stateDisconnecting
    stateFailed
    stateClosed
)
```

State meanings:

| State | Meaning |
|---|---|
| `configured` | Valid declaration, no usable cache and no live session |
| `cached` | Descriptors registered, no live session |
| `connecting` | One connection and refresh attempt is active |
| `connected` | Live session available for leases |
| `disconnecting` | Idle or final session close is active |
| `failed` | Last connection failed; a later call may try again |
| `closed` | Manager shutdown is final; no new work accepted |

A failed runtime may retain valid cached descriptors. Failure state and cache
availability should be stored separately even if status presents a combined
view.

### Runtime fields

A representative runtime structure is:

```go
type serverRuntime struct {
    mu      sync.Mutex
    manager *Manager
    spec    publicmcp.ServerSpec
    owner   string

    state       runtimeState
    client      *sdkmcp.Client
    session     *sdkmcp.ClientSession
    connectDone chan struct{}
    connectErr  error

    activeCalls int
    refreshing  bool
    lastUsed    time.Time

    cachedCatalog catalog
    used          map[string]string

    runtimeCtx    context.Context
    runtimeCancel context.CancelFunc

    refreshReq  chan struct{}
    refreshStop chan struct{}
    refreshDone chan struct{}
}
```

Do not hold `mu` during process startup, network I/O, SDK calls, registry
replacement, cache writes, or session close.

### Legal transitions

The normal transitions are:

```text
configured → connecting → connected
configured → failed

cached → connecting → connected
cached → failed

failed → connecting → connected

connected → disconnecting → cached
connected → disconnecting → configured

any non-closed state → closed
```

A runtime must not return from `closed` to another state.

## Connection and call flow

### Connection sharing

Implement a synchronized connection operation with one in-flight attempt per
runtime. Waiting callers should observe the same result.

The connection attempt should use a runtime-owned context with the configured
connection timeout. A single caller cancellation must not necessarily cancel a
connection needed by other callers. Each waiting caller must still be able to
stop waiting when its own context is cancelled.

A simple contract is:

```go
func (rt *serverRuntime) acquire(
    ctx context.Context,
) (*sdkmcp.ClientSession, func(), error)
```

`acquire` should:

1. reject a final closed state;
2. return a lease immediately when connected;
3. wait on the existing `connectDone` when connecting;
4. start one connection when cached, configured, or failed;
5. increment `activeCalls` only for a valid live session;
6. return an idempotent release function.

Release decrements the active count and updates `lastUsed`. It must not close
the session synchronously.

### Tool execution

Change `remoteTool.Run` from direct session access to a lease:

```go
func (t *remoteTool) Run(
    ctx context.Context,
    raw json.RawMessage,
    _ tools.ToolHost,
) (tools.ToolResult, error) {
    session, release, err := t.runtime.acquire(ctx)
    if err != nil {
        return tools.ErrorResult(err), nil
    }
    defer release()

    if !t.runtime.hasLiveTool(t.remoteName) {
        return tools.ErrorResult(errStaleCachedTool), nil
    }

    result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
        Name:      t.remoteName,
        Arguments: args,
    })
    // Existing bounded result conversion follows.
}
```

Use the caller context for `CallTool`, progress, and cancellation. Do not retry
the tool call automatically after a transport error because the remote side may
have completed a write or execution before the connection failed.

A transport failure may mark the runtime disconnected or failed for the next
call. Reconnection and replay are distinct decisions.

### Refresh before first cached call

A cached schema can be stale. The first connection in a process must refresh
the live catalog before handing out the first call lease.

After refresh:

- continue if the original remote tool still exists;
- return a bounded stale-catalog result if it was removed;
- use the new live descriptor for later calls;
- do not reinterpret cached arguments using a changed schema silently.

Snow's normal provider request may already contain the cached schema. If the
schema changed materially, failing the current call and allowing the next
provider continuation to use the refreshed schema is safer than guessing.

## Idle shutdown

Use one manager-owned idle worker rather than one timer per server. Start it
only when at least one runtime has idle shutdown enabled.

On each bounded interval, collect eligible runtimes without holding the manager
lock during close operations. A runtime is eligible when:

- state is connected;
- lifecycle permits idle shutdown;
- `activeCalls == 0`;
- no refresh is active;
- no connection or disconnect is active;
- `now - lastUsed` exceeds the configured timeout;
- manager shutdown has not begun.

Idle disconnect should:

1. transition atomically from connected to disconnecting;
2. detach the live session from new acquisitions;
3. stop and join the connection-specific refresh worker;
4. close the SDK session with a bounded timeout;
5. clear client and session pointers;
6. retain cached descriptors and metadata;
7. transition to cached or configured;
8. update status.

A caller arriving during disconnect may wait for completion and reconnect, or
receive a bounded retryable error. Waiting and reconnecting provides better
transparency but requires a tested transition signal.

Application shutdown remains final and must cancel the idle worker before
closing runtimes.

## Catalog refresh

### Live refresh

Retain the current bounded listing and descriptor construction behavior:

- paginate tools up to existing limits;
- validate duplicate and empty names;
- preserve resource and prompt bridges;
- derive canonical Snow names deterministically;
- classify risk from the transport;
- use atomic owner replacement;
- rebuild the routing indexes through the existing changed callback;
- skip replacement when descriptor fingerprints are unchanged.

After successful registry replacement, serialize the bounded catalog and write
the metadata cache. Registry state remains authoritative for the running
process; a cache-write failure should update status but must not roll back an
otherwise valid live catalog.

### List-changed notifications

Only a connected server can send `tools/list_changed`. Keep the existing
coalescing and debounce behavior.

A refresh must hold a runtime activity guard so the idle worker cannot close the
session during listing or registry replacement. If disconnection begins first,
the refresh should stop cleanly and reconnect on the next call.

### Canonical name stability

Cached descriptors must use the same `canonical`, collision, and owner rules as
live descriptors. Store original remote names, not only the generated Snow
names. Recompute canonical names from the complete cached server catalog so
collisions remain deterministic.

A catalog replacement must remove tools that disappeared. Never merge a new
list over old descriptors without deleting absent owner entries.

## Permissions and security

### Permission order

For a valid cache, the order must be:

```text
provider requests cached descriptor
    ↓
Snow execution permission check
    ↓
approved descriptor Run
    ↓
MCP server connection or process launch
    ↓
MCP call
```

This ensures a lazy stdio command does not start merely because its schema was
selected. Stdio tools remain execution risk, and Streamable HTTP tools remain
network risk. Cached server annotations must not lower those classifications.

### Bootstrap exception

Automatic cache bootstrap occurs during startup and therefore cannot use a
normal model-requested tool permission prompt. It has the same trust boundary as
current eager MCP startup and must be documented as such.

A later strict mode should require one of:

- explicit `snow mcp check <name> --refresh-cache`;
- an approved synthetic server activation tool;
- an embedder-provided authorization decision.

Headless ask behavior must remain fail-closed.

### Process boundary

Lazy loading is not a sandbox. Once connected, stdio servers run with the
user's OS privileges. HTTP servers can send requests with configured headers.
Project declarations remain trust-gated before cache lookup or execution.

### Cache content

Do not cache:

- environment values;
- HTTP headers;
- bearer or OAuth tokens;
- credential-like command arguments;
- server stderr;
- tool results;
- resource bodies;
- prompt expansion results;
- provider-private data;
- session messages.

Bound every server-controlled string and schema before persistence and again
when loading.

## Status and observability

Keep `Status.Connected` for compatibility and add fields additively:

```go
type Status struct {
    // Existing fields omitted.

    State      string    `json:"state,omitempty"`
    Cached     bool      `json:"cached,omitempty"`
    CachedAt   time.Time `json:"cached_at,omitempty"`
    LastUsedAt time.Time `json:"last_used_at,omitempty"`
}
```

Do not expose full cache keys, fingerprints, project paths, headers,
environment values, or connection internals.

Suggested user-visible states are:

```text
configured
cached
connecting
connected
idle-disconnected
failed
closed
```

Examples:

```text
chrome-devtools  cached       26 tools
filesystem       connected     8 tools
postgres         failed       connection timed out
```

Emit bounded lifecycle events through the existing event stream only if a
stable public use case requires them. The first implementation can update MCP
status snapshots without introducing new protocol event types.

Metrics useful in tests and optional diagnostics include:

- startup cache hits and misses;
- bootstrap count and duration;
- cold connection duration;
- live catalog refresh duration;
- idle disconnect count;
- cache read/write failure count;
- stale-tool rejection count;
- concurrent callers sharing one connection attempt.

Never include arguments, user query text, credentials, or raw server output in
these diagnostics.

## Code changes

### `pkg/mcp/spec.go`

- Add lifecycle constants.
- Add `Lifecycle` and `IdleTimeoutMS` fields.
- Validate enum and nonnegative timeout values.
- Add status state/cache fields without removing `Connected`.
- Extend focused public-contract tests.

### `internal/mcp/manager.go`

- Add runtime-state and catalog types.
- Add cache and clock abstractions for deterministic tests.
- Add manager idle-worker fields.
- Separate manager lifetime context from individual calls.

### `internal/mcp/manager_runtime.go`

- Replace unconditional connection setup with initialization by lifecycle.
- Extract transport/session creation from permanent runtime construction.
- Add synchronized acquire/release behavior.
- Add non-final disconnect.
- Recreate connection-specific refresh workers on reconnect.
- Update status transitions.
- Keep final close idempotent and bounded.

Split cohesive files before any Go source exceeds the repository's 1,000-line
limit. Likely new files are:

```text
internal/mcp/cache.go
internal/mcp/lifecycle.go
internal/mcp/catalog.go
internal/mcp/status.go
```

### `internal/mcp/tools.go`

- Acquire a live session through the runtime.
- Verify original tool presence after first refresh.
- Preserve current result conversion and bounds.
- Avoid automatic replay after ambiguous failure.
- Apply the same lease to resource and prompt bridges.

### `internal/app/new.go`

Replace the assumption that every manager runtime is connected. A possible API
is:

```go
mcpManager.Initialize(ctx, mcpSpecs)
```

Initialization should register cached descriptors, connect eager servers, and
bootstrap uncached lazy servers. It must complete before the initial router is
built so descriptors are indexed deterministically.

### Configuration and CLI management

Update the configuration merger and MCP management surfaces to preserve and
render lifecycle fields. Add:

```text
--lifecycle eager|lazy|lazy-keep-alive
--idle-timeout <duration>
```

Add explicit cache inspection and refresh only after the core lifecycle works.
Potential commands are:

```sh
snow mcp cache
snow mcp check chrome-devtools --refresh-cache
snow mcp refresh chrome-devtools
```

Do not make inspection commands start servers unless their name and help text
explicitly state that they perform a live refresh.

### Documentation

Update:

- `docs/mcp.md` for current behavior after implementation;
- `docs/configuration.md` for fields and defaults;
- `docs/security.md` for bootstrap and cache boundaries;
- `IMPLEMENTATION.md` for architecture and verification status;
- CLI help and SDK references for additive public fields.

## Implementation phases

### Phase 1: state and configuration

- Add lifecycle configuration with eager default.
- Introduce explicit runtime states.
- Refactor live connect and final close without changing eager behavior.
- Add deterministic transition and concurrent-close tests.
- Preserve every existing MCP integration test.

### Phase 2: secure metadata cache

- Define bounded cache types.
- Implement safe partition keys and fingerprints.
- Add atomic `0600` read/write behavior.
- Serialize and reconstruct tool descriptors.
- Add corruption, truncation, stale-age, project-scope, symlink, and permission
  tests.
- Continue connecting eagerly while validating cache fidelity.

### Phase 3: opt-in lazy connection

- Load cached descriptors without connecting.
- Bootstrap cache misses once and disconnect them.
- Add acquire/release connection sharing.
- Refresh before the first cached call.
- Add idle disconnect while retaining descriptors.
- Support lazy behavior for stdio and Streamable HTTP transports.

### Phase 4: resources, prompts, and dynamic catalogs

- Cache or lazily reconstruct resource and prompt bridge metadata.
- Preserve pagination and subscription behavior.
- Persist successful `tools/list_changed` refreshes.
- Verify atomic routing-index replacement across reconnects.

### Phase 5: management and strict startup

- Add explicit cache status and refresh commands.
- Add a no-bootstrap strict mode only with a usable activation/discovery path.
- Consider `lazy-keep-alive` reconnect after the basic lazy state machine is
  proven.
- Evaluate changing the default from eager in a separately reviewed release.

## Verification plan

### Unit tests

Cover:

- lifecycle validation and zero-value compatibility;
- valid cache reconstruction without transport creation;
- cache miss and expiry bootstrap;
- cache partitioning by project and configuration scope;
- bounded and corrupted cache input;
- atomic replacement and `0600` mode;
- symlink and unexpected-file-type rejection;
- concurrent calls starting exactly one process;
- caller cancellation while waiting for connection;
- manager cancellation during connection;
- idle worker ignoring active calls and refreshes;
- idle disconnect preserving descriptors;
- final close preventing reconnect;
- stale cached tool removal;
- schema change rejection for the current call;
- list-changed cache update;
- cache-write failure retaining live registry state;
- no retry after an ambiguous call failure;
- status transitions and secret-free messages.

### Integration tests

Use local deterministic MCP servers to verify:

1. A valid cached lazy stdio server does not start during `app.New`.
2. `search_tools` finds its cached descriptor without starting it.
3. Denied permission prevents process startup.
4. Allowed execution starts the server once and completes the call.
5. Two concurrent allowed calls share one startup.
6. The server exits after idle timeout.
7. A later call reconnects and succeeds.
8. Application close terminates a connecting or connected server.
9. A cached Streamable HTTP server makes no startup request.
10. Live catalog changes atomically replace cache, registry, and router.
11. A project cache is not used before trust approval.
12. Chrome-style exclusive-resource failure remains bounded and recoverable.

### Performance checks

Measure before and after with representative local servers:

- Snow startup with zero, one, five, and ten configured MCP servers;
- resident process and memory count before first use;
- cache-load and descriptor-reconstruction duration;
- first-call cold latency;
- hot-call latency after connection;
- idle shutdown latency;
- cache size for large catalogs;
- concurrent cold-call startup count.

Expected outcomes:

- valid-cache lazy startup performs no MCP network or process work;
- startup cost grows with cache metadata, not server initialization latency;
- hot-call overhead remains negligible compared with MCP transport cost;
- cold calls pay one bounded initialization and refresh;
- idle shutdown leaves no child process behind.

### Repository verification

Run affected checks from the repository root:

```sh
gofmt -w pkg/mcp/*.go internal/mcp/*.go internal/app/*.go cmd/snow/*.go
go test ./pkg/mcp ./internal/mcp ./internal/app ./cmd/snow
go test -race ./internal/mcp ./internal/app ./internal/agent
go test ./...
go vet ./...
```

Run the wider RPC, SDK, and packaging matrix if public status or configuration
fields cross those surfaces. After a successfully verified feature change, run:

```sh
./scripts/install-local.sh
```

## Acceptance criteria

The feature is complete only when:

- existing configurations remain eager by default;
- a cached lazy server performs no process launch or network request at startup;
- cached tools remain searchable through Snow's existing router;
- permission denial prevents lazy connection;
- concurrent first calls start one connection;
- first connection refreshes and validates cached metadata before execution;
- stale or removed tools fail safely without calling a different tool;
- idle shutdown never closes an active call or refresh;
- idle disconnect retains valid descriptors;
- reconnect works after idle shutdown;
- final app close is bounded and prevents reconnect;
- cache files are bounded, atomic, mode `0600`, and secret-free;
- project cache data is trust- and root-partitioned;
- list-changed updates registry, router, and cache consistently;
- stdio remains execution risk and HTTP remains network risk;
- no tool call is automatically replayed after ambiguous failure;
- status distinguishes cached, connecting, connected, and failed states;
- existing MCP resources, prompts, cancellation, pagination, and output bounds
  remain intact;
- focused race tests, full tests, vet, and local installation succeed.

## Risks and mitigations

### Stale metadata

A cache may advertise a removed tool or old schema. Always refresh before the
first live call, reject changed current calls, and atomically replace the
catalog.

### Cache poisoning

A same-user process can alter the cache. Use private atomic files, strict
validation, bounds, and untrusted-content treatment. Never let cached metadata
alter transport risk or bypass permission.

### Connection races

Calls, cancellation, idle shutdown, refresh, and app close can overlap. Use an
explicit state machine, one in-flight connection signal, call leases, and race
tests. Do not hold lifecycle locks during I/O.

### First-run surprise

Automatic bootstrap still starts every uncached server once. Preserve eager as
the compatibility default, identify bootstrap in status, and add explicit
priming or activation before offering strict no-start behavior.

### Cold-call latency

The first call pays process launch, negotiation, and refresh. Keep the server
resident for a bounded idle window and expose connection state so the delay is
understandable.

### Chrome profile lock

Lazy startup avoids locks when browser tools are unused but cannot permit two
live Chrome processes to share one profile. Continue recommending
`--isolated` or distinct profiles for concurrent browser use.

### Non-idempotent replay

A transport failure does not prove that a remote write failed. Reconnect for a
future call, but never replay the failed call automatically.

### Scope growth

Do not combine lazy lifecycle work with OAuth UI, MCP Apps, task support,
process multiplexing, or a new proxy-tool API. Keep the first slice confined to
connection lifecycle and cached tool descriptors.

## Deferred work

Defer these features until the basic lazy lifecycle is stable:

- changing lazy mode to the default;
- cross-Snow process sharing of one MCP server;
- an `rmcp-mux`-style daemon or socket transport;
- automatic Chrome profile allocation;
- a Pi-style generic `mcp` proxy tool;
- reliable reconnect health checks;
- automatic replay of any tool call;
- persisted server instructions;
- cached resource bodies or prompt expansions;
- strict no-bootstrap mode without an activation path;
- distributed cache synchronization;
- hot adoption of a process started by another Snow instance;
- MCP Apps, Tasks, or interactive OAuth UI changes.

## Related documents

- [Model Context Protocol](mcp.md) — current transport, configuration,
  capability bridge, and security behavior.
- [Tool routing](tool-routing.md) — deferred descriptors, BM25 routing, and
  `search_tools` behavior.
- [Security model](security.md) — process privileges, trust, permissions, and
  bounded external content.
- [Architecture and roadmap](../IMPLEMENTATION.md) — package boundaries and
  full verification matrix.
- [Configuration](configuration.md) — current Snow configuration reference.
