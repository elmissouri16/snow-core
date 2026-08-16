# Python and JavaScript/TypeScript SDKs

Snow's non-Go SDKs are typed local clients for the same Go runtime used by the
CLI and Go SDK. They do not reimplement providers, tools, sessions,
permissions, or the agent loop.

```text
Python or Node host
        |  JSONL over private stdin/stdout pipes
        v
external snow --mode rpc process
        |
        v
Go agent runtime, providers, tools, sessions, goals, and subagents
```

This design keeps behavior and security fixes in one runtime. The Go SDK
remains the only in-process embedding surface.

> **Note:** The language SDKs and RPC protocol v1 are pre-alpha. The packages
> are checked in and tested but intentionally not published to PyPI or npm yet.

## On this page

- [Installation](#installation)
- [Quick start: Python](#quick-start-python)
- [Quick start: JavaScript and
  TypeScript](#quick-start-javascript-and-typescript)
- [Lifecycle](#lifecycle)
- [External binary policy](#external-binary-policy)
- [Security defaults](#security-defaults)
- [Events and streaming](#events-and-streaming)
- [Model-requested user input](#model-requested-user-input)
- [Error handling](#error-handling)
- [Running tests and CI conformance](#running-tests-and-ci-conformance)
- [Compatibility matrix](#compatibility-matrix)
- [Related documents](#related-documents)

## Installation

| Host | Package | Runtime dependencies | Minimum runtime |
|---|---|---|---|
| Python | [`sdk/python`](../sdk/python) (`snow_sdk`) | none | Python 3.9 |
| JavaScript and TypeScript | [`sdk/javascript`](../sdk/javascript) (`@snow-core/sdk`) | none | Node.js 22 |

Use the packages directly from the checkout; there are no published wheels or
npm packages yet.

## Quick start: Python

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

`SnowClient.start` waits for and validates `rpc_ready`. `prompt` first waits
for admission and then for the definitive `prompt_completed` frame. Event
iterators are independent and bounded; slow consumers receive an explicit
overflow error. Unknown event fields remain available through
`AgentEvent.raw`, and bounded `diagnostics` retain responses with unknown IDs
without crashing the reader.

The Python client also exposes `request`, `abort`, `session_info`,
`session_rename`, `branch_fork`, `session_fork`, `session_worktree_fork`,
`models`, `subagent_models`, `set_model`, `set_thinking`, `set_mode`, `steer`,
`follow_up`, the `goal_*` and `subagent_*` command families, and
`reply_user_input`/`reject_user_input`. `prompt` accepts an optional `mode`
(`default` or `plan`); a timeout aborts the run and consumes its terminal
completion before raising `SnowTimeoutError`. Event iterator capacity is
configurable.

## Quick start: JavaScript and TypeScript

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
uses a separate bounded queue per callback, supports async listeners, and
reports overflow or listener failures through `onError` plus bounded
diagnostics. Every subscriber receives an isolated payload copy. `AbortSignal`
can stop an iterator, request cancellation of an active prompt, or terminate
the owned process.

The public declarations include RPC responses, session and model data, events,
user input, and the SDK error hierarchy. The command surface includes
`request`, `abort`, `sessionInfo`, `sessionRename`, `branchFork`, `sessionFork`,
`sessionWorktreeFork`, `models`, `subagentModels`, model and mode setters,
`steer`/`followUp`, the `goal*` and `subagent*` method families, and
`replyUserInput`/`rejectUserInput`. `prompt` accepts a `mode` option (`default`
or `plan`); timeout handling aborts and consumes terminal completion before
raising `SnowTimeoutError`. Iterator and subscription capacities are
configurable.

## Lifecycle

Every process starts with a first frame similar to:

```json
{
  "type": "rpc_ready",
  "protocol_version": "1",
  "snow_version": "0.1.0-dev",
  "capabilities": [
    "active_input",
    "branch_management",
    "goals",
    "models_list",
    "prompt_completion",
    "session_forks",
    "session_info",
    "subagent_models",
    "subagents",
    "user_input"
  ],
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
exact child-capable provider and model pairs plus runtime enablement metadata.

Canonical Draft 2020-12 schemas live under
[`pkg/protocol/schema/rpc/v1`](../pkg/protocol/schema/rpc/v1). Go tests resolve
all references without network access and validate representative public DTOs.
Language clients preserve unknown additive fields for forward compatibility.

Both clients default to a 10 s startup timeout, a 120 s request timeout, a 5 s
close timeout, a 16 MiB frame bound, and a 256-item event queue; each value is
configurable per client.

## External binary policy

Both clients require `snow` on `PATH` or an explicit executable path. Neither
package downloads, installs, upgrades, or embeds a Snow binary.

PyPI and npm publishing and automatic binary downloads remain deliberately
deferred. The first published clients should pin a compatible RPC major
version. If binary downloading is added later, it must be opt-in, verify
release checksums, support offline or manual installation, and never silently
upgrade the executable.

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

## Events and streaming

Both clients stream the same normalized `AgentEvent` objects described in the
[event stream](rpc.md#event-stream) section of the RPC guide.

Python uses `snow.events()`, an independent bounded async iterator. Its
capacity is configurable; a slow consumer receives an explicit overflow error
rather than a stalled transport. Unknown event fields stay available through
`AgentEvent.raw`, and bounded `diagnostics` retain responses with unknown IDs
without crashing the reader.

JavaScript provides two observation styles. `Snow.events()` is an independent
bounded async iterator with configurable capacity. `subscribe()` uses a
separate bounded queue per callback, supports async listeners, and reports
overflow or listener failures through `onError` plus bounded diagnostics.
Every subscriber receives an isolated payload copy, so one listener cannot
mutate another listener's event.

## Model-requested user input

Both clients can either consume `user_input_request` directly or install an
async handler. Python accepts `user_input_handler=` in `SnowClient.start`;
JavaScript accepts `userInputHandler` in `Snow.start` and calls it as
`(request, {signal})`. The handler receives the `user_input` object (`id`,
optional `tool_call_id`, and `questions`) and must return:

```json
{"answers": [{"id": "<question-id>", "answer": "<string>"}]}
```

The result must cover exactly the requested question IDs with string values.
The event is published to observers before the handler runs. A valid result
sends `user_input_reply`; validation or handler failure sends
`user_input_reject`.

This channel answers model questions only. It never approves tool permissions.
RPC permission mode `ask` remains fail-closed.

## Error handling

Both clients:

- invoke the executable directly without a shell;
- keep stderr separate from protocol stdout and retain only a bounded tail;
- bound input and output frames and event queues;
- serialize writes and correlate out-of-order responses by ID;
- reject pending operations if the child exits or emits invalid JSON or UTF-8;
- close stdin for orderly shutdown, then terminate and kill after bounded
  waits;
- provide distinct process, protocol, version, command, prompt, timeout,
  cancellation, closed-client, and subscription-overflow errors.

Python exposes these through a `SnowError` hierarchy: `SnowClosedError`,
`SnowProcessError`, `SnowProtocolError`, `SnowVersionError`,
`SnowCommandError`, `SnowPromptError`, `SnowTimeoutError`,
`SnowCancelledError`, and `SnowSubscriptionOverflowError`. JavaScript exports
the same hierarchy from `@snow-core/sdk`.

## Running tests and CI conformance

```sh
# Python unit tests; real integration runs when SNOW_TEST_BINARY is set.
PYTHONPATH=sdk/python/src python3 -m unittest discover -s sdk/python/tests -v
python3 -m compileall -q sdk/python/src sdk/python/tests

# JavaScript type, unit, and package checks; integration uses SNOW_TEST_BINARY.
(cd sdk/javascript && npm test && npm run pack:check)

# Build one external runtime for both integration suites.
go build -o ./snow ./cmd/snow
SNOW_TEST_BINARY="$PWD/snow" PYTHONPATH=sdk/python/src \
  python3 -m unittest discover -s sdk/python/tests -v
(cd sdk/javascript && SNOW_TEST_BINARY="$PWD/../../snow" npm run test:integration)
```

Linux and macOS CI run both SDKs, both real-binary integrations, package
checks, and runnable Python and JavaScript examples.

## Compatibility matrix

| Language | Package and import | Minimum runtime | RPC version | Publication state |
|---|---|---|---|---|
| Python | `snow_sdk` (`sdk/python`) | Python 3.9 | protocol v1 | checked in, not on PyPI |
| JavaScript and TypeScript | `@snow-core/sdk` (`sdk/javascript`) | Node.js 22 | protocol v1 | checked in, not on npm |

Both clients require the `prompt_completion` and `session_info` capabilities
announced by `rpc_ready` and reject a Snow binary that lacks them.

## Related documents

- [Go SDK](sdk.md)
- [JSONL RPC](rpc.md)
- [Security model](security.md)
- [Using Snow](using-snow.md)
- [Sessions](sessions.md)
