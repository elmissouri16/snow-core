"""Wire-level JSON-RPC 2.0 constants and validation helpers for Snow plugins.

This module is intentionally small and dependency-free. It implements the exact
message shapes Snow's protocol-v2 accepts; it is not a general JSON-RPC
framework.
"""

from __future__ import annotations

import json
from typing import Dict, Any

JSON = Any

# Protocol version inserted automatically; authors should never set it.
PROTOCOL_VERSION = 2

# Bounded UTF-8 byte limits used before writing frames.
MAX_FRAME_BYTES = 4 * 1024 * 1024  # 4 MiB upper bound (host default)
MAX_OUTPUT_BYTES = 256 * 1024  # 256 KiB result/output bound


def validate_request(message: Dict[str, Any]) -> str:
    """Return an error string for an invalid JSON-RPC request, else empty.

    Notifications (no ``id``) are accepted as long as they are JSON-RPC 2.0 and
    carry a string method.
    """
    if not isinstance(message, dict):
        return "invalid request: not an object"
    version = message.get("jsonrpc")
    if version != "2.0":
        return 'invalid request: jsonrpc must be "2.0"'
    method = message.get("method")
    if not isinstance(method, str) or not method:
        return "invalid request: missing string method"
    if "id" in message:
        request_id = message["id"]
        if not isinstance(request_id, str) or not request_id:
            return "invalid request: id must be a non-empty string"
    return ""


def encode_frame(payload: Dict[str, Any]) -> bytes:
    """Encode one JSON object as UTF-8 plus a trailing LF."""
    encoded = json.dumps(
        payload, separators=(",", ":"), ensure_ascii=False
    ).encode("utf-8")
    if len(encoded) > MAX_FRAME_BYTES:
        raise ValueError(f"frame exceeds the {MAX_FRAME_BYTES}-byte bound")
    return encoded + b"\n"


def parse_frame(line: bytes) -> Dict[str, Any]:
    """Parse one LF-terminated UTF-8 JSON frame."""
    try:
        message = json.loads(line.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        raise ValueError("parse error") from None
    if not isinstance(message, dict):
        raise ValueError("invalid request: not an object")
    return message
