"""Async JSON-RPC 2.0 runtime for Snow protocol-v2 plugin subprocesses.

Owns framing, dispatch, progress/log writes, cancellation, deadlines,
concurrency bounds, event delivery, graceful shutdown, and stdout discipline.
User handlers must never write to stdout directly; route diagnostics to stderr.
"""

from __future__ import annotations

import asyncio
import inspect
import json
import os
import sys
import time
from typing import Any, Callable, Dict, List, Optional, Set

from .context import ToolContext
from .errors import ToolError
from .plugin import Plugin
from .protocol import (
    MAX_FRAME_BYTES,
    MAX_OUTPUT_BYTES,
    PROTOCOL_VERSION,
    encode_frame,
    parse_frame,
    validate_request,
)
from .results import text_result

MAX_ACTIVE_CALLS = 8
MAX_EVENT_QUEUE = 64
MAX_RESULT_BYTES = MAX_OUTPUT_BYTES


class _Writer:
    """Serialized protocol stdout writer."""

    def __init__(self) -> None:
        self._stream = sys.stdout.buffer
        self._lock = asyncio.Lock()

    async def write(self, payload: Dict[str, Any]) -> None:
        encoded = encode_frame(payload)
        async with self._lock:
            self._stream.write(encoded)
            self._stream.flush()


class _HostContext:
    """Immutable context handed to setup/shutdown/event hooks."""

    __slots__ = ("cwd", "session_id", "host_version", "host_capabilities", "config")

    def __init__(
        self,
        *,
        cwd: str,
        session_id: str,
        host_version: str,
        host_capabilities: List[str],
        config: Any,
    ) -> None:
        self.cwd = cwd
        self.session_id = session_id
        self.host_version = host_version
        self.host_capabilities = host_capabilities
        self.config = config


