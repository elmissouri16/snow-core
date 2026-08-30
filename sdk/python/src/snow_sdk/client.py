"""Asynchronous, dependency-free client for Snow's local JSONL RPC mode."""

from __future__ import annotations

import asyncio
import copy
import json
import os
import uuid
from dataclasses import dataclass
from typing import Any, Awaitable, Callable, Dict, Iterable, Mapping, Optional, Set

from .errors import (
    SnowCancelledError,
    SnowClosedError,
    SnowCommandError,
    SnowProcessError,
    SnowPromptError,
    SnowProtocolError,
    SnowSubscriptionOverflowError,
    SnowTimeoutError,
    SnowVersionError,
)
from .options import SnowOptions
from .types import AgentEvent, JSONDict, PromptResult, RPCReady

PROTOCOL_VERSION = "1"
_REQUIRED_CAPABILITIES = {"prompt_completion", "session_info"}
_MAX_STDERR_BYTES = 64 * 1024


@dataclass
class _SubscriptionEnd:
    error: Optional[BaseException] = None


class EventSubscription:
    """Independent bounded asynchronous view of agent events."""

    def __init__(self, client: "SnowClient", capacity: int):
        self._client = client
        self._queue: asyncio.Queue[Any] = asyncio.Queue(maxsize=capacity)
        self._closed = False

    def __aiter__(self) -> "EventSubscription":
        return self

    async def __anext__(self) -> AgentEvent:
        item = await self._queue.get()
        if isinstance(item, _SubscriptionEnd):
            self._closed = True
            if item.error is not None:
                raise item.error
            raise StopAsyncIteration
        return item

    def close(self) -> None:
        self._client._remove_subscription(self)
        self._finish(None)

    def _push(self, event: AgentEvent) -> None:
        if self._closed:
            return
        isolated = AgentEvent(type=event.type, raw=copy.deepcopy(event.raw))
        try:
            self._queue.put_nowait(isolated)
        except asyncio.QueueFull:
            self._client._remove_subscription(self)
            self._finish(SnowSubscriptionOverflowError("event subscription queue overflow"))

    def _finish(self, error: Optional[BaseException]) -> None:
        if self._closed:
            return
        self._closed = True
        while self._queue.full():
            self._queue.get_nowait()
        self._queue.put_nowait(_SubscriptionEnd(error))


UserInputHandler = Callable[[JSONDict], Awaitable[JSONDict]]
PermissionHandler = Callable[[JSONDict], Awaitable[str]]


