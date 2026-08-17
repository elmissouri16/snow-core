# JavaScript and Python Plugin SDK Implementation Plan

This document proposes dependency-free JavaScript/TypeScript and Python SDKs
for authoring Snow protocol-v2 plugins. It defines the intended public API,
runtime responsibilities, package layout, conformance strategy, implementation
phases, and release gates. The external plugin wire contract remains owned by
[External plugin protocol v2](plugin-protocol.md).

> **Note:** This is a future implementation plan, not current behavior. The
> existing `@snow-core/sdk` and `snow-core-sdk` packages are RPC clients for
> controlling a Snow process; they must remain separate from the plugin
> authoring SDKs described here.

## On this page

- [Decision and goals](#decision-and-goals)
- [Package boundaries](#package-boundaries)
- [Intended author experience](#intended-author-experience)
- [Minimum public contract](#minimum-public-contract)
- [Runtime responsibilities](#runtime-responsibilities)
- [Type safety and schema handling](#type-safety-and-schema-handling)
- [Conformance and verification](#conformance-and-verification)
- [Implementation phases](#implementation-phases)
- [Acceptance criteria](#acceptance-criteria)
- [Deferred work](#deferred-work)
- [Related documents](#related-documents)

## Decision and goals

Keep Snow's persistent JSON-RPC 2.0 JSONL subprocess protocol as the extension
application binary interface (ABI). Improve author ergonomics by extracting the
repeated framing, lifecycle, progress, cancellation, event, and shutdown logic
from the reference runtimes into two small packages:

- `@snow-core/plugin` for Node.js and TypeScript;
- `snow-plugin`, imported as `snow_plugin`, for Python.

The SDKs should let authors declare manifests and tools without understanding
wire-level request IDs or JSON-RPC methods. They must preserve Snow's existing
permission, process, and lifecycle boundaries rather than creating another tool
execution path.

The initial goals are:

- zero runtime dependencies;
- one persistent interpreter process per plugin package;
- declarative manifests, tools, risks, discovery metadata, and events;
- idiomatic cancellation through `AbortSignal` and `asyncio.Task`;
- serialized, backpressure-aware protocol output;
- local declaration validation with bounded diagnostics;
- matching behavior across JavaScript, Python, and Snow's Go host;
- compatibility with `snow plugin check`;
- small generated plugins whose source contains tool behavior rather than
  protocol machinery.

The SDKs are not a sandbox, a general JSON-RPC framework, or replacements for
Model Context Protocol (MCP). JavaScript and Python plugins continue to run with
the user's operating-system privileges.

## Package boundaries

Do not merge plugin authoring into the existing language RPC clients. Use
separate package roots and publication names so imports reveal whether code
controls Snow or runs as a Snow extension.

| Package | Purpose |
|---|---|
| `@snow-core/sdk` | Existing JavaScript/TypeScript client for Snow RPC mode |
| `snow-core-sdk` | Existing Python client for Snow RPC mode |
| `@snow-core/plugin` | Proposed Node.js/TypeScript plugin runtime |
| `snow-plugin` | Proposed Python plugin runtime |

A representative repository layout is:

```text
sdk/
├── javascript/                  # Existing @snow-core/sdk RPC client
├── python/                      # Existing snow-core-sdk RPC client
├── plugin-javascript/           # Proposed @snow-core/plugin
│   ├── package.json
│   ├── README.md
│   ├── src/
│   │   ├── index.js
│   │   ├── index.d.ts
│   │   ├── runtime.js
│   │   ├── protocol.js
│   │   ├── context.js
│   │   ├── results.js
│   │   └── errors.js
│   └── test/
└── plugin-python/               # Proposed snow-plugin
    ├── pyproject.toml
    ├── README.md
    ├── src/snow_plugin/
    │   ├── __init__.py
    │   ├── plugin.py
    │   ├── runtime.py
    │   ├── protocol.py
    │   ├── context.py
    │   ├── results.py
    │   ├── errors.py
    │   └── py.typed
    └── tests/
```

Keep the low-level reference runtimes under `examples/plugins` as executable
wire documentation and conformance fixtures. SDK-based examples should be
added beside them after the local packages are usable.

## Intended author experience

### JavaScript and TypeScript

A JavaScript plugin should contain its manifest, schemas, and handlers without
implementing the JSON-RPC server:

```javascript
import {
  definePlugin,
  serve,
  textResult,
} from "@snow-core/plugin";

const plugin = definePlugin({
  manifest: {
    id: "example-js",
    name: "Example JavaScript tools",
    version: "1.0.0",
  },

  tools: [
    {
      name: "echo",
      description: "Echo some text",
      risk: "read",
      parameters: {
        type: "object",
        properties: {
          text: { type: "string" },
        },
        required: ["text"],
        additionalProperties: false,
      },

      async execute(arguments, context) {
        context.signal.throwIfAborted();
        await context.progress("Preparing echo");
        return textResult(arguments.text, {
          details: {
            runtime: "node",
            length: arguments.text.length,
          },
        });
      },
    },
  ],

  events: {
    async tool_end(event, context) {
      await context.log("debug", `Tool completed: ${event.tool_name}`);
    },
  },
});

await serve(plugin);
```

The runtime should use native ECMAScript modules and ship TypeScript
declarations. It should support synchronous handlers and handlers returning a
promise.

### Python

Python should expose an idiomatic decorator API while retaining an explicit
plugin object and entry point:

```python
from snow_plugin import Plugin, ToolContext, text_result

plugin = Plugin(
    plugin_id="example-python",
    name="Example Python tools",
    version="1.0.0",
)


@plugin.tool(
    name="echo",
    description="Echo some text",
    risk="read",
    parameters={
        "type": "object",
        "properties": {
            "text": {"type": "string"},
        },
        "required": ["text"],
        "additionalProperties": False,
    },
)
async def echo(arguments: dict, context: ToolContext):
    await context.progress("Preparing echo")
    context.raise_if_cancelled()

    text = str(arguments["text"])
    return text_result(
        text,
        details={
            "runtime": "python",
            "length": len(text),
        },
    )


if __name__ == "__main__":
    plugin.run()
```

The Python runtime should preserve Python 3.9 compatibility and support both
ordinary functions and async functions. Cancellation of blocking synchronous
code remains cooperative and must be documented.

## Minimum public contract

### Plugin definition

Both SDKs should support:

- a manifest ID, name, and version;
- optional plugin-level capabilities;
- tool declarations;
- event handlers;
- optional setup and shutdown hooks.

The SDK should insert protocol version 2 automatically. Authors should not
normally set or negotiate the wire protocol version themselves.

Setup receives the initialized host context and may return plugin-private state
for handlers. The context includes effective working directory, session ID,
host version, negotiated host capabilities, and private plugin configuration.
The SDK must never copy private configuration into automatic diagnostics.

### Tool definition

Each tool supports:

- `name`;
- `description`;
- JSON Schema `parameters`;
- `risk` (`read`, `write`, `exec`, or `network`);
- optional capabilities;
- optional discovery metadata;
- an `execute` handler.

The SDK validates identifiers, duplicate names, non-empty descriptions, schema
object shape, risk values, discovery modes, and callable handlers before it
starts serving requests. Snow remains authoritative and revalidates descriptors
during initialization.

### Tool context

The JavaScript context should have an interface similar to:

```typescript
interface ToolContext {
  callId: string;
  requestId: string;
  cwd: string;
  sessionId: string;
  signal: AbortSignal;
  deadline?: Date;
  config: unknown;

  progress(
    message: string,
    options?: { done?: boolean; isError?: boolean },
  ): Promise<void>;

  log(
    severity: "debug" | "info" | "warning" | "error",
    message: string,
  ): Promise<void>;
}
```

The Python context should expose the same concepts idiomatically:

```python
@dataclass(frozen=True)
class ToolContext:
    call_id: str
    request_id: str
    cwd: str
    session_id: str
    deadline: Optional[float]
    config: Any

    async def progress(
        self,
        message: str,
        *,
        done: bool = False,
        is_error: bool = False,
    ) -> None: ...

    async def log(self, severity: str, message: str) -> None: ...

    def raise_if_cancelled(self) -> None: ...
```

Progress and logging methods are asynchronous so the runtime can preserve
writer ordering and backpressure.

### Result helpers

Most tools should not construct raw content blocks. Both SDKs should provide:

- `textResult` / `text_result`;
- `errorResult` / `error_result`;
- a validated advanced result constructor;
- optional private `details` that never enter provider-facing content.

JavaScript example:

```javascript
return textResult("done", {
  details: { changed: 3 },
});
```

Python example:

```python
return text_result(
    "done",
    details={"changed": 3},
)
```

The advanced constructor accepts protocol content blocks but validates them
before writing a response.

### Expected errors

Provide a small public `ToolError` type for expected, bounded tool failures:

```javascript
throw new ToolError("Record does not exist");
```

```python
raise ToolError("Record does not exist")
```

Unexpected exceptions produce a bounded JSON-RPC error. Full stack traces may
be written to stderr in development mode, but responses must not contain
arguments, private configuration, environment variables, or credentials.

## Runtime responsibilities

### Framing and validation

The SDK runtime owns:

- one UTF-8 JSON object per LF-terminated line;
- JSON-RPC version and request-shape validation;
- non-empty string request IDs;
- `initialize`, `tools/list`, `tools/call`, and `shutdown` dispatch;
- cancellation, progress, logging, and subscribed event notifications;
- response correlation and bounded error messages;
- graceful stdin EOF handling.

Do not add a generic JSON-RPC dependency. Snow's framing, call IDs, progress,
cancellation, output discipline, events, and shutdown semantics are specific
enough that a small dedicated implementation is easier to audit.

### Cancellation and deadlines

For every JavaScript tool call, create an `AbortController` and index it by
request ID and tool-call ID. Abort it when Snow sends
`notifications/cancelled`, when the host deadline expires, or during shutdown.
Remove the indexes when execution finishes and suppress responses after Snow
has cancelled the request.

For every Python tool call, create an `asyncio.Task`, index it by both IDs, and
cancel it on the same boundaries. Apply positive host deadlines with
`asyncio.wait_for`. Handle `asyncio.CancelledError` separately from ordinary
handler errors.

Neither SDK can forcibly cancel arbitrary blocking user code. JavaScript
handlers must pass their `AbortSignal` into cancellable APIs. Python handlers
must use cooperative async APIs or deliberately isolate appropriate blocking
work.

### Stdout discipline and backpressure

The protocol writer must capture the original stdout stream before user code
runs. Ordinary diagnostics should default to stderr to prevent common logging
mistakes from corrupting the protocol.

For JavaScript, route ordinary console methods to stderr while retaining the
captured `process.stdout` for protocol frames. Serialize all writes. If
`write()` returns `false`, wait for the writable stream's `drain` event before
sending another frame.

For Python, capture `sys.stdout.buffer` for the protocol and route ordinary
`print()` output to stderr. Use one writer lock or bounded writer queue and
flush complete frames. Direct writes by user dependencies cannot be fully
prevented and remain documented as unsafe.

### Concurrency and events

Use one nonblocking reader, a task per accepted tool call, one serialized
writer, active request/call maps, and a bounded concurrency semaphore. Tool
execution must not prevent the reader from processing cancellation or shutdown.

Offer runtime limits such as:

```javascript
await serve(plugin, {
  maxConcurrency: 8,
  maxEventQueue: 64,
});
```

```python
plugin.run(
    max_concurrency=8,
    max_event_queue=64,
)
```

Snow's `PluginSpec` remains authoritative; SDK limits are defense in depth.

Derive `supported_events` from registered event handlers instead of asking the
author to duplicate the list. Process events through a separate bounded queue.
A blocked or failed observer must not block stdin processing, tool execution,
or shutdown. On overflow, drop best-effort events and emit one bounded
rate-limited diagnostic.

### Lifecycle hooks

Support optional setup and shutdown hooks:

```javascript
definePlugin({
  async setup(context) {
    return { client: await createClient(context.config) };
  },

  async shutdown(state) {
    await state.client.close();
  },
});
```

Shutdown processing should:

1. stop accepting new calls;
2. cancel active handlers;
3. wait for a bounded grace period;
4. invoke the shutdown hook once;
5. send the shutdown response when possible;
6. close cleanly.

Make close behavior idempotent because explicit shutdown, stdin EOF, process
signals, and handler failures can race.

## Type safety and schema handling

The JavaScript package should ship handwritten TypeScript declarations and
support generic argument types:

```typescript
interface EchoArguments {
  text: string;
}

defineTool<EchoArguments>({
  // Schema remains explicit and authoritative.
  async execute(arguments, context) {
    return textResult(arguments.text);
  },
});
```

The Python package should ship `py.typed` and typed definitions for handlers,
contexts, results, manifests, discovery metadata, event payloads, risks, and
log severities. Use `Protocol`, `TypedDict`, `Generic`, and `Literal` where they
remain compatible with Python 3.9.

Do not implement a JSON Schema validator from scratch. Keep schemas explicit
and let Snow remain authoritative. Schema-to-TypeScript/Python generation may
be added later as a development tool after the runtime contract stabilizes.

Unknown additive initialize fields and event fields should be preserved or
ignored safely according to the protocol contract. Unsupported protocol major
versions must fail before tool execution.

## Conformance and verification

Create shared language-neutral fixtures, for example:

```text
testdata/plugin-v2/
├── initialize.json
├── tools-list.json
├── tool-call.json
├── cancellation.json
├── event.json
├── malformed-frame.txt
├── concurrent-calls.jsonl
└── expected/
```

Run the same behavioral scenarios against:

- the raw JavaScript reference runtime;
- an `@snow-core/plugin` fixture;
- the raw Python reference runtime;
- a `snow-plugin` fixture;
- Snow's Go `ExternalHost`.

The conformance matrix should cover:

1. initialization and manifest negotiation;
2. tool listing and descriptor validation;
3. successful calls and structured tool errors;
4. unexpected handler exceptions;
5. bounded progress and logging;
6. private `details` preservation;
7. cancellation by request ID and call ID;
8. local and host deadlines;
9. concurrent calls completing out of order;
10. serialized stdout and writer backpressure;
11. subscribed events only and full event queues;
12. malformed JSON and invalid request shapes;
13. unknown methods and notifications;
14. shutdown during active calls and stdin EOF;
15. oversized input, output, progress, and diagnostics;
16. Unicode and LF framing boundaries;
17. ordinary logging not corrupting stdout;
18. provider-free `snow plugin check` integration;
19. 10,000 small calls without sustained memory growth;
20. macOS and Linux explicit interpreter launching.

Representative package checks are:

```sh
(cd sdk/plugin-javascript && npm test && npm pack --dry-run)
PYTHONPATH=sdk/plugin-python/src \
  python3 -m unittest discover -s sdk/plugin-python/tests -v
python3 -m compileall -q \
  sdk/plugin-python/src sdk/plugin-python/tests
```

Build a Python wheel only in packaging CI or an environment with the approved
build tooling available. The normal Go suite must remain network-free.

## Implementation phases

### Phase 1: conformance foundation

- Extract language-neutral protocol-v2 test vectors.
- Add a Go harness that runs a declared external command against the fixtures.
- Keep current raw examples passing without behavior changes.
- Define the JavaScript and Python public types before implementing helpers.

### Phase 2: private minimal SDKs

- Add private, unpublished package roots.
- Implement manifest and tool definitions.
- Implement initialize, list, call, progress, cancellation, and shutdown.
- Add result and expected-error helpers.
- Keep zero runtime dependencies.
- Exercise both packages through Snow's real `ExternalHost`.

### Phase 3: developer ergonomics

- Add typed tool contexts and lifecycle hooks.
- Redirect common console and `print()` diagnostics to stderr.
- Add event handlers with derived subscriptions and bounded queues.
- Add declaration validation and bounded diagnostics.
- Add complete TypeScript declarations and Python typing markers.
- Add SDK-based examples while retaining low-level reference fixtures.

### Phase 4: supervised authoring

Update the bundled `$plugin-builder` skill after the SDK packages are usable.
Keep dependency-free raw templates for offline fallback, and add SDK templates
that generate concise tool-focused source.

A later CLI may scaffold disabled plugin declarations:

```sh
snow plugin init --runtime javascript my-plugin
snow plugin init --runtime python my-plugin
```

Scaffolding must not install dependencies, execute validation, register, or
enable a plugin without the existing explicit review and permission steps.

### Phase 5: publication

Publish `0.x` packages only after the wire fixtures and package surfaces remain
stable across supported platforms. Document the protocol compatibility matrix,
package support window, and upgrade policy before removing npm/PyPI publication
from the deferred list.

## Acceptance criteria

The first publishable SDK release must satisfy all of these conditions:

- the external protocol and Go plugin API remain compatible;
- the SDKs add no embedded runtime, cgo dependency, or bundled interpreter;
- both runtime packages have zero production dependencies;
- raw and SDK fixtures pass the same protocol-v2 conformance suite;
- cancellation and deadlines work without blocking the reader;
- concurrent responses remain correlated and stdout writes remain serialized;
- unsubscribed plugins receive zero events;
- a full event queue cannot block tool calls or the agent loop;
- risk defaults and invalid-risk failure behavior match Snow;
- private details survive adaptation but stay out of provider-facing content;
- ordinary console and `print()` use cannot corrupt protocol stdout;
- package contents contain only intended source, declarations, metadata, and
  documentation;
- `snow plugin check` validates packaged JavaScript and Python examples;
- small hot calls remain below 1 ms p95 on supported CI hosts;
- empty example initialization remains below 250 ms p95;
- 10,000 small calls show no sustained memory growth;
- macOS and Linux CI cover explicit interpreter launching;
- affected Go, JavaScript, Python, race, vet, and packaging checks pass.

## Deferred work

Do not include these features in the initial SDK implementation:

- combining plugin authoring with the existing RPC client packages;
- a generic third-party JSON-RPC framework;
- runtime downloads or bundled Node/Python interpreters;
- full JSON Schema validation or custom schema engines;
- automatic package installation;
- automatic plugin registration, enablement, or hot loading;
- reliable or transactional event replay;
- automatic credential or environment inheritance;
- schema-to-language code generation;
- a plugin marketplace;
- goja, QuickJS, CPython embedding, or a WASM plugin ABI;
- claims that plugin SDKs sandbox extension code.

Evaluate schema code generation, scaffolding, and publication only after the
minimal runtime and conformance fixtures are stable.

## Related documents

- [Plugins](plugins.md) — current extension behavior and security model.
- [External plugin protocol v2](plugin-protocol.md) — canonical wire contract.
- [JavaScript and Python plugin research](plugin-js-python-research.md) —
  runtime decision, benchmarks, and architectural alternatives.
- [Python and JavaScript/TypeScript SDKs](language-sdks.md) — separate RPC
  client packages for controlling Snow.
- [Security model](security.md) — process privilege and trust boundaries.