class _Runtime:
    def __init__(self, plugin: Plugin) -> None:
        self.plugin = plugin
        self.writer = _Writer()
        self.semaphore = asyncio.Semaphore(MAX_ACTIVE_CALLS)
        self.active_tasks: Set[asyncio.Task[Any]] = set()
        self.request_tasks: Dict[str, asyncio.Task[Any]] = {}
        self.call_tasks: Dict[str, asyncio.Task[Any]] = {}
        self.event_queue: asyncio.Queue[Any] = asyncio.Queue(maxsize=MAX_EVENT_QUEUE)
        self.closing = False
        self.setup_done = False
        self.host_config: Any = None
        self.event_drop_reported = False

    # -- output helpers --------------------------------------------------

    async def _respond_result(self, request_id: str, result: Any) -> None:
        await self.writer.write(
            {"jsonrpc": "2.0", "id": request_id, "result": result}
        )

    async def _respond_error(
        self, request_id: Optional[str], code: int, message: str
    ) -> None:
        if request_id is None:
            return  # notifications never receive responses
        bounded = str(message)[:4096]
        await self.writer.write(
            {
                "jsonrpc": "2.0",
                "id": request_id,
                "error": {"code": code, "message": bounded},
            }
        )

    # -- dispatch ---------------------------------------------------------

    async def _handle_request(self, message: Dict[str, Any]) -> None:
        method = message.get("method")
        params = message.get("params")
        params = params if isinstance(params, dict) else {}
        request_id = message.get("id")
        is_notification = request_id is None

        if is_notification:
            if method == "notifications/cancelled":
                call_id = str(params.get("call_id") or "")
                req_id = str(params.get("request_id") or "")
                task = self.request_tasks.get(req_id) or self.call_tasks.get(call_id)
                if task is not None:
                    task.cancel()
            elif method == "notifications/event":
                event_type = params.get("type")
                if isinstance(event_type, str) and event_type in self.plugin.event_handlers():
                    try:
                        self.event_queue.put_nowait((event_type, params))
                    except asyncio.QueueFull:
                        if not self.event_drop_reported:
                            self.event_drop_reported = True
                            await self.writer.write(
                                {
                                    "jsonrpc": "2.0",
                                    "method": "notifications/log",
                                    "params": {
                                        "severity": "warning",
                                        "message": "event queue overflow; dropping best-effort events",
                                    },
                                }
                            )
            return

        if not isinstance(request_id, str):
            await self._respond_error(None, -32600, "invalid request id")
            return

        if method == "initialize":
            await self._handle_initialize(request_id, params)
        elif method == "tools/list":
            await self._respond_result(request_id, self.plugin.tools_list())
        elif method == "tools/call":
            await self._handle_tools_call(request_id, params)
        elif method == "shutdown":
            await self._handle_shutdown(request_id)
        else:
            await self._respond_error(
                request_id, -32601, f"unknown method {method}"
            )

    async def _handle_initialize(
        self, request_id: str, params: Dict[str, Any]
    ) -> None:
        protocol_version = params.get("protocol_version")
        if protocol_version != PROTOCOL_VERSION:
            await self._respond_error(
                request_id,
                -32600,
                f"unsupported protocol version {protocol_version!r}; "
                f"this runtime speaks {PROTOCOL_VERSION}",
            )
            return

        host_capabilities = params.get("host_capabilities")
        if not isinstance(host_capabilities, list):
            host_capabilities = []
        config = params.get("config")
        self.host_config = config

        manifest = self.plugin.manifest()
        if self.plugin.capabilities:
            manifest["capabilities"] = list(self.plugin.capabilities)
        if self.plugin.supported_events:
            manifest["supported_events"] = list(self.plugin.supported_events)
        if self.plugin.allowed_tools:
            manifest["allowed_tools"] = list(self.plugin.allowed_tools)

        context = _HostContext(
            cwd=str(params.get("cwd") or os.getcwd()),
            session_id=str(params.get("session_id") or ""),
            host_version=str(params.get("host_version") or ""),
            host_capabilities=[str(item) for item in host_capabilities],
            config=config,
        )

        if self.plugin._setup is not None:
            try:
                state = await _maybe_await(
                    self.plugin._setup, context, config
                )
            except Exception as exc:
                await self._respond_error(
                    request_id,
                    -32000,
                    f"plugin setup failed: {type(exc).__name__}",
                )
                return
            extra = {}
            if isinstance(state, tuple) and len(state) == 2:
                self.plugin._state, extra = state
            else:
                self.plugin._state = state
            if isinstance(extra, dict):
                manifest.update(extra)

        self.setup_done = True
        await self._respond_result(
            request_id,
            {
                "manifest": manifest,
                "capabilities": list(self.plugin.capabilities),
                "supported_events": list(self.plugin.supported_events),
                "limits": {
                    "max_active_calls": MAX_ACTIVE_CALLS,
                    "max_result_bytes": MAX_RESULT_BYTES,
                },
            },
        )

    async def _handle_tools_call(
        self, request_id: str, params: Dict[str, Any]
    ) -> None:
        tool_name = params.get("name")
        call_id = params.get("call_id")
        if not isinstance(tool_name, str) or not tool_name:
            await self._respond_error(
                request_id, -32602, "tools/call requires a string name"
            )
            return
        if not isinstance(call_id, str) or not call_id:
            await self._respond_error(
                request_id, -32602, "tools/call requires a non-empty call_id"
            )
            return
        arguments = params.get("arguments")
        if arguments is not None and not isinstance(arguments, dict):
            await self._respond_error(
                request_id, -32602, "tools/call arguments must be an object"
            )
            return
        tool = self.plugin.get_tool(tool_name)
        if tool is None:
            await self._respond_error(
                request_id, -32602, f"unknown tool {tool_name}"
            )
            return
        if request_id in self.request_tasks or call_id in self.call_tasks:
            await self._respond_error(request_id, -32602, "duplicate request or call id")
            return
        if len(self.request_tasks) >= MAX_ACTIVE_CALLS:
            await self._respond_error(request_id, -32000, "concurrency limit reached")
            return

        timeout_ms = 0
        try:
            timeout_ms = int(params.get("timeout_ms") or 0)
        except (TypeError, ValueError):
            timeout_ms = 0
        if timeout_ms < 0:
            timeout_ms = 0

        context = ToolContext(
            call_id=call_id,
            request_id=request_id,
            cwd=str(params.get("cwd") or os.getcwd()),
            session_id=str(params.get("session_id") or ""),
            deadline=(time.time() + timeout_ms / 1000.0) if timeout_ms > 0 else None,
            config=self.host_config,
            state=self.plugin._state,
            _write=self.writer.write,
        )

        async def invoke() -> None:
            async with self.semaphore:
                try:
                    result = await self._run_tool(
                        tool, arguments or {}, context, timeout_ms
                    )
                    await self._respond_result(request_id, result)
                except asyncio.CancelledError:
                    # Snow has stopped waiting; a late response is ignored.
                    raise
                except ToolError as exc:
                    await self._respond_result(
                        request_id,
                        text_result(str(exc), is_error=True),
                    )
                except asyncio.TimeoutError:
                    await self._respond_error(
                        request_id, -32000, "tool timed out"
                    )
                except Exception as exc:
                    await self._respond_error(
                        request_id, -32000, f"tool failed: {type(exc).__name__}"
                    )

        task = asyncio.create_task(invoke())
        self.request_tasks[request_id] = task
        self.call_tasks[call_id] = task
        self.active_tasks.add(task)

        def done(completed: asyncio.Task[Any]) -> None:
            self.active_tasks.discard(completed)
            self.request_tasks.pop(request_id, None)
            self.call_tasks.pop(call_id, None)

        task.add_done_callback(done)

    async def _run_tool(
        self,
        tool: Any,
        arguments: Dict[str, Any],
        context: ToolContext,
        timeout_ms: int,
    ) -> Dict[str, Any]:
        result = _maybe_await(tool.handler, arguments, context)
        if timeout_ms > 0:
            result = asyncio.wait_for(result, timeout=timeout_ms / 1000.0)
        return _bounded_result(_validate_result(await result))

    async def _handle_shutdown(self, request_id: str) -> None:
        self.closing = True
        active_calls = list(self.request_tasks.values())
        for task in active_calls:
            task.cancel()
        if active_calls:
            await asyncio.wait(active_calls, timeout=1.0)
        if self.plugin._shutdown is not None and self.setup_done:
            try:
                await asyncio.wait_for(
                    _maybe_await(
                        self.plugin._shutdown,
                        _HostContext(
                            cwd=str(os.getcwd()),
                            session_id="",
                            host_version="",
                            host_capabilities=[],
                            config=self.host_config,
                        ),
                        self.plugin._state,
                    ),
                    timeout=1.0,
                )
            except Exception:
                # Shutdown hooks are best-effort; never fail the shutdown response.
                pass
        await self._respond_result(request_id, {})
        self.closing = True

    async def _dispatch_event(
        self, event_type: str, params: Dict[str, Any]
    ) -> None:
        handler = self.plugin.event_handlers().get(event_type)
        if handler is None:
            return
        try:
            await _maybe_await(handler, params, self.plugin._state)
        except Exception:
            # Event handlers are observation-only and must not break the loop.
            pass

    async def _event_loop(self) -> None:
        while True:
            event_type, params = await self.event_queue.get()
            try:
                await self._dispatch_event(event_type, params)
            finally:
                self.event_queue.task_done()

    # -- main loop --------------------------------------------------------

    async def run(self) -> None:
        event_task = asyncio.create_task(self._event_loop())
        while not self.closing:
            try:
                line = await asyncio.to_thread(
                    sys.stdin.buffer.readline, MAX_FRAME_BYTES + 2
                )
            except Exception:
                break
            if not line:
                break
            if len(line) > MAX_FRAME_BYTES + 1:
                while line and not line.endswith(b"\n"):
                    line = await asyncio.to_thread(
                        sys.stdin.buffer.readline, MAX_FRAME_BYTES + 2
                    )
                await self._respond_error(None, -32700, "input frame exceeds bound")
                continue
            if not line.strip():
                continue
            try:
                message = parse_frame(line)
            except ValueError as exc:
                await self._respond_error(None, -32700, str(exc))
                continue
            validation_error = validate_request(message)
            if validation_error:
                request_id = message.get("id")
                if not isinstance(request_id, str):
                    request_id = None
                await self._respond_error(request_id, -32600, validation_error)
                continue

            task = asyncio.create_task(self._handle_request(message))
            self.active_tasks.add(task)

            def done(completed: asyncio.Task[Any]) -> None:
                self.active_tasks.discard(completed)

            task.add_done_callback(done)

        # Drain remaining active work with a bounded grace period.
        pending = [task for task in self.active_tasks if not task.done()]
        if pending:
            _, still_pending = await asyncio.wait(pending, timeout=1.0)
            for task in still_pending:
                task.cancel()
            if still_pending:
                await asyncio.wait(still_pending, timeout=0.25)
        event_task.cancel()
        await asyncio.gather(event_task, return_exceptions=True)


