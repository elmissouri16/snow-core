#!/usr/bin/env python3
"""Snow Python SDK template. Replace PLUGIN_ID and echo."""

import sys
from pathlib import Path

vendored_sdk = Path(__file__).resolve().parent / "vendor" / "python"
if not (vendored_sdk / "snow_plugin").is_dir():
    raise RuntimeError(
        "vendored snow-plugin SDK is unavailable; run `snow plugin sdk vendor "
        "--runtime python <plugin-directory>` before validation"
    )
sys.path.insert(0, str(vendored_sdk))

from snow_plugin import Plugin, text_result


plugin = Plugin(
    plugin_id="PLUGIN_ID",
    name="PLUGIN_ID generated plugin",
    version="0.1.0",
)


@plugin.tool(
    name="echo",
    description="Replace this example with the reusable capability.",
    parameters={
        "type": "object",
        "properties": {"text": {"type": "string"}},
        "required": ["text"],
        "additionalProperties": False,
    },
    risk="read",
)
async def echo(arguments, context):
    await context.progress("Preparing result")
    context.raise_if_cancelled()
    text = str(arguments["text"])
    return text_result(text, details={"length": len(text)})


if __name__ == "__main__":
    plugin.run()
