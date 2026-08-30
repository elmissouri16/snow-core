# Snow Python SDK

`snow-core-sdk` is a dependency-free asynchronous Python 3.9+ client for a local
Snow process. The Go binary remains the runtime; this package starts
`snow --mode rpc`, validates the protocol-v1 handshake, correlates responses,
and exposes normalized events.

> Pre-alpha: package publication is intentionally deferred. Use this checked-in
> package through `PYTHONPATH=sdk/python/src` while the RPC contract stabilizes.

## Requirements

- Python 3.9 or newer.
- A compatible `snow` executable on `PATH`, or an explicit command tuple.

The SDK does not download or embed a binary.

## Minimal example

```python
import asyncio

from snow_sdk import SnowClient, SnowOptions


async def main():
    options = SnowOptions(
        command=("snow",),
        provider="opencode-go",
        cwd="/path/to/project",
    )
    async with await SnowClient.start(options) as snow:
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

`prompt()` waits for the definitive `prompt_completed` RPC frame. It does not
mistake the agent's earlier `turn_done` event for transport-level success.

## Safe defaults

`SnowOptions` defaults to permission `deny`, thinking `off`, an ephemeral
session, and disabled plugins, MCP, skills, and subagents. Snow and any enabled
tool still run with the user's OS privileges; RPC is not a sandbox.

Credentials resolve through Snow's auth file or inherited environment. The SDK
deliberately has no API-key command-line option because process arguments can be
visible to other local processes.

## Discovery and raw commands

```python
info = await snow.session_info()
models = await snow.models()
child_models = await snow.subagent_models()
response = await snow.request("goal_get")
```

Unknown additive event fields are retained in `AgentEvent.raw`. Independent
event iterators use bounded queues and fail explicitly if a consumer cannot
keep up.


When the binary announces `multimodal_prompts`, `prompt` accepts an additive
`content` list of `{"type": "text"/"image", ...}` blocks. The legacy
`message` argument stays valid; an empty `message` is allowed only when an
image block is present.

## RPC parity batch

The client exposes idiomatic wrappers for the runtime parity commands:

```python
compaction = await snow.compact()
branches = await snow.branches_list()
await snow.branch_select("branch-1")
await snow.branch_rename("branch-1", "feature")
await snow.branch_delete("branch-1")
messages = await snow.messages_list()
usage = await snow.usage()
pending = await snow.pending_inputs()
cleared = await snow.pending_inputs_clear()
diagnostics = await snow.configuration_diagnostics()   # command: "diagnostics"
debug = await snow.debug_status()
await snow.debug_enable()
await snow.debug_clear()
dump = await snow.debug_dump()                          # optional destination path
await snow.debug_disable()
await snow.goal_set("Ship the RPC client")              # goal_create compatibility alias
await snow.set_reasoning_summary("concise")            # off|auto|concise|detailed
await snow.set_text_verbosity("high")                  # low|medium|high
```

`configuration_diagnostics()` is named to avoid colliding with the existing
client-side `snow.diagnostics` transport-log attribute; it sends the
`diagnostics` RPC command and returns the server's configuration warnings.

Typed response helpers are available for the richer payloads and accept the
raw response dictionary from the matching method:

```python
from snow_sdk import (
    BranchesList, CompactionResult, DebugDumpResult, DebugStatus,
    DiagnosticsList, MessagesList, PendingInputs, UsageCost, UsageSnapshot,
)

result = CompactionResult.from_response(await snow.compact())
branches = BranchesList.from_response(await snow.branches_list())
messages = MessagesList.from_response(await snow.messages_list())
usage = UsageSnapshot.from_response(await snow.usage())  # includes cache_read_known and UsageCost
pending = PendingInputs.from_response(await snow.pending_inputs())
diagnostics = DiagnosticsList.from_response(await snow.configuration_diagnostics())
debug = DebugStatus.from_response(await snow.debug_status())
dump = DebugDumpResult.from_response(await snow.debug_dump())
```

All wrappers return the raw response dict, preserve unknown additive fields,
and remain dependency-free. Before using these methods with an older Snow
binary, inspect `snow.ready.capabilities` for `compaction`,
`branch_management`, `messages_list`, `usage`, `pending_inputs`, `diagnostics`,
`debug_diagnostics`, and `response_controls` as applicable.

## Interactive permissions

Permission mode `ask` remains fail-closed unless a trusted host installs a
handler or manually replies to events:

```python
async def decide(request):
    return "allow_session"  # allow|allow_session|allow_always|deny

snow = await SnowClient.start(
    SnowOptions(permission="ask"), permission_handler=decide,
)
```

The correlated `permission_request` event is published before the handler runs.
Handler errors or invalid decisions send `permission_reject`. Event-loop hosts
can omit the handler, observe the event, and call
`reply_permission(request_id, decision)` or `reject_permission(request_id)`
manually. Without either a handler or a manual reply, the request remains
pending until the prompt is canceled or the process closes; it is never
implicitly approved.

Failed RPC commands raise `SnowCommandError`. Its `error_code` property retains
Snow's stable machine-readable code when present, while `response` (also
available as `raw`) retains the complete additive failure frame.

## Development

From the repository root:

```sh
PYTHONPATH=sdk/python/src python3 -m unittest discover -s sdk/python/tests -v
python3 -m compileall -q sdk/python/src sdk/python/tests
```

Set `SNOW_TEST_BINARY=/path/to/snow` to enable the real-binary integration test.
