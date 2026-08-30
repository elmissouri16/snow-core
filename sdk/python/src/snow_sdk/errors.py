"""Snow SDK exception hierarchy."""

from typing import Any, Dict, Optional


class SnowError(Exception):
    """Base error for the Python SDK."""


class SnowClosedError(SnowError):
    """The client is closed or the Snow process is unavailable."""


class SnowProcessError(SnowError):
    """The Snow subprocess failed or exited unexpectedly."""


class SnowProtocolError(SnowError):
    """The subprocess emitted an invalid RPC frame."""


class SnowVersionError(SnowProtocolError):
    """The subprocess uses an unsupported RPC protocol version."""


class SnowCommandError(SnowError):
    """An RPC command returned a failure response."""

    def __init__(
        self,
        command: str,
        request_id: str,
        message: str,
        response: Optional[Dict[str, Any]] = None,
    ):
        self.command = command
        self.request_id = request_id
        self.response = response if response is not None else {}
        self.raw = self.response
        self.error_code = str(self.response.get("error_code", ""))
        super().__init__(f"{command} ({request_id}): {message}")


class SnowPromptError(SnowError):
    """An admitted prompt failed before terminal completion."""

    def __init__(self, request_id: str, message: str):
        self.request_id = request_id
        super().__init__(f"prompt ({request_id}): {message}")


class SnowTimeoutError(SnowError):
    """An SDK lifecycle or command deadline expired."""


class SnowCancelledError(SnowError):
    """An admitted prompt was canceled."""


class SnowSubscriptionOverflowError(SnowError):
    """A subscriber could not keep up with the event stream."""