class SnowClient:
    """Own one persistent ``snow --mode rpc`` subprocess."""

    def __init__(
        self,
        options: SnowOptions,
        user_input_handler: Optional[UserInputHandler],
        permission_handler: Optional[PermissionHandler],
    ):
        self.options = options
        self.user_input_handler = user_input_handler
        self.permission_handler = permission_handler
        self.ready: Optional[RPCReady] = None
        self._process: Optional[asyncio.subprocess.Process] = None
        self._write_lock = asyncio.Lock()
        self._pending: Dict[str, asyncio.Future[JSONDict]] = {}
        self._prompts: Dict[str, asyncio.Future[PromptResult]] = {}
        self._abandoned_prompts: Set[str] = set()
        self._subscriptions: Set[EventSubscription] = set()
        self._handler_tasks: Set[asyncio.Task[Any]] = set()
        self._ready_future: asyncio.Future[RPCReady] = asyncio.get_running_loop().create_future()
        self._reader_task: Optional[asyncio.Task[Any]] = None
        self._stderr_task: Optional[asyncio.Task[Any]] = None
        self._wait_task: Optional[asyncio.Task[Any]] = None
        self._fatal_kill_handle: Optional[asyncio.TimerHandle] = None
        self._stderr_tail = bytearray()
        self._write_limit = options.max_frame_bytes
        self.diagnostics: list[JSONDict] = []
        self._failure: Optional[BaseException] = None
        self._closing = False
        self._closed = False

    @classmethod
    async def start(
        cls,
        options: Optional[SnowOptions] = None,
        *,
        user_input_handler: Optional[UserInputHandler] = None,
        permission_handler: Optional[PermissionHandler] = None,
    ) -> "SnowClient":
        options = options or SnowOptions()
        options.validate()
        client = cls(options, user_input_handler, permission_handler)
        env = os.environ.copy() if options.inherit_environment else {}
        if options.environment is not None:
            env.update(options.environment)
        argv = options.argv()
        try:
            client._process = await asyncio.create_subprocess_exec(
                *argv,
                cwd=options.cwd,
                env=env,
                stdin=asyncio.subprocess.PIPE,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                limit=options.max_frame_bytes + 1,
            )
        except OSError as exc:
            raise SnowProcessError(f"failed to start Snow executable: {exc}") from exc
        client._reader_task = asyncio.create_task(client._read_stdout(), name="snow-rpc-stdout")
        client._stderr_task = asyncio.create_task(client._read_stderr(), name="snow-rpc-stderr")
        client._wait_task = asyncio.create_task(client._wait_process(), name="snow-rpc-process")
        try:
            client.ready = await asyncio.wait_for(client._ready_future, options.startup_timeout)
        except asyncio.TimeoutError as exc:
            await client.close()
            raise SnowTimeoutError("timed out waiting for rpc_ready") from exc
        except BaseException:
            await client.close()
            raise
        return client

    async def __aenter__(self) -> "SnowClient":
        return self

    async def __aexit__(self, _exc_type: Any, _exc: Any, _tb: Any) -> None:
        await self.close()

    @property
    def closed(self) -> bool:
        return self._closed

    def events(self, capacity: Optional[int] = None) -> EventSubscription:
        self._ensure_open()
        actual_capacity = self.options.event_queue_size if capacity is None else capacity
        if not isinstance(actual_capacity, int) or actual_capacity <= 0:
            raise ValueError("capacity must be a positive integer")
        subscription = EventSubscription(self, actual_capacity)
        self._subscriptions.add(subscription)
        return subscription

    async def request(
        self,
        command: str,
        *,
        request_id: Optional[str] = None,
        timeout: Optional[float] = None,
        **fields: Any,
    ) -> JSONDict:
        self._ensure_open()
        request_id = request_id or self._new_id(command)
        if request_id in self._pending or (request_id in self._prompts and command != "prompt"):
            raise SnowProtocolError(f"duplicate active request id {request_id!r}")
        future: asyncio.Future[JSONDict] = asyncio.get_running_loop().create_future()
        self._pending[request_id] = future
        payload: JSONDict = {"id": request_id, "type": command}
        payload.update(fields)
        try:
            await self._send(payload)
            return await asyncio.wait_for(future, timeout or self.options.request_timeout)
        except asyncio.TimeoutError as exc:
            self._pending.pop(request_id, None)
            future.cancel()
            raise SnowTimeoutError(f"timed out waiting for {command} ({request_id})") from exc
        except BaseException:
            self._pending.pop(request_id, None)
            if future.done() and not future.cancelled():
                future.exception()
            raise

    async def prompt(
        self,
        message: str = "",
        *,
        content: Optional[Iterable[Mapping[str, Any]]] = None,
        mode: str = "",
        timeout: Optional[float] = None,
    ) -> PromptResult:
        self._ensure_open()
        request_id = self._new_id("prompt")
        terminal: asyncio.Future[PromptResult] = asyncio.get_running_loop().create_future()
        self._prompts[request_id] = terminal
        fields: JSONDict = {"message": message}
        if content is not None:
            fields["content"] = [dict(block) for block in content]
        if mode:
            fields["mode"] = mode
        try:
            await self.request("prompt", request_id=request_id, timeout=timeout, **fields)
            return await asyncio.wait_for(asyncio.shield(terminal), timeout or self.options.request_timeout)
        except asyncio.TimeoutError as exc:
            await self._abort_and_consume_prompt(request_id, terminal)
            raise SnowTimeoutError(f"timed out waiting for prompt completion ({request_id})") from exc
        except SnowTimeoutError:
            await self._abort_and_consume_prompt(request_id, terminal)
            raise
        except asyncio.CancelledError:
            await self._abort_and_consume_prompt(request_id, terminal)
            raise
        except BaseException:
            self._prompts.pop(request_id, None)
            raise

    async def _abort_and_consume_prompt(self, request_id: str, terminal: asyncio.Future[PromptResult]) -> None:
        try:
            await asyncio.shield(self.abort())
        except BaseException:
            pass
        try:
            await asyncio.wait_for(asyncio.shield(terminal), self.options.close_timeout)
        except BaseException:
            pass
        if self._prompts.pop(request_id, None) is not None:
            terminal.cancel()
            self._abandoned_prompts.add(request_id)
            while len(self._abandoned_prompts) > 128:
                self._abandoned_prompts.pop()

    async def abort(self) -> JSONDict:
        return await self.request("abort")

    async def session_info(self) -> JSONDict:
        return await self.request("session_info")

    async def session_rename(self, name: str) -> JSONDict:
        return await self.request("session_rename", params={"name": name})

    async def branch_fork(self, **params: Any) -> JSONDict:
        """Create and activate a branch in the current session."""
        return await self.request("branch_fork", params=params)

    async def session_fork(self, **params: Any) -> JSONDict:
        """Create a detached independent session in the current workspace."""
        return await self.request("session_fork", params=params)

    async def session_worktree_fork(self, **params: Any) -> JSONDict:
        """Create a detached Git worktree and independent session."""
        return await self.request("session_worktree_fork", params=params)

    async def models(self) -> JSONDict:
        return await self.request("models_list")

    async def subagent_models(self) -> JSONDict:
        return await self.request("subagent_models")

    async def set_model(self, model: str) -> JSONDict:
        return await self.request("set_model", model=model)

    async def set_thinking(self, thinking: str) -> JSONDict:
        return await self.request("set_thinking", thinking=thinking)

    async def set_mode(self, mode: str) -> JSONDict:
        return await self.request("set_mode", mode=mode)

    async def steer(self, message: str) -> JSONDict:
        return await self.request("steer", message=message)

    async def follow_up(self, message: str) -> JSONDict:
        return await self.request("follow_up", message=message)

    async def compact(self) -> JSONDict:
        """Manually compact the active branch context."""
        return await self.request("compact")

    async def branches_list(self) -> JSONDict:
        """Return durable branches in the active session."""
        return await self.request("branches_list")

    async def branch_select(self, branch_id: str) -> JSONDict:
        """Switch the active branch."""
        return await self.request("branch_select", params={"branch_id": branch_id})

    async def branch_rename(self, branch_id: str, name: str) -> JSONDict:
        """Rename a branch without changing its stable ID."""
        return await self.request("branch_rename", params={"branch_id": branch_id, "name": name})

    async def branch_delete(self, branch_id: str) -> JSONDict:
        """Delete a non-active branch."""
        return await self.request("branch_delete", params={"branch_id": branch_id})

    async def messages_list(self) -> JSONDict:
        """Return the linearized message list for the active branch."""
        return await self.request("messages_list")

    async def usage(self) -> JSONDict:
        """Return aggregate token/cache usage for the active branch."""
        return await self.request("usage")

    async def pending_inputs(self) -> JSONDict:
        """Return the submission-ordered queue of eligible steer/follow-up input."""
        return await self.request("pending_inputs")

    async def pending_inputs_clear(self) -> JSONDict:
        """Return and remove undelivered queued root input."""
        return await self.request("pending_inputs_clear")

    async def configuration_diagnostics(self) -> JSONDict:
        """Return non-fatal configuration warnings from the Snow process.

        Named configuration_diagnostics to avoid colliding with the existing
        client-side ``diagnostics`` transport-log attribute.
        """
        return await self.request("diagnostics")

    async def debug_status(self) -> JSONDict:
        """Return process-local diagnostic capture status and limits."""
        return await self.request("debug_status")

    async def debug_enable(self) -> JSONDict:
        """Enable process-local diagnostic capture."""
        return await self.request("debug_enable")

    async def debug_disable(self) -> JSONDict:
        """Disable process-local diagnostic capture without clearing it."""
        return await self.request("debug_disable")

    async def debug_clear(self) -> JSONDict:
        """Clear retained diagnostic events."""
        return await self.request("debug_clear")

    async def debug_dump(self, path: str = "") -> JSONDict:
        """Write a redacted diagnostic dump, optionally to ``path``."""
        fields: JSONDict = {}
        if path:
            fields["params"] = {"path": path}
        return await self.request("debug_dump", **fields)

    async def mcp_servers(self) -> JSONDict:
        """Return secret-free status for negotiated MCP servers."""
        return await self.request("mcp_servers")

    async def skills(self) -> JSONDict:
        """Return the full skill catalog plus discovery diagnostics."""
        return await self.request("skills")

    async def set_reasoning_summary(self, reasoning_summary: str) -> JSONDict:
        """Set the provider reasoning-summary preference (off|auto|concise|detailed)."""
        return await self.request("set_reasoning_summary", reasoning_summary=reasoning_summary)

    async def set_text_verbosity(self, text_verbosity: str) -> JSONDict:
        """Set the provider text-verbosity preference (low|medium|high)."""
        return await self.request("set_text_verbosity", text_verbosity=text_verbosity)

    async def goal_get(self) -> JSONDict:
        return await self.request("goal_get")

    async def goal_create(self, objective: str, *, token_budget: Optional[int] = None, replace: bool = False) -> JSONDict:
        params: JSONDict = {"objective": objective}
        if token_budget is not None:
            params["token_budget"] = token_budget
        if replace:
            params["replace"] = True
        return await self.request("goal_create", params=params)

    async def goal_set(self, objective: str, *, token_budget: Optional[int] = None, replace: bool = False) -> JSONDict:
        """Create a goal through the protocol's ``goal_set`` compatibility alias."""
        params: JSONDict = {"objective": objective}
        if token_budget is not None:
            params["token_budget"] = token_budget
        if replace:
            params["replace"] = True
        return await self.request("goal_set", params=params)

    async def goal_edit(self, objective: str) -> JSONDict:
        return await self.request("goal_edit", params={"objective": objective})

    async def goal_pause(self) -> JSONDict:
        return await self.request("goal_pause")

    async def goal_resume(self) -> JSONDict:
        return await self.request("goal_resume")

    async def goal_clear(self) -> JSONDict:
        return await self.request("goal_clear")

    async def goal_continue(self) -> JSONDict:
        return await self.request("goal_continue")

    async def subagent_spawn(self, **params: Any) -> JSONDict:
        return await self.request("subagent_spawn", params=params)

    async def subagent_send_message(self, target: str, message: str) -> JSONDict:
        return await self.request("subagent_send_message", params={"target": target, "message": message})

    async def subagent_followup(self, target: str, message: str) -> JSONDict:
        return await self.request("subagent_followup", params={"target": target, "message": message})

    async def subagent_wait(self, *, timeout_ms: int = 0, until: str = "activity") -> JSONDict:
        return await self.request("subagent_wait", params={"timeout_ms": timeout_ms, "until": until})

    async def subagent_interrupt(self, target: str) -> JSONDict:
        return await self.request("subagent_interrupt", params={"target": target})

    async def subagent_close(self, target: str) -> JSONDict:
        return await self.request("subagent_close", params={"target": target})

    async def subagent_resume(self, target: str) -> JSONDict:
        return await self.request("subagent_resume", params={"target": target})

    async def subagent_list(self, path_prefix: str = "") -> JSONDict:
        fields: JSONDict = {}
        if path_prefix:
            fields["params"] = {"path_prefix": path_prefix}
        return await self.request("subagent_list", **fields)

    async def subagent_get(self, target: str) -> JSONDict:
        return await self.request("subagent_get", params={"target": target})

    async def subagent_ready(self) -> JSONDict:
        return await self.request("subagent_ready")

    async def reply_user_input(self, request_id: str, answers: Any) -> JSONDict:
        return await self.request(
            "user_input_reply",
            params={"request_id": request_id, "answers": answers},
        )

    async def reject_user_input(self, request_id: str) -> JSONDict:
        return await self.request("user_input_reject", params={"request_id": request_id})

    async def reply_permission(self, request_id: str, decision: str) -> JSONDict:
        """Resolve a pending ask-mode permission request with a decision:
        allow|allow_session|allow_always|deny."""
        return await self.request(
            "permission_reply",
            params={"request_id": request_id, "decision": decision},
        )

    async def reject_permission(self, request_id: str) -> JSONDict:
        """Decline a pending ask-mode permission request."""
        return await self.request(
            "permission_reject",
            params={"request_id": request_id},
        )

    async def close(self) -> None:
        if self._closed:
            return
        self._closing = True
        if self._fatal_kill_handle is not None:
            self._fatal_kill_handle.cancel()
            self._fatal_kill_handle = None
        process = self._process
        if process is not None and process.stdin is not None and not process.stdin.is_closing():
            process.stdin.close()
            try:
                await process.stdin.wait_closed()
            except (BrokenPipeError, ConnectionResetError):
                pass
        if process is not None and process.returncode is None:
            try:
                await asyncio.wait_for(process.wait(), self.options.close_timeout)
            except asyncio.TimeoutError:
                process.terminate()
                try:
                    await asyncio.wait_for(process.wait(), self.options.close_timeout)
                except asyncio.TimeoutError:
                    process.kill()
                    await process.wait()
        tasks = [task for task in (self._reader_task, self._stderr_task, self._wait_task) if task is not None]
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
        for task in list(self._handler_tasks):
            task.cancel()
        if self._handler_tasks:
            await asyncio.gather(*self._handler_tasks, return_exceptions=True)
        self._finish(SnowClosedError("Snow client closed"), subscriptions_error=None)
        self._closed = True

    async def _send(self, payload: JSONDict) -> None:
        self._ensure_open()
        process = self._process
        if process is None or process.stdin is None:
            raise SnowClosedError("Snow RPC stdin is unavailable")
        encoded = (json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n").encode("utf-8")
        if len(encoded) > self._write_limit:
            raise SnowProtocolError("request frame exceeds configured maximum")
        async with self._write_lock:
            try:
                process.stdin.write(encoded)
                await process.stdin.drain()
            except (BrokenPipeError, ConnectionResetError) as exc:
                error = SnowProcessError("Snow RPC stdin closed")
                self._fatal(error)
                raise error from exc

    async def _read_stdout(self) -> None:
        assert self._process is not None and self._process.stdout is not None
        try:
            while True:
                try:
                    line = await self._process.stdout.readline()
                except (ValueError, asyncio.LimitOverrunError) as exc:
                    raise SnowProtocolError("RPC output frame exceeds configured maximum") from exc
                if not line:
                    break
                if len(line) > self.options.max_frame_bytes:
                    raise SnowProtocolError("RPC output frame exceeds configured maximum")
                if not line.endswith(b"\n"):
                    raise SnowProtocolError("RPC output ended with an unterminated frame")
                frame = line[:-1]
                if not frame:
                    continue
                try:
                    decoded = frame.decode("utf-8")
                    message = json.loads(decoded)
                except (UnicodeDecodeError, json.JSONDecodeError) as exc:
                    raise SnowProtocolError(f"invalid RPC output frame: {exc}") from exc
                if not isinstance(message, dict) or not isinstance(message.get("type"), str):
                    raise SnowProtocolError("RPC output frame is not an object with a type")
                self._route(message)
            if not self._closing:
                assert self._process is not None
                try:
                    await asyncio.wait_for(asyncio.shield(self._process.wait()), 0.05)
                except asyncio.TimeoutError:
                    raise SnowProcessError("Snow RPC stdout closed unexpectedly")
        except BaseException as exc:
            self._fatal(exc)

    async def _read_stderr(self) -> None:
        assert self._process is not None and self._process.stderr is not None
        while True:
            chunk = await self._process.stderr.read(4096)
            if not chunk:
                return
            self._stderr_tail.extend(chunk)
            if len(self._stderr_tail) > _MAX_STDERR_BYTES:
                del self._stderr_tail[: len(self._stderr_tail) - _MAX_STDERR_BYTES]

    async def _wait_process(self) -> None:
        assert self._process is not None
        code = await self._process.wait()
        if self._fatal_kill_handle is not None:
            self._fatal_kill_handle.cancel()
            self._fatal_kill_handle = None
        if self._stderr_task is not None:
            await self._stderr_task
        if not self._closing:
            detail = self._stderr_tail.decode("utf-8", "replace").strip()
            suffix = f": {detail}" if detail else ""
            self._fatal(SnowProcessError(f"Snow process exited with status {code}{suffix}"))

    def _route(self, message: JSONDict) -> None:
        kind = message["type"]
        if kind == "rpc_ready":
            if self._ready_future.done():
                self._fatal(SnowProtocolError("duplicate rpc_ready frame"))
                return
            raw_version = message.get("protocol_version")
            if not isinstance(raw_version, str):
                self._ready_future.set_exception(SnowProtocolError("rpc_ready protocol_version must be a string"))
                return
            version = raw_version
            snow_version = message.get("snow_version")
            if not isinstance(snow_version, str):
                self._ready_future.set_exception(SnowProtocolError("rpc_ready snow_version must be a string"))
                return
            raw_capabilities = message.get("capabilities")
            if not isinstance(raw_capabilities, list) or any(not isinstance(value, str) for value in raw_capabilities):
                self._ready_future.set_exception(SnowProtocolError("rpc_ready capabilities must be an array of strings"))
                return
            capabilities = tuple(raw_capabilities)
            if version != PROTOCOL_VERSION:
                self._ready_future.set_exception(SnowVersionError(f"unsupported RPC protocol version {version!r}"))
                return
            missing = sorted(_REQUIRED_CAPABILITIES.difference(capabilities))
            if missing:
                self._ready_future.set_exception(SnowVersionError(f"Snow RPC is missing capabilities: {', '.join(missing)}"))
                return
            max_input_bytes = message.get("max_input_bytes")
            if type(max_input_bytes) is not int or max_input_bytes <= 0:
                self._ready_future.set_exception(SnowProtocolError("rpc_ready has an invalid max_input_bytes"))
                return
            self._write_limit = min(self.options.max_frame_bytes, max_input_bytes)
            self._ready_future.set_result(
                RPCReady(
                    protocol_version=version,
                    snow_version=snow_version,
                    capabilities=capabilities,
                    max_input_bytes=max_input_bytes,
                    raw=message,
                )
            )
            return
        if not self._ready_future.done() or self._ready_future.cancelled():
            self._fatal(SnowProtocolError("received RPC output before rpc_ready"))
            return
        if kind == "response":
            request_id = str(message.get("id", ""))
            future = self._pending.pop(request_id, None)
            if future is None:
                self._diagnose("unknown_response", message)
                return
            if message.get("success") is True:
                future.set_result(message)
            else:
                future.set_exception(
                    SnowCommandError(
                        str(message.get("command", "unknown")),
                        request_id,
                        str(message.get("error", "command failed")),
                        message,
                    )
                )
            return
        if kind == "prompt_completed":
            request_id = str(message.get("request_id", ""))
            future = self._prompts.pop(request_id, None)
            if future is None:
                if request_id in self._abandoned_prompts:
                    self._abandoned_prompts.discard(request_id)
                    self._diagnose("late_prompt_completion", message)
                else:
                    # A bounded abandoned-prompt set may have evicted this ID.
                    # Unknown responses are already nonfatal; treat terminal
                    # prompt frames the same way so a delayed completion cannot
                    # tear down an otherwise healthy transport.
                    self._diagnose("unknown_prompt_completion", message)
                return
            status = str(message.get("status", ""))
            if status == "completed":
                future.set_result(PromptResult(request_id=request_id, status=status, raw=message))
            elif status == "canceled":
                future.set_exception(SnowCancelledError(f"prompt ({request_id}) was canceled"))
            elif status == "failed":
                future.set_exception(SnowPromptError(request_id, str(message.get("error", "prompt failed"))))
            else:
                future.set_exception(SnowProtocolError(f"unknown prompt status {status!r}"))
            return
        event = AgentEvent(type=kind, raw=message)
        for subscription in list(self._subscriptions):
            subscription._push(event)
        if kind == "user_input_request" and self.user_input_handler is not None:
            task = asyncio.create_task(self._handle_user_input(message), name="snow-user-input")
            self._handler_tasks.add(task)
            task.add_done_callback(self._handler_tasks.discard)
        if kind == "permission_request" and self.permission_handler is not None:
            task = asyncio.create_task(self._handle_permission(message), name="snow-permission")
            self._handler_tasks.add(task)
            task.add_done_callback(self._handler_tasks.discard)

    async def _handle_permission(self, event: JSONDict) -> None:
        permission = event.get("permission")
        request = permission.get("request") if isinstance(permission, dict) else None
        if not isinstance(request, dict) or not isinstance(request.get("id"), str):
            self._fatal(SnowProtocolError("permission_request omitted request data"))
            return
        request_id = request["id"]
        try:
            decision = await self.permission_handler(request)  # type: ignore[misc]
            if decision not in {"allow", "allow_session", "allow_always", "deny"}:
                raise SnowProtocolError("permission handler returned an invalid decision")
            await self.reply_permission(request_id, decision)
        except asyncio.CancelledError:
            raise
        except BaseException:
            try:
                await self.reject_permission(request_id)
            except BaseException:
                pass

    async def _handle_user_input(self, event: JSONDict) -> None:
        request = event.get("user_input")
        if not isinstance(request, dict) or not isinstance(request.get("id"), str):
            self._fatal(SnowProtocolError("user_input_request omitted request data"))
            return
        request_id = request["id"]
        try:
            response = await self.user_input_handler(request)  # type: ignore[misc]
            answers = response.get("answers")
            if not isinstance(answers, list):
                raise SnowProtocolError("user input handler must return an answers list")
            expected = [question.get("id") for question in request.get("questions", []) if isinstance(question, dict)]
            actual = [answer.get("id") for answer in answers if isinstance(answer, dict)]
            if len(actual) != len(answers) or len(set(actual)) != len(actual) or set(actual) != set(expected):
                raise SnowProtocolError("user input handler answers do not match question ids")
            if any(not isinstance(answer.get("answer"), str) for answer in answers):
                raise SnowProtocolError("user input handler answers must be strings")
            await self.reply_user_input(request_id, answers)
        except asyncio.CancelledError:
            raise
        except BaseException:
            try:
                await self.reject_user_input(request_id)
            except BaseException:
                pass

    def _diagnose(self, kind: str, frame: JSONDict) -> None:
        self.diagnostics.append({"kind": kind, "frame": frame})
        if len(self.diagnostics) > 128:
            del self.diagnostics[: len(self.diagnostics) - 128]

    def _fatal(self, error: BaseException) -> None:
        if self._closed or self._failure is not None:
            return
        self._failure = error
        if not self._ready_future.done():
            self._ready_future.set_exception(error)
        self._finish(error, subscriptions_error=error)
        process = self._process
        if not self._closing and process is not None and process.returncode is None:
            try:
                process.terminate()
            except ProcessLookupError:
                return
            self._fatal_kill_handle = asyncio.get_running_loop().call_later(
                self.options.close_timeout,
                self._kill_after_fatal,
                process,
            )

    def _kill_after_fatal(self, process: asyncio.subprocess.Process) -> None:
        self._fatal_kill_handle = None
        if process.returncode is not None:
            return
        try:
            process.kill()
        except ProcessLookupError:
            pass

    def _finish(self, error: BaseException, subscriptions_error: Optional[BaseException]) -> None:
        for future in list(self._pending.values()):
            if not future.done():
                future.set_exception(error)
        self._pending.clear()
        for future in list(self._prompts.values()):
            if not future.done():
                future.set_exception(error)
        self._prompts.clear()
        for subscription in list(self._subscriptions):
            subscription._finish(subscriptions_error)
        self._subscriptions.clear()

    def _remove_subscription(self, subscription: EventSubscription) -> None:
        self._subscriptions.discard(subscription)

    def _ensure_open(self) -> None:
        if self._failure is not None:
            raise self._failure
        if self._closed or self._closing:
            raise SnowClosedError("Snow client is closed")
        if self._process is None or self._process.returncode is not None:
            raise SnowClosedError("Snow process is not running")

    @staticmethod
    def _new_id(prefix: str) -> str:
        return f"{prefix}-{uuid.uuid4().hex}"
