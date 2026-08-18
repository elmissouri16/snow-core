"""Expected, bounded tool failures for Snow plugins.

``ToolError`` is the single public expected-error type. Raising it from a
handler produces a structured ``is_error`` tool result with the bounded message
instead of failing the JSON-RPC request. Unexpected exceptions are reported as
bounded JSON-RPC errors on the wire.
"""


class ToolError(Exception):
    """A bounded, user-facing tool failure.

    The message is sent to Snow as an ``is_error`` result (or a bounded JSON-RPC
    error for unexpected handler failures). Keep it short and never include
    secrets, credentials, environment variables, or private configuration.
    """

    def __init__(self, message: str) -> None:
        if not isinstance(message, str) or not message.strip():
            raise TypeError("ToolError requires a non-empty string message")
        if len(message.encode("utf-8")) > 4096:
            raise ValueError("ToolError message exceeds the 4096-byte bound")
        super().__init__(message)
        self.message = message

    def __str__(self) -> str:  # pragma: no cover - trivial
        return self.message