def _maybe_await(value: Callable[..., Any], *args: Any) -> Any:
    """Invoke ``value(*args)`` and await it if it returns an awaitable.

    Returns a coroutine for awaitable results and the raw value otherwise, so
    callers can uniformly ``await _maybe_await(...)``.
    """
    result = value(*args)
    if inspect.isawaitable(result):
        return asyncio.ensure_future(result)
    async def _completed() -> Any:
        return result
    return _completed()


def _bounded_result(result: Dict[str, Any]) -> Dict[str, Any]:
    try:
        encoded = json.dumps(result, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
    except (TypeError, ValueError):
        return text_result("tool result is not JSON serializable", is_error=True)
    if len(encoded) > MAX_RESULT_BYTES:
        return text_result("tool result exceeds the configured output limit", is_error=True)
    return result


def _validate_result(result: Any) -> Dict[str, Any]:
    """Normalize and validate a handler return value into a tool result."""
    if result is None:
        return text_result("tool returned no result", is_error=True)
    if isinstance(result, dict):
        if "content" in result:
            content = result.get("content")
            details = result.get("details", {})
            if not isinstance(content, list) or not content:
                return text_result("tool result content must be a non-empty list", is_error=True)
            if not isinstance(details, dict):
                return text_result("tool result details must be an object", is_error=True)
            for block in content:
                if not isinstance(block, dict) or not isinstance(block.get("type"), str) or not block.get("type"):
                    return text_result("tool result contains an invalid content block", is_error=True)
            return {
                "content": content,
                "details": details,
                "is_error": bool(result.get("is_error", False)),
            }
        text = result.get("text")
        if not isinstance(text, str) or not text.strip():
            return text_result("tool result object requires a non-empty text field", is_error=True)
        details = result.get("details")
        if details is not None and not isinstance(details, dict):
            return text_result("tool result details must be an object", is_error=True)
        return text_result(text, details=details)
    if isinstance(result, str):
        if not result.strip():
            return text_result("tool returned empty text", is_error=True)
        return text_result(result)
    return text_result(f"unexpected tool result type {type(result).__name__}", is_error=True)


def serve(plugin: Plugin) -> None:
    """Serve ``plugin`` from stdin/stdout until shutdown or EOF.

    The protocol writer captures the original stdout buffer, then common
    ``print()`` diagnostics are redirected to stderr for the runtime lifetime.
    Direct writes to a previously captured stdout handle remain unsupported.
    """
    plugin._ensure_valid()
    runtime = _Runtime(plugin)
    original_stdout = sys.stdout
    sys.stdout = sys.stderr
    try:
        asyncio.run(runtime.run())
    finally:
        sys.stdout = original_stdout
