# Node SDK

Use `@elmissouri16/snow-sdk` to control an installed Snow subprocess from
JavaScript or TypeScript. The SDK owns the process and JSONL RPC transport; the
Go binary still owns providers, permissions, tools, sessions, and the single
agent loop. It is not the in-process Go SDK or the Goja plugin API.

> **Note:** The checkout package is `0.1.0-dev` and is not published to npm yet.
> Build and install a local tarball as shown below. Registry installation is a
> future, separately authorized release step, not a working installation path
> promised by this guide.

## On this page

- [Requirements and local installation](#requirements-and-local-installation)
- [First prompt](#first-prompt)
- [Launch options](#launch-options)
- [Events and prompt completion](#events-and-prompt-completion)
- [Models, sessions, and modes](#models-sessions-and-modes)
- [Approvals and questions](#approvals-and-questions)
- [Advanced RPC and errors](#advanced-rpc-and-errors)
- [Cancellation and shutdown](#cancellation-and-shutdown)
- [Development and verification](#development-and-verification)
- [Related documents](#related-documents)

## Requirements and local installation

- Node >=22.12.0 on macOS or Linux. ESM, CommonJS, and TypeScript declarations
  are included, with no runtime npm dependencies.
- A compatible Snow executable already installed on `PATH`, or an explicit
  executable path. The SDK does not download or install Snow and has no install
  hooks. Building Snow from source needs Go 1.27 (`1.27rc3` currently).
- A provider configured separately for real model requests. `fake` needs no
  credentials and makes no provider network requests.

From the repository root:

```sh
go build -o build/bin/snow ./cmd/snow
(cd sdk/node && npm ci --ignore-scripts && npm run build)
mkdir -p build/dist
(cd build/sdk-node/package && npm pack --ignore-scripts --pack-destination ../../dist)
(cd examples/sdk-node && npm install --ignore-scripts --no-save --package-lock=false ../../build/dist/elmissouri16-snow-sdk-0.1.0-dev.tgz)
SNOW_EXECUTABLE="$PWD/build/bin/snow" node examples/sdk-node/example.mjs
```

For another host project, install the same tarball there with
`npm install --ignore-scripts /absolute/path/to/the/package.tgz`. Generated
package output belongs in `build/sdk-node/package`; tarballs belong in
`build/dist`, not beside SDK source. The SDK does not bundle the Snow binary.

## First prompt

Run this inside a project where the local package has been installed:

```javascript
import { SnowClient } from '@elmissouri16/snow-sdk';

const client = await SnowClient.start({
  cwd: process.cwd(),
  executable: process.env.SNOW_EXECUTABLE ?? 'snow',
  provider: 'fake',
  noSession: true,
  permission: 'deny',
});

const drain = (async () => {
  for await (const event of client.events()) {
    if (event.type === 'text_delta' && typeof event.text === 'string') {
      process.stdout.write(event.text);
    }
  }
})();
// Observe errors immediately rather than leaving a rejected stream unhandled.
const observedDrain = drain.catch((error) => {
  console.error('Event stream failed:', error);
  process.exitCode = 1;
});

try {
  const run = await client.prompt('Say hello briefly.');
  const result = await run.result;
  console.log(`\nPrompt ${run.requestId}: ${result.status}`);
  if (result.status !== 'completed') process.exitCode = 1;
} finally {
  await client.close();
  await observedDrain;
}
```

The CLI fake provider returns an empty successful turn: do not expect a greeting
or any `text_delta` frames. It verifies subprocess startup, admission,
completion, and shutdown without credentials. The runnable
[examples](../examples/sdk-node) also include CommonJS and TypeScript:

```javascript
const { SnowClient } = require('@elmissouri16/snow-sdk');
```

For real requests, authenticate outside the SDK with `snow login chatgpt`, then
explicitly run the example with `SNOW_PROVIDER=chatgpt`. Never include keys or
OAuth tokens in source, prompts, or logs. See [Providers](providers.md).

## Launch options

`SnowClient.start(options)` launches RPC directly, without a shell. `start`
resolves after the handshake; `client.ready` is the validated readiness object,
not a second startup promise. The readiness frame carries protocol
version, build version, capabilities, and framing limits; the client verifies
compatibility instead of guessing an alternate protocol.

| Option | Purpose |
|---|---|
| `cwd` | Working directory for the Snow child; set explicitly for clarity |
| `executable` | Executable path or command; defaults to `snow` on `PATH` |
| `provider`, `model` | Explicit provider/model selection |
| `session` | Existing session path to resume |
| `noSession` | Ephemeral session; do not combine with `session` |
| `permission` | `deny` (default), `ask`, or deliberately trusted `allow` |
| `tools` | Nonempty built-in tool allowlist; `[]` is rejected |
| `thinking` | Optional startup thinking level |
| `env` | Replacement child environment, not an overlay |
| `signal` | AbortSignal for the owned client lifecycle |

When supplying `env`, explicitly include required values such as `PATH`,
`HOME`, and `SNOW_HOME`; omitted values are not inherited. Do not put credentials
in diagnostic dumps of the launch options. A workspace and tool allowlist are
not a whole-process sandbox. Configured extensions are not implicitly disabled.
Snow still loads applicable normal configuration and enforces its own trust and
permission gates.

Resource settings in `StartOptions` have these defaults:

| Option | Default |
|---|---|
| `maxFrameBytes` | 16 MiB |
| `eventBuffer` | 64 notifications |
| `maxEventBytes` | 64 MiB queued event data |
| `maxPending` | 64 pending requests |
| `maxPendingBytes` | 64 MiB queued/writing request data, released after write |
| `handshakeTimeoutMs` | 10,000 ms |
| `writeTimeoutMs` | 10,000 ms |
| `responseTimeoutMs` | 30,000 ms |
| `shutdownTimeoutMs` | 1,000 ms per graceful shutdown phase |

The child stderr stream is discarded, not retained or exposed as SDK diagnostics.
For startup diagnosis, run Snow directly with the same non-secret flags.
Consult the exported types for supported bounds. An exhausted bound or invalid
transport frame fails the connection rather than silently dropping events.
A request timeout is not proof that the remote operation stopped or did not
mutate state; do not
blindly retry state-changing commands.

## Events and prompt completion

Call `client.events()` once and continuously drain its single ordered
`AsyncIterable`. Start the consumer before awaiting a prompt result. A slow or
missing consumer can exhaust the bounded event queue; this is a transport
failure, not permission to drop notifications. Fan out events in your host if
multiple UI components need them; do not open multiple SDK consumers.

Events use the public wire shape. In particular, a `text_delta` event has a
string `event.text`, not `event.text.delta`. Thinking, tool, permission,
question, mode, and lifecycle events retain the public RPC fields. Response
correlation does not reorder the observed event stream. The notification union
includes unknown future frames; check field types even after narrowing `type`,
as the example does with `text`. Open-schema values are `unknown`, not `any`.

`await client.prompt(message, { mode })` returns a `PromptRun` after command
admission. `mode` is optional. The handle exposes:

- `requestId` for correlation;
- `result`, a promise for the definitive same-ID `prompt_completed` frame;
- `cancel()`, an explicit abort request while that run is active.

Await `run.result` even if you see `turn_done`, an error event, or a legacy
same-ID failure response. Those observations are not terminal prompt
completion. The completion status is `completed`, `failed`, or `canceled`.
A failed/canceled completion is a result to inspect; a lost connection cannot
fabricate a definitive completion and rejects outstanding local work instead.
Prompt admission and prompt completion are separate operations.

## Models, sessions, and modes

Convenience groups call existing RPC commands and return typed results. For
example, `models.set({ model, provider?, thinking? })` durably updates the
project selection; `sessions.rename(name, sessionId?)` renames a session;
`modes.set("plan")` and `modes.thinking(level)` use core controls:

| Group | Methods |
|---|---|
| `client.models` | `list`, `discover`, `set` |
| `client.sessions` | `list`, `info`, `create`, `open`, `rename`, `messages` |
| `client.modes` | `set`, `thinking` |
| `client.permissions` | `reply`, `reject` |
| `client.questions` | `reply`, `reject` |

Use the exported method types for their request parameters and result shapes.
Schemas that leave a value open become `unknown`, never an unchecked `any`.
Discovery may contact a provider; listing the cached catalog is a different
operation. Session operations preserve Snow's append-only history and runtime
admission rules. Mode changes use the same authoritative core controls as the
other surfaces. The SDK does not transfer provider-private continuity or expose
high-level provider-account management. The high-level prompt accepts text and
optional mode; advanced multimodal requests use typed `rpc` and capabilities.

## Approvals and questions

Keep the default `permission: 'deny'` for headless work. With `ask`, the host
must actively consume and service `permission_request` events using
`client.permissions.reply(requestId, decision)` or `reject(requestId)`. Render
the request to a trusted person or apply an explicitly authorized host policy;
do not automatically approve.
If no such broker exists, use `deny` rather than leaving a prompt waiting.

Model questions arrive as `user_input_request` events. Service them separately
with `client.questions.reply(requestId, answers)` or `reject(requestId)`, using
their typed response fields. A question answer is not tool permission.
Approving a tool does not answer a question. Check the advertised RPC capabilities before
relying on optional interactions; see [User input](user-input.md) and
[RPC permission interaction](rpc.md#permission-interaction).

## Advanced RPC and errors

`client.rpc(request, options?)` is the lower-level typed command surface for
integrations that need existing commands beyond the convenience groups:

```typescript
const response = await client.rpc({ type: 'models_list' });
if (response.success) {
  console.log(response.data?.current);
} else {
  // Present a bounded remote diagnostic only after reviewing it as untrusted.
  console.error('Remote models_list failed');
}
```

It returns the response envelope, including `success: false` and the remote error, rather than throwing
for a remote command failure. Transport errors still reject locally. Inspect
`success` before using result data; open-schema values require narrowing.

Convenience methods turn remote command failures into `RemoteRpcError`.
The default RPC wait is 30 seconds; pass `timeoutMs` in per-call options to
override it. Its `signal` cancels only that local wait, not remote execution.
Startup, framing, timeout, process-exit, and overflow failures are transport or
lifecycle errors, not successful remote results. There is no automatic retry,
replay, account switch, or fallback model selection. Advanced RPC calls remain
subject to the Snow server's capabilities and authority; types do not grant
permission. [JSONL RPC](rpc.md) is the canonical wire and command reference.

## Cancellation and shutdown

`run.cancel()` explicitly requests abort only for the active run. Canceling a
finished handle must not abort a later prompt. Keep consuming events and await
`run.result` to learn the actual terminal status; an abort acknowledgment alone
is not a completion frame.

`client.close()` is idempotent and bounded. It ends command admission, closes
stdin for graceful EOF shutdown, then escalates direct-child termination if
necessary and reaps that child. It settles pending local requests and ends the
event stream. Lifecycle `signal` cancellation likewise closes the owned
connection; it is not a per-prompt success/cancellation result.

The client is not a descendant-process supervisor. Closing Snow does not
promise to terminate every process started by Bash, plugins, MCP servers, or
subagents. Neither subprocess ownership nor an AbortSignal creates an OS
sandbox. Use external containment when descendant or filesystem isolation is
required. See [Security model](security.md).

## Development and verification

Run from the repository root with Node >=22.12.0 and Go 1.27rc3:

```sh
go build -o build/bin/snow ./cmd/snow
(cd sdk/node && npm ci --ignore-scripts)
(cd sdk/node && npm run generate:check && npm run typecheck && npm run build && npm test)
SNOW_BIN="$PWD/build/bin/snow" npm --prefix sdk/node run test:integration
```

The SDK CI matrix uses Node 22 and 24 on Ubuntu and Node 24 on macOS. It checks
generated protocol declarations, typechecks, builds both module formats, runs
unit and real subprocess integration tests, verifies tarball imports, and runs
the examples with `fake`. Existing frontend and Go release gates remain
independent and required. Live-provider checks are explicit, manual, and
secret-free in recorded evidence.

## Related documents

- [Runnable Node examples](../examples/sdk-node)
- [JSONL RPC](rpc.md)
- [Go SDK](sdk.md)
- [Security model](security.md)
- [Release policy](releases.md)
