# JavaScript and Python Plugin Research

This record explains why Snow keeps a persistent JSON-RPC v2 subprocess
architecture as the JavaScript/Python plugin ABI instead of embedding language
runtimes in the Go binary. It captures the alternatives considered, the local
benchmarks, the hardening already implemented, and the work still deferred.

## On this page

- [Summary](#summary)
- [Context](#context)
- [Decision](#decision)
- [Alternatives considered](#alternatives-considered)
- [Results](#results)
- [Risks and deferrals](#risks-and-deferrals)
- [Sources](#sources)

## Summary

Keep Snow's persistent JSON-RPC v2 subprocess architecture as the
JavaScript/Python plugin ABI. Improve its developer experience and conformance
rather than embedding language runtimes in the Go binary.

Host hardening, the complete wire reference, `snow plugin check`, and
dependency-free JavaScript/Python examples are implemented. Published npm/PyPI
helper packages remain deferred until the protocol fixtures stabilize.

## Context

Snow needs an easy way to author plugins in JavaScript or Python without:

- materially increasing tool-call latency;
- adding cgo or native runtime dependencies to the Snow binary;
- rebuilding Node/npm or Python package/runtime behavior inside Go;
- weakening existing permission, trust, output, timeout, or shutdown controls;
- creating a large cross-platform release matrix.

Snow already has a language-neutral external plugin host under
`internal/plugin`. It starts an explicit argv process, performs a JSON-RPC 2.0
handshake, registers namespaced tools, multiplexes calls, forwards bounded
progress and subscribed agent events, handles cancellation, captures
diagnostics, and shuts the process down in reverse load order.

The main gap was author ergonomics and protocol completeness, not runtime
architecture.

## Decision

Use persistent JSONL subprocess runtimes as the default plugin ABI. Snow keeps
one resident runtime per plugin package and copies JSON over pipes. This
preserves full Node/npm and Python environments at low implementation cost
because the external host already exists. Embedded runtimes, WASM, and MCP
remain alternatives for specific cases, not the default.

## Alternatives considered

| Approach | Runtime cost | Implementation/release cost | Ecosystem compatibility | Decision |
|---|---|---|---|---|
| Persistent JSONL subprocess | One resident runtime plus JSON/pipe copies | Low; host already exists | Full Node/npm and Python environments | Default |
| Embedded goja | No process or IPC | Snow must provide modules, event loop, filesystem/network APIs, cancellation, limits, and concurrency policy | JavaScript but not Node/npm | Defer; constrained scripts only |
| Embedded QuickJS | No IPC | Native/cgo bindings and per-platform testing; still no Node APIs | Modern JavaScript subset | Reject as default |
| Embedded CPython | No IPC | Native linking, interpreter/stdlib distribution, reference management, GIL/lifecycle, extension compatibility | Full Python only if packaged correctly | Reject |
| WASM/wazero | Portable capability boundary | New ABI, host imports, async/cancellation and toolchain work | Good for compiled compute, poor transparent JS/Python support | Possible future ABI |
| MCP | Similar stdio/HTTP costs | Already implemented through the official Go SDK | Broad agent-tool ecosystem | Use for interoperable servers |

### Why not embed JavaScript?

A pure-Go engine such as goja avoids cgo, but one runtime is not goroutine-safe
and Node-compatible modules/event-loop behavior are not inherent. Snow would
need to own module loading, timers/promises, npm compatibility decisions,
filesystem/network bridges, cancellation, memory accounting, and failure
containment. This is substantially more complexity than a persistent Node
process and places plugin failures in Snow's address space.

QuickJS improves language coverage but normally introduces native bindings or
bundled artifacts. It still does not provide the Node ecosystem expected by
JavaScript plugin authors.

### Why not embed Python?

CPython's official embedding API requires interpreter initialization, native
headers/libraries, data conversion, and reference handling. Official guidance
notes that locating the correct compile/link flags is not necessarily trivial,
and extension modules add operating-system, architecture, libc, and Python
version compatibility. Eliminating a small pipe/JSON cost does not justify that
burden for an LLM-bound tool runtime.

### Why keep MCP separate?

MCP is the preferred path for reusable tools, resources, and prompts that should
work across agent hosts. Snow's plugin ABI remains useful for Snow-specific
lifecycle, private configuration, bounded progress, and observation-only agent
events. A stdio MCP server has similar process and JSON costs; choose between
them by interoperability, not performance.

## Results

### Local performance probe

A temporary probe exercised Snow's real `ExternalHost` with persistent Node and
Python echo runtimes on Darwin arm64:

```text
Node 24.16:
  initialize p50  37.8 ms
  initialize p95  43.5 ms
  2,000 serial hot calls: 30.3 µs/call average

Python 3.9:
  initialize p50  80.8 ms
  initialize p95  92.3 ms
  2,000 serial hot calls: 32.7 µs/call average
```

The initialization measurement includes process spawn, `initialize`, and
`tools/list`. Calls used small JSON payloads and no tool work. The runtime stays
alive for the Snow session, so startup is amortized.

These results show that IPC is not a meaningful bottleneck for typical coding
agent tools, where provider, network, filesystem, compiler, or subprocess work
usually takes milliseconds to seconds. They are not universal guarantees:
antivirus, large imports, large frames, native modules, and per-process memory
still require supported-platform measurement.

The more important performance risks are:

1. one interpreter's resident memory per plugin package;
2. exposing too many direct tool schemas to the model instead of deferred
   discovery;
3. forwarding high-frequency text/thinking events to uninterested plugins;
4. plugins that block stdout, ignore cancellation, or emit unbounded
   diagnostics.

Related tools should share one process. Do not create one interpreter process
per tool.

### Required host hardening

The first implementation slice addressed these items before describing
JavaScript/Python plugins as first-class:

1. **Honor `supported_events`.** The initialize response is an explicit
   subscription. Snow forwards only listed event types; omitted or empty lists
   receive no events. Delivery remains bounded and best effort so a slow plugin
   cannot block the agent loop. This removes unnecessary serialization, pipe
   traffic, and queue pressure from text/thinking deltas.
2. **Preserve result `details`.** External `tools/call` results already decoded
   `details`, but the registry adapter dropped them. The adapter now preserves a
   cloned raw JSON value as private host metadata. Details remain excluded from
   the provider-facing conversation.
3. **Support optional tool risk metadata.** External tool descriptors may
   declare `read`, `write`, `exec`, or `network`. Omitted risk defaults to
   `exec`; invalid values fail initialization. Per-tool capabilities are also
   retained and combined with plugin-level capabilities. Risk is trusted
   metadata used by the central permission service, not containment inside the
   subprocess. A malicious plugin labeled `read` still has the current user's OS
   privileges. Only trusted plugins should receive a less restrictive
   classification.
4. **Publish the complete protocol contract.**
   [`plugin-protocol.md`](plugin-protocol.md) is the canonical wire reference
   for
   LF framing and stdout/stderr rules,
   initialization and validation, tool schemas/discovery/risk/capabilities,
   calls and content/details results, progress and logging, cancellation and
   local deadlines, subscribed best-effort agent events, errors, output limits,
   EOF, and shutdown.
5. **Improve diagnostics.** `snow plugin check <manifest-or-executable>` starts
   only the plugin runtime and reports manifest/protocol validity,
   initialization time and effective cwd, negotiated tools and effective
   plugin/tool capabilities, risks, discovery modes, subscribed event types,
   informational runtime limits, bounded stderr/protocol diagnostics with
   best-effort common-credential redaction, and graceful shutdown failures.
   JSON output is available with `--json`.
6. **Make interpreter configuration explicit.** External plugins receive an
   empty environment unless `PluginSpec.Env` is set. This reduces accidental
   credential inheritance but affects `/usr/bin/env` shebangs, subprocess
   lookup, locale, certificate configuration, and some package behaviors.
   Reference manifests therefore show an argv command plus a deliberately
   minimal `PATH`. Production declarations should prefer absolute interpreter
   and script paths, such as a Python virtual environment's interpreter.

### Reference runtime design

Dependency-free examples live under:

```text
examples/plugins/javascript/
examples/plugins/python/
```

Both implementations provide:

- initialize and tool discovery;
- optional tool risk;
- string JSON-RPC IDs;
- serialized stdout writes;
- concurrent request tracking;
- progress bound to a non-empty call ID;
- cancellation and local timeout handling;
- structured content and private details;
- event subscription;
- stderr-only diagnostics;
- graceful shutdown.

The examples are executable documentation and integration fixtures. They avoid a
third-party generic JSON-RPC dependency because such packages do not normally
encode Snow's JSONL framing, progress, call IDs, cancellation, output
discipline, or shutdown semantics.

### Future SDK ergonomics

After the examples and conformance tests remain stable, they can be extracted
into:

- `@snow-core/plugin` for Node.js/TypeScript;
- `snow-plugin` for Python.

The intended API is a declarative plugin definition with tool handlers receiving
an execution context:

```text
ToolContext
  call/request IDs
  effective cwd
  AbortSignal or asyncio cancellation
  bounded progress(message)
  host deadline
```

Both SDKs should have no runtime dependencies. TypeScript tooling may remain a
development dependency; Python can use `asyncio`, `json`, and standard streams.

## Risks and deferrals

### Deferred work

Do not add until a demonstrated requirement outweighs its cost:

- embedded goja, QuickJS, or CPython;
- a WASM plugin ABI;
- runtime downloading or bundling;
- executable directory scans or a plugin marketplace;
- hot reload;
- reliable event replay;
- parallel plugin startup before sequential startup is measured as material;
- npm/PyPI publication before protocol conformance is stable.

Publishing is deliberately deferred because package release/version support is
an ongoing maintenance commitment. The wire behavior should be stabilized by
cross-language conformance fixtures first.

### Acceptance criteria

Before publishing language SDKs:

- no embedded runtime, cgo dependency, or bundled interpreter is added;
- the existing Go plugin API remains compatible;
- Node and Python fixtures pass initialize, list/call, progress, cancellation,
  event, malformed-frame, timeout, and shutdown tests;
- unsubscribed plugins receive zero agent events;
- external risk defaults to `exec` and invalid values fail initialization;
- external `details` survive registry adaptation;
- a full event queue never blocks the agent loop;
- small hot calls remain below 1 ms p95 on supported CI hosts;
- empty example initialization remains below 250 ms p95 on supported CI hosts;
- 10,000 small calls show no sustained memory growth;
- macOS and Linux test explicit interpreter launching;
- `go test ./...`, `go vet ./...`, and focused race tests pass.

### Security boundary

Process separation provides crash isolation and a termination boundary, not a
sandbox. JavaScript/Python plugins, stdio MCP servers, and Go plugins execute
with the user's OS privileges. Project trust controls whether project-declared
configuration is loaded; it does not confine an already running plugin.

Use explicit declarations, minimal environments, permission defaults, bounded
I/O, deadlines, and containers/VMs/OS sandboxing for untrusted code.

## Sources

- [JSON-RPC 2.0 specification](https://www.jsonrpc.org/specification)
- [Node.js readline](https://nodejs.org/api/readline.html)
- [Node.js stream backpressure](https://nodejs.org/api/stream.html)
- [Python embedding documentation][python-embedding]
- [Python asyncio platform support][python-asyncio]
- [goja](https://github.com/dop251/goja)
- [QuickJS](https://bellard.org/quickjs/quickjs.html)
- [Go cgo documentation](https://pkg.go.dev/cmd/cgo)
- [wazero](https://github.com/tetratelabs/wazero)
- [MCP architecture][mcp-arch]
- `pkg/plugin/plugin.go`
- `internal/plugin/external.go`
- `internal/plugin/manager.go`

[python-embedding]: https://docs.python.org/3/extending/embedding.html
[python-asyncio]: https://docs.python.org/3/library/asyncio-platforms.html
[mcp-arch]: https://modelcontextprotocol.io/specification/latest/architecture

## Related documents

- [Plugins](plugins.md)
- [External plugin protocol v2](plugin-protocol.md)
- [Tool routing](tool-routing.md)
- [MCP](mcp.md)
