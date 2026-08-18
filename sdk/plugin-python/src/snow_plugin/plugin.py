"""Snow protocol-v2 plugin authoring surface.

Defines the `Plugin` manifest+tools object, the decorator tool registration
API, lifecycle hooks, event handlers, and the `run()` entry point. Keep this
module free of wire-level protocol details; the runtime in `runtime.py` owns
framing and dispatch.
"""

from __future__ import annotations

import re
from typing import Any, Callable, Dict, List, Optional, Tuple

from .errors import ToolError

__all__ = ["Plugin", "ToolError", "ToolContext", "error_result", "result", "text_result"]

_ID_RE = re.compile(r"^[a-z0-9][a-z0-9_-]{0,63}$")
_TOOL_RE = re.compile(r"^[a-z][a-z0-9_-]{0,127}$")
_RISKS = ("read", "write", "exec", "network")
_DISCOVERY_MODES = ("always", "deferred")

Handler = Callable[..., Any]


def _validate_id(plugin_id: str) -> None:
    if not isinstance(plugin_id, str) or not _ID_RE.match(plugin_id):
        raise ValueError("plugin id must match ^[a-z0-9][a-z0-9_-]{0,63}$")


def _validate_name(name: str) -> None:
    if not isinstance(name, str) or not name.strip():
        raise ValueError("plugin name must be a non-empty string")


def _validate_version(version: str) -> None:
    if not isinstance(version, str) or not version.strip():
        raise ValueError("plugin version must be a non-empty string")


class Tool:
    """A declared Snow tool descriptor and handler."""

    name: str
    description: str
    risk: str
    parameters: Dict[str, Any]
    capabilities: List[str]
    discovery: Optional[Dict[str, Any]]
    handler: Handler
    private_config: Any

    def __init__(
        self,
        name: str,
        description: str,
        handler: Handler,
        *,
        risk: str = "exec",
        parameters: Optional[Dict[str, Any]] = None,
        capabilities: Optional[List[str]] = None,
        discovery: Optional[Dict[str, Any]] = None,
        private_config: Any = None,
    ) -> None:
        if not isinstance(name, str) or not _TOOL_RE.match(name):
            raise ValueError("tool name must match ^[a-z][a-z0-9_-]{0,127}$")
        if not isinstance(description, str) or not description.strip():
            raise ValueError("tool description must be a non-empty string")
        if risk not in _RISKS:
            raise ValueError(f"tool risk must be one of {_RISKS}")
        params = parameters if parameters is not None else {
            "type": "object",
            "properties": {},
            "additionalProperties": False,
        }
        if not isinstance(params, dict) or params.get("type") != "object":
            raise ValueError("tool parameters must be a JSON Schema object")
        if not callable(handler):
            raise ValueError("tool handler must be callable")
        self.name = name
        self.description = description
        self.risk = risk
        self.parameters = params
        self.capabilities = list(capabilities or [])
        self.discovery = discovery
        self.handler = handler
        self.private_config = private_config

    def descriptor(self) -> Dict[str, Any]:
        descriptor: Dict[str, Any] = {
            "name": self.name,
            "description": self.description,
            "risk": self.risk,
            "parameters": self.parameters,
        }
        if self.capabilities:
            descriptor["capabilities"] = list(self.capabilities)
        if self.discovery is not None:
            descriptor["discovery"] = self.discovery
        return descriptor


