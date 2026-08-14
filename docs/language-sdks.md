# Python and JavaScript/TypeScript SDKs

Snow's non-Go SDKs are typed local clients for the same Go runtime used by the
CLI and Go SDK. They do not reimplement providers, tools, sessions, permissions,
or the agent loop.

```text
Python or Node host
        │  JSONL over private stdin/stdout pipes
        ▼
external snow --mode rpc process
        │
        ▼
Go agent runtime, providers, tools, sessions, goals, and subagents
```

This design keeps behavior and security fixes in one runtime. The Go SDK remains
the only in-process embedding surface.

> **Stability:** the language SDKs and RPC protocol v1 are pre-alpha. The
> packages are checked in and tested but intentionally not published to PyPI or
> npm yet.

## Packages

| Host | Package | Runtime dependencies | Minimum runtime |
|---|---|---:|---:|
| Python | [`sdk/python`](../sdk/python) (`snow_sdk`) | none | Python 3.9 |
| JavaScript/TypeScript | [`sdk/javascript`](../sdk/javascript) (`@snow-core/sdk`) | none | Node.js 22 |

Both clients require `snow` on `PATH` or an explicit executable path. Neither
package downloads, installs, upgrades, or embeds a Snow binary.

## Python

Use the checkout directly:

```sh
PYTHONPATH=/path/to/snow-core/sdk/python/src python3 app.py
```

```python
import asyncio
from snow_sdk import SnowClient, SnowOptions


async def main():
    async with await SnowClient.start(SnowOptions(
        command=("/path/to/snow",),
        provider="fake",
        cwd="/path/to/project",
    )) as snow:
        events = snow.events()
        prompt = asyncio.create_task(snow.prompt("Review this repository"))
        async for event in events:
            if event.type == "text_delta" and "agent" not in event.raw:
                print(event.get("text", ""), end="", flush=True)
            if event.type == "turn_done" and "agent" not in event.raw:
                events.close()
        await prompt


asyncio.run(main())
```

`SnowClient.start` waits for and validates `rpc_ready`. `prompt` first waits for
admission and then for the definitive `prompt_completed` frame. Event iterators
are independent and bounded; slow consumers receive an explicit overflow error.
Unknown event fields remain available through `AgentEvent.raw`; bounded
`diagnostics` retain responses with unknown IDs without crashing the reader.

The SDK also exposes `request`, `abort`, `session_info`, `models`,
`subagent_models`, and model-requested input reply/reject helpers.

## JavaScript and TypeScript

The checked-in package is native ESM JavaScript with bundled TypeScript
declarations:

```js
import { Snow } from "/path/to/snow-core/sdk/javascript/src/index.js";

const snow = await Snow.start({
  executable: "/path/to/snow",
  provider: "fake",
  cwd: "/path/to/project",
});

let rejectEvents;
const eventFailure = new Promise((_, reject) => { rejectEvents = reject; });
const unsubscribe = snow.subscribe((event) => {
  if (event.type === "text_delta" && !event.agent) {
    process.stdout.write(event.text ?? "");
  }
}, { onError: rejectEvents });

try {
  await Promise.race([
    snow.prompt("Review this repository"),
    eventFailure,
  ]);
} finally {
  unsubscribe();
  await snow.close();
}
```

`Snow.events()` provides an independent bounded async iterator. `subscribe()`
uses a separate bounded queue per callback, supports async listeners, and reports
overflow/listener failures through `onError` plus bounded diagnostics. Every
subscriber receives an isolated payload copy. `AbortSignal` can stop an iterator,
request cancellation of an active prompt, or terminate the owned process. The
public declarations include RPC responses, session/model data, events, user
input, and the SDK error hierarchy.

## Protocol v1 contract

Every process starts with a first frame similar to:

```json
{
  "type": "rpc_ready",
  "protocol_version": "1",
  "snow_version": "0.1.0-dev",
  "capabilities": ["models_list", "prompt_completion", "session_info"],
  "max_input_bytes": 16777216
}
```

The SDKs reject unsupported versions or missing required capabilities before
sending commands. A prompt then has two separate lifecycle signals:

```json
{"id":"p1","type":"response","command":"prompt","success":true}
{"type":"turn_done","turn_id":"..."}
{"type":"prompt_completed","request_id":"p1","status":"completed"}
```

The response is admission, `turn_done` is the agent lifecycle boundary, and
`prompt_completed` is the definitive RPC result. Terminal status is
`completed`, `failed`, or `canceled`.

`models_list` discovers the active provider catalog. `subagent_models` returns
exact child-capable provider/model pairs plus runtime enablement metadata.

Canonical Draft 2020-12 schemas live under
[`pkg/protocol/schema/rpc/v1`](../pkg/protocol/schema/rpc/v1). Go tests resolve
all references without network access and validate representative public DTOs.
Language clients preserve unknown additive fields for forward compatibility.

## Model-requested user input

Both clients can either consume `user_input_request` directly or install an
async handler. The event is published to observers before the handler runs. A
successful handler sends `user_input_reply`; failure sends
`user_input_reject`.

This channel answers model questions only. It never approves tool permissions.
RPC permission mode `ask` remains fail-closed.

## Process and error behavior

Both clients:

- invoke the executable directly without a shell;
- keep stderr separate from protocol stdout and retain only a bounded tail;
- bound input/output frames and event queues;
- serialize writes and correlate out-of-order responses by ID;
- reject pending operations if the child exits or emits invalid JSON/UTF-8;
- close stdin for orderly shutdown, then terminate/kill after bounded waits;
- provide distinct process, protocol, version, command, prompt, timeout,
  cancellation, closed-client, and subscription-overflow errors.

## Security defaults

The language SDKs default to:

```text
--permission deny
--thinking off
--no-session
--no-plugins
--no-mcp
--no-skills
--no-subagents
```

These defaults reduce authority but do not create a sandbox. Snow and any
enabled shell, plugin, MCP server, or subagent run with the user's OS
privileges.

Credentials should resolve from Snow's auth store or a caller-controlled
environment. The SDKs intentionally do not expose API-key command-line fields,
because process arguments may be visible to other local processes.

## Verification

```sh
# Python unit tests; real integration runs when SNOW_TEST_BINARY is set.
PYTHONPATH=sdk/python/src python3 -m unittest discover -s sdk/python/tests -v
python3 -m compileall -q sdk/python/src sdk/python/tests

# JavaScript type/unit/package checks; integration uses SNOW_TEST_BINARY.
(cd sdk/javascript && npm test && npm run pack:check)

# Build one external runtime for both integration suites.
go build -o ./snow ./cmd/snow
SNOW_TEST_BINARY="$PWD/snow" PYTHONPATH=sdk/python/src \
  python3 -m unittest discover -s sdk/python/tests -v
(cd sdk/javascript && SNOW_TEST_BINARY="$PWD/../../snow" npm run test:integration)
```

Linux and macOS CI run both SDKs, both real-binary integrations, package checks,
and runnable Python/JavaScript examples.

## Publication policy

PyPI/npm publishing and automatic binary downloads remain deliberately deferred.
The first published clients should pin a compatible RPC major version. If binary
downloading is added later, it must be opt-in, verify release checksums, support
offline/manual installation, and never silently upgrade the executable.
