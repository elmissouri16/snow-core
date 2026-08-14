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

## Development

From the repository root:

```sh
PYTHONPATH=sdk/python/src python3 -m unittest discover -s sdk/python/tests -v
python3 -m compileall -q sdk/python/src sdk/python/tests
```

Set `SNOW_TEST_BINARY=/path/to/snow` to enable the real-binary integration test.