class Plugin:
    """A Snow protocol-v2 plugin.

    Use the constructor with the decorator API::

        plugin = Plugin(plugin_id="example", name="Example", version="1.0.0")

        @plugin.tool(name="echo", description="Echo text", risk="read",
                     parameters={...})
        async def echo(arguments, context): ...

    then `plugin.run()` to serve requests from stdin/stdout.
    """

    plugin_id: str
    name: str
    version: str
    capabilities: List[str]
    supported_events: List[str]
    allowed_tools: List[str]

    def __init__(
        self,
        plugin_id: str,
        name: str,
        version: str,
        *,
        capabilities: Optional[List[str]] = None,
        supported_events: Optional[List[str]] = None,
        allowed_tools: Optional[List[str]] = None,
    ) -> None:
        _validate_id(plugin_id)
        _validate_name(name)
        _validate_version(version)
        for item in capabilities or []:
            if not isinstance(item, str) or not item:
                raise ValueError("plugin capabilities must be strings")
        for item in supported_events or []:
            if not isinstance(item, str) or not item:
                raise ValueError("supported_events must be strings")
        self.plugin_id = plugin_id
        self.name = name
        self.version = version
        self.capabilities = list(capabilities or [])
        self.supported_events = list(supported_events or [])
        self.allowed_tools = list(allowed_tools or [])
        self._tools: Dict[str, Tool] = {}
        self._setup: Optional[Handler] = None
        self._shutdown: Optional[Handler] = None
        self._events: Dict[str, Handler] = {}
        self._state: Any = None

    # -- lifecycle hooks -------------------------------------------------

    def on_setup(self, fn: Handler) -> Handler:
        """Register a setup hook receiving ``(context, config)``."""
        if not callable(fn):
            raise TypeError("on_setup requires a callable")
        self._setup = fn
        return fn

    def on_shutdown(self, fn: Handler) -> Handler:
        """Register a shutdown hook receiving ``(context, config)``."""
        if not callable(fn):
            raise TypeError("on_shutdown requires a callable")
        self._shutdown = fn
        return fn

    def on_event(self, event_type: str, fn: Handler) -> None:
        """Register a handler for the given subscribed event type."""
        if not isinstance(event_type, str) or not event_type:
            raise TypeError("event type must be a non-empty string")
        if not callable(fn):
            raise TypeError("on_event requires a callable")
        if event_type not in self.supported_events:
            self.supported_events.append(event_type)
        self._events[event_type] = fn

    def event_handlers(self) -> Dict[str, Handler]:
        return dict(self._events)

    # -- tool registration ----------------------------------------------

    def tool(
        self,
        name: str,
        description: str,
        *,
        risk: str = "exec",
        parameters: Optional[Dict[str, Any]] = None,
        capabilities: Optional[List[str]] = None,
        discovery: Optional[Dict[str, Any]] = None,
        resolve: Optional[Callable[[str], str]] = None,
    ) -> Callable[[Handler], Handler]:
        """Register a tool handler. Supports sync and async functions.

        ``resolve`` is an optional private callback mapping handler names to
        immutable values resolved at registration time (for example a path).
        Use it instead of importing private configuration into handlers.
        """

        def decorator(handler_value: Handler) -> Handler:
            if not callable(handler_value):
                raise TypeError("plugin.tool requires a callable handler")
            tool_name = name
            if tool_name in self._tools:
                raise ValueError(f"duplicate tool name {tool_name!r}")
            private_config = self._resolve_private(resolve, tool_name)
            self._tools[tool_name] = Tool(
                tool_name,
                description,
                handler_value,
                risk=risk,
                parameters=parameters,
                capabilities=capabilities,
                discovery=discovery,
                private_config=private_config,
            )
            return handler_value

        return decorator

    def _resolve_private(
        self, resolve: Optional[Callable[[str], str]], name: str
    ) -> Any:
        if resolve is None:
            return None
        try:
            return resolve(name)
        except Exception as exc:
            raise RuntimeError(
                f"tool {name!r} resolver failed: {type(exc).__name__}"
            ) from None

    # -- helpers ---------------------------------------------------------

    def tool_names(self) -> List[str]:
        return list(self._tools)

    def manifest(self) -> Dict[str, Any]:
        return {
            "id": self.plugin_id,
            "name": self.name,
            "version": self.version,
            "protocol_version": 2,
        }

    def tools_list(self) -> Dict[str, Any]:
        return {"tools": [tool.descriptor() for tool in self._tools.values()]}

    def get_tool(self, name: str) -> Optional[Tool]:
        return self._tools.get(name)

    def _ensure_valid(self) -> None:
        if not self._tools:
            raise ValueError("plugin must declare at least one tool")
        for tool in self._tools.values():
            if tool.discovery is not None:
                if not isinstance(tool.discovery, dict):
                    raise ValueError("tool discovery must be an object")
                mode = tool.discovery.get("mode", "always")
                if mode not in _DISCOVERY_MODES:
                    raise ValueError(
                        f"tool {tool.name!r} discovery mode must be always or deferred"
                    )
            if not isinstance(tool.parameters, dict):
                raise ValueError("tool parameters must be an object")

    def run(self) -> None:
        """Serve the plugin from stdin/stdout using the async runtime."""
        from .runtime import serve

        self._ensure_valid()
        serve(self)


def serve_plugin(plugin: Plugin) -> None:
    """Serve ``plugin``; equivalent to ``plugin.run()`` but explicit."""
    plugin.run()
