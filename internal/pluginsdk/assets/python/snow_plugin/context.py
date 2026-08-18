"""Typed per-call context handed to Snow tool handlers."""

from __future__ import annotations

import asyncio
from dataclasses import dataclass, field
from typing import Any, Callable, Dict, Optional

MAX_PROGRESS_BYTES = 16 * 1024
_LOG_SEVERITIES = ("debug", "info", "warning", "error")


@dataclass(frozen=True)
class ToolContext:
    """Per-call context for a Snow tool handler.

    ``deadline`` is the host deadline as an upstream unix timestamp in seconds,
    or ``None`` when no deadline exists. ``config`` is the private plugin
    configuration supplied by Snow; ``state`` is the value returned by setup.
    Neither must be copied into automatic diagnostics or result content.
    """

    call_id: str
    request_id: str
    cwd: str
    session_id: str
    deadline: Optional[float]
    config: Any
    state: Any = None
    _write: Callable[[Dict[str, Any]], Any] = field(default=lambda payload: None)

    async def progress(
        self,
        message: str,
        *,
        done: bool = False,
        is_error: bool = False,
    ) -> None:
        """Report bounded progress for this call.

        Snow ignores progress with an empty call ID; the SDK rejects it before
        writing a frame so a buggy handler cannot silently corrupt progress
        accounting.
        """
        if not isinstance(message, str) or not message.strip():
            raise ValueError("progress requires a non-empty string message")
        if len(message.encode("utf-8")) > MAX_PROGRESS_BYTES:
            raise ValueError(
                f"progress message exceeds {MAX_PROGRESS_BYTES} bytes"
            )
        if not self.call_id:
            raise ValueError("progress requires a non-empty call_id")
        await self._write(
            {
                "jsonrpc": "2.0",
                "method": "notifications/progress",
                "params": {
                    "call_id": self.call_id,
                    "message": message,
                    "done": bool(done),
                    "is_error": bool(is_error),
                },
            }
        )

    async def log(self, severity: str, message: str) -> None:
        """Report a bounded protocol diagnostic to Snow's log.

        ``severity`` is one of ``debug``, ``info``, ``warning``, or ``error``.
        """
        if severity not in _LOG_SEVERITIES:
            raise ValueError(
                "log severity must be debug, info, warning, or error"
            )
        if not isinstance(message, str) or not message.strip():
            raise ValueError("log requires a non-empty string message")
        if len(message.encode("utf-8")) > MAX_PROGRESS_BYTES:
            raise ValueError(
                f"log message exceeds {MAX_PROGRESS_BYTES} bytes"
            )
        await self._write(
            {
                "jsonrpc": "2.0",
                "method": "notifications/log",
                "params": {"severity": severity, "message": message},
            }
        )

    def raise_if_cancelled(self) -> None:
        """Cooperatively raise ``asyncio.CancelledError`` when this tool call
        was cancelled.

        Python cancellation is cooperative: blocking synchronous work cannot be
        forcibly cancelled. Call this between cooperative awaits to abort
        promptly instead of continuing to write results Snow will ignore.
        """
        current = asyncio.current_task()
        if current is None:
            return
        cancelling = getattr(current, "cancelling", None)
        if current.cancelled() or (cancelling is not None and cancelling() > 0):
            raise asyncio.CancelledError
