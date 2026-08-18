# snow-plugin (private, unpublished)

Zero-dependency Python SDK for authoring Snow protocol-v2 plugins. This package
is **not published**; it lives in the Snow repository for local development.

The wire contract is owned by `docs/plugin-protocol.md`. stdout belongs
exclusively to JSON-RPC frames; route diagnostics to stderr (the SDK routes
ordinary `print()` to stderr automatically).

## Usage

```python
from snow_plugin import Plugin, text_result

plugin = Plugin(plugin_id="example-python", name="Example", version="1.0.0")


@plugin.tool(
    name="echo",
    description="Echo text",
    risk="read",
    parameters={
        "type": "object",
        "properties": {"text": {"type": "string"}},
        "required": ["text"],
        "additionalProperties": False,
    },
)
async def echo(args, ctx):
    await ctx.progress("echoing")  # async context methods
    return text_result(args["text"], details={"length": len(args["text"])})


if __name__ == "__main__":
    plugin.run()
```

`plugin.run()` inserts protocol version 2 automatically, owns framing/dialog
with the Snow host, and supports cancellation, deadlines, progress, logging,
events, lifecycle hooks, and bounded concurrency. Tool contexts keep the
private host `config` separate from setup-derived `state`.

## Development

```sh
PYTHONPATH=sdk/plugin-python/src \
  python3 -m unittest discover -s sdk/plugin-python/tests -v
python3 -m compileall -q sdk/plugin-python/src sdk/plugin-python/tests
# Optional local package check; no publication:
python3 -m pip wheel --no-deps sdk/plugin-python
```
