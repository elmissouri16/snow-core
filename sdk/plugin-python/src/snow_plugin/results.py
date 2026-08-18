"""Public result builders for Snow tool calls.

Snow's tool result contract is::

    {"content": [...], "details": {...}, "is_error": false}

``content`` is the provider-facing content block list. ``details`` is private
host metadata that Snow preserves but never surfaces to the model, logs, or
rendering. The builders below are the portable way to produce results.
"""

from typing import Any, Dict, List, Optional

from .errors import ToolError

__all__ = ["result", "text_result", "error_result"]


def _block_text(text: str) -> Dict[str, Any]:
    if not isinstance(text, str) or not text.strip():
        raise TypeError("text_result requires a non-empty string")
    return {"type": "text", "text": text}


def result(
    content: List[Dict[str, Any]],
    *,
    details: Optional[Dict[str, Any]] = None,
    is_error: bool = False,
) -> Dict[str, Any]:
    """Build a validated tool result.

    ``content`` must be a non-empty list of protocol content blocks. At minimum
    each block needs ``{"type": "text", "text": ...}``; arbitrary future block
    types pass through as long as they are JSON objects with a string ``type``.
    ``details`` is preserved as private host metadata and never provider-facing.
    """
    if not isinstance(content, list) or not content:
        raise TypeError("result requires a non-empty content block list")
    for block in content:
        if not isinstance(block, dict):
            raise TypeError("result content blocks must be JSON objects")
        block_type = block.get("type")
        if not isinstance(block_type, str) or not block_type:
            raise TypeError("result content blocks require a string type")
    if details is not None and not isinstance(details, dict):
        raise TypeError("result details must be a JSON object")
    return {
        "content": content,
        "details": details if details is not None else {},
        "is_error": bool(is_error),
    }


def text_result(
    text: str,
    *,
    details: Optional[Dict[str, Any]] = None,
    is_error: bool = False,
) -> Dict[str, Any]:
    """Return a simple text tool result with optional private details."""
    if not isinstance(text, str) or not text.strip():
        raise TypeError("text_result requires a non-empty string")
    return result([_block_text(text)], details=details, is_error=is_error)


def error_result(message: str, **details: Any) -> Dict[str, Any]:
    """Return a structured, bounded tool error.

    This completes the JSON-RPC request successfully with ``is_error: true`` so
    Snow records the call as a tool completion rather than a transport failure.
    """
    error_message = str(message)
    if not error_message.strip():
        raise TypeError("error_result requires a non-empty string message")
    if len(error_message.encode("utf-8")) > 4096:
        raise ValueError("error_result message exceeds the 4096-byte bound")
    return text_result(error_message, details=details, is_error=True)


__all__ = ["ToolError", "error_result", "result", "text_result"]
