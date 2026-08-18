#!/usr/bin/env python3
"""Dependency-free snow-plugin SDK example (protocol v2).

Run with: snow --plugin examples/plugins/python-sdk/manifest.json
"""

import asyncio
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT / "sdk" / "plugin-python" / "src"))

from snow_plugin import Plugin, text_result

plugin = Plugin(
    plugin_id="example-python-sdk",
    name="Snow Python SDK example",
    version="1.0.0",
)
plugin.on_event("tool_end", lambda _event, _context: None)


@plugin.tool(
    name="echo",
    description="Echo text after an optional delay",
    risk="read",
    parameters={
        "type": "object",
        "properties": {
            "text": {"type": "string"},
            "delay_ms": {"type": "integer", "minimum": 0, "maximum": 5000},
        },
        "required": ["text"],
        "additionalProperties": False,
    },
)
async def echo(args, ctx):
    await ctx.progress("Preparing echo")
    delay_ms = max(0, min(5000, int(args.get("delay_ms", 0))))
    if delay_ms:
        await asyncio.sleep(delay_ms / 1000)
    ctx.raise_if_cancelled()
    return text_result(
        str(args["text"]),
        details={"runtime": "python-sdk", "length": len(str(args["text"]))},
    )


if __name__ == "__main__":
    plugin.run()
