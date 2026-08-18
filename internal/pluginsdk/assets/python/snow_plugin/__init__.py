"""Snow protocol-v2 plugin authoring SDK (Python).

Private, local, unpublished package for writing Snow plugins. This package is
distinct from the ``snow-core-sdk`` RPC client that controls a Snow process.
"""

from .context import ToolContext
from .errors import ToolError
from .plugin import Plugin, serve_plugin
from .results import error_result, result, text_result

__all__ = [
    "Plugin",
    "ToolContext",
    "ToolError",
    "error_result",
    "result",
    "serve_plugin",
    "text_result",
]
